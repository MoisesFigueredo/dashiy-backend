package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/canal/metricas-financeiro-app/backend/internal/cache"
	syncservice "github.com/canal/metricas-financeiro-app/backend/internal/sync"
	"github.com/canal/metricas-financeiro-app/backend/internal/utmify"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	dashboardSummaryTTL = 3 * time.Minute
	dashboardsListTTL   = 12 * time.Hour
	adObjectsTTL        = 3 * time.Minute

	// Sync intervals.
	summaryInterval    = 2 * time.Minute
	dashboardsInterval = 6 * time.Hour
	adObjectsInterval  = 2 * time.Minute
	adSyncInterval     = 5 * time.Minute

	// Per-sync timeout to prevent stuck syncs.
	syncTimeout = 3 * time.Minute

	// Maximum concurrent MCP requests during sync.
	maxConcurrentFetches = 5
)

// Ad object levels to sync.
var adLevels = []string{"account", "campaign", "adset", "ad"}

// ──────────────────────────────────────────────
// Types — cached dashboard summary (aggregated)
// ──────────────────────────────────────────────

// CachedDashboardSummary is the pre-computed aggregated response stored in Redis.
type CachedDashboardSummary struct {
	Date            string            `json:"date"`
	PrimaryCurrency string            `json:"primary_currency"`
	Totals          *CurrencySummary  `json:"totals"`
	Currencies      []CurrencySummary `json:"currencies"`
	ProfitByHour    []HourPoint       `json:"profit_by_hour"`
	Dashboards      []DashboardItem   `json:"dashboards"`
	Extended        *ExtendedTotals   `json:"extended"`
	CachedAt        string            `json:"cached_at"`
}

type CurrencySummary struct {
	Currency string   `json:"currency"`
	Spent    float64  `json:"spent"`
	Revenue  float64  `json:"revenue"`
	Profit   float64  `json:"profit"`
	ROAS     *float64 `json:"roas,omitempty"`
}

type HourPoint struct {
	Hour   int64   `json:"hour"`
	Profit float64 `json:"profit"`
}

type DashboardItem struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Currency   string   `json:"currency"`
	TimeZone   int      `json:"time_zone"`
	Spent      float64  `json:"spent"`
	Revenue    float64  `json:"revenue"`
	NetRevenue float64  `json:"net_revenue"`
	Profit     float64  `json:"profit"`
	ROAS       *float64 `json:"roas,omitempty"`
	Status     string   `json:"status"`
	Error      *string  `json:"error,omitempty"`
}

// ──────────────────────────────────────────────
// Extended totals — all Utmify MCP metrics
// ──────────────────────────────────────────────

type ExtendedTotals struct {
	// Revenue (cents).
	GrossRevenue      float64 `json:"gross_revenue"`
	NetRevenue        float64 `json:"net_revenue"`
	PendingRevenue    float64 `json:"pending_revenue"`
	RefundedRevenue   float64 `json:"refunded_revenue"`
	ChargebackRevenue float64 `json:"chargeback_revenue"`

	// Costs & profit (cents).
	Spent        float64 `json:"spent"`
	Profit       float64 `json:"profit"`
	ProductsCost float64 `json:"products_cost"`
	Taxes        float64 `json:"taxes"`
	Fees         float64 `json:"fees"`
	MetaAdsTax   float64 `json:"meta_ads_tax"`
	TotalTax     float64 `json:"total_tax"`
	CustomSpent  float64 `json:"custom_spent"`

	// Ratios.
	ROAS           *float64 `json:"roas"`
	ROI            *float64 `json:"roi"`
	ProfitMargin   *float64 `json:"profit_margin"`
	RefundRate     float64  `json:"refund_rate"`
	ChargebackRate float64  `json:"chargeback_rate"`

	// Orders.
	OrdersTotal       int64 `json:"orders_total"`
	OrdersApproved    int64 `json:"orders_approved"`
	OrdersPending     int64 `json:"orders_pending"`
	OrdersRefunded    int64 `json:"orders_refunded"`
	OrdersChargedback int64 `json:"orders_chargedback"`

	// Engagement.
	Clicks            int64 `json:"clicks"`
	PageViews         int64 `json:"page_views"`
	InitiateCheckouts int64 `json:"initiate_checkouts"`
	Leads             int64 `json:"leads"`

	// Breakdowns.
	SalesByProduct []ProductSale     `json:"sales_by_product"`
	SalesByPayment *PaymentBreakdown `json:"sales_by_payment"`
}

