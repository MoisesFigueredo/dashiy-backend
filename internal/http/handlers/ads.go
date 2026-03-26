package handlers

import (
	"strings"
	"time"

	"github.com/canal/metricas-financeiro-app/backend/internal/http/middleware"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type AdsHandler struct {
	db *pgxpool.Pool
}

type currencySummary struct {
	Currency string   `json:"currency"`
	Spent    float64  `json:"spent"`
	Revenue  float64  `json:"revenue"`
	Profit   float64  `json:"profit"`
	ROAS     *float64 `json:"roas,omitempty"`
}

func NewAdsHandler(db *pgxpool.Pool) *AdsHandler {
	return &AdsHandler{db: db}
}

func (h *AdsHandler) List(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)
	date := defaultDate(c.Query("date"))
	offerCode := c.Query("offer")
	status := c.Query("status")

	rows, err := h.db.Query(c.Context(), `
		SELECT
			a.id,
			a.external_id,
			a.name,
			COALESCE(a.name_parsed->>'offer_code', '') AS offer_code,
			COALESCE(o.name, a.name_parsed->>'offer_code', '') AS offer_name,
			copy_collab.name AS copy_name,
			copy_collab.code AS copy_code,
			editor_collab.name AS editor_name,
			editor_collab.code AS editor_code,
			COALESCE(ud.currency, 'BRL') AS currency,
			s.spend,
			s.revenue,
			s.profit,
			s.roas,
			s.cpa,
			s.hook_rate,
			s.body_rate,
			s.ctr,
			s.object_status,
			s.effective_status,
			s.snapshot_date
		FROM ad_metric_snapshots s
		INNER JOIN ads a
			ON a.id = s.ad_id
		LEFT JOIN offers o
			ON o.company_id = a.company_id
		   AND o.niche_id = a.niche_id
		   AND o.code = COALESCE(a.name_parsed->>'offer_code', '')
		LEFT JOIN utmify_dashboards ud
			ON ud.company_id = a.company_id
		   AND ud.niche_id = a.niche_id
		LEFT JOIN LATERAL (
			SELECT u.full_name AS name, COALESCE(u.code, '') AS code
			FROM ad_collaborators ac
			INNER JOIN users u
				ON u.id = ac.user_id
			WHERE ac.ad_id = a.id
			  AND ac.role = 'copywriter'
			ORDER BY ac.updated_at DESC
			LIMIT 1
		) AS copy_collab ON TRUE
		LEFT JOIN LATERAL (
			SELECT u.full_name AS name, COALESCE(u.code, '') AS code
			FROM ad_collaborators ac
			INNER JOIN users u
				ON u.id = ac.user_id
			WHERE ac.ad_id = a.id
			  AND ac.role = 'editor'
			ORDER BY ac.updated_at DESC
			LIMIT 1
		) AS editor_collab ON TRUE
		WHERE s.company_id = $1
		  AND s.snapshot_date = $2
		  AND ($3 = '' OR COALESCE(a.name_parsed->>'offer_code', '') = $3)
		  AND (
			$4 = ''
			OR UPPER(COALESCE(NULLIF(s.effective_status, ''), NULLIF(s.object_status, ''), 'UNKNOWN')) = UPPER($4)
		  )
		ORDER BY s.profit DESC, a.name ASC
	`, scope.CompanyID, date, offerCode, status)
	if err != nil {
		return err
	}
	defer rows.Close()

	type response struct {
		ID              string  `json:"id"`
		ExternalID      string  `json:"external_id"`
		Name            string  `json:"name"`
		OfferCode       string  `json:"offer_code"`
		OfferName       string  `json:"offer_name"`
		CopyName        *string `json:"copy_name,omitempty"`
		CopyCode        *string `json:"copy_code,omitempty"`
		EditorName      *string `json:"editor_name,omitempty"`
		EditorCode      *string `json:"editor_code,omitempty"`
		Currency        string  `json:"currency"`
		Spend           float64 `json:"spend"`
		Revenue         float64 `json:"revenue"`
		Profit          float64 `json:"profit"`
		ROAS            float64 `json:"roas"`
		CPA             float64 `json:"cpa"`
		HookRate        float64 `json:"hook_rate"`
		BodyRate        float64 `json:"body_rate"`
		CTR             float64 `json:"ctr"`
		Status          string  `json:"status"`
		EffectiveStatus string  `json:"effective_status"`
		SnapshotDate    string  `json:"snapshot_date"`
	}

	items := make([]response, 0)
	for rows.Next() {
		var (
			item         response
			adID         uuid.UUID
			spend        decimal.Decimal
			revenue      decimal.Decimal
			profit       decimal.Decimal
			roas         decimal.Decimal
			cpa          decimal.Decimal
			hookRate     decimal.Decimal
			bodyRate     decimal.Decimal
			ctr          decimal.Decimal
			snapshotDate time.Time
		)
		if err := rows.Scan(
			&adID,
			&item.ExternalID,
			&item.Name,
			&item.OfferCode,
			&item.OfferName,
			&item.CopyName,
			&item.CopyCode,
			&item.EditorName,
			&item.EditorCode,
			&item.Currency,
			&spend,
			&revenue,
			&profit,
			&roas,
			&cpa,
			&hookRate,
			&bodyRate,
			&ctr,
			&item.Status,
			&item.EffectiveStatus,
			&snapshotDate,
		); err != nil {
			return err
		}

		item.ID = adID.String()
		item.Spend = decimalToFloat(spend)
		item.Revenue = decimalToFloat(revenue)
		item.Profit = decimalToFloat(profit)
		item.ROAS = decimalToFloat(roas)
		item.CPA = decimalToFloat(cpa)
		item.HookRate = decimalToFloat(hookRate)
		item.BodyRate = decimalToFloat(bodyRate)
		item.CTR = decimalToFloat(ctr)
		item.SnapshotDate = snapshotDate.Format("2006-01-02")
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"date":  date.Format("2006-01-02"),
		"items": items,
	})
}

