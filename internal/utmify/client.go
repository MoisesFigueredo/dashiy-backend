package utmify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/canal/metricas-financeiro-app/backend/internal/config"
)

const (
	defaultTimeout     = 30 * time.Second
	dateLayout         = "2006-01-02"
	dateTimeLayout     = "2006-01-02T15:04:05-07:00"
	rateLimitRetryWait = 5 * time.Second
	rateLimitMaxRetries = 12 // 12 * 5s = 60s max
)

type Client struct {
	baseURL   string
	http      *http.Client
	requestID atomic.Int64
}

type DateRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Dashboard struct {
	ID             string              `json:"id"`
	Name           string              `json:"name"`
	MetaProfiles   []AdPlatformProfile `json:"metaProfiles"`
	GoogleProfiles []AdPlatformProfile `json:"googleProfiles"`
	KwaiProfiles   []AdPlatformProfile `json:"kwaiProfiles"`
	TikTokProfiles []AdPlatformProfile `json:"tikTokProfiles"`
	Platforms      []string            `json:"platforms"`
	Products       []string            `json:"products"`
	TimeZone       int                 `json:"timeZone"`
	ViewType       string              `json:"viewType"`
	Currency       string              `json:"currency"`
	CreatedAt      time.Time           `json:"createdAt"`
}

type AdPlatformProfile struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	AdAccounts []AdAccountRef `json:"adAccounts"`
}

type AdAccountRef struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type DashboardSummary struct {
	OrdersCount       DashboardOrdersCount      `json:"ordersCount"`
	ProfitByHourGross []CentsByHour             `json:"profitByHourGross"`
	ProfitByHourNet   []CentsByHour             `json:"profitByHourNet"`
	HourlyCumulative  DashboardHourlyCumulative `json:"hourlyCumulative"`
	Commissions       DashboardCommissions      `json:"comissions"`
	Statistics        DashboardStatistics       `json:"statistics"`
	Ads               DashboardAdsSummary       `json:"ads"`
	Analytics         DashboardAnalytics        `json:"analytics"`
	TikTokSearchLimit *DashboardTikTokLimit     `json:"tikTokSearchLimit,omitempty"`
}

type DashboardOrdersCount struct {
	Total              int64                    `json:"total"`
	Approved           int64                    `json:"approved"`
	Pending            int64                    `json:"pending"`
	Refunded           int64                    `json:"refunded"`
	Chargedback        int64                    `json:"chargedback"`
	TotalCreditCard    int64                    `json:"totalCreditCard"`
	ApprovedCreditCard int64                    `json:"approvedCreditCard"`
	RefusedCreditCard  int64                    `json:"refusedCreditCard"`
	RefundedCreditCard int64                    `json:"refundedCreditCard"`
	ByUtmTerm          []CountByUTMTerm         `json:"byUtmTerm"`
	BySrc              []CountBySrc             `json:"bySrc"`
	ByHour             []CountByHour            `json:"byHour"`
	ByUtmSource        []CountByUTMSource       `json:"byUtmSource"`
	ByDayOfWeek        []CountByDayOfWeek       `json:"byDayOfWeek"`
	ByProductName      []CountByProductName     `json:"byProductName"`
	ByCustomerCountry  []CountByCustomerCountry `json:"byCustomerCountry"`
}

type CountByUTMTerm struct {
	Count   int64   `json:"count"`
	UTMTerm *string `json:"utmTerm"`
}

type CountBySrc struct {
	Count int64   `json:"count"`
	Src   *string `json:"src"`
}

type CountByHour struct {
	Count int64 `json:"count"`
	Hour  int64 `json:"hour"`
}

type CountByUTMSource struct {
	Count     int64   `json:"count"`
	UTMSource *string `json:"utmSource"`
}

type CountByDayOfWeek struct {
	Count     int64 `json:"count"`
	DayOfWeek int64 `json:"dayOfWeek"`
}

type CountByProductName struct {
	Count       int64   `json:"count"`
	Revenue     float64 `json:"revenue"`
	ProductName string  `json:"productName"`
}

type CountByCustomerCountry struct {
	Count   int64   `json:"count"`
	Country *string `json:"country"`
}