type ProductSale struct {
	ProductName string  `json:"product_name"`
	Count       int64   `json:"count"`
	Revenue     float64 `json:"revenue"`
}

type PaymentBreakdown struct {
	Pix        float64 `json:"pix"`
	CreditCard float64 `json:"credit_card"`
	Boleto     float64 `json:"boleto"`
}

// ──────────────────────────────────────────────
// Per-dashboard cached MCP response
// ──────────────────────────────────────────────

// CachedDashboardItemSummary stores a single dashboard's MCP summary.
// The scheduler caches one of these per dashboard; the handler aggregates
// only the dashboards belonging to the requesting company.
type CachedDashboardItemSummary struct {
	DashboardID   string                   `json:"dashboard_id"`
	DashboardName string                   `json:"dashboard_name"`
	Currency      string                   `json:"currency"`
	TimeZone      int                      `json:"time_zone"`
	Date          string                   `json:"date"`
	Summary       *utmify.DashboardSummary `json:"summary,omitempty"`
	Error         string                   `json:"error,omitempty"`
	CachedAt      string                   `json:"cached_at"`
}

// ──────────────────────────────────────────────
// Ad object cache types
// ──────────────────────────────────────────────

// CachedAdObjects holds ad objects for a dashboard+level combo.
type CachedAdObjects struct {
	DashboardID   string            `json:"dashboard_id"`
	DashboardName string            `json:"dashboard_name"`
	Currency      string            `json:"currency"`
	Level         string            `json:"level"`
	Date          string            `json:"date"`
	Objects       []utmify.AdObject `json:"objects"`
	CachedAt      string            `json:"cached_at"`
}

// CachedAllAdObjects holds ad objects aggregated across all dashboards for a level.
type CachedAllAdObjects struct {
	Level      string            `json:"level"`
	Date       string            `json:"date"`
	Dashboards []CachedAdObjects `json:"dashboards"`
	CachedAt   string            `json:"cached_at"`
}

// ──────────────────────────────────────────────
// DashboardSummaryInput — input to the pure aggregation function
// ──────────────────────────────────────────────

// DashboardSummaryInput pairs dashboard metadata with its fetched MCP summary.
type DashboardSummaryInput struct {
	ID       string
	Name     string
	Currency string
	TimeZone int
	Summary  *utmify.DashboardSummary // nil when fetch failed
	Error    string
}

// ──────────────────────────────────────────────
// Scheduler
// ──────────────────────────────────────────────

// Scheduler runs periodic sync jobs that fetch from Utmify MCP and store in PostgreSQL.
// Redis is used as an optional performance cache on top of the DB.
type Scheduler struct {
	db          *pgxpool.Pool
	cache       *cache.Redis // optional
	client      *utmify.Client
	syncService *syncservice.Service // for ad metrics + commissions sync
	logger      *slog.Logger
	stop        chan struct{}
}

func NewScheduler(db *pgxpool.Pool, cache *cache.Redis, client *utmify.Client, syncService *syncservice.Service, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		db:          db,
		cache:       cache,
		client:      client,
		syncService: syncService,
		logger:      logger,
		stop:        make(chan struct{}),
	}
}

// Start launches the background sync loops. Call Stop() to shut down.
func (s *Scheduler) Start() {
	s.logger.Info("starting background sync scheduler",
		slog.Duration("summary_interval", summaryInterval),
		slog.Duration("dashboards_interval", dashboardsInterval),
		slog.Duration("ad_objects_interval", adObjectsInterval),
		slog.Duration("ad_sync_interval", adSyncInterval),
	)

	go s.loop("dashboard_summary", summaryInterval, s.syncDashboardSummary)
	go s.loop("dashboards_list", dashboardsInterval, s.syncDashboardsList)
	go s.loop("ad_objects", adObjectsInterval, s.syncAdObjects)
	go s.loop("ad_metrics_sync", adSyncInterval, s.syncAdMetrics)
}