func (h *AdsHandler) Summary(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)
	startDate, err := parseRequiredDate(c.Query("startDate"), "startDate")
	if err != nil {
		return err
	}
	endDate, err := parseRequiredDate(c.Query("endDate"), "endDate")
	if err != nil {
		return err
	}

	rows, err := h.db.Query(c.Context(), `
		SELECT
			COALESCE(ud.currency, 'BRL') AS currency,
			COALESCE(SUM(s.spend), 0) AS spend,
			COALESCE(SUM(s.revenue), 0) AS revenue,
			COALESCE(SUM(s.profit), 0) AS profit
		FROM ad_metric_snapshots s
		INNER JOIN ads a
			ON a.id = s.ad_id
		LEFT JOIN utmify_dashboards ud
			ON ud.company_id = a.company_id
		   AND ud.niche_id = a.niche_id
		WHERE s.company_id = $1
		  AND s.snapshot_date >= $2
		  AND s.snapshot_date <= $3
		GROUP BY COALESCE(ud.currency, 'BRL')
		ORDER BY currency
	`, scope.CompanyID, startDate, endDate)
	if err != nil {
		return err
	}
	defer rows.Close()

	summaries := make([]currencySummary, 0)
	for rows.Next() {
		var (
			item    currencySummary
			spend   decimal.Decimal
			revenue decimal.Decimal
			profit  decimal.Decimal
		)
		if err := rows.Scan(&item.Currency, &spend, &revenue, &profit); err != nil {
			return err
		}

		item.Spent = decimalToFloat(spend)
		item.Revenue = decimalToFloat(revenue)
		item.Profit = decimalToFloat(profit)
		item.ROAS = computeRatio(item.Revenue, item.Spent)
		summaries = append(summaries, item)
	}

	if err := rows.Err(); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"start_date": startDate.Format("2006-01-02"),
		"end_date":   endDate.Format("2006-01-02"),
		"currencies": summaries,
	})
}

func defaultDate(raw string) time.Time {
	if strings.TrimSpace(raw) == "" {
		now := time.Now().UTC()
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	}
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(raw))
	if err != nil {
		now := time.Now().UTC()
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	}
	return parsed
}
