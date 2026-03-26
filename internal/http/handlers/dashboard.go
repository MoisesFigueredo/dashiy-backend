package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
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

type DashboardHandler struct {
	db     *pgxpool.Pool
	client *utmify.Client
	cache  *cache.Redis
}

type dashboardListItemResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Currency string `json:"currency"`
	TimeZone int    `json:"time_zone"`
}

type dashboardHistoryResponse struct {
	Period      string                  `json:"period"`
	Granularity string                  `json:"granularity"`
	Currency    string                  `json:"currency"`
	Current     dashboardHistoryPeriod  `json:"current"`
	Previous    *dashboardHistoryPeriod `json:"previous"`
}

type dashboardHistoryPeriod struct {
	StartDate string                  `json:"startDate"`
	EndDate   string                  `json:"endDate"`
	Points    []dashboardHistoryPoint `json:"points"`
}

type dashboardHistoryPoint struct {
	Date              string                      `json:"date"`
	Revenue           float64                     `json:"revenue"`
	GrossRevenue      float64                     `json:"grossRevenue"`
	NetRevenue        float64                     `json:"netRevenue"`
	Spent             float64                     `json:"spent"`
	Profit            float64                     `json:"profit"`
	ROAS              *float64                    `json:"roas"`
	ROI               *float64                    `json:"roi"`
	CPA               *float64                    `json:"cpa"`
	ProfitMargin      *float64                    `json:"profitMargin"`
	AvgTicket         *float64                    `json:"avgTicket"`
	OrdersTotal       int64                       `json:"ordersTotal"`
	OrdersApproved    int64                       `json:"ordersApproved"`
	OrdersPending     int64                       `json:"ordersPending"`
	OrdersRefunded    int64                       `json:"ordersRefunded"`
	OrdersChargedback int64                       `json:"ordersChargedback"`
	RefundRate        float64                     `json:"refundRate"`
	ChargebackRate    float64                     `json:"chargebackRate"`
	Clicks            int64                       `json:"clicks"`
	PageViews         int64                       `json:"pageViews"`
	InitiateCheckouts int64                       `json:"initiateCheckouts"`
	Leads             int64                       `json:"leads"`
	SalesByUtmSource  []historyUtmSourceBreakdown `json:"salesByUtmSource"`
	SalesByPayment    *historyPaymentBreakdown    `json:"salesByPayment"`
	SalesByProduct    []historyProductBreakdown   `json:"salesByProduct"`
	SalesByCountry    []historyCountryBreakdown   `json:"salesByCountry"`
}

type historyUtmSourceBreakdown struct {
	Source  string  `json:"source"`
	Count   int64   `json:"count"`
	Revenue float64 `json:"revenue"`
}

type historyPaymentBreakdown struct {
	Pix        float64 `json:"pix"`
	CreditCard float64 `json:"creditCard"`
	Boleto     float64 `json:"boleto"`
}

type historyProductBreakdown struct {
	ProductName string  `json:"productName"`
	Count       int64   `json:"count"`
	Revenue     float64 `json:"revenue"`
}

type historyCountryBreakdown struct {
	Country string `json:"country"`
	Count   int64  `json:"count"`
}

type dashboardHistorySummary struct {
	OrdersCount struct {
		Total       int64 `json:"total"`
		Approved    int64 `json:"approved"`
		Pending     int64 `json:"pending"`
		Refunded    int64 `json:"refunded"`
		Chargedback int64 `json:"chargedback"`
		ByUtmSource []struct {
			Count     int64   `json:"count"`
			Revenue   float64 `json:"revenue"`
			UTMSource *string `json:"utmSource"`
			Source    *string `json:"source"`
		} `json:"byUtmSource"`
		ByProductName []struct {
			Count       int64   `json:"count"`
			Revenue     float64 `json:"revenue"`
			ProductName string  `json:"productName"`
		} `json:"byProductName"`
		ByCustomerCountry []struct {
			Count   int64   `json:"count"`
			Country *string `json:"country"`
		} `json:"byCustomerCountry"`
	} `json:"ordersCount"`
	Commissions struct {
		Net                    float64 `json:"net"`
		Gross                  float64 `json:"gross"`
		PendingGrossRevenue    float64 `json:"pendingGrossRevenue"`
		RefundedGrossRevenue   float64 `json:"refundedGrossRevenue"`
		ChargebackGrossRevenue float64 `json:"chargebackGrossRevenue"`
	} `json:"comissions"`
	Statistics struct {
		RefundRate                 float64 `json:"refundRate"`
		RevenueChargedbackRate     float64 `json:"revenueChargedbackRate"`
		RevenuePercByPaymentMethod struct {
			Pix        float64 `json:"pix"`
			CreditCard float64 `json:"creditCard"`
			Boleto     float64 `json:"boleto"`
		} `json:"revenuePercByPaymentMethod"`
		Pix struct {
			Approved struct {
				Commission *float64 `json:"comission"`
			} `json:"approved"`
		} `json:"pix"`
		Card struct {
			Approved struct {
				Commission *float64 `json:"comission"`
			} `json:"approved"`
		} `json:"card"`
		Boleto struct {
			Approved struct {
				Commission *float64 `json:"comission"`
			} `json:"approved"`
		} `json:"boleto"`
	} `json:"statistics"`
	Ads struct {
		Spent             float64 `json:"spent"`
		Clicks            int64   `json:"clicks"`
		PageViews         int64   `json:"pageViews"`
		InitiateCheckouts int64   `json:"initiateCheckouts"`
		Leads             int64   `json:"leads"`
	} `json:"ads"`
	Analytics struct {
		Profit       float64  `json:"profit"`
		ROAS         *float64 `json:"roas"`
		ROI          *float64 `json:"roi"`
		CPA          *float64 `json:"cpa"`
		ProfitMargin *float64 `json:"profitMargin"`
		AvgTicket    *float64 `json:"avgTicket"`
	} `json:"analytics"`
}