func (s *Scheduler) Stop() {
	close(s.stop)
}

func (s *Scheduler) loop(name string, interval time.Duration, fn func(ctx context.Context)) {
	ctx := context.Background()

	s.logger.Info("running initial sync", slog.String("job", name))
	fn(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			s.logger.Info("stopping sync loop", slog.String("job", name))
			return
		case <-ticker.C:
			fn(ctx)
		}
	}
}

// ──────────────────────────────────────────────
// Dashboard summary sync — writes to PostgreSQL
// ──────────────────────────────────────────────

func (s *Scheduler) syncDashboardSummary(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()

	start := time.Now()
	now := time.Now().UTC()

	dashboards, err := s.client.GetDashboards(ctx)
	if err != nil {
		s.logger.Error("sync dashboard summary: fetch dashboards failed", slog.String("error", err.Error()))
		return
	}

	// Fetch individual dashboard summaries concurrently.
	items := s.fetchAndPersistSummaries(ctx, dashboards, now)

	// Also build and cache global aggregated summary in Redis (optional, for performance).
	date := now.Format("2006-01-02")
	if s.cache != nil {
		globalSummary := AggregateDashboardSummaries(items, date)
		globalSummary.CachedAt = time.Now().UTC().Format(time.RFC3339)

		cacheKey := cache.DashboardSummaryKey(date)
		if err := s.cache.Set(ctx, cacheKey, globalSummary, dashboardSummaryTTL); err != nil {
			s.logger.Error("sync dashboard summary: cache global set failed", slog.String("error", err.Error()))
		}
	}

	successCount := 0
	for _, item := range items {
		if item.Summary != nil {
			successCount++
		}
	}

	s.logger.Info("synced dashboard summaries to DB",
		slog.String("date", date),
		slog.Int("dashboards", len(dashboards)),
		slog.Int("success", successCount),
		slog.Duration("took", time.Since(start)),
	)
}

// fetchAndPersistSummaries fetches MCP summaries for all dashboards concurrently
// and persists each to PostgreSQL (+ optional Redis cache).
func (s *Scheduler) fetchAndPersistSummaries(ctx context.Context, dashboards []utmify.Dashboard, now time.Time) []DashboardSummaryInput {
	type indexedResult struct {
		index int
		input DashboardSummaryInput
	}

	items := make([]DashboardSummaryInput, len(dashboards))
	results := make(chan indexedResult, len(dashboards))
	sem := make(chan struct{}, maxConcurrentFetches)

	var wg sync.WaitGroup
	for i, dashboard := range dashboards {
		wg.Add(1)
		go func(i int, dashboard utmify.Dashboard) {
			defer wg.Done()

			// Acquire semaphore slot.
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results <- indexedResult{
					index: i,
					input: DashboardSummaryInput{
						ID: dashboard.ID, Name: dashboard.Name,
						Currency: dashboard.Currency, TimeZone: dashboard.TimeZone,
						Error: ctx.Err().Error(),
					},
				}
				return
			}

			// Use dashboard's local date (fixes timezone bug).
			date := utmify.LocalDate(now, dashboard.TimeZone)

			input := DashboardSummaryInput{
				ID:       dashboard.ID,
				Name:     dashboard.Name,
				Currency: dashboard.Currency,
				TimeZone: dashboard.TimeZone,
			}

			summary, err := fetchDashboardSummaryWithRetry(ctx, s.client, dashboard, date)
			if err != nil {
				input.Error = err.Error()
				if s.logger != nil {
					s.logger.Warn("sync dashboard summary: fetch failed",
						slog.String("dashboard", dashboard.Name),
						slog.String("dashboard_id", dashboard.ID),
						slog.String("date", date),
						slog.String("error", err.Error()),
					)
				}
			} else {
				input.Summary = summary

				// Persist to PostgreSQL.
				if err := s.upsertDashboardSummarySnapshot(ctx, dashboard, date, summary); err != nil {
					s.logger.Error("sync dashboard summary: DB upsert failed",
						slog.String("dashboard_id", dashboard.ID),
						slog.String("error", err.Error()),
					)
				}

				// Optional: cache in Redis for fast reads.
				if s.cache != nil {
					cached := CachedDashboardItemSummary{
						DashboardID:   dashboard.ID,
						DashboardName: dashboard.Name,
						Currency:      dashboard.Currency,
						TimeZone:      dashboard.TimeZone,
						Date:          date,
						Summary:       summary,
						CachedAt:      time.Now().UTC().Format(time.RFC3339),
					}
					key := cache.DashboardItemSummaryKey(dashboard.ID, date)
					if cacheErr := s.cache.Set(ctx, key, cached, dashboardSummaryTTL); cacheErr != nil {
						s.logger.Error("sync dashboard summary: cache individual set failed",
							slog.String("dashboard_id", dashboard.ID),
							slog.String("error", cacheErr.Error()),
						)
					}
				}
			}

			results <- indexedResult{index: i, input: input}
		}(i, dashboard)
	}

	// Wait for all goroutines then close results channel.
	go func() {
		wg.Wait()
		close(results)
	}()

	for r := range results {
		items[r.index] = r.input
	}

	return items
}

