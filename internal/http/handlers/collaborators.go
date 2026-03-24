package handlers

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/canal/metricas-financeiro-app/backend/internal/http/middleware"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type CollaboratorsHandler struct {
	db *pgxpool.Pool
}

func NewCollaboratorsHandler(db *pgxpool.Pool) *CollaboratorsHandler {
	return &CollaboratorsHandler{db: db}
}

func (h *CollaboratorsHandler) List(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)

	rows, err := h.db.Query(c.Context(), `
		SELECT
			id,
			name,
			code,
			role,
			commission_rate_min,
			commission_rate_max,
			pix_key,
			salary_base,
			whatsapp,
			status,
			created_at,
			updated_at
		FROM collaborators
		WHERE company_id = $1
		ORDER BY name ASC
	`, scope.CompanyID)
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
		); err != nil {
			return err
		}

		item.ID = id.String()
		item.CommissionRateMin = decimalToFloat(commissionRateMin)
		item.CommissionRateMax = decimalToFloat(commissionRateMax)
		item.SalaryBase = decimalToFloat(salaryBase)
		item.CreatedAt = &createdAt
		item.UpdatedAt = &updatedAt
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return err
	}

	return c.JSON(items)
}

func (h *CollaboratorsHandler) Create(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)

	var request struct {
		Name              string   `json:"name"`
		Code              string   `json:"code"`
		Role              string   `json:"role"`
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
	role, err := normalizeCollaboratorRole(request.Role)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if name == "" || code == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name and code are required")
	}

	status := normalizeCollaboratorStatus(request.Status)
	commissionRateMin := decimalFromOptionalFloat(request.CommissionRateMin)
	commissionRateMax := decimalFromOptionalFloat(request.CommissionRateMax)
	if commissionRateMax.LessThan(commissionRateMin) {
		return fiber.NewError(fiber.StatusBadRequest, "commission_rate_max must be greater than or equal to commission_rate_min")
	}
	salaryBase := decimalFromOptionalFloat(request.SalaryBase)

	var (
		id        uuid.UUID
		createdAt time.Time
		updatedAt time.Time
	)
	err = h.db.QueryRow(c.Context(), `
		INSERT INTO collaborators (
			company_id,
			name,
			code,
			role,
			commission_rate_min,
			commission_rate_max,
			pix_key,
			salary_base,
			whatsapp,
			status
		) VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, NULLIF($9, ''), $10)
		RETURNING id, created_at, updated_at
	`, scope.CompanyID, name, code, role, commissionRateMin, commissionRateMax, strings.TrimSpace(request.PixKey), salaryBase, strings.TrimSpace(request.Whatsapp), status).Scan(&id, &createdAt, &updatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fiber.NewError(fiber.StatusConflict, "collaborator code already exists")
		}
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"id":                  id.String(),
		"name":                name,
		"code":                code,
		"role":                role,
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

func (h *CollaboratorsHandler) MonthlyCommissions(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)
	collaboratorID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid collaborator id")
	}

	monthRaw := strings.TrimSpace(c.Query("month"))
	if monthRaw == "" {
		return fiber.NewError(fiber.StatusBadRequest, "month is required")
	}

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
		SELECT id, name, code, role, commission_rate_min, commission_rate_max, status
		FROM collaborators
		WHERE company_id = $1
		  AND id = $2
	`, scope.CompanyID, collaboratorID).Scan(
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
		  AND ce.collaborator_id = $2
		  AND ce.source_type = 'ad_snapshot'
		  AND ce.snapshot_date >= $3
		  AND ce.snapshot_date < $4
		GROUP BY ce.role, ce.ad_id, a.name, offer_code, offer_name, currency
		ORDER BY commission_amount DESC, a.name ASC
	`, scope.CompanyID, collaboratorID, monthStart, monthEnd)
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
