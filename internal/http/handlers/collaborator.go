package handlers

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/canal/metricas-financeiro-app/backend/internal/http/middleware"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CollaboratorHandler struct {
	db *pgxpool.Pool
}

type collaboratorProfileResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"`
	Role string `json:"role"`
}

type collaboratorSummaryResponse struct {
	TotalCreatives  int      `json:"totalCreatives"`
	ActiveCreatives int      `json:"activeCreatives"`
	TotalRevenue    float64  `json:"totalRevenue"`
	TotalSpend      float64  `json:"totalSpend"`
	TotalProfit     float64  `json:"totalProfit"`
	TotalCommission float64  `json:"totalCommission"`
	AvgROAS         *float64 `json:"avgROAS"`
	AvgCPA          *float64 `json:"avgCPA"`
}

type collaboratorCreativeResponse struct {
	AdID        string   `json:"adId"`
	AdName      string   `json:"adName"`
	OfferName   string   `json:"offerName"`
	Status      string   `json:"status"`
	Spend       float64  `json:"spend"`
	Revenue     float64  `json:"revenue"`
	Profit      float64  `json:"profit"`
	ROAS        *float64 `json:"roas"`
	CPA         *float64 `json:"cpa"`
	Commission  float64  `json:"commission"`
	Impressions int64    `json:"impressions"`
	Clicks      int64    `json:"clicks"`
	HookRate    *float64 `json:"hookRate"`
	CTR         *float64 `json:"ctr"`
	Orders      int64    `json:"orders"`
}

type collaboratorCommissionHistoryPoint struct {
	Date           string  `json:"date"`
	Commission     float64 `json:"commission"`
	Revenue        float64 `json:"revenue"`
	Profit         float64 `json:"profit"`
	CreativesCount int64   `json:"creativesCount"`
}

type collaboratorMeResponse struct {
	Collaborator      collaboratorProfileResponse          `json:"collaborator"`
	Period            string                               `json:"period"`
	Summary           collaboratorSummaryResponse          `json:"summary"`
	Creatives         []collaboratorCreativeResponse       `json:"creatives"`
	CommissionHistory []collaboratorCommissionHistoryPoint `json:"commissionHistory"`
}

type collaboratorHistoryPoint struct {
	Date            string   `json:"date"`
	Revenue         float64  `json:"revenue"`
	Spend           float64  `json:"spend"`
	Profit          float64  `json:"profit"`
	Commission      float64  `json:"commission"`
	CreativesActive int64    `json:"creativesActive"`
	ROAS            *float64 `json:"roas"`
}

type collaboratorHistoryResponse struct {
	Period string                     `json:"period"`
	Points []collaboratorHistoryPoint `json:"points"`
}

type collaboratorProfile struct {
	ID   uuid.UUID
	Name string
	Code string
	Role string
}

func NewCollaboratorHandler(db *pgxpool.Pool) *CollaboratorHandler {
	return &CollaboratorHandler{db: db}
}