// upsertDashboardSummarySnapshot persists a single dashboard's MCP summary to PostgreSQL.
func (s *Scheduler) upsertDashboardSummarySnapshot(ctx context.Context, dashboard utmify.Dashboard, date string, summary *utmify.DashboardSummary) error {
	rawJSON, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("marshal dashboard summary: %w", err)
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO dashboard_summary_snapshots (
			dashboard_external_id, dashboard_name, currency, time_zone,
			snapshot_date, raw_summary, fetched_at
		) VALUES ($1, $2, $3, $4, $5, $6::jsonb, now())
		ON CONFLICT (dashboard_external_id, snapshot_date)
		DO UPDATE SET
			dashboard_name = EXCLUDED.dashboard_name,
			currency = EXCLUDED.currency,
			time_zone = EXCLUDED.time_zone,
			raw_summary = EXCLUDED.raw_summary,
			fetched_at = EXCLUDED.fetched_at
	`, dashboard.ID, strings.TrimSpace(dashboard.Name), dashboard.Currency, dashboard.TimeZone, date, string(rawJSON))

	return err
}

// ──────────────────────────────────────────────
// Dashboards list sync
// ──────────────────────────────────────────────

func (s *Scheduler) syncDashboardsList(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()

	start := time.Now()

	dashboards, err := s.client.GetDashboards(ctx)
	if err != nil {
		s.logger.Error("sync dashboards list: fetch failed", slog.String("error", err.Error()))
		return
	}

	// Cache in Redis for backward compatibility.
	if s.cache != nil {
		if err := s.cache.Set(ctx, cache.DashboardsListKey(), dashboards, dashboardsListTTL); err != nil {
			s.logger.Error("sync dashboards list: cache set failed", slog.String("error", err.Error()))
		}
	}

	s.logger.Info("synced dashboards list",
		slog.Int("count", len(dashboards)),
		slog.Duration("took", time.Since(start)),
	)
}

// ──────────────────────────────────────────────
// Ad objects sync — writes to PostgreSQL
// ──────────────────────────────────────────────

func (s *Scheduler) syncAdObjects(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()

	start := time.Now()
	now := time.Now().UTC()

	dashboards, err := s.client.GetDashboards(ctx)
	if err != nil {
		s.logger.Error("sync ad objects: fetch dashboards failed", slog.String("error", err.Error()))
		return
	}

	totalObjects := 0

	for _, level := range adLevels {
		allDashboards := make([]CachedAdObjects, 0, len(dashboards))

		// Fetch ad objects concurrently per level.
		type adResult struct {
			cached  *CachedAdObjects
			objects int
		}
		resultsCh := make(chan adResult, len(dashboards))
		sem := make(chan struct{}, maxConcurrentFetches)

		var wg sync.WaitGroup
		for _, dashboard := range dashboards {
			if len(dashboard.MetaProfiles) == 0 {
				continue
			}

			wg.Add(1)
			go func(dashboard utmify.Dashboard) {
				defer wg.Done()

				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					return
				}

				date := utmify.LocalDate(now, dashboard.TimeZone)

				objects, err := s.client.GetMetaAdObjectsForDashboard(ctx, dashboard, level, date, date)
				if err != nil {
					s.logger.Warn("sync ad objects: fetch failed",
						slog.String("dashboard", dashboard.Name),
						slog.String("level", level),
						slog.String("error", err.Error()),
					)
					return
				}

				// Persist to PostgreSQL.
				if err := s.upsertAdObjectSnapshot(ctx, dashboard, level, date, objects); err != nil {
					s.logger.Error("sync ad objects: DB upsert failed",
						slog.String("dashboard_id", dashboard.ID),
						slog.String("level", level),
						slog.String("error", err.Error()),
					)
				}

				cached := CachedAdObjects{
					DashboardID:   dashboard.ID,
					DashboardName: dashboard.Name,
					Currency:      dashboard.Currency,
					Level:         level,
					Date:          date,
					Objects:       objects,
					CachedAt:      time.Now().UTC().Format(time.RFC3339),
				}

				// Optional: per-dashboard per-level Redis cache.
				if s.cache != nil {
					key := cache.AdObjectsKey(dashboard.ID, level, date)
					if err := s.cache.Set(ctx, key, cached, adObjectsTTL); err != nil {
						s.logger.Error("sync ad objects: cache set failed",
							slog.String("key", key),
							slog.String("error", err.Error()),
						)
					}
				}

				resultsCh <- adResult{cached: &cached, objects: len(objects)}
			}(dashboard)
		}

		go func() {
			wg.Wait()
			close(resultsCh)
		}()

		for r := range resultsCh {
			if r.cached != nil {
				allDashboards = append(allDashboards, *r.cached)
				totalObjects += r.objects
			}
		}

		// Optional: aggregated Redis cache for all dashboards at this level.
		if s.cache != nil {
			date := now.Format("2006-01-02")
			allKey := cache.AllAdObjectsKey(level, date)
			allCached := CachedAllAdObjects{
				Level:      level,
				Date:       date,
				Dashboards: allDashboards,
				CachedAt:   time.Now().UTC().Format(time.RFC3339),
			}
			if err := s.cache.Set(ctx, allKey, allCached, adObjectsTTL); err != nil {
				s.logger.Error("sync ad objects: cache set all failed",
					slog.String("key", allKey),
					slog.String("error", err.Error()),
				)
			}
		}
	}

	s.logger.Info("synced ad objects to DB",
		slog.Int("total_objects", totalObjects),
		slog.Int("levels", len(adLevels)),
		slog.Int("dashboards", len(dashboards)),
		slog.Duration("took", time.Since(start)),
	)
}

// deduplicateAdObjects removes duplicate ad objects by ID, keeping the last occurrence.
func deduplicateAdObjects(objects []utmify.AdObject) []utmify.AdObject {
	seen := make(map[string]int, len(objects))
	deduped := make([]utmify.AdObject, 0, len(objects))
	for _, obj := range objects {
		key := strings.TrimSpace(obj.ID)
		if key == "" {
			deduped = append(deduped, obj)
			continue
		}
		if idx, exists := seen[key]; exists {
			deduped[idx] = obj // overwrite with latest
		} else {
			seen[key] = len(deduped)
			deduped = append(deduped, obj)
		}
	}
	return deduped
}

// upsertAdObjectSnapshot persists a dashboard's ad objects for a level+date to PostgreSQL.
func (s *Scheduler) upsertAdObjectSnapshot(ctx context.Context, dashboard utmify.Dashboard, level, date string, objects []utmify.AdObject) error {
	objects = deduplicateAdObjects(objects)
	rawJSON, err := json.Marshal(objects)
	if err != nil {
		return fmt.Errorf("marshal ad objects: %w", err)
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO ad_object_snapshots (
			dashboard_external_id, dashboard_name, currency, level,
			snapshot_date, raw_objects, object_count, fetched_at
		) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, now())
		ON CONFLICT (dashboard_external_id, level, snapshot_date)
		DO UPDATE SET
			dashboard_name = EXCLUDED.dashboard_name,
			currency = EXCLUDED.currency,
			raw_objects = EXCLUDED.raw_objects,
			object_count = EXCLUDED.object_count,
			fetched_at = EXCLUDED.fetched_at
	`, dashboard.ID, strings.TrimSpace(dashboard.Name), dashboard.Currency, level, date, string(rawJSON), len(objects))

	return err
}