type CentsByHour struct {
	Hour  int64   `json:"hour"`
	Cents float64 `json:"cents"`
}

type DashboardHourlyCumulative struct {
	ProfitByHourGrossCumulative  []CentsByHour `json:"profitByHourGrossCumulative"`
	ProfitByHourNetCumulative    []CentsByHour `json:"profitByHourNetCumulative"`
	RevenueByHourGrossCumulative []CentsByHour `json:"revenueByHourGrossCumulative"`
	RevenueByHourNetCumulative   []CentsByHour `json:"revenueByHourNetCumulative"`
	InvestmentByHourCumulative   []CentsByHour `json:"investmentByHourCumulative"`
}

type DashboardCommissions struct {
	Net                    float64 `json:"net"`
	Gross                  float64 `json:"gross"`
	PendingGrossRevenue    float64 `json:"pendingGrossRevenue"`
	RefundedGrossRevenue   float64 `json:"refundedGrossRevenue"`
	ChargebackGrossRevenue float64 `json:"chargebackGrossRevenue"`
	SalesReturnedGross     float64 `json:"salesReturnedGross"`
}

type DashboardStatistics struct {
	RefundRate                 float64                    `json:"refundRate"`
	RevenueChargedbackRate     float64                    `json:"revenueChargedbackRate"`
	RevenuePercByPaymentMethod RevenuePercByPaymentMethod `json:"revenuePercByPaymentMethod"`
	Card                       PaymentMethodStatsExtended `json:"card"`
	Pix                        PaymentMethodStatsPending  `json:"pix"`
	Boleto                     PaymentMethodStatsPending  `json:"boleto"`
	Others                     PaymentMethodStatsPending  `json:"others"`
}

type RevenuePercByPaymentMethod struct {
	Pix        float64 `json:"pix"`
	CreditCard float64 `json:"creditCard"`
	Boleto     float64 `json:"boleto"`
}

type PaymentMethodStatsItem struct {
	OrdersCount  *int64   `json:"ordersCount"`
	Commission   *float64 `json:"comission"`
	QuantityRate *float64 `json:"qttRate"`
}

type PaymentMethodStats struct {
	CommissionPerc float64                `json:"comissionPerc"`
	Approved       PaymentMethodStatsItem `json:"approved"`
	Refunded       PaymentMethodStatsItem `json:"refunded"`
}

type PaymentMethodStatsExtended struct {
	PaymentMethodStats
	Refused     PaymentMethodStatsItem `json:"refused"`
	Chargedback PaymentMethodStatsItem `json:"chargedback"`
}

type PaymentMethodStatsPending struct {
	PaymentMethodStats
	Pending PaymentMethodStatsItem `json:"pending"`
}

type DashboardAdsSummary struct {
	Spent             float64         `json:"spent"`
	Clicks            int64           `json:"clicks"`
	PageViews         int64           `json:"pageViews"`
	InitiateCheckouts int64           `json:"initiateCheckouts"`
	Leads             int64           `json:"leads"`
	Meta              AdsPlatformInfo `json:"meta"`
	Google            AdsPlatformInfo `json:"google"`
	Kwai              AdsPlatformInfo `json:"kwai"`
	TikTok            AdsPlatformInfo `json:"tikTok"`
}

type AdsPlatformInfo struct {
	Spent             float64 `json:"spent"`
	Clicks            int64   `json:"clicks"`
	PageViews         int64   `json:"pageViews"`
	InitiateCheckouts int64   `json:"initiateCheckouts"`
	Leads             int64   `json:"leads"`
}

type DashboardAnalytics struct {
	ROAS                   *float64 `json:"roas"`
	Profit                 float64  `json:"profit"`
	Taxes                  float64  `json:"taxes"`
	Fees                   float64  `json:"fees"`
	MetaAdsTax             float64  `json:"metaAdsTax"`
	TotalTaxWithMetaAdsTax float64  `json:"totalTaxWithMetaAdsTax"`
	ProductsCost           float64  `json:"productsCost"`
	ProfitMargin           *float64 `json:"profitMargin"`
	ROI                    *float64 `json:"roi"`
	AvgTicket              *float64 `json:"avgTicket"`
	AvgCPA                 *float64 `json:"avgCpa"`
	ARPU                   *float64 `json:"arpu"`
	CustomSpent            float64  `json:"customSpent"`
	CPA                    *float64 `json:"cpa"`
	Conversations          *float64 `json:"conversations"`
	CostPerConversation    *float64 `json:"costPerConversation"`
	CostPerLead            *float64 `json:"costPerLead"`
}

