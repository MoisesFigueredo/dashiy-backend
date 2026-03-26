package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/canal/metricas-financeiro-app/backend/internal/auth"
	"github.com/canal/metricas-financeiro-app/backend/internal/http/middleware"
	syncservice "github.com/canal/metricas-financeiro-app/backend/internal/sync"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type CollaboratorsHandler struct {
	db          *pgxpool.Pool
	syncService *syncservice.Service
	logger      *slog.Logger
}

func NewCollaboratorsHandler(db *pgxpool.Pool, syncService *syncservice.Service, logger *slog.Logger) *CollaboratorsHandler {
	return &CollaboratorsHandler{db: db, syncService: syncService, logger: logger}
}

// collaborator roles used to filter users that are collaborators
var collaboratorRoles = []string{"copywriter", "editor", "gestor_trafego", "desenvolvedor", "closer"}

// ── List ────────────────────────────────────────────────────────────

func (h *CollaboratorsHandler) List(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)

	rows, err := h.db.Query(c.Context(), `
		SELECT
			u.id,
			u.full_name,
			COALESCE(u.code, ''),
			u.role,
			u.commission_rate_min,
			u.commission_rate_max,
			u.pix_key,
			u.salary_base,
			u.whatsapp,
			u.status,
			u.created_at,
			u.updated_at,
			u.email
		FROM users u
		WHERE u.company_id = $1
		  AND u.role = ANY($2::text[])
		ORDER BY u.full_name ASC
	`, scope.CompanyID, collaboratorRoles)
	if err != nil {
		return err
	}
	defer rows.Close()

	type response struct {
		ID                string     `json:"id"`
		Name              string     `json:"name"`
		Code              string     `json:"code"`
		Role              string     `json:"role"`
		CommissionRateMin float64    `json:"commission_rate_min"`
		CommissionRateMax float64    `json:"commission_rate_max"`
		PixKey            *string    `json:"pix_key,omitempty"`
		SalaryBase        float64    `json:"salary_base"`
		Whatsapp          *string    `json:"whatsapp,omitempty"`
		Status            string     `json:"status"`
		CreatedAt         *time.Time `json:"created_at,omitempty"`
		UpdatedAt         *time.Time `json:"updated_at,omitempty"`
		Email             *string    `json:"email,omitempty"`
	}

	items := make([]response, 0)
	for rows.Next() {
		var (
			item              response
			id                uuid.UUID
			commissionRateMin decimal.Decimal
			commissionRateMax decimal.Decimal
			salaryBase        decimal.Decimal
			createdAt         time.Time
			updatedAt         time.Time
			email             string
		)
		if err := rows.Scan(
			&id,
			&item.Name,
			&item.Code,
			&item.Role,
			&commissionRateMin,
			&commissionRateMax,
			&item.PixKey,
			&salaryBase,
			&item.Whatsapp,
			&item.Status,
			&createdAt,
			&updatedAt,
			&email,
		); err != nil {
			return err
		}

		item.ID = id.String()
		item.CommissionRateMin = decimalToFloat(commissionRateMin)
		item.CommissionRateMax = decimalToFloat(commissionRateMax)
		item.SalaryBase = decimalToFloat(salaryBase)
		item.CreatedAt = &createdAt
		item.UpdatedAt = &updatedAt
		item.Email = &email
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return err
	}

	return c.JSON(items)
}

// ── Create ──────────────────────────────────────────────────────────

