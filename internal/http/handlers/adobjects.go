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

// AdObjectsHandler serves ad objects data from PostgreSQL (populated by background sync).
type AdObjectsHandler struct {
	db     *pgxpool.Pool
	cache  *cache.Redis
	client *utmify.Client
}

func NewAdObjectsHandler(db *pgxpool.Pool, cache *cache.Redis, client *utmify.Client) *AdObjectsHandler {
	return &AdObjectsHandler{db: db, cache: cache, client: client}
}

// List returns ad objects for a given level, optionally filtered by dashboard.
// GET /ads/objects?level=account&date=2026-03-23&dashboard=<id>
func (h *AdObjectsHandler) List(c *fiber.Ctx) error {
	level := strings.TrimSpace(c.Query("level", "account"))
	date := strings.TrimSpace(c.Query("date"))
	dashboardID := strings.TrimSpace(c.Query("dashboard"))

	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}

	if !isValidLevel(level) {
		return fiber.NewError(fiber.StatusBadRequest, "level must be one of: account, campaign, adset, ad")
	}

	// If a specific dashboard is requested, try DB for that dashboard.
	if dashboardID != "" {
		return h.listByDashboard(c, dashboardID, level, date)
	}

	// Otherwise return objects for all company dashboards.
	return h.listByCompany(c, level, date)
}

func (h *AdObjectsHandler) listByDashboard(c *fiber.Ctx, dashboardID, level, date string) error {
	// Try PostgreSQL first.
	cached, err := h.loadAdObjectsFromDB(c.Context(), dashboardID, level, date)
	if err == nil && cached != nil {
		return c.JSON(cached)
	}

	// DB miss — try Redis cache.
	if h.cache != nil {
		var redisCached workers.CachedAdObjects
		found, err := h.cache.Get(c.Context(), cache.AdObjectsKey(dashboardID, level, date), &redisCached)
		if err == nil && found {
			return c.JSON(redisCached)
		}
	}

	// Last resort: fetch live from MCP.
	objects, err := h.client.GetMetaAdObjects(c.Context(), dashboardID, level, date, date)
	if err != nil {
		return err
	}

	return c.JSON(workers.CachedAdObjects{
		DashboardID: dashboardID,
		Level:       level,
		Date:        date,
		Objects:     objects,
		CachedAt:    "",
	})
}

// listByCompany returns ad objects only for dashboards belonging to the
// requesting company. Falls back to global DB data when the company has
// no registered dashboards yet.
func (h *AdObjectsHandler) listByCompany(c *fiber.Ctx, level, date string) error {
	scope := middleware.GetCompanyContext(c)

	// Look up which dashboards belong to this company.
	externalIDs, err := h.getCompanyDashboardIDs(c.Context(), scope.CompanyID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load company dashboards")
	}

	// No registered dashboards — fall back to global DB data.
	if len(externalIDs) == 0 {
		return h.listAllFallback(c, level, date)
	}

	// Collect per-dashboard ad objects from DB (primary) or MCP (fallback).
	allDashboards := make([]workers.CachedAdObjects, 0, len(externalIDs))
	for _, extID := range externalIDs {
		// Try DB first.
		cached, err := h.loadAdObjectsFromDB(c.Context(), extID, level, date)
		if err == nil && cached != nil {
			allDashboards = append(allDashboards, *cached)
			continue
		}

		// Try Redis cache.
		if h.cache != nil {
			var redisCached workers.CachedAdObjects
			key := cache.AdObjectsKey(extID, level, date)
			found, err := h.cache.Get(c.Context(), key, &redisCached)
			if err == nil && found {
				allDashboards = append(allDashboards, redisCached)
				continue
			}
		}

		// DB + cache miss — fetch live from MCP.
		objects, err := h.client.GetMetaAdObjects(c.Context(), extID, level, date, date)
		if err != nil {
			continue // skip this dashboard
		}
		allDashboards = append(allDashboards, workers.CachedAdObjects{
			DashboardID: extID,
			Level:       level,
			Date:        date,
			Objects:     objects,
		})
	}

	return c.JSON(workers.CachedAllAdObjects{
		Level:      level,
		Date:       date,
		Dashboards: allDashboards,
		CachedAt:   time.Now().UTC().Format(time.RFC3339),
	})
}