func NewDashboardHandler(db *pgxpool.Pool, client *utmify.Client, cache *cache.Redis) *DashboardHandler {
	return &DashboardHandler{db: db, client: client, cache: cache}
}

func (h *DashboardHandler) List(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)

	dashboards, err := loadAccessibleDashboards(c.Context(), h.db, scope)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load company dashboards")
	}

	response := make([]dashboardListItemResponse, 0, len(dashboards))
	for _, dashboard := range dashboards {
		response = append(response, dashboardListItemResponse{
			ID:       dashboard.ExternalID,
			Name:     dashboard.Name,
			Currency: dashboard.Currency,
			TimeZone: dashboard.TimeZone,
		})
	}

	return c.JSON(response)
}

func (h *DashboardHandler) Summary(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)
	date := strings.TrimSpace(c.Query("date"))
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}
	dashboardID := strings.TrimSpace(c.Query("dashboard"))

	dashboards, err := loadAccessibleDashboards(c.Context(), h.db, scope)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load company dashboards")
	}

	selectedDashboards, ok := filterDashboardsByID(dashboards, dashboardID)
	if dashboardID != "" && !ok {
		return fiber.NewError(fiber.StatusNotFound, "dashboard not found")
	}

	if len(selectedDashboards) == 0 {
		return c.JSON(emptyDashboardSummary(date))
	}

	cacheKey := dashboardSummaryCacheKey(scope, dashboardID, date)
	if h.cache != nil {
		var cached workers.CachedDashboardSummary
		found, err := h.cache.Get(c.Context(), cacheKey, &cached)
		if err == nil && found {
			return c.JSON(cached)
		}
	}

	inputs := h.resolveInputsFromDB(c.Context(), selectedDashboards, date)
	summary := workers.AggregateDashboardSummaries(inputs, date)
	summary.CachedAt = time.Now().UTC().Format(time.RFC3339)

	if h.cache != nil {
		_ = h.cache.Set(c.Context(), cacheKey, summary, time.Minute)
	}

	return c.JSON(summary)
}