func (h *CollaboratorsHandler) Create(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)

	var request struct {
		Name              string   `json:"name"`
		Code              string   `json:"code"`
		Role              string   `json:"role"`
		Email             string   `json:"email"`
		Password          string   `json:"password"`
		CommissionRateMin *float64 `json:"commission_rate_min"`
		CommissionRateMax *float64 `json:"commission_rate_max"`
		PixKey            string   `json:"pix_key"`
		SalaryBase        *float64 `json:"salary_base"`
		Whatsapp          string   `json:"whatsapp"`
		Status            string   `json:"status"`
	}
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	name := strings.TrimSpace(request.Name)
	code := strings.ToUpper(strings.TrimSpace(request.Code))
	email := strings.ToLower(strings.TrimSpace(request.Email))
	role, err := normalizeCollaboratorRole(request.Role)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if name == "" || code == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name and code are required")
	}
	if email == "" {
		return fiber.NewError(fiber.StatusBadRequest, "email is required")
	}
	if strings.TrimSpace(request.Password) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "password is required")
	}

	passwordHash, err := auth.HashPassword(request.Password)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	status := normalizeCollaboratorStatus(request.Status)
	commissionRateMin := decimalFromOptionalFloat(request.CommissionRateMin)
	commissionRateMax := decimalFromOptionalFloat(request.CommissionRateMax)
	if commissionRateMax.LessThan(commissionRateMin) {
		return fiber.NewError(fiber.StatusBadRequest, "commission_rate_max must be greater than or equal to commission_rate_min")
	}
	salaryBase := decimalFromOptionalFloat(request.SalaryBase)

	var (
		userID    uuid.UUID
		createdAt time.Time
		updatedAt time.Time
	)
	err = h.db.QueryRow(c.Context(), `
		INSERT INTO users (
			company_id, code, full_name, email, password_hash, role,
			commission_rate_min, commission_rate_max,
			pix_key, salary_base, whatsapp, status
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''), $10, NULLIF($11, ''), $12)
		RETURNING id, created_at, updated_at
	`, scope.CompanyID, code, name, email, passwordHash, role,
		commissionRateMin, commissionRateMax,
		strings.TrimSpace(request.PixKey), salaryBase,
		strings.TrimSpace(request.Whatsapp), status,
	).Scan(&userID, &createdAt, &updatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fiber.NewError(fiber.StatusConflict, "email or code already in use")
		}
		return err
	}

	// Link the new collaborator to existing ads in background.
	if h.syncService != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if err := h.syncService.LinkCollaboratorToAds(ctx, scope.CompanyID, userID, code, commissionRateMin, commissionRateMax); err != nil {
				h.logger.Error("failed to link collaborator to existing ads",
					slog.String("user_id", userID.String()),
					slog.String("code", code),
					slog.String("error", err.Error()),
				)
			}
		}()
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"id":                  userID.String(),
		"name":                name,
		"code":                code,
		"role":                role,
		"email":               email,
		"commission_rate_min": decimalToFloat(commissionRateMin),
		"commission_rate_max": decimalToFloat(commissionRateMax),
		"pix_key":             emptyStringPointer(request.PixKey),
		"salary_base":         decimalToFloat(salaryBase),
		"whatsapp":            emptyStringPointer(request.Whatsapp),
		"status":              status,
		"created_at":          createdAt,
		"updated_at":          updatedAt,
	})
}

// ── Update ──────────────────────────────────────────────────────────