type DashboardTikTokLimit struct {
	Limit   int  `json:"limit"`
	Reached bool `json:"reached"`
}

type AdObjectsResponse struct {
	Results        []AdObject        `json:"results"`
	SoldProducts   []json.RawMessage `json:"soldProducts"`
	UntrackedCount int64             `json:"untrackedCount"`
}

type ApprovedOrdersByProduct struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	ApprovedOrdersCount int64   `json:"approvedOrdersCount"`
	CPA                 float64 `json:"cpa"`
}

type AdObject struct {
	ID                        string                             `json:"id"`
	ProfileID                 string                             `json:"profileId"`
	AccountID                 string                             `json:"accountId"`
	CampaignID                string                             `json:"campaignId"`
	AdsetID                   string                             `json:"adsetId"`
	AdID                      string                             `json:"adId"`
	Name                      string                             `json:"name"`
	Level                     string                             `json:"level"`
	Status                    string                             `json:"status"`
	EffectiveStatus           string                             `json:"effectiveStatus"`
	AccountStatus             *string                            `json:"accountStatus"`
	CreatedTime               *time.Time                         `json:"createdTime"`
	DailyBudget               *float64                           `json:"dailyBudget"`
	LifetimeBudget            *float64                           `json:"lifetimeBudget"`
	Spend                     float64                            `json:"spend"`
	Revenue                   float64                            `json:"revenue"`
	GrossRevenue              float64                            `json:"grossRevenue"`
	Profit                    float64                            `json:"profit"`
	ProfitMargin              *float64                           `json:"profitMargin"`
	ROAS                      *float64                           `json:"roas"`
	ROI                       *float64                           `json:"roi"`
	CPA                       *float64                           `json:"cpa"`
	ApprovedOrdersCount       int64                              `json:"approvedOrdersCount"`
	PendingOrdersCount        int64                              `json:"pendingOrdersCount"`
	TotalOrdersCount          int64                              `json:"totalOrdersCount"`
	RefusedOrdersCount        int64                              `json:"refusedOrdersCount"`
	RefundedOrdersCount       int64                              `json:"refundedOrdersCount"`
	RefundedRevenue           float64                            `json:"refundedRevenue"`
	PendingRevenue            float64                            `json:"pendingRevenue"`
	ProductCosts              float64                            `json:"productCosts"`
	Tax                       float64                            `json:"tax"`
	Fees                      float64                            `json:"fees"`
	MetaAdsTax                float64                            `json:"metaAdsTax"`
	InlineLinkClicks          int64                              `json:"inlineLinkClicks"`
	InlineLinkClickCTR        float64                            `json:"inlineLinkClickCtr"`
	CostPerInlineLinkClick    *float64                           `json:"costPerInlineLinkClick"`
	CPM                       float64                            `json:"cpm"`
	Impressions               int64                              `json:"impressions"`
	LandingPageViews          int64                              `json:"landingPageViews"`
	CostPerLandingPageView    *float64                           `json:"costPerLandingPageView"`
	Leads                     int64                              `json:"leads"`
	CostPerLead               *float64                           `json:"costPerLead"`
	InitiateCheckout          int64                              `json:"initiateCheckout"`
	CostPerInitiateCheckout   *float64                           `json:"costPerInitiateCheckout"`
	Conversations             int64                              `json:"conversations"`
	CostPerConversation       *float64                           `json:"costPerConversation"`
	Frequency                 *float64                           `json:"frequency"`
	ARPU                      *float64                           `json:"arpu"`
	ICR                       *float64                           `json:"icr"`
	CON                       *float64                           `json:"con"`
	Conversion                *float64                           `json:"conversion"`
	VideoViews                int64                              `json:"videoViews"`
	Video75Watched            int64                              `json:"video75Watched"`
	VideoViews3Seconds        int64                              `json:"videoViews3Seconds"`
	Retention                 *float64                           `json:"retention"`
	Hook                      *float64                           `json:"hook"`
	HookPlayRate              *float64                           `json:"hookPlayRate"`
	HoldRate                  *float64                           `json:"holdRate"`
	BodyConversion            *float64                           `json:"bodyConversion"`
	BodyRetention             *float64                           `json:"bodyRetention"`
	ClickConversion           *float64                           `json:"clickConversion"`
	CheckoutConversion        *float64                           `json:"checkoutConversion"`
	CTA                       *float64                           `json:"cta"`
	CA                        *string                            `json:"ca"`
	Card                      *string                            `json:"card"`
	Cycle                     json.RawMessage                    `json:"cycle"`
	TotalSpent                *float64                           `json:"totalSpent"`
	BudgetUpdate              json.RawMessage                    `json:"budgetUpdate"`
	SalesFromFacebook         int64                              `json:"salesFromFacebook"`
	ApprovedOrdersByProductID map[string]ApprovedOrdersByProduct `json:"approvedOrdersByProductId"`
}

type toolCallRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      int64          `json:"id"`
	Method  string         `json:"method"`
	Params  toolCallParams `json:"params"`
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type toolResponse struct {
	Result *toolResult `json:"result"`
	Error  *toolError  `json:"error"`
}

type toolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewClient(cfg config.UTMifyConfig) (*Client, error) {
	if strings.TrimSpace(cfg.MCPURL) == "" {
		return nil, fmt.Errorf("UTMIFY_MCP_URL is required")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	client := &Client{
		baseURL: strings.TrimSpace(cfg.MCPURL),
		http: &http.Client{
			Timeout: timeout,
		},
	}
	client.requestID.Store(1)
	return client, nil
}

func (c *Client) GetDashboards(ctx context.Context) ([]Dashboard, error) {
	var dashboards []Dashboard
	if err := c.callTool(ctx, "get_dashboards", map[string]any{}, &dashboards); err != nil {
		return nil, err
	}
	return dashboards, nil
}

func (c *Client) GetDashboardSummary(ctx context.Context, dashboardID string, startDate string, endDate string) (*DashboardSummary, error) {
	dashboard, err := c.lookupDashboard(ctx, dashboardID)
	if err != nil {
		return nil, err
	}
	return c.GetDashboardSummaryForDashboard(ctx, dashboard, startDate, endDate)
}

func (c *Client) GetDashboardSummaryForDashboard(ctx context.Context, dashboard Dashboard, startDate string, endDate string) (*DashboardSummary, error) {
	dateRange, err := BuildDateRange(startDate, endDate, dashboard.TimeZone)
	if err != nil {
		return nil, err
	}

	var summary DashboardSummary
	if err := c.callTool(ctx, "get_dashboard_summary", map[string]any{
		"dashboardId": dashboard.ID,
		"dateRange":   dateRange,
	}, &summary); err != nil {
		return nil, err
	}

	return &summary, nil
}

func (c *Client) GetMetaAdObjects(ctx context.Context, dashboardID string, level string, startDate string, endDate string) ([]AdObject, error) {
	dashboard, err := c.lookupDashboard(ctx, dashboardID)
	if err != nil {
		return nil, err
	}
	return c.GetMetaAdObjectsForDashboard(ctx, dashboard, level, startDate, endDate)
}

func (c *Client) GetMetaAdObjectsForDashboard(ctx context.Context, dashboard Dashboard, level string, startDate string, endDate string) ([]AdObject, error) {
	dateRange, err := BuildDateRange(startDate, endDate, dashboard.TimeZone)
	if err != nil {
		return nil, err
	}

	var response AdObjectsResponse
	if err := c.callTool(ctx, "get_meta_ad_objects", map[string]any{
		"dashboardId": dashboard.ID,
		"level":       level,
		"dateRange":   dateRange,
	}, &response); err != nil {
		return nil, err
	}

	return response.Results, nil
}

func LocalDate(now time.Time, timeZone int) string {
	return now.In(fixedLocation(timeZone)).Format(dateLayout)
}

func BuildDateRange(startDate string, endDate string, timeZone int) (DateRange, error) {
	start, err := parseCalendarDate(startDate)
	if err != nil {
		return DateRange{}, fmt.Errorf("parse start date: %w", err)
	}

	end, err := parseCalendarDate(endDate)
	if err != nil {
		return DateRange{}, fmt.Errorf("parse end date: %w", err)
	}

	location := fixedLocation(timeZone)
	from := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, location)
	to := time.Date(end.Year(), end.Month(), end.Day(), 23, 59, 59, 0, location)
	return DateRange{
		From: from.Format(dateTimeLayout),
		To:   to.Format(dateTimeLayout),
	}, nil
}