func (h *CollaboratorHandler) Me(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)
	if !scope.IsCollaborator || scope.UserID == nil {
		return fiber.NewError(fiber.StatusForbidden, "collaborator access required")
	}

	period, days, err := parsePeriod(c.Query("period"), "30d")
	if err != nil {
		return err
	}

	dashboardID := strings.TrimSpace(c.Query("dashboard"))
	if dashboardID != "" {
		dashboards, err := loadAccessibleDashboards(c.Context(), h.db, scope)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "failed to load collaborator dashboards")
		}
		if _, ok := filterDashboardsByID(dashboards, dashboardID); !ok {
			return fiber.NewError(fiber.StatusNotFound, "dashboard not found")
		}
	}

	profile, err := h.loadCollaboratorProfile(c.Context(), scope.CompanyID, *scope.UserID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "collaborator not found")
	}

	dateRange := buildHistoryRange(days, time.Now().UTC())
	creatives, err := h.loadCollaboratorCreatives(c.Context(), scope.CompanyID, profile.ID, dateRange, dashboardID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load collaborator creatives")
	}

	commissionHistory, err := h.loadCollaboratorCommissionHistory(c.Context(), scope.CompanyID, profile.ID, dateRange, dashboardID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load collaborator history")
	}

	var (
		totalRevenue    float64
		totalSpend      float64
		totalProfit     float64
		totalCommission float64
		totalOrders     int64
		activeCreatives int
	)
	for _, creative := range creatives {
		totalRevenue += creative.Revenue
		totalSpend += creative.Spend
		totalProfit += creative.Profit
		totalCommission += creative.Commission
		totalOrders += creative.Orders
		if creative.Status == "active" {
			activeCreatives++
		}
	}

	response := collaboratorMeResponse{
		Collaborator: collaboratorProfileResponse{
			ID:   profile.ID.String(),
			Name: profile.Name,
			Code: profile.Code,
			Role: profile.Role,
		},
		Period: period,
		Summary: collaboratorSummaryResponse{
			TotalCreatives:  len(creatives),
			ActiveCreatives: activeCreatives,
			TotalRevenue:    totalRevenue,
			TotalSpend:      totalSpend,
			TotalProfit:     totalProfit,
			TotalCommission: totalCommission,
			AvgROAS:         computeRatio(totalRevenue, totalSpend),
			AvgCPA:          computeRatio(totalSpend, float64(totalOrders)),
		},
		Creatives:         creatives,
		CommissionHistory: commissionHistory,
	}

	return c.JSON(response)
}

func (h *CollaboratorHandler) MeHistory(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)
	if !scope.IsCollaborator || scope.UserID == nil {
		return fiber.NewError(fiber.StatusForbidden, "collaborator access required")
	}

	period, days, err := parsePeriod(c.Query("period"), "30d")
	if err != nil {
		return err
	}

	dateRange := buildHistoryRange(days, time.Now().UTC())
	history, err := h.loadCollaboratorCommissionHistory(c.Context(), scope.CompanyID, *scope.UserID, dateRange, "")
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load collaborator history")
	}

	points := make([]collaboratorHistoryPoint, 0, len(history))
	for _, item := range history {
		points = append(points, collaboratorHistoryPoint{
			Date:            item.Date,
			Revenue:         item.Revenue,
			Spend:           0,
			Profit:          item.Profit,
			Commission:      item.Commission,
			CreativesActive: item.CreativesCount,
		})
	}

	// Reload spend values so ROAS reflects the same period aggregation.
	spendByDate, err := h.loadCollaboratorSpendByDate(c.Context(), scope.CompanyID, *scope.UserID, dateRange)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load collaborator spend history")
	}

	for index := range points {
		if spend, exists := spendByDate[points[index].Date]; exists {
			points[index].Spend = spend
			points[index].ROAS = computeRatio(points[index].Revenue, spend)
		}
	}

	return c.JSON(collaboratorHistoryResponse{
		Period: period,
		Points: points,
	})
}

func (h *CollaboratorHandler) loadCollaboratorProfile(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) (collaboratorProfile, error) {
	var profile collaboratorProfile
	err := h.db.QueryRow(ctx, `
		SELECT id, full_name, COALESCE(code, ''), role
		FROM users
		WHERE company_id = $1
		  AND id = $2
		LIMIT 1
	`, companyID, userID).Scan(&profile.ID, &profile.Name, &profile.Code, &profile.Role)
	return profile, err
}