func (h *CollaboratorsHandler) Update(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)
	userID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid collaborator id")
	}

	var request struct {
		Name              *string  `json:"name"`
		Code              *string  `json:"code"`
		Role              *string  `json:"role"`
		Email             *string  `json:"email"`
		Password          *string  `json:"password"`
		CommissionRateMin *float64 `json:"commission_rate_min"`
		CommissionRateMax *float64 `json:"commission_rate_max"`
		PixKey            *string  `json:"pix_key"`
		SalaryBase        *float64 `json:"salary_base"`
		Whatsapp          *string  `json:"whatsapp"`
		Status            *string  `json:"status"`
	}
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	// Load current user
	var (
		currentName   string
		currentCode   string
		currentRole   string
		currentMinDec decimal.Decimal
		currentMaxDec decimal.Decimal
		currentPix    *string
		currentSalary decimal.Decimal
		currentWA     *string
		currentStatus string
	)
	err = h.db.QueryRow(c.Context(), `
		SELECT full_name, COALESCE(code, ''), role, commission_rate_min, commission_rate_max,
		       pix_key, salary_base, whatsapp, status
		FROM users
		WHERE company_id = $1 AND id = $2
	`, scope.CompanyID, userID).Scan(
		&currentName, &currentCode, &currentRole,
		&currentMinDec, &currentMaxDec,
		&currentPix, &currentSalary, &currentWA, &currentStatus,
	)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "collaborator not found")
	}

	// Apply updates
	name := currentName
	if request.Name != nil {
		name = strings.TrimSpace(*request.Name)
	}
	code := currentCode
	if request.Code != nil {
		code = strings.ToUpper(strings.TrimSpace(*request.Code))
	}
	role := currentRole
	if request.Role != nil {
		role, err = normalizeCollaboratorRole(*request.Role)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
	}
	commissionMin := currentMinDec
	if request.CommissionRateMin != nil {
		commissionMin = decimalFromOptionalFloat(request.CommissionRateMin)
	}
	commissionMax := currentMaxDec
	if request.CommissionRateMax != nil {
		commissionMax = decimalFromOptionalFloat(request.CommissionRateMax)
	}
	if commissionMax.LessThan(commissionMin) {
		return fiber.NewError(fiber.StatusBadRequest, "commission_rate_max must be >= commission_rate_min")
	}
	pixKey := currentPix
	if request.PixKey != nil {
		pixKey = emptyStringPointer(*request.PixKey)
	}
	salaryBase := currentSalary
	if request.SalaryBase != nil {
		salaryBase = decimalFromOptionalFloat(request.SalaryBase)
	}
	whatsapp := currentWA
	if request.Whatsapp != nil {
		whatsapp = emptyStringPointer(*request.Whatsapp)
	}
	status := currentStatus
	if request.Status != nil {
		status = normalizeCollaboratorStatus(*request.Status)
	}

	if name == "" || code == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name and code are required")
	}

	// Build update query
	email := ""
	if request.Email != nil {
		email = strings.ToLower(strings.TrimSpace(*request.Email))
		if email == "" {
			return fiber.NewError(fiber.StatusBadRequest, "email cannot be empty")
		}
	}

	if email != "" {
		if _, err := h.db.Exec(c.Context(), `
			UPDATE users
			SET full_name = $3, code = $4, role = $5,
				commission_rate_min = $6, commission_rate_max = $7,
				pix_key = $8, salary_base = $9, whatsapp = $10,
				status = $11, email = $12, updated_at = now()
			WHERE company_id = $1 AND id = $2
		`, scope.CompanyID, userID,
			name, code, role,
			commissionMin, commissionMax,
			pixKey, salaryBase, whatsapp,
			status, email,
		); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return fiber.NewError(fiber.StatusConflict, "email or code already in use")
			}
			return err
		}
	} else {
		if _, err := h.db.Exec(c.Context(), `
			UPDATE users
			SET full_name = $3, code = $4, role = $5,
				commission_rate_min = $6, commission_rate_max = $7,
				pix_key = $8, salary_base = $9, whatsapp = $10,
				status = $11, updated_at = now()
			WHERE company_id = $1 AND id = $2
		`, scope.CompanyID, userID,
			name, code, role,
			commissionMin, commissionMax,
			pixKey, salaryBase, whatsapp,
			status,
		); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return fiber.NewError(fiber.StatusConflict, "code already in use")
			}
			return err
		}
	}

	if request.Password != nil && strings.TrimSpace(*request.Password) != "" {
		hash, err := auth.HashPassword(*request.Password)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		if _, err := h.db.Exec(c.Context(), `
			UPDATE users SET password_hash = $3, updated_at = now()
			WHERE company_id = $1 AND id = $2
		`, scope.CompanyID, userID, hash); err != nil {
			return err
		}
	}

	return c.JSON(fiber.Map{"ok": true})
}

// ── Delete (soft delete) ────────────────────────────────────────────

