package handlers

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/canal/metricas-financeiro-app/backend/internal/cache"
	"github.com/canal/metricas-financeiro-app/backend/internal/http/middleware"
	"github.com/canal/metricas-financeiro-app/backend/internal/utmify"
	"github.com/canal/metricas-financeiro-app/backend/internal/workers"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// companyDashboard holds the minimal dashboard info needed to fetch summaries.
type companyDashboard struct {
	ExternalID string
	Name       string
	Currency   string
	TimeZone   int
}

type DashboardHandler struct {
	db     *pgxpool.Pool
	client *utmify.Client
	cache  *cache.Redis
}

func NewDashboardHandler(db *pgxpool.Pool, client *utmify.Client, cache *cache.Redis) *DashboardHandler {
	return &DashboardHandler{db: db, client: client, cache: cache}
}

func (h *DashboardHandler) Summary(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)
	date := strings.TrimSpace(c.Query("date"))
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}

	companyID := scope.CompanyID

	// 1) Try company-level Redis cache (optional, short TTL for performance).
	if h.cache != nil {
		var cached workers.CachedDashboardSummary
		key := cache.CompanySummaryKey(companyID.String(), date)
		found, err := h.cache.Get(c.Context(), key, &cached)
		if err == nil && found {
			return c.JSON(cached)
		}
	}

	// 2) Look up the company's dashboards from DB.
	dashboards, err := h.getCompanyDashboards(c.Context(), companyID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load company dashboards")
	}

	// If the company has no dashboards registered yet, fall back to
	// global DB data or live MCP call.
	if len(dashboards) == 0 {
		return h.fallbackGlobalSummary(c, date)
	}

	// 3) For each company dashboard, try DB first then MCP fallback.
	inputs := h.resolveInputsFromDB(c.Context(), dashboards, date)

	// 4) Aggregate only this company's dashboards.
	summary := workers.AggregateDashboardSummaries(inputs, date)
	summary.CachedAt = time.Now().UTC().Format(time.RFC3339)

	// 5) Cache the company-scoped aggregation in Redis (optional).
	if h.cache != nil {
		key := cache.CompanySummaryKey(companyID.String(), date)
		_ = h.cache.Set(c.Context(), key, summary, 1*time.Minute)
	}

	return c.JSON(summary)
}

// getCompanyDashboards queries the DB for dashboards linked to the company.
func (h *DashboardHandler) getCompanyDashboards(ctx context.Context, companyID uuid.UUID) ([]companyDashboard, error) {
	rows, err := h.db.Query(ctx, `
		SELECT external_id, name, currency, time_zone
		FROM utmify_dashboards
		WHERE company_id = $1 AND active = true
		ORDER BY name
	`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dashboards []companyDashboard
	for rows.Next() {
		var d companyDashboard
		if err := rows.Scan(&d.ExternalID, &d.Name, &d.Currency, &d.TimeZone); err != nil {
			return nil, err
		}
		dashboards = append(dashboards, d)
	}
	return dashboards, rows.Err()
}

// resolveInputsFromDB builds DashboardSummaryInputs by reading from the
// dashboard_summary_snapshots table first, falling back to live MCP on miss.
func (h *DashboardHandler) resolveInputsFromDB(ctx context.Context, dashboards []companyDashboard, date string) []workers.DashboardSummaryInput {
	inputs := make([]workers.DashboardSummaryInput, 0, len(dashboards))

	for _, d := range dashboards {
		input := workers.DashboardSummaryInput{
			ID:       d.ExternalID,
			Name:     d.Name,
			Currency: d.Currency,
			TimeZone: d.TimeZone,
		}

		// Try PostgreSQL first (primary source).
		var rawJSON []byte
		err := h.db.QueryRow(ctx, `
			SELECT raw_summary
			FROM dashboard_summary_snapshots
			WHERE dashboard_external_id = $1 AND snapshot_date = $2
		`, d.ExternalID, date).Scan(&rawJSON)

		if err == nil && len(rawJSON) > 0 {
			var summary utmify.DashboardSummary
			if jsonErr := json.Unmarshal(rawJSON, &summary); jsonErr == nil {
				input.Summary = &summary
				inputs = append(inputs, input)
				continue
			}
		}

		// DB miss — fall back to live MCP call.
		dashboard := utmify.Dashboard{
			ID:       d.ExternalID,
			Name:     d.Name,
			Currency: d.Currency,
			TimeZone: d.TimeZone,
		}
		summary, err := h.client.GetDashboardSummaryForDashboard(ctx, dashboard, date, date)
		if err != nil {
			input.Error = err.Error()
		} else {
			input.Summary = summary
		}

		inputs = append(inputs, input)
	}

	return inputs
}

// fallbackGlobalSummary is used when the company has no dashboards registered
// in the DB yet. It tries all dashboard summaries from DB, then falls back to MCP.
func (h *DashboardHandler) fallbackGlobalSummary(c *fiber.Ctx, date string) error {
	// Try building from all dashboard_summary_snapshots for this date.
	inputs, err := h.loadAllSummariesFromDB(c.Context(), date)
	if err == nil && len(inputs) > 0 {
		summary := workers.AggregateDashboardSummaries(inputs, date)
		summary.CachedAt = time.Now().UTC().Format(time.RFC3339)
		return c.JSON(summary)
	}

	// DB empty — try Redis cache.
	if h.cache != nil {
		var cached workers.CachedDashboardSummary
		found, err := h.cache.Get(c.Context(), cache.DashboardSummaryKey(date), &cached)
		if err == nil && found {
			return c.JSON(cached)
		}
	}

	// Last resort: live MCP call.
	dashboards, err := h.client.GetDashboards(c.Context())
	if err != nil {
		return err
	}

	summary := workers.BuildDashboardSummary(c.Context(), h.client, nil, dashboards, date)
	summary.CachedAt = time.Now().UTC().Format(time.RFC3339)

	return c.JSON(summary)
}

// loadAllSummariesFromDB reads all dashboard_summary_snapshots for a given date
// and builds DashboardSummaryInputs from the raw JSONB data.
func (h *DashboardHandler) loadAllSummariesFromDB(ctx context.Context, date string) ([]workers.DashboardSummaryInput, error) {
	rows, err := h.db.Query(ctx, `
		SELECT dashboard_external_id, dashboard_name, currency, time_zone, raw_summary
		FROM dashboard_summary_snapshots
		WHERE snapshot_date = $1
		ORDER BY dashboard_name
	`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var inputs []workers.DashboardSummaryInput
	for rows.Next() {
		var (
			externalID string
			name       string
			currency   string
			timeZone   int
			rawJSON    []byte
		)
		if err := rows.Scan(&externalID, &name, &currency, &timeZone, &rawJSON); err != nil {
			return nil, err
		}

		input := workers.DashboardSummaryInput{
			ID:       externalID,
			Name:     name,
			Currency: currency,
			TimeZone: timeZone,
		}

		var summary utmify.DashboardSummary
		if jsonErr := json.Unmarshal(rawJSON, &summary); jsonErr == nil {
			input.Summary = &summary
		} else {
			input.Error = jsonErr.Error()
		}

		inputs = append(inputs, input)
	}

	return inputs, rows.Err()
}