func (h *CollaboratorHandler) loadCollaboratorCreatives(
	ctx context.Context,
	companyID uuid.UUID,
	collaboratorID uuid.UUID,
	dateRange historyDateRange,
	dashboardID string,
) ([]collaboratorCreativeResponse, error) {
	rows, err := h.db.Query(ctx, `
		WITH collaborator_ads AS (
			SELECT DISTINCT ac.ad_id
			FROM ad_collaborators ac
			INNER JOIN ads a
				ON a.id = ac.ad_id
			LEFT JOIN utmify_dashboards ud
				ON ud.company_id = a.company_id
			   AND ud.niche_id = a.niche_id
			WHERE ac.company_id = $1
			  AND ac.user_id = $2
			  AND ($5 = '' OR ud.external_id = $5)
		),
		commissions_by_ad AS (
			SELECT ce.ad_id, COALESCE(SUM(ce.commission_value), 0)::float8 AS commission
			FROM commission_entries ce
			INNER JOIN collaborator_ads ca
				ON ca.ad_id = ce.ad_id
			WHERE ce.company_id = $1
			  AND ce.user_id = $2
			  AND ce.source_type = 'ad_snapshot'
			  AND ce.snapshot_date >= $3
			  AND ce.snapshot_date <= $4
			GROUP BY ce.ad_id
		)
		SELECT
			a.id,
			a.name,
			COALESCE(o.name, NULLIF(a.name_parsed->>'offer_code', ''), 'Sem oferta') AS offer_name,
			COALESCE(SUM(s.spend), 0)::float8 AS spend,
			COALESCE(SUM(s.revenue), 0)::float8 AS revenue,
			COALESCE(SUM(s.profit), 0)::float8 AS profit,
			COALESCE(c.commission, 0)::float8 AS commission,
			COALESCE(SUM(s.impressions), 0)::bigint AS impressions,
			COALESCE(SUM(s.clicks), 0)::bigint AS clicks,
			AVG(NULLIF(s.hook_rate, 0))::float8 AS hook_rate,
			CASE
				WHEN COALESCE(SUM(s.impressions), 0) > 0
					THEN COALESCE(SUM(s.clicks), 0)::float8 / COALESCE(SUM(s.impressions), 0)::float8
				ELSE NULL
			END AS ctr,
			COALESCE(SUM(s.approved_orders_count), 0)::bigint AS orders,
			BOOL_OR(lower(COALESCE(NULLIF(s.effective_status, ''), NULLIF(s.object_status, ''), 'unknown')) = 'active') AS is_active,
			COALESCE(MAX(NULLIF(s.effective_status, '')), MAX(NULLIF(s.object_status, '')), 'unknown') AS fallback_status
		FROM collaborator_ads ca
		INNER JOIN ads a
			ON a.id = ca.ad_id
		INNER JOIN ad_metric_snapshots s
			ON s.ad_id = a.id
		   AND s.snapshot_date >= $3
		   AND s.snapshot_date <= $4
		LEFT JOIN offers o
			ON o.company_id = a.company_id
		   AND o.niche_id = a.niche_id
		   AND o.code = COALESCE(a.name_parsed->>'offer_code', '')
		LEFT JOIN commissions_by_ad c
			ON c.ad_id = a.id
		GROUP BY a.id, a.name, offer_name, c.commission
		ORDER BY COALESCE(SUM(s.profit), 0) DESC, a.name ASC
	`, companyID, collaboratorID, dateRange.StartDate, dateRange.EndDate, dashboardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]collaboratorCreativeResponse, 0)
	for rows.Next() {
		var (
			item           collaboratorCreativeResponse
			adID           uuid.UUID
			spend          float64
			revenue        float64
			profit         float64
			commission     float64
			hookRate       *float64
			ctr            *float64
			isActive       bool
			fallbackStatus string
		)
		if err := rows.Scan(
			&adID,
			&item.AdName,
			&item.OfferName,
			&spend,
			&revenue,
			&profit,
			&commission,
			&item.Impressions,
			&item.Clicks,
			&hookRate,
			&ctr,
			&item.Orders,
			&isActive,
			&fallbackStatus,
		); err != nil {
			return nil, err
		}

		item.AdID = adID.String()
		item.Status = normalizeCollaboratorCreativeStatus(isActive, fallbackStatus)
		item.Spend = moneyToCents(spend)
		item.Revenue = moneyToCents(revenue)
		item.Profit = moneyToCents(profit)
		item.Commission = moneyToCents(commission)
		item.ROAS = computeRatio(item.Revenue, item.Spend)
		item.CPA = computeRatio(item.Spend, float64(item.Orders))
		item.HookRate = nullableFloat(hookRate)
		item.CTR = nullableFloat(ctr)
		items = append(items, item)
	}

	return items, rows.Err()
}