func (h *CollaboratorsHandler) Delete(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)
	userID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid collaborator id")
	}

	tag, err := h.db.Exec(c.Context(), `
		UPDATE users
		SET status = 'inactive', active = false, updated_at = now()
		WHERE company_id = $1 AND id = $2 AND role = ANY($3::text[])
	`, scope.CompanyID, userID, collaboratorRoles)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fiber.NewError(fiber.StatusNotFound, "collaborator not found")
	}

	return c.JSON(fiber.Map{"ok": true})
}

// ── UpdateDashboardCommission ────────────────────────────────────────

func (h *CollaboratorsHandler) UpdateDashboardCommission(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)
	dashboardID := strings.TrimSpace(c.Params("dashboardId"))
	userID, err := uuid.Parse(c.Params("collaboratorId"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid collaborator id")
	}

	var request struct {
		CommissionPctMin *float64 `json:"commission_pct_min"`
		CommissionPctMax *float64 `json:"commission_pct_max"`
	}
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	commissionMin := decimalFromOptionalFloat(request.CommissionPctMin)
	commissionMax := decimalFromOptionalFloat(request.CommissionPctMax)
	if commissionMax.LessThan(commissionMin) {
		return fiber.NewError(fiber.StatusBadRequest, "commission_pct_max must be >= commission_pct_min")
	}

	tag, err := h.db.Exec(c.Context(), `
		UPDATE ad_collaborators ac
		SET commission_pct_min = $4,
		    commission_pct_max = $5,
		    updated_at = now()
		FROM utmify_dashboards ud
		WHERE ud.company_id = ac.company_id
		  AND ud.niche_id = ac.niche_id
		  AND ac.company_id = $1
		  AND ud.external_id = $2
		  AND ac.user_id = $3
	`, scope.CompanyID, dashboardID, userID, commissionMin, commissionMax)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fiber.NewError(fiber.StatusNotFound, "collaborator not linked to this dashboard")
	}

	return c.JSON(fiber.Map{"ok": true, "updated": tag.RowsAffected()})
}

// ── MonthlyCommissions ──────────────────────────────────────────────