func (h *DashboardHandler) History(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)
	period, days, err := parsePeriod(c.Query("period"), "")
	if err != nil {
		return err
	}

	dashboardID := strings.TrimSpace(c.Query("dashboard"))
	compare := strings.EqualFold(strings.TrimSpace(c.Query("compare")), "true")

	dashboards, err := loadAccessibleDashboards(c.Context(), h.db, scope)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load company dashboards")
	}

	selectedDashboards, ok := filterDashboardsByID(dashboards, dashboardID)
	if dashboardID != "" && !ok {
		return fiber.NewError(fiber.StatusNotFound, "dashboard not found")
	}

	currentRange := buildHistoryRange(days, time.Now().UTC())
	response := dashboardHistoryResponse{
		Period:      period,
		Granularity: "day",
		Currency:    firstDashboardCurrency(selectedDashboards),
		Current: dashboardHistoryPeriod{
			StartDate: currentRange.StartDate.Format("2006-01-02"),
			EndDate:   currentRange.EndDate.Format("2006-01-02"),
			Points:    []dashboardHistoryPoint{},
		},
		Previous: nil,
	}

	if len(selectedDashboards) == 0 {
		return c.JSON(response)
	}

	cacheKey := dashboardHistoryCacheKey(scope, dashboardID, period, compare)
	if h.cache != nil {
		var cached dashboardHistoryResponse
		found, err := h.cache.Get(c.Context(), cacheKey, &cached)
		if err == nil && found {
			return c.JSON(cached)
		}
	}

	currentPoints, err := h.loadHistoryPoints(c.Context(), scope.CompanyID, selectedDashboards, currentRange)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load dashboard history")
	}
	response.Current.Points = currentPoints

	if compare {
		previousRange := buildPreviousHistoryRange(currentRange, days)
		previousPoints, err := h.loadHistoryPoints(c.Context(), scope.CompanyID, selectedDashboards, previousRange)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "failed to load dashboard history comparison")
		}
		if len(previousPoints) > 0 {
			response.Previous = &dashboardHistoryPeriod{
				StartDate: previousRange.StartDate.Format("2006-01-02"),
				EndDate:   previousRange.EndDate.Format("2006-01-02"),
				Points:    previousPoints,
			}
		}
	}

	if h.cache != nil {
		_ = h.cache.Set(c.Context(), cacheKey, response, 5*time.Minute)
	}

	return c.JSON(response)
}

func (h *DashboardHandler) resolveInputsFromDB(ctx context.Context, dashboards []companyDashboard, date string) []workers.DashboardSummaryInput {
	inputs := make([]workers.DashboardSummaryInput, 0, len(dashboards))

	for _, dashboard := range dashboards {
		input := workers.DashboardSummaryInput{
			ID:       dashboard.ExternalID,
			Name:     dashboard.Name,
			Currency: dashboard.Currency,
			TimeZone: dashboard.TimeZone,
		}

		var rawJSON []byte
		err := h.db.QueryRow(ctx, `
			SELECT raw_summary
			FROM dashboard_summary_snapshots
			WHERE dashboard_external_id = $1
			  AND snapshot_date = $2
		`, dashboard.ExternalID, date).Scan(&rawJSON)
		if err == nil && len(rawJSON) > 0 {
			var summary utmify.DashboardSummary
			if jsonErr := json.Unmarshal(rawJSON, &summary); jsonErr == nil {
				input.Summary = &summary
				inputs = append(inputs, input)
				continue
			}
		}

		summary, err := h.client.GetDashboardSummaryForDashboard(ctx, utmify.Dashboard{
			ID:       dashboard.ExternalID,
			Name:     dashboard.Name,
			Currency: dashboard.Currency,
			TimeZone: dashboard.TimeZone,
		}, date, date)
		if err != nil {
			input.Error = err.Error()
		} else {
			input.Summary = summary
		}

		inputs = append(inputs, input)
	}

	return inputs
}