func (h *CollaboratorHandler) loadCollaboratorCommissionHistory(
	ctx context.Context,
	companyID uuid.UUID,
	collaboratorID uuid.UUID,
	dateRange historyDateRange,
	dashboardID string,
) ([]collaboratorCommissionHistoryPoint, error) {
	rows, err := h.db.Query(ctx, `
		WITH collaborator_ads AS (
			SELECT DISTINCT ac.ad_id
			FROM ad_collaborators ac
			INNER JOIN ads a
				ON a.id = ac.ad_id
			LEFT JOIN utmify_dashboards ud
				ON ud.company_id = a.company_id
			   AND ud.niche_id = a.niche_id
			WHERE ac.company_id = $1
			  AND ac.user_id = $2
			  AND ($5 = '' OR ud.external_id = $5)
		)
		SELECT
			s.snapshot_date,
			COALESCE(SUM(s.revenue), 0)::float8 AS revenue,
			COALESCE(SUM(s.profit), 0)::float8 AS profit,
			COALESCE(SUM(ce.commission_value), 0)::float8 AS commission,
			COUNT(DISTINCT CASE
				WHEN lower(COALESCE(NULLIF(s.effective_status, ''), NULLIF(s.object_status, ''), 'unknown')) = 'active'
					THEN s.ad_id
				END
			)::bigint AS creatives_count
		FROM collaborator_ads ca
		INNER JOIN ad_metric_snapshots s
			ON s.ad_id = ca.ad_id
		   AND s.snapshot_date >= $3
		   AND s.snapshot_date <= $4
		LEFT JOIN commission_entries ce
			ON ce.company_id = $1
		   AND ce.user_id = $2
		   AND ce.ad_id = s.ad_id
		   AND ce.snapshot_date = s.snapshot_date
		   AND ce.source_type = 'ad_snapshot'
		GROUP BY s.snapshot_date
		ORDER BY s.snapshot_date ASC
	`, companyID, collaboratorID, dateRange.StartDate, dateRange.EndDate, dashboardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]collaboratorCommissionHistoryPoint, 0)
	for rows.Next() {
		var (
			snapshotDate time.Time
			revenue      float64
			profit       float64
			commission   float64
			point        collaboratorCommissionHistoryPoint
		)
		if err := rows.Scan(&snapshotDate, &revenue, &profit, &commission, &point.CreativesCount); err != nil {
			return nil, err
		}

		point.Date = snapshotDate.Format("2006-01-02")
		point.Revenue = moneyToCents(revenue)
		point.Profit = moneyToCents(profit)
		point.Commission = moneyToCents(commission)
		points = append(points, point)
	}

	return points, rows.Err()
}

func (h *CollaboratorHandler) loadCollaboratorSpendByDate(
	ctx context.Context,
	companyID uuid.UUID,
	collaboratorID uuid.UUID,
	dateRange historyDateRange,
) (map[string]float64, error) {
	rows, err := h.db.Query(ctx, `
		WITH collaborator_ads AS (
			SELECT DISTINCT ad_id
			FROM ad_collaborators
			WHERE company_id = $1
			  AND user_id = $2
		)
		SELECT
			s.snapshot_date,
			COALESCE(SUM(s.spend), 0)::float8 AS spend
		FROM collaborator_ads ca
		INNER JOIN ad_metric_snapshots s
			ON s.ad_id = ca.ad_id
		   AND s.snapshot_date >= $3
		   AND s.snapshot_date <= $4
		GROUP BY s.snapshot_date
	`, companyID, collaboratorID, dateRange.StartDate, dateRange.EndDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	response := make(map[string]float64)
	for rows.Next() {
		var (
			snapshotDate time.Time
			spend        float64
		)
		if err := rows.Scan(&snapshotDate, &spend); err != nil {
			return nil, err
		}
		response[snapshotDate.Format("2006-01-02")] = moneyToCents(spend)
	}

	return response, rows.Err()
}

func normalizeCollaboratorCreativeStatus(isActive bool, fallbackStatus string) string {
	if isActive {
		return "active"
	}

	fallbackStatus = strings.TrimSpace(fallbackStatus)
	if fallbackStatus == "" {
		return "unknown"
	}

	return fallbackStatus
}

func moneyToCents(value float64) float64 {
	return math.Round(value * 100)
}

func nullableFloat(value *float64) *float64 {
	if value == nil || math.IsNaN(*value) {
		return nil
	}
	return value
}