func (h *CollaboratorsHandler) MonthlyCommissions(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)
	userID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid collaborator id")
	}

	monthRaw := strings.TrimSpace(c.Query("month"))
	if monthRaw == "" {
		return fiber.NewError(fiber.StatusBadRequest, "month is required")
	}
	dashboardFilter := strings.TrimSpace(c.Query("dashboard"))

	monthStart, err := time.Parse("2006-01", monthRaw)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "month must use YYYY-MM format")
	}
	monthEnd := monthStart.AddDate(0, 1, 0)

	type collaboratorInfo struct {
		ID                uuid.UUID
		Name              string
		Code              string
		Role              string
		CommissionRateMin decimal.Decimal
		CommissionRateMax decimal.Decimal
		Status            string
	}
	var collaborator collaboratorInfo
	err = h.db.QueryRow(c.Context(), `
		SELECT id, full_name, COALESCE(code, ''), role, commission_rate_min, commission_rate_max, status
		FROM users
		WHERE company_id = $1
		  AND id = $2
	`, scope.CompanyID, userID).Scan(
		&collaborator.ID,
		&collaborator.Name,
		&collaborator.Code,
		&collaborator.Role,
		&collaborator.CommissionRateMin,
		&collaborator.CommissionRateMax,
		&collaborator.Status,
	)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "collaborator not found")
	}

	rows, err := h.db.Query(c.Context(), `
		SELECT
			ce.role,
			ce.ad_id,
			a.name,
			COALESCE(a.name_parsed->>'offer_code', '') AS offer_code,
			COALESCE(o.name, a.name_parsed->>'offer_code', '') AS offer_name,
			COALESCE(ud.currency, 'BRL') AS currency,
			COUNT(*) AS snapshot_days,
			COALESCE(SUM(ce.revenue_amount), 0) AS revenue_amount,
			COALESCE(SUM(ce.spend_amount), 0) AS spend_amount,
			COALESCE(SUM(ce.base_amount), 0) AS profit_amount,
			COALESCE(SUM(ce.commission_value), 0) AS commission_amount,
			MAX(ce.snapshot_date) AS last_snapshot_date
		FROM commission_entries ce
		INNER JOIN ads a
			ON a.id = ce.ad_id
		LEFT JOIN offers o
			ON o.company_id = a.company_id
		   AND o.niche_id = a.niche_id
		   AND o.code = COALESCE(a.name_parsed->>'offer_code', '')
		LEFT JOIN utmify_dashboards ud
			ON ud.company_id = a.company_id
		   AND ud.niche_id = a.niche_id
		WHERE ce.company_id = $1
		  AND ce.user_id = $2
		  AND ce.source_type = 'ad_snapshot'
		  AND ce.snapshot_date >= $3
		  AND ce.snapshot_date < $4
		  AND ($5 = '' OR ud.external_id = $5)
		GROUP BY ce.role, ce.ad_id, a.name, offer_code, offer_name, currency
		ORDER BY commission_amount DESC, a.name ASC
	`, scope.CompanyID, userID, monthStart, monthEnd, dashboardFilter)
	if err != nil {
		return err
	}
	defer rows.Close()

	type item struct {
		Role             string  `json:"role"`
		AdID             string  `json:"ad_id"`
		Creative         string  `json:"creative"`
		OfferCode        string  `json:"offer_code"`
		OfferName        string  `json:"offer_name"`
		Currency         string  `json:"currency"`
		SnapshotDays     int64   `json:"snapshot_days"`
		RevenueAmount    float64 `json:"revenue_amount"`
		SpendAmount      float64 `json:"spend_amount"`
		ProfitAmount     float64 `json:"profit_amount"`
		CommissionAmount float64 `json:"commission_amount"`
		LastSnapshotDate string  `json:"last_snapshot_date"`
	}

	items := make([]item, 0)
	type commissionTotals struct {
		Currency         string   `json:"currency"`
		RevenueAmount    float64  `json:"revenue_amount"`
		SpendAmount      float64  `json:"spend_amount"`
		ProfitAmount     float64  `json:"profit_amount"`
		CommissionAmount float64  `json:"commission_amount"`
		ROAS             *float64 `json:"roas,omitempty"`
	}

	currencyTotals := map[string]*commissionTotals{}
	for rows.Next() {
		var (
			entry            item
			adID             uuid.UUID
			revenueAmount    decimal.Decimal
			spendAmount      decimal.Decimal
			profitAmount     decimal.Decimal
			commissionAmount decimal.Decimal
			lastSnapshotDate time.Time
		)
		if err := rows.Scan(
			&entry.Role,
			&adID,
			&entry.Creative,
			&entry.OfferCode,
			&entry.OfferName,
			&entry.Currency,
			&entry.SnapshotDays,
			&revenueAmount,
			&spendAmount,
			&profitAmount,
			&commissionAmount,
			&lastSnapshotDate,
		); err != nil {
			return err
		}

		entry.AdID = adID.String()
		entry.RevenueAmount = decimalToFloat(revenueAmount)
		entry.SpendAmount = decimalToFloat(spendAmount)
		entry.ProfitAmount = decimalToFloat(profitAmount)
		entry.CommissionAmount = decimalToFloat(commissionAmount)
		entry.LastSnapshotDate = lastSnapshotDate.Format("2006-01-02")
		items = append(items, entry)

		total, exists := currencyTotals[entry.Currency]
		if !exists {
			total = &commissionTotals{Currency: entry.Currency}
			currencyTotals[entry.Currency] = total
		}
		total.RevenueAmount += entry.RevenueAmount
		total.SpendAmount += entry.SpendAmount
		total.ProfitAmount += entry.ProfitAmount
		total.CommissionAmount += entry.CommissionAmount
	}

	if err := rows.Err(); err != nil {
		return err
	}

	totals := make([]commissionTotals, 0, len(currencyTotals))
	for _, total := range currencyTotals {
		total.ROAS = computeRatio(total.RevenueAmount, total.SpendAmount)
		totals = append(totals, *total)
	}

	return c.JSON(fiber.Map{
		"month": monthRaw,
		"collaborator": fiber.Map{
			"id":                  collaborator.ID.String(),
			"name":                collaborator.Name,
			"code":                collaborator.Code,
			"role":                collaborator.Role,
			"commission_rate_min": decimalToFloat(collaborator.CommissionRateMin),
			"commission_rate_max": decimalToFloat(collaborator.CommissionRateMax),
			"status":              collaborator.Status,
		},
		"active_creatives": len(items),
		"totals":           totals,
		"items":            items,
	})
}