func (h *DashboardHandler) loadHistoryPoints(ctx context.Context, companyID uuid.UUID, dashboards []companyDashboard, dateRange historyDateRange) ([]dashboardHistoryPoint, error) {
	dashboardIDs := make([]string, 0, len(dashboards))
	for _, dashboard := range dashboards {
		dashboardIDs = append(dashboardIDs, dashboard.ExternalID)
	}

	if len(dashboardIDs) == 0 {
		return []dashboardHistoryPoint{}, nil
	}

	rows, err := h.db.Query(ctx, `
		SELECT s.snapshot_date, s.raw_summary
		FROM dashboard_summary_snapshots s
		INNER JOIN utmify_dashboards ud
			ON ud.external_id = s.dashboard_external_id
		   AND ud.company_id = $1
		WHERE s.snapshot_date >= $2
		  AND s.snapshot_date <= $3
		  AND s.dashboard_external_id = ANY($4::text[])
		ORDER BY s.snapshot_date ASC, s.dashboard_external_id ASC
	`, companyID, dateRange.StartDate, dateRange.EndDate, dashboardIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byDate := make(map[string][]dashboardHistorySummary)
	for rows.Next() {
		var (
			snapshotDate time.Time
			rawJSON      []byte
			summary      dashboardHistorySummary
		)
		if err := rows.Scan(&snapshotDate, &rawJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rawJSON, &summary); err != nil {
			continue
		}

		dateKey := snapshotDate.Format("2006-01-02")
		byDate[dateKey] = append(byDate[dateKey], summary)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	dates := make([]string, 0, len(byDate))
	for date := range byDate {
		dates = append(dates, date)
	}
	sort.Strings(dates)

	points := make([]dashboardHistoryPoint, 0, len(dates))
	for _, date := range dates {
		points = append(points, aggregateHistoryPoint(date, byDate[date]))
	}

	return points, nil
}

func aggregateHistoryPoint(date string, summaries []dashboardHistorySummary) dashboardHistoryPoint {
	point := dashboardHistoryPoint{
		Date:             date,
		SalesByUtmSource: []historyUtmSourceBreakdown{},
		SalesByProduct:   []historyProductBreakdown{},
		SalesByCountry:   []historyCountryBreakdown{},
	}

	sourceMap := make(map[string]*historyUtmSourceBreakdown)
	productMap := make(map[string]*historyProductBreakdown)
	countryMap := make(map[string]*historyCountryBreakdown)

	var (
		chargebackRevenue float64
		avgTicketWeighted float64
		avgTicketWeight   float64
		paymentPixAbs     float64
		paymentCardAbs    float64
		paymentBoletoAbs  float64
		fallbackPix       float64
		fallbackCard      float64
		fallbackBoleto    float64
		fallbackWeight    float64
	)

	for _, summary := range summaries {
		point.Revenue += summary.Commissions.Net
		point.GrossRevenue += summary.Commissions.Gross
		point.NetRevenue += summary.Commissions.Net
		point.Spent += summary.Ads.Spent
		point.Profit += summary.Analytics.Profit

		point.OrdersTotal += summary.OrdersCount.Total
		point.OrdersApproved += summary.OrdersCount.Approved
		point.OrdersPending += summary.OrdersCount.Pending
		point.OrdersRefunded += summary.OrdersCount.Refunded
		point.OrdersChargedback += summary.OrdersCount.Chargedback

		point.Clicks += summary.Ads.Clicks
		point.PageViews += summary.Ads.PageViews
		point.InitiateCheckouts += summary.Ads.InitiateCheckouts
		point.Leads += summary.Ads.Leads

		chargebackRevenue += summary.Commissions.ChargebackGrossRevenue

		if summary.Analytics.AvgTicket != nil && summary.OrdersCount.Approved > 0 {
			avgTicketWeighted += *summary.Analytics.AvgTicket * float64(summary.OrdersCount.Approved)
			avgTicketWeight += float64(summary.OrdersCount.Approved)
		}

		for _, item := range summary.OrdersCount.ByUtmSource {
			source := normalizedBreakdownLabel(item.UTMSource, item.Source, "outros")
			existing := sourceMap[source]
			if existing == nil {
				existing = &historyUtmSourceBreakdown{Source: source}
				sourceMap[source] = existing
			}
			existing.Count += item.Count
			existing.Revenue += item.Revenue
		}

		for _, item := range summary.OrdersCount.ByProductName {
			productName := strings.TrimSpace(item.ProductName)
			if productName == "" {
				productName = "Sem nome"
			}
			existing := productMap[productName]
			if existing == nil {
				existing = &historyProductBreakdown{ProductName: productName}
				productMap[productName] = existing
			}
			existing.Count += item.Count
			existing.Revenue += item.Revenue
		}

		for _, item := range summary.OrdersCount.ByCustomerCountry {
			country := normalizedBreakdownLabel(item.Country, nil, "OUTROS")
			existing := countryMap[country]
			if existing == nil {
				existing = &historyCountryBreakdown{Country: country}
				countryMap[country] = existing
			}
			existing.Count += item.Count
		}

		if summary.Statistics.Pix.Approved.Commission != nil {
			paymentPixAbs += *summary.Statistics.Pix.Approved.Commission
		}
		if summary.Statistics.Card.Approved.Commission != nil {
			paymentCardAbs += *summary.Statistics.Card.Approved.Commission
		}
		if summary.Statistics.Boleto.Approved.Commission != nil {
			paymentBoletoAbs += *summary.Statistics.Boleto.Approved.Commission
		}

		if summary.Commissions.Gross > 0 {
			fallbackPix += summary.Statistics.RevenuePercByPaymentMethod.Pix * summary.Commissions.Gross
			fallbackCard += summary.Statistics.RevenuePercByPaymentMethod.CreditCard * summary.Commissions.Gross
			fallbackBoleto += summary.Statistics.RevenuePercByPaymentMethod.Boleto * summary.Commissions.Gross
			fallbackWeight += summary.Commissions.Gross
		}
	}

	if point.Spent > 0 {
		roas := point.GrossRevenue / point.Spent
		roi := point.NetRevenue / point.Spent
		point.ROAS = &roas
		point.ROI = &roi
	}

	if point.OrdersApproved > 0 {
		cpa := point.Spent / float64(point.OrdersApproved)
		point.CPA = &cpa
	}

	if avgTicketWeight > 0 {
		avgTicket := avgTicketWeighted / avgTicketWeight
		point.AvgTicket = &avgTicket
	}

	if point.NetRevenue > 0 {
		profitMargin := point.Profit / point.NetRevenue
		point.ProfitMargin = &profitMargin
	}

	if point.OrdersTotal > 0 {
		point.RefundRate = float64(point.OrdersRefunded) / float64(point.OrdersTotal)
	}

	if point.GrossRevenue > 0 {
		point.ChargebackRate = chargebackRevenue / point.GrossRevenue
	}

	point.SalesByUtmSource = sortedUtmSources(sourceMap)
	point.SalesByProduct = sortedProducts(productMap)
	point.SalesByCountry = sortedCountries(countryMap)

	paymentTotal := paymentPixAbs + paymentCardAbs + paymentBoletoAbs
	if paymentTotal > 0 {
		point.SalesByPayment = &historyPaymentBreakdown{
			Pix:        paymentPixAbs / paymentTotal,
			CreditCard: paymentCardAbs / paymentTotal,
			Boleto:     paymentBoletoAbs / paymentTotal,
		}
	} else if fallbackWeight > 0 {
		point.SalesByPayment = &historyPaymentBreakdown{
			Pix:        fallbackPix / fallbackWeight,
			CreditCard: fallbackCard / fallbackWeight,
			Boleto:     fallbackBoleto / fallbackWeight,
		}
	}

	return point
}

func sortedUtmSources(items map[string]*historyUtmSourceBreakdown) []historyUtmSourceBreakdown {
	response := make([]historyUtmSourceBreakdown, 0, len(items))
	for _, item := range items {
		response = append(response, *item)
	}

	sort.Slice(response, func(i, j int) bool {
		if response[i].Count == response[j].Count {
			return response[i].Revenue > response[j].Revenue
		}
		return response[i].Count > response[j].Count
	})

	return response
}

func sortedProducts(items map[string]*historyProductBreakdown) []historyProductBreakdown {
	response := make([]historyProductBreakdown, 0, len(items))
	for _, item := range items {
		response = append(response, *item)
	}

	sort.Slice(response, func(i, j int) bool {
		if response[i].Revenue == response[j].Revenue {
			return response[i].Count > response[j].Count
		}
		return response[i].Revenue > response[j].Revenue
	})

	return response
}

func sortedCountries(items map[string]*historyCountryBreakdown) []historyCountryBreakdown {
	response := make([]historyCountryBreakdown, 0, len(items))
	for _, item := range items {
		response = append(response, *item)
	}

	sort.Slice(response, func(i, j int) bool {
		return response[i].Count > response[j].Count
	})

	return response
}

func normalizedBreakdownLabel(primary *string, fallback *string, defaultValue string) string {
	if primary != nil && strings.TrimSpace(*primary) != "" {
		return strings.TrimSpace(*primary)
	}
	if fallback != nil && strings.TrimSpace(*fallback) != "" {
		return strings.TrimSpace(*fallback)
	}
	return defaultValue
}

func emptyDashboardSummary(date string) workers.CachedDashboardSummary {
	return workers.CachedDashboardSummary{
		Date:            date,
		PrimaryCurrency: "BRL",
		Currencies:      []workers.CurrencySummary{},
		Dashboards:      []workers.DashboardItem{},
		CachedAt:        time.Now().UTC().Format(time.RFC3339),
	}
}

func firstDashboardCurrency(dashboards []companyDashboard) string {
	for _, dashboard := range dashboards {
		if strings.TrimSpace(dashboard.Currency) != "" {
			return dashboard.Currency
		}
	}
	return "BRL"
}

func dashboardSummaryCacheKey(scope middleware.CompanyContext, dashboardID, date string) string {
	return fmt.Sprintf("dashboard:summary:%s:%s:%s:%s", scope.CompanyID.String(), cacheAccessScope(scope), normalizedCacheValue(dashboardID), date)
}

func dashboardHistoryCacheKey(scope middleware.CompanyContext, dashboardID, period string, compare bool) string {
	return fmt.Sprintf("dashboard:history:%s:%s:%s:%t:%s", scope.CompanyID.String(), normalizedCacheValue(dashboardID), period, compare, cacheAccessScope(scope))
}

func normalizedCacheValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "all"
	}
	return value
}

func cacheAccessScope(scope middleware.CompanyContext) string {
	if scope.IsCollaborator && scope.UserID != nil {
		return scope.UserID.String()
	}
	role := strings.TrimSpace(scope.Role)
	if role == "" {
		return "company"
	}
	return role
}