// ──────────────────────────────────────────────
// Ad metrics sync — runs SyncService for each company
// ──────────────────────────────────────────────

func (s *Scheduler) syncAdMetrics(ctx context.Context) {
	if s.syncService == nil {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()

	start := time.Now()

	// Find all companies that have registered dashboards.
	companyIDs, err := s.getCompaniesWithDashboards(ctx)
	if err != nil {
		s.logger.Error("sync ad metrics: failed to list companies", slog.String("error", err.Error()))
		return
	}

	if len(companyIDs) == 0 {
		s.logger.Info("sync ad metrics: no companies with dashboards, skipping")
		return
	}

	var totalAds, totalSnapshots, totalCommissions int
	var syncErrors int

	for _, companyID := range companyIDs {
		summary, err := s.syncService.SyncToday(ctx, companyID)
		if err != nil {
			s.logger.Error("sync ad metrics: company sync failed",
				slog.String("company_id", companyID.String()),
				slog.String("error", err.Error()),
			)
			syncErrors++
			continue
		}

		totalAds += summary.AdsSynced
		totalSnapshots += summary.SnapshotsSynced
		totalCommissions += summary.CommissionsSynced
		syncErrors += len(summary.Errors)
	}

	s.logger.Info("synced ad metrics to DB",
		slog.Int("companies", len(companyIDs)),
		slog.Int("ads", totalAds),
		slog.Int("snapshots", totalSnapshots),
		slog.Int("commissions", totalCommissions),
		slog.Int("errors", syncErrors),
		slog.Duration("took", time.Since(start)),
	)
}

// getCompaniesWithDashboards returns all company UUIDs that have at least one registered dashboard.
func (s *Scheduler) getCompaniesWithDashboards(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT company_id
		FROM utmify_dashboards
		WHERE active = true
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ──────────────────────────────────────────────
// AggregateDashboardSummaries — pure aggregation function
// ──────────────────────────────────────────────

// AggregateDashboardSummaries builds a CachedDashboardSummary from pre-fetched
// individual dashboard summaries. No MCP calls are made — this is a pure
// computation over already-available data.
func AggregateDashboardSummaries(inputs []DashboardSummaryInput, date string) CachedDashboardSummary {
	items := make([]DashboardItem, 0, len(inputs))
	currencyTotals := map[string]*CurrencySummary{}
	hourlyByCurrency := map[string]map[int64]float64{}

	// Extended totals accumulator (all values in cents from MCP).
	ext := &ExtendedTotals{}
	productMap := map[string]*ProductSale{}
	var paymentPixAbs, paymentCardAbs, paymentBoletoAbs float64 // absolute commission values in cents

	for _, input := range inputs {
		item := DashboardItem{
			ID:       input.ID,
			Name:     input.Name,
			Currency: input.Currency,
			TimeZone: input.TimeZone,
			Status:   "ok",
		}

		summary := input.Summary
		if summary == nil {
			message := input.Error
			if message == "" {
				message = "unknown error"
			}
			item.Status = "error"
			item.Error = &message
			items = append(items, item)
			continue
		}

		// Per-dashboard totals in cents (frontend divides via formatCurrencyBRL).
		item.Spent = summary.Ads.Spent
		item.Revenue = summary.Commissions.Gross
		item.NetRevenue = summary.Commissions.Net
		item.Profit = summary.Analytics.Profit
		if summary.Analytics.ROAS != nil {
			item.ROAS = summary.Analytics.ROAS
		} else {
			item.ROAS = computeRatio(item.Revenue, item.Spent)
		}
		items = append(items, item)

		// Currency totals (backward compat).
		total, exists := currencyTotals[item.Currency]
		if !exists {
			total = &CurrencySummary{Currency: item.Currency}
			currencyTotals[item.Currency] = total
		}
		total.Spent += item.Spent
		total.Revenue += item.Revenue
		total.Profit += item.Profit

		// Hourly profit.
		hourMap, exists := hourlyByCurrency[item.Currency]
		if !exists {
			hourMap = map[int64]float64{}
			hourlyByCurrency[item.Currency] = hourMap
		}
		for _, hour := range summary.ProfitByHourNet {
			hourMap[hour.Hour] += hour.Cents / 100
		}

		// ── Accumulate extended totals (raw cents) ──

		// Revenue.
		ext.GrossRevenue += summary.Commissions.Gross
		ext.NetRevenue += summary.Commissions.Net
		ext.PendingRevenue += summary.Commissions.PendingGrossRevenue
		ext.RefundedRevenue += summary.Commissions.RefundedGrossRevenue
		ext.ChargebackRevenue += summary.Commissions.ChargebackGrossRevenue

		// Costs & profit.
		ext.Spent += summary.Ads.Spent
		ext.Profit += summary.Analytics.Profit
		ext.ProductsCost += summary.Analytics.ProductsCost
		ext.Taxes += summary.Analytics.Taxes
		ext.Fees += summary.Analytics.Fees
		ext.MetaAdsTax += summary.Analytics.MetaAdsTax
		ext.TotalTax += summary.Analytics.TotalTaxWithMetaAdsTax
		ext.CustomSpent += summary.Analytics.CustomSpent

		// Orders.
		ext.OrdersTotal += summary.OrdersCount.Total
		ext.OrdersApproved += summary.OrdersCount.Approved
		ext.OrdersPending += summary.OrdersCount.Pending
		ext.OrdersRefunded += summary.OrdersCount.Refunded
		ext.OrdersChargedback += summary.OrdersCount.Chargedback

		// Engagement.
		ext.Clicks += summary.Ads.Clicks
		ext.PageViews += summary.Ads.PageViews
		ext.InitiateCheckouts += summary.Ads.InitiateCheckouts
		ext.Leads += summary.Ads.Leads

		// Sales by product.
		for _, p := range summary.OrdersCount.ByProductName {
			existing, ok := productMap[p.ProductName]
			if ok {
				existing.Count += p.Count
				existing.Revenue += p.Revenue
			} else {
				productMap[p.ProductName] = &ProductSale{
					ProductName: p.ProductName,
					Count:       p.Count,
					Revenue:     p.Revenue,
				}
			}
		}

		// Payment method breakdown: use absolute approved commission values
		// from each payment method instead of the broken percentage fields.
		// These values are in cents and always correct.
		if c := summary.Statistics.Pix.Approved.Commission; c != nil {
			paymentPixAbs += *c
		}
		if c := summary.Statistics.Card.Approved.Commission; c != nil {
			paymentCardAbs += *c
		}
		if c := summary.Statistics.Boleto.Approved.Commission; c != nil {
			paymentBoletoAbs += *c
		}
	}

	// Finalize product sales.
	salesByProduct := make([]ProductSale, 0, len(productMap))
	for _, p := range productMap {
		salesByProduct = append(salesByProduct, *p)
	}
	sort.Slice(salesByProduct, func(i, j int) bool {
		return salesByProduct[i].Count > salesByProduct[j].Count
	})
	ext.SalesByProduct = salesByProduct

	// Finalize payment breakdown from absolute commission values.
	// Derive percentages by dividing each method's total by the grand total.
	paymentTotal := paymentPixAbs + paymentCardAbs + paymentBoletoAbs
	if paymentTotal > 0 {
		ext.SalesByPayment = &PaymentBreakdown{
			Pix:        paymentPixAbs / paymentTotal,
			CreditCard: paymentCardAbs / paymentTotal,
			Boleto:     paymentBoletoAbs / paymentTotal,
		}
	}

	// Compute ratios from aggregated values.
	// ROAS = grossRevenue / spent (standard).
	ext.ROAS = computeRatio(ext.GrossRevenue, ext.Spent)
	// ROI = netRevenue / spent (matches Utmify's definition).
	ext.ROI = computeRatio(ext.NetRevenue, ext.Spent)
	// ProfitMargin = profit / netRevenue (matches Utmify's definition).
	ext.ProfitMargin = computeRatio(ext.Profit, ext.NetRevenue)
	if ext.OrdersTotal > 0 {
		ext.RefundRate = float64(ext.OrdersRefunded) / float64(ext.OrdersTotal)
		ext.ChargebackRate = float64(ext.OrdersChargedback) / float64(ext.OrdersTotal)
	}

	// Build currency list.
	currencies := make([]CurrencySummary, 0, len(currencyTotals))
	for _, total := range currencyTotals {
		total.ROAS = computeRatio(total.Revenue, total.Spent)
		currencies = append(currencies, *total)
	}

	sort.Slice(currencies, func(i, j int) bool {
		if strings.EqualFold(currencies[i].Currency, "BRL") {
			return true
		}
		if strings.EqualFold(currencies[j].Currency, "BRL") {
			return false
		}
		return currencies[i].Spent > currencies[j].Spent
	})

	primaryCurrency := ""
	if len(currencies) > 0 {
		primaryCurrency = currencies[0].Currency
	}

	profitByHour := make([]HourPoint, 0)
	if hourMap, exists := hourlyByCurrency[primaryCurrency]; exists {
		for hour, profit := range hourMap {
			profitByHour = append(profitByHour, HourPoint{Hour: hour, Profit: profit})
		}
		sort.Slice(profitByHour, func(i, j int) bool {
			return profitByHour[i].Hour < profitByHour[j].Hour
		})
	}

	var totals *CurrencySummary
	if len(currencies) > 0 {
		totals = &currencies[0]
	}

	return CachedDashboardSummary{
		Date:            date,
		PrimaryCurrency: primaryCurrency,
		Totals:          totals,
		Currencies:      currencies,
		ProfitByHour:    profitByHour,
		Dashboards:      items,
		Extended:        ext,
	}
}

// BuildDashboardSummary is the legacy function that fetches from MCP and aggregates.
// Used as a fallback when DB data is unavailable.
func BuildDashboardSummary(ctx context.Context, client *utmify.Client, logger *slog.Logger, dashboards []utmify.Dashboard, date string) CachedDashboardSummary {
	inputs := make([]DashboardSummaryInput, 0, len(dashboards))

	for _, dashboard := range dashboards {
		input := DashboardSummaryInput{
			ID:       dashboard.ID,
			Name:     dashboard.Name,
			Currency: dashboard.Currency,
			TimeZone: dashboard.TimeZone,
		}

		summary, err := fetchDashboardSummaryWithRetry(ctx, client, dashboard, date)
		if err != nil {
			if logger != nil {
				logger.Warn("build summary: dashboard failed",
					slog.String("dashboard", dashboard.Name),
					slog.String("dashboard_id", dashboard.ID),
					slog.String("error", err.Error()),
				)
			}
			input.Error = err.Error()
		} else {
			input.Summary = summary
		}

		inputs = append(inputs, input)
	}

	return AggregateDashboardSummaries(inputs, date)
}

// fetchDashboardSummaryWithRetry tries to fetch a dashboard summary, retrying once on failure.
func fetchDashboardSummaryWithRetry(ctx context.Context, client *utmify.Client, dashboard utmify.Dashboard, date string) (*utmify.DashboardSummary, error) {
	summary, err := client.GetDashboardSummaryForDashboard(ctx, dashboard, date, date)
	if err != nil {
		// Retry once after a short pause.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}
		summary, err = client.GetDashboardSummaryForDashboard(ctx, dashboard, date, date)
	}
	return summary, err
}

func computeRatio(numerator float64, denominator float64) *float64 {
	if denominator == 0 {
		return nil
	}
	value := numerator / denominator
	return &value
}