// ── ListByDashboard ─────────────────────────────────────────────────

func (h *CollaboratorsHandler) ListByDashboard(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)
	dashboardID := strings.TrimSpace(c.Params("dashboardId"))
	if dashboardID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "dashboard id is required")
	}

	rows, err := h.db.Query(c.Context(), `
		SELECT DISTINCT
			u.id,
			u.full_name,
			COALESCE(u.code, ''),
			u.role,
			u.commission_rate_min,
			u.commission_rate_max,
			u.pix_key,
			u.salary_base,
			u.whatsapp,
			u.status,
			COALESCE(
				(SELECT ac2.commission_pct_min FROM ad_collaborators ac2
				 INNER JOIN utmify_dashboards ud2 ON ud2.company_id = ac2.company_id AND ud2.niche_id = ac2.niche_id
				 WHERE ac2.user_id = u.id AND ud2.external_id = $2
				 LIMIT 1),
				u.commission_rate_min
			) AS dashboard_commission_min,
			COALESCE(
				(SELECT ac2.commission_pct_max FROM ad_collaborators ac2
				 INNER JOIN utmify_dashboards ud2 ON ud2.company_id = ac2.company_id AND ud2.niche_id = ac2.niche_id
				 WHERE ac2.user_id = u.id AND ud2.external_id = $2
				 LIMIT 1),
				u.commission_rate_max
			) AS dashboard_commission_max,
			COALESCE(
				(SELECT dao.allowed FROM dashboard_access_overrides dao
				 WHERE dao.company_id = ac.company_id
				   AND dao.user_id = u.id
				   AND dao.dashboard_id = $2),
				true
			) AS dashboard_allowed
		FROM ad_collaborators ac
		INNER JOIN users u
			ON u.id = ac.user_id
		INNER JOIN utmify_dashboards ud
			ON ud.company_id = ac.company_id
		   AND ud.niche_id = ac.niche_id
		WHERE ac.company_id = $1
		  AND ud.external_id = $2
		ORDER BY u.full_name ASC
	`, scope.CompanyID, dashboardID)
	if err != nil {
		return err
	}
	defer rows.Close()

	type response struct {
		ID                     string  `json:"id"`
		Name                   string  `json:"name"`
		Code                   string  `json:"code"`
		Role                   string  `json:"role"`
		CommissionRateMin      float64 `json:"commission_rate_min"`
		CommissionRateMax      float64 `json:"commission_rate_max"`
		PixKey                 *string `json:"pix_key,omitempty"`
		SalaryBase             float64 `json:"salary_base"`
		Whatsapp               *string `json:"whatsapp,omitempty"`
		Status                 string  `json:"status"`
		DashboardCommissionMin float64 `json:"dashboard_commission_min"`
		DashboardCommissionMax float64 `json:"dashboard_commission_max"`
		DashboardAllowed       bool    `json:"dashboard_allowed"`
	}

	items := make([]response, 0)
	for rows.Next() {
		var (
			item              response
			id                uuid.UUID
			commissionRateMin decimal.Decimal
			commissionRateMax decimal.Decimal
			salaryBase        decimal.Decimal
			dashboardMin      decimal.Decimal
			dashboardMax      decimal.Decimal
		)
		if err := rows.Scan(
			&id,
			&item.Name,
			&item.Code,
			&item.Role,
			&commissionRateMin,
			&commissionRateMax,
			&item.PixKey,
			&salaryBase,
			&item.Whatsapp,
			&item.Status,
			&dashboardMin,
			&dashboardMax,
			&item.DashboardAllowed,
		); err != nil {
			return err
		}

		item.ID = id.String()
		item.CommissionRateMin = decimalToFloat(commissionRateMin)
		item.CommissionRateMax = decimalToFloat(commissionRateMax)
		item.SalaryBase = decimalToFloat(salaryBase)
		item.DashboardCommissionMin = decimalToFloat(dashboardMin)
		item.DashboardCommissionMax = decimalToFloat(dashboardMax)
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return err
	}

	return c.JSON(items)
}