// loadAdObjectsFromDB reads ad objects from the ad_object_snapshots table.
func (h *AdObjectsHandler) loadAdObjectsFromDB(ctx context.Context, dashboardID, level, date string) (*workers.CachedAdObjects, error) {
	var (
		dashboardName string
		currency      string
		rawJSON       []byte
		fetchedAt     time.Time
	)

	err := h.db.QueryRow(ctx, `
		SELECT dashboard_name, currency, raw_objects, fetched_at
		FROM ad_object_snapshots
		WHERE dashboard_external_id = $1 AND level = $2 AND snapshot_date = $3
	`, dashboardID, level, date).Scan(&dashboardName, &currency, &rawJSON, &fetchedAt)

	if err != nil {
		return nil, err
	}

	var objects []utmify.AdObject
	if err := json.Unmarshal(rawJSON, &objects); err != nil {
		return nil, err
	}

	return &workers.CachedAdObjects{
		DashboardID:   dashboardID,
		DashboardName: dashboardName,
		Currency:      currency,
		Level:         level,
		Date:          date,
		Objects:       objects,
		CachedAt:      fetchedAt.Format(time.RFC3339),
	}, nil
}

// getCompanyDashboardIDs returns the external MCP dashboard IDs for a company.
func (h *AdObjectsHandler) getCompanyDashboardIDs(ctx context.Context, companyID uuid.UUID) ([]string, error) {
	rows, err := h.db.Query(ctx, `
		SELECT external_id
		FROM utmify_dashboards
		WHERE company_id = $1 AND active = true
		ORDER BY name
	`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// listAllFallback uses DB data for all dashboards (when no utmify_dashboards
// records exist yet for the company).
func (h *AdObjectsHandler) listAllFallback(c *fiber.Ctx, level, date string) error {
	// Try loading all ad object snapshots from DB for this level+date.
	allDashboards, err := h.loadAllAdObjectsFromDB(c.Context(), level, date)
	if err == nil && len(allDashboards) > 0 {
		return c.JSON(workers.CachedAllAdObjects{
			Level:      level,
			Date:       date,
			Dashboards: allDashboards,
			CachedAt:   time.Now().UTC().Format(time.RFC3339),
		})
	}

	// DB empty — try Redis cache.
	if h.cache != nil {
		var cached workers.CachedAllAdObjects
		found, err := h.cache.Get(c.Context(), cache.AllAdObjectsKey(level, date), &cached)
		if err == nil && found {
			return c.JSON(cached)
		}
	}

	// Last resort: fetch live from MCP for all dashboards.
	dashboards, err := h.client.GetDashboards(c.Context())
	if err != nil {
		return err
	}

	result := make([]workers.CachedAdObjects, 0, len(dashboards))
	for _, dashboard := range dashboards {
		localDate := utmify.LocalDate(time.Now().UTC(), dashboard.TimeZone)
		objects, err := h.client.GetMetaAdObjectsForDashboard(c.Context(), dashboard, level, localDate, localDate)
		if err != nil {
			continue
		}
		result = append(result, workers.CachedAdObjects{
			DashboardID:   dashboard.ID,
			DashboardName: dashboard.Name,
			Currency:      dashboard.Currency,
			Level:         level,
			Date:          localDate,
			Objects:       objects,
		})
	}

	return c.JSON(workers.CachedAllAdObjects{
		Level:      level,
		Date:       date,
		Dashboards: result,
	})
}

// loadAllAdObjectsFromDB reads all ad_object_snapshots for a given level+date.
func (h *AdObjectsHandler) loadAllAdObjectsFromDB(ctx context.Context, level, date string) ([]workers.CachedAdObjects, error) {
	rows, err := h.db.Query(ctx, `
		SELECT dashboard_external_id, dashboard_name, currency, raw_objects, fetched_at
		FROM ad_object_snapshots
		WHERE level = $1 AND snapshot_date = $2
		ORDER BY dashboard_name
	`, level, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var allDashboards []workers.CachedAdObjects
	for rows.Next() {
		var (
			dashboardID   string
			dashboardName string
			currency      string
			rawJSON       []byte
			fetchedAt     time.Time
		)
		if err := rows.Scan(&dashboardID, &dashboardName, &currency, &rawJSON, &fetchedAt); err != nil {
			return nil, err
		}

		var objects []utmify.AdObject
		if err := json.Unmarshal(rawJSON, &objects); err != nil {
			continue
		}

		allDashboards = append(allDashboards, workers.CachedAdObjects{
			DashboardID:   dashboardID,
			DashboardName: dashboardName,
			Currency:      currency,
			Level:         level,
			Date:          date,
			Objects:       objects,
			CachedAt:      fetchedAt.Format(time.RFC3339),
		})
	}

	return allDashboards, rows.Err()
}

func isValidLevel(level string) bool {
	switch level {
	case "account", "campaign", "adset", "ad":
		return true
	}
	return false
}
