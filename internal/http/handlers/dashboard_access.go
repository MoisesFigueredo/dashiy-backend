package handlers

import (
	"context"
	"strings"
	"time"

	"github.com/canal/metricas-financeiro-app/backend/internal/http/middleware"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type companyDashboard struct {
	ExternalID string
	Name       string
	Currency   string
	TimeZone   int
}

type historyDateRange struct {
	StartDate time.Time
	EndDate   time.Time
}

func loadAccessibleDashboards(ctx context.Context, db *pgxpool.Pool, scope middleware.CompanyContext) ([]companyDashboard, error) {
	if scope.IsCollaborator {
		if scope.UserID == nil {
			return []companyDashboard{}, nil
		}
		return loadCollaboratorDashboards(ctx, db, scope.CompanyID, *scope.UserID)
	}

	return loadCompanyDashboards(ctx, db, scope.CompanyID)
}

func loadCompanyDashboards(ctx context.Context, db *pgxpool.Pool, companyID uuid.UUID) ([]companyDashboard, error) {
	rows, err := db.Query(ctx, `
		SELECT external_id, name, currency, time_zone
		FROM utmify_dashboards
		WHERE company_id = $1
		  AND active = true
		ORDER BY name
	`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dashboards []companyDashboard
	for rows.Next() {
		var dashboard companyDashboard
		if err := rows.Scan(&dashboard.ExternalID, &dashboard.Name, &dashboard.Currency, &dashboard.TimeZone); err != nil {
			return nil, err
		}
		dashboards = append(dashboards, dashboard)
	}

	return dashboards, rows.Err()
}

func loadCollaboratorDashboards(ctx context.Context, db *pgxpool.Pool, companyID uuid.UUID, userID uuid.UUID) ([]companyDashboard, error) {
	rows, err := db.Query(ctx, `
		SELECT DISTINCT ud.external_id, ud.name, ud.currency, ud.time_zone
		FROM ad_collaborators ac
		INNER JOIN utmify_dashboards ud
			ON ud.company_id = ac.company_id
		   AND ud.niche_id = ac.niche_id
		WHERE ac.company_id = $1
		  AND ac.user_id = $2
		  AND ud.active = true
		  AND NOT EXISTS (
		      SELECT 1 FROM dashboard_access_overrides dao
		      WHERE dao.company_id = ac.company_id
		        AND dao.user_id = ac.user_id
		        AND dao.dashboard_id = ud.external_id
		        AND dao.allowed = false
		  )
		ORDER BY ud.name
	`, companyID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dashboards []companyDashboard
	for rows.Next() {
		var dashboard companyDashboard
		if err := rows.Scan(&dashboard.ExternalID, &dashboard.Name, &dashboard.Currency, &dashboard.TimeZone); err != nil {
			return nil, err
		}
		dashboards = append(dashboards, dashboard)
	}

	return dashboards, rows.Err()
}

func filterDashboardsByID(dashboards []companyDashboard, dashboardID string) ([]companyDashboard, bool) {
	dashboardID = strings.TrimSpace(dashboardID)
	if dashboardID == "" {
		return dashboards, true
	}

	for _, dashboard := range dashboards {
		if dashboard.ExternalID == dashboardID {
			return []companyDashboard{dashboard}, true
		}
	}

	return nil, false
}

func isRestrictedDashboardRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "copywriter", "editor", "closer":
		return true
	default:
		return false
	}
}

func parsePeriod(raw string, fallback string) (string, int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = fallback
	}

	switch value {
	case "7d":
		return value, 7, nil
	case "30d":
		return value, 30, nil
	default:
		return "", 0, fiber.NewError(fiber.StatusBadRequest, "period must be one of: 7d, 30d")
	}
}

func buildHistoryRange(days int, now time.Time) historyDateRange {
	endDate := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	startDate := endDate.AddDate(0, 0, -(days - 1))

	return historyDateRange{
		StartDate: startDate,
		EndDate:   endDate,
	}
}

func buildPreviousHistoryRange(current historyDateRange, days int) historyDateRange {
	endDate := current.StartDate.AddDate(0, 0, -1)
	startDate := endDate.AddDate(0, 0, -(days - 1))

	return historyDateRange{
		StartDate: startDate,
		EndDate:   endDate,
	}
}