// ── GetDashboardAccess ───────────────────────────────────────────────

func (h *CollaboratorsHandler) GetDashboardAccess(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)
	dashboardID := strings.TrimSpace(c.Params("dashboardId"))
	if dashboardID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "dashboard id is required")
	}

	rows, err := h.db.Query(c.Context(), `
		SELECT dao.user_id, dao.allowed
		FROM dashboard_access_overrides dao
		WHERE dao.company_id = $1
		  AND dao.dashboard_id = $2
	`, scope.CompanyID, dashboardID)
	if err != nil {
		return err
	}
	defer rows.Close()

	type entry struct {
		UserID  string `json:"user_id"`
		Allowed bool   `json:"allowed"`
	}

	items := make([]entry, 0)
	for rows.Next() {
		var (
			userID  uuid.UUID
			allowed bool
		)
		if err := rows.Scan(&userID, &allowed); err != nil {
			return err
		}
		items = append(items, entry{UserID: userID.String(), Allowed: allowed})
	}

	if err := rows.Err(); err != nil {
		return err
	}

	return c.JSON(items)
}

// ── SetDashboardAccess ───────────────────────────────────────────────

func (h *CollaboratorsHandler) SetDashboardAccess(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)
	dashboardID := strings.TrimSpace(c.Params("dashboardId"))
	userID, err := uuid.Parse(c.Params("collaboratorId"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid collaborator id")
	}

	var request struct {
		Allowed *bool `json:"allowed"`
	}
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if request.Allowed == nil {
		return fiber.NewError(fiber.StatusBadRequest, "allowed field is required")
	}

	var updatedBy *uuid.UUID
	if scope.UserID != nil {
		updatedBy = scope.UserID
	}

	_, err = h.db.Exec(c.Context(), `
		INSERT INTO dashboard_access_overrides (company_id, user_id, dashboard_id, allowed, updated_by)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (company_id, user_id, dashboard_id)
		DO UPDATE SET allowed = EXCLUDED.allowed,
		              updated_by = EXCLUDED.updated_by,
		              updated_at = now()
	`, scope.CompanyID, userID, dashboardID, *request.Allowed, updatedBy)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{"ok": true, "allowed": *request.Allowed})
}

// ── Helpers ─────────────────────────────────────────────────────────

func normalizeCollaboratorRole(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "copywriter", "copy":
		return "copywriter", nil
	case "editor":
		return "editor", nil
	case "gestor de trafego", "gestor de tráfego", "gestor_trafego":
		return "gestor_trafego", nil
	case "desenvolvedor", "developer":
		return "desenvolvedor", nil
	case "closer":
		return "closer", nil
	default:
		return "", fmt.Errorf("invalid role")
	}
}

func normalizeCollaboratorStatus(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), "inactive") {
		return "inactive"
	}
	return "active"
}

func decimalFromOptionalFloat(value *float64) decimal.Decimal {
	if value == nil {
		return decimal.Zero
	}
	return decimal.NewFromFloat(*value).Round(4)
}

func emptyStringPointer(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