func (c *Client) lookupDashboard(ctx context.Context, dashboardID string) (Dashboard, error) {
	dashboards, err := c.GetDashboards(ctx)
	if err != nil {
		return Dashboard{}, err
	}

	for _, dashboard := range dashboards {
		if dashboard.ID == dashboardID {
			return dashboard, nil
		}
	}

	return Dashboard{}, fmt.Errorf("dashboard %s not found", dashboardID)
}

var errRateLimited = fmt.Errorf("rate limited")

func (c *Client) callTool(ctx context.Context, name string, args map[string]any, dest any) error {
	for attempt := 0; attempt <= rateLimitMaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(rateLimitRetryWait):
			}
		}

		err := c.doToolCall(ctx, name, args, dest)
		if err == nil {
			return nil
		}
		if err == errRateLimited {
			continue
		}
		return err
	}
	return fmt.Errorf("%s: rate limit não liberou após %d tentativas", name, rateLimitMaxRetries)
}

func (c *Client) doToolCall(ctx context.Context, name string, args map[string]any, dest any) error {
	requestPayload := toolCallRequest{
		JSONRPC: "2.0",
		ID:      c.requestID.Add(1),
		Method:  "tools/call",
		Params: toolCallParams{
			Name:      name,
			Arguments: args,
		},
	}

	body, err := json.Marshal(requestPayload)
	if err != nil {
		return fmt.Errorf("marshal %s request: %w", name, err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create %s request: %w", name, err)
	}
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Content-Type", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("call %s: %w", name, err)
	}
	defer response.Body.Close()

	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read %s response: %w", name, err)
	}

	if response.StatusCode == http.StatusTooManyRequests {
		return errRateLimited
	}

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%s returned status %d: %s", name, response.StatusCode, strings.TrimSpace(string(payload)))
	}

	var rpcResponse toolResponse
	if err := json.Unmarshal(payload, &rpcResponse); err != nil {
		return fmt.Errorf("decode %s rpc response: %w", name, err)
	}

	if rpcResponse.Error != nil {
		return fmt.Errorf("%s rpc error: %s", name, strings.TrimSpace(rpcResponse.Error.Message))
	}
	if rpcResponse.Result == nil {
		return fmt.Errorf("%s returned an empty result", name)
	}

	text := joinContentText(rpcResponse.Result.Content)
	if rpcResponse.Result.IsError && strings.Contains(text, "Rate limit") {
		return errRateLimited
	}
	if rpcResponse.Result.IsError {
		return fmt.Errorf("%s tool error: %s", name, text)
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("%s returned empty content", name)
	}

	if err := json.Unmarshal([]byte(text), dest); err != nil {
		return fmt.Errorf("decode %s content: %w", name, err)
	}
	return nil
}

func joinContentText(items []toolContent) string {
	var builder strings.Builder
	for _, item := range items {
		if strings.TrimSpace(item.Text) == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(item.Text)
	}
	return strings.TrimSpace(builder.String())
}

func parseCalendarDate(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, fmt.Errorf("date is required")
	}

	if len(value) >= len(dateLayout) {
		if parsed, err := time.Parse(dateLayout, value[:len(dateLayout)]); err == nil {
			return parsed, nil
		}
	}

	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}

	return time.Time{}, fmt.Errorf("unsupported date %q", raw)
}

func fixedLocation(timeZone int) *time.Location {
	offset := timeZone * 60 * 60
	name := fmt.Sprintf("UTC%+d", timeZone)
	return time.FixedZone(name, offset)
}
