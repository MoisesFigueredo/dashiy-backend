package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	adparser "github.com/canal/metricas-financeiro-app/backend/internal/ads"
	"github.com/canal/metricas-financeiro-app/backend/internal/commissions"
	"github.com/canal/metricas-financeiro-app/backend/internal/utmify"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

const snapshotDateLayout = "2006-01-02"

type Service struct {
	db                *pgxpool.Pool
	utmify            *utmify.Client
	logger            *slog.Logger
	commissionService *commissions.Service
}

type SyncSummary struct {
	SyncedAt          time.Time            `json:"synced_at"`
	DashboardsScanned int                  `json:"dashboards_scanned"`
	DashboardsSynced  int                  `json:"dashboards_synced"`
	AdsSynced         int                  `json:"ads_synced"`
	SnapshotsSynced   int                  `json:"snapshots_synced"`
	CommissionsSynced int                  `json:"commissions_synced"`
	Errors            []DashboardSyncError `json:"errors"`
}

type DashboardSyncError struct {
	DashboardID   string `json:"dashboard_id"`
	DashboardName string `json:"dashboard_name"`
	Date          string `json:"date"`
	Message       string `json:"message"`
}

type dashboardCounts struct {
	Ads         int
	Snapshots   int
	Commissions int
}

type collaboratorRecord struct {
	ID                uuid.UUID
	Name              string
	Code              string
	Role              string
	CommissionRateMin decimal.Decimal
	CommissionRateMax decimal.Decimal
}

func NewService(db *pgxpool.Pool, utmifyClient *utmify.Client, logger *slog.Logger, commissionService *commissions.Service) *Service {
	return &Service{
		db:                db,
		utmify:            utmifyClient,
		logger:            logger,
		commissionService: commissionService,
	}
}

func (s *Service) SyncToday(ctx context.Context, companyID uuid.UUID) (*SyncSummary, error) {
	dashboards, err := s.utmify.GetDashboards(ctx)
	if err != nil {
		return nil, fmt.Errorf("load dashboards from utmify: %w", err)
	}

	summary := &SyncSummary{
		SyncedAt:          time.Now().UTC(),
		DashboardsScanned: len(dashboards),
		Errors:            make([]DashboardSyncError, 0),
	}

	now := time.Now().UTC()
	for _, dashboard := range dashboards {
		syncDate := utmify.LocalDate(now, dashboard.TimeZone)
		adObjects, err := s.utmify.GetMetaAdObjectsForDashboard(ctx, dashboard, "ad", syncDate, syncDate)
		if err != nil {
			summary.Errors = append(summary.Errors, DashboardSyncError{
				DashboardID:   dashboard.ID,
				DashboardName: dashboard.Name,
				Date:          syncDate,
				Message:       err.Error(),
			})
			continue
		}

		counts, err := s.syncDashboard(ctx, companyID, dashboard, syncDate, adObjects)
		if err != nil {
			summary.Errors = append(summary.Errors, DashboardSyncError{
				DashboardID:   dashboard.ID,
				DashboardName: dashboard.Name,
				Date:          syncDate,
				Message:       err.Error(),
			})
			continue
		}

		summary.DashboardsSynced++
		summary.AdsSynced += counts.Ads
		summary.SnapshotsSynced += counts.Snapshots
		summary.CommissionsSynced += counts.Commissions
	}

	return summary, nil
}

func (s *Service) syncDashboard(ctx context.Context, companyID uuid.UUID, dashboard utmify.Dashboard, snapshotDate string, adObjects []utmify.AdObject) (dashboardCounts, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return dashboardCounts{}, fmt.Errorf("begin dashboard sync transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	nicheID, err := s.upsertDashboardScope(ctx, tx, companyID, dashboard)
	if err != nil {
		return dashboardCounts{}, err
	}

	parsedSnapshotDate, err := time.Parse(snapshotDateLayout, snapshotDate)
	if err != nil {
		return dashboardCounts{}, fmt.Errorf("parse snapshot date: %w", err)
	}

	counts := dashboardCounts{}
	for _, adObject := range adObjects {
		if strings.TrimSpace(adObject.ID) == "" {
			continue
		}

		commissionsSynced, err := s.syncAdObject(ctx, tx, companyID, nicheID, dashboard, parsedSnapshotDate, adObject)
		if err != nil {
			return dashboardCounts{}, fmt.Errorf("sync ad %s (%s): %w", adObject.Name, adObject.ID, err)
		}

		counts.Ads++
		counts.Snapshots++
		counts.Commissions += commissionsSynced
	}

	if _, err := tx.Exec(ctx, `
		UPDATE utmify_dashboards
		SET last_synced_at = now(), updated_at = now()
		WHERE company_id = $1
		  AND external_id = $2
	`, companyID, dashboard.ID); err != nil {
		return dashboardCounts{}, fmt.Errorf("touch utmify dashboard sync timestamp: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return dashboardCounts{}, fmt.Errorf("commit dashboard sync: %w", err)
	}

	return counts, nil
}

func (s *Service) syncAdObject(
	ctx context.Context,
	tx pgx.Tx,
	companyID uuid.UUID,
	nicheID uuid.UUID,
	dashboard utmify.Dashboard,
	snapshotDate time.Time,
	adObject utmify.AdObject,
) (int, error) {
	parsedName, parseErr := adparser.ParseName(adObject.Name)
	if parseErr != nil {
		s.logger.Warn("unable to parse ad nomenclature",
			slog.String("dashboard_id", dashboard.ID),
			slog.String("ad_id", adObject.ID),
			slog.String("name", adObject.Name),
			slog.String("error", parseErr.Error()),
		)
	}
	adExternalID := firstNonEmpty(adObject.AdID, adObject.ID)

	var offerCode string
	if parsedName != nil {
		offerCode = parsedName.OfferCode
		if _, err := s.upsertOffer(ctx, tx, companyID, nicheID, parsedName.OfferCode); err != nil {
			return 0, err
		}
	}

	accountID, err := s.upsertAdAccount(ctx, tx, companyID, nicheID, dashboard, adObject)
	if err != nil {
		return 0, err
	}

	campaignID, err := s.upsertCampaign(ctx, tx, companyID, nicheID, accountID, adObject)
	if err != nil {
		return 0, err
	}

	adsetID, err := s.upsertAdset(ctx, tx, companyID, nicheID, campaignID, adObject)
	if err != nil {
		return 0, err
	}

	adID, err := s.upsertAd(ctx, tx, companyID, nicheID, adsetID, adObject, parsedName, adExternalID)
	if err != nil {
		return 0, err
	}

	rawPayload, err := json.Marshal(adObject)
	if err != nil {
		return 0, fmt.Errorf("marshal raw ad payload: %w", err)
	}

	chargebackAmount := extractChargebackAmount(rawPayload)
	if err := s.upsertSnapshot(ctx, tx, companyID, nicheID, adID, snapshotDate, adObject, chargebackAmount, rawPayload); err != nil {
		return 0, err
	}

	if parsedName == nil {
		return 0, nil
	}

	collaborators, err := s.findCollaboratorsByCodes(ctx, tx, companyID, []string{parsedName.CopyCode, parsedName.EditorCode})
	if err != nil {
		return 0, err
	}

	syncedCommissions := 0
	for _, collaborator := range collaborators {
		role := collaboratorRoleForCode(parsedName, collaborator.Code)
		if role == "" {
			continue
		}

		if err := s.upsertAdCollaborator(ctx, tx, companyID, nicheID, adID, collaborator, role); err != nil {
			return 0, err
		}

		if err := s.upsertCommissionEntry(ctx, tx, companyID, nicheID, adID, snapshotDate, dashboard.Currency, offerCode, collaborator, role, adObject, chargebackAmount); err != nil {
			return 0, err
		}
		syncedCommissions++
	}

	return syncedCommissions, nil
}

func (s *Service) upsertDashboardScope(ctx context.Context, tx pgx.Tx, companyID uuid.UUID, dashboard utmify.Dashboard) (uuid.UUID, error) {
	slug := fmt.Sprintf("utmify-%s", strings.ToLower(strings.TrimSpace(dashboard.ID)))

	var nicheID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO niches (company_id, name, slug, active)
		VALUES ($1, $2, $3, TRUE)
		ON CONFLICT (company_id, slug)
		DO UPDATE SET
			name = EXCLUDED.name,
			active = TRUE,
			updated_at = now()
		RETURNING id
	`, companyID, strings.TrimSpace(dashboard.Name), slug).Scan(&nicheID); err != nil {
		return uuid.Nil, fmt.Errorf("upsert niche for dashboard %s: %w", dashboard.ID, err)
	}

	platformsJSON, err := json.Marshal(dashboard.Platforms)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal dashboard platforms: %w", err)
	}
	productsJSON, err := json.Marshal(dashboard.Products)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal dashboard products: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO utmify_dashboards (
			company_id,
			niche_id,
			external_id,
			name,
			time_zone,
			currency,
			view_type,
			active,
			platforms,
			products,
			last_synced_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, TRUE, $8::jsonb, $9::jsonb, now())
		ON CONFLICT (company_id, external_id)
		DO UPDATE SET
			niche_id = EXCLUDED.niche_id,
			name = EXCLUDED.name,
			time_zone = EXCLUDED.time_zone,
			currency = EXCLUDED.currency,
			view_type = EXCLUDED.view_type,
			active = EXCLUDED.active,
			platforms = EXCLUDED.platforms,
			products = EXCLUDED.products,
			last_synced_at = EXCLUDED.last_synced_at,
			updated_at = now()
	`, companyID, nicheID, dashboard.ID, strings.TrimSpace(dashboard.Name), dashboard.TimeZone, dashboard.Currency, dashboard.ViewType, string(platformsJSON), string(productsJSON)); err != nil {
		return uuid.Nil, fmt.Errorf("upsert utmify dashboard %s: %w", dashboard.ID, err)
	}

	return nicheID, nil
}

func (s *Service) upsertOffer(ctx context.Context, tx pgx.Tx, companyID uuid.UUID, nicheID uuid.UUID, code string) (uuid.UUID, error) {
	var offerID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO offers (company_id, niche_id, code, name, active)
		VALUES ($1, $2, $3, $3, TRUE)
		ON CONFLICT (company_id, niche_id, code)
		DO UPDATE SET
			active = TRUE,
			updated_at = now()
		RETURNING id
	`, companyID, nicheID, strings.TrimSpace(code)).Scan(&offerID); err != nil {
		return uuid.Nil, fmt.Errorf("upsert offer %s: %w", code, err)
	}
	return offerID, nil
}

func (s *Service) upsertAdAccount(ctx context.Context, tx pgx.Tx, companyID uuid.UUID, nicheID uuid.UUID, dashboard utmify.Dashboard, adObject utmify.AdObject) (uuid.UUID, error) {
	var accountID uuid.UUID
	accountName := firstNonEmpty(stringPointerValue(adObject.CA), adObject.AccountID)
	if err := tx.QueryRow(ctx, `
		INSERT INTO ad_accounts (company_id, niche_id, external_id, name, currency, timezone, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (company_id, external_id)
		DO UPDATE SET
			niche_id = EXCLUDED.niche_id,
			name = EXCLUDED.name,
			currency = EXCLUDED.currency,
			timezone = EXCLUDED.timezone,
			status = EXCLUDED.status,
			updated_at = now()
		RETURNING id
	`, companyID, nicheID, strings.TrimSpace(adObject.AccountID), accountName, dashboard.Currency, fmt.Sprintf("UTC%+d", dashboard.TimeZone), normalizeObjectStatus(adObject.Status, adObject.EffectiveStatus)).Scan(&accountID); err != nil {
		return uuid.Nil, fmt.Errorf("upsert ad account %s: %w", adObject.AccountID, err)
	}
	return accountID, nil
}

func (s *Service) upsertCampaign(ctx context.Context, tx pgx.Tx, companyID uuid.UUID, nicheID uuid.UUID, accountID uuid.UUID, adObject utmify.AdObject) (uuid.UUID, error) {
	var campaignID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO campaigns (company_id, niche_id, ad_account_id, external_id, name, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (company_id, external_id)
		DO UPDATE SET
			niche_id = EXCLUDED.niche_id,
			ad_account_id = EXCLUDED.ad_account_id,
			name = EXCLUDED.name,
			status = EXCLUDED.status,
			updated_at = now()
		RETURNING id
	`, companyID, nicheID, accountID, strings.TrimSpace(adObject.CampaignID), firstNonEmpty(adObject.CampaignID, "campaign"), normalizeObjectStatus(adObject.Status, adObject.EffectiveStatus)).Scan(&campaignID); err != nil {
		return uuid.Nil, fmt.Errorf("upsert campaign %s: %w", adObject.CampaignID, err)
	}
	return campaignID, nil
}

func (s *Service) upsertAdset(ctx context.Context, tx pgx.Tx, companyID uuid.UUID, nicheID uuid.UUID, campaignID uuid.UUID, adObject utmify.AdObject) (uuid.UUID, error) {
	var adsetID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO adsets (company_id, niche_id, campaign_id, external_id, name, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (company_id, external_id)
		DO UPDATE SET
			niche_id = EXCLUDED.niche_id,
			campaign_id = EXCLUDED.campaign_id,
			name = EXCLUDED.name,
			status = EXCLUDED.status,
			updated_at = now()
		RETURNING id
	`, companyID, nicheID, campaignID, strings.TrimSpace(adObject.AdsetID), firstNonEmpty(adObject.AdsetID, "adset"), normalizeObjectStatus(adObject.Status, adObject.EffectiveStatus)).Scan(&adsetID); err != nil {
		return uuid.Nil, fmt.Errorf("upsert adset %s: %w", adObject.AdsetID, err)
	}
	return adsetID, nil
}

func (s *Service) upsertAd(ctx context.Context, tx pgx.Tx, companyID uuid.UUID, nicheID uuid.UUID, adsetID uuid.UUID, adObject utmify.AdObject, parsedName *adparser.ParsedName, adExternalID string) (uuid.UUID, error) {
	nameParsed := map[string]any{
		"raw_name": adObject.Name,
	}
	if parsedName != nil {
		nameParsed["sequence"] = parsedName.Sequence
		nameParsed["offer_code"] = parsedName.OfferCode
		nameParsed["landing_code"] = parsedName.LandingCode
		nameParsed["creative_variant"] = parsedName.CreativeVariant
		nameParsed["hook_code"] = parsedName.HookCode
		nameParsed["copy_code"] = parsedName.CopyCode
		nameParsed["editor_code"] = parsedName.EditorCode
	}

	nameParsedJSON, err := json.Marshal(nameParsed)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal parsed ad payload: %w", err)
	}

	var validatedAt any
	if isActiveStatus(adObject.EffectiveStatus, adObject.Status) {
		validatedAt = time.Now().UTC()
	}

	var adID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO ads (company_id, niche_id, adset_id, external_id, name, name_parsed, status, validated_at, utm_content)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $5)
		ON CONFLICT (company_id, external_id)
		DO UPDATE SET
			niche_id = EXCLUDED.niche_id,
			adset_id = EXCLUDED.adset_id,
			name = EXCLUDED.name,
			name_parsed = EXCLUDED.name_parsed,
			status = EXCLUDED.status,
			validated_at = EXCLUDED.validated_at,
			utm_content = EXCLUDED.utm_content,
			updated_at = now()
		RETURNING id
	`, companyID, nicheID, adsetID, strings.TrimSpace(adExternalID), strings.TrimSpace(adObject.Name), string(nameParsedJSON), mapInternalAdStatus(adObject.EffectiveStatus, adObject.Status), validatedAt).Scan(&adID); err != nil {
		return uuid.Nil, fmt.Errorf("upsert ad %s: %w", adExternalID, err)
	}

	return adID, nil
}

func (s *Service) upsertSnapshot(
	ctx context.Context,
	tx pgx.Tx,
	companyID uuid.UUID,
	nicheID uuid.UUID,
	adID uuid.UUID,
	snapshotDate time.Time,
	adObject utmify.AdObject,
	chargebackAmount decimal.Decimal,
	rawPayload []byte,
) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO ad_metric_snapshots (
			company_id,
			niche_id,
			ad_id,
			snapshot_date,
			impressions,
			clicks,
			spend,
			cpc,
			cpm,
			ctr,
			reach,
			frequency,
			hook_rate,
			body_rate,
			view_page,
			initiate_checkout,
			cost_per_ic,
			fetched_at,
			revenue,
			gross_revenue,
			profit,
			chargeback_amount,
			roas,
			roi,
			cpa,
			approved_orders_count,
			total_orders_count,
			pending_orders_count,
			video_views,
			video_views_3_seconds,
			video_75_watched,
			hook_play_rate,
			icr,
			connect_rate,
			conversion,
			object_status,
			effective_status,
			raw_payload
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8, $9, $10, 0, $11, $12, $13, $14, $15, $16, now(),
			$17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36::jsonb
		)
		ON CONFLICT (ad_id, snapshot_date)
		DO UPDATE SET
			impressions = EXCLUDED.impressions,
			clicks = EXCLUDED.clicks,
			spend = EXCLUDED.spend,
			cpc = EXCLUDED.cpc,
			cpm = EXCLUDED.cpm,
			ctr = EXCLUDED.ctr,
			frequency = EXCLUDED.frequency,
			hook_rate = EXCLUDED.hook_rate,
			body_rate = EXCLUDED.body_rate,
			view_page = EXCLUDED.view_page,
			initiate_checkout = EXCLUDED.initiate_checkout,
			cost_per_ic = EXCLUDED.cost_per_ic,
			fetched_at = EXCLUDED.fetched_at,
			revenue = EXCLUDED.revenue,
			gross_revenue = EXCLUDED.gross_revenue,
			profit = EXCLUDED.profit,
			chargeback_amount = EXCLUDED.chargeback_amount,
			roas = EXCLUDED.roas,
			roi = EXCLUDED.roi,
			cpa = EXCLUDED.cpa,
			approved_orders_count = EXCLUDED.approved_orders_count,
			total_orders_count = EXCLUDED.total_orders_count,
			pending_orders_count = EXCLUDED.pending_orders_count,
			video_views = EXCLUDED.video_views,
			video_views_3_seconds = EXCLUDED.video_views_3_seconds,
			video_75_watched = EXCLUDED.video_75_watched,
			hook_play_rate = EXCLUDED.hook_play_rate,
			icr = EXCLUDED.icr,
			connect_rate = EXCLUDED.connect_rate,
			conversion = EXCLUDED.conversion,
			object_status = EXCLUDED.object_status,
			effective_status = EXCLUDED.effective_status,
			raw_payload = EXCLUDED.raw_payload
	`, companyID, nicheID, adID, snapshotDate,
		adObject.Impressions,
		adObject.InlineLinkClicks,
		centsToMoney(adObject.Spend),
		centsToMoney(pointerFloatValue(adObject.CostPerInlineLinkClick)),
		centsToMoney(adObject.CPM),
		floatToDecimal(adObject.InlineLinkClickCTR),
		floatToDecimal(pointerFloatValue(adObject.Frequency)),
		floatToDecimal(pointerFloatValue(adObject.Hook)),
		floatToDecimal(pointerFloatValue(adObject.Retention)),
		adObject.LandingPageViews,
		adObject.InitiateCheckout,
		centsToMoney(pointerFloatValue(adObject.CostPerInitiateCheckout)),
		centsToMoney(adObject.Revenue),
		centsToMoney(adObject.GrossRevenue),
		centsToMoney(adObject.Profit),
		chargebackAmount,
		floatToDecimal(pointerFloatValue(adObject.ROAS)),
		floatToDecimal(pointerFloatValue(adObject.ROI)),
		centsToMoney(pointerFloatValue(adObject.CPA)),
		adObject.ApprovedOrdersCount,
		adObject.TotalOrdersCount,
		adObject.PendingOrdersCount,
		adObject.VideoViews,
		adObject.VideoViews3Seconds,
		adObject.Video75Watched,
		floatToDecimal(pointerFloatValue(adObject.HookPlayRate)),
		floatToDecimal(pointerFloatValue(adObject.ICR)),
		floatToDecimal(pointerFloatValue(adObject.CON)),
		floatToDecimal(pointerFloatValue(adObject.Conversion)),
		strings.TrimSpace(adObject.Status),
		strings.TrimSpace(adObject.EffectiveStatus),
		string(rawPayload),
	); err != nil {
		return fmt.Errorf("upsert ad metric snapshot for %s: %w", adObject.AdID, err)
	}

	return nil
}

func (s *Service) findCollaboratorsByCodes(ctx context.Context, tx pgx.Tx, companyID uuid.UUID, codes []string) ([]collaboratorRecord, error) {
	filtered := make([]string, 0, len(codes))
	seen := map[string]struct{}{}
	for _, code := range codes {
		normalized := strings.ToUpper(strings.TrimSpace(code))
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		filtered = append(filtered, normalized)
	}

	if len(filtered) == 0 {
		return nil, nil
	}

	rows, err := tx.Query(ctx, `
		SELECT id, full_name, COALESCE(code, ''), role, commission_rate_min, commission_rate_max
		FROM users
		WHERE company_id = $1
		  AND status = 'active'
		  AND code = ANY($2::text[])
		ORDER BY full_name
	`, companyID, filtered)
	if err != nil {
		return nil, fmt.Errorf("query collaborators by code: %w", err)
	}
	defer rows.Close()

	var collaborators []collaboratorRecord
	for rows.Next() {
		var item collaboratorRecord
		if err := rows.Scan(&item.ID, &item.Name, &item.Code, &item.Role, &item.CommissionRateMin, &item.CommissionRateMax); err != nil {
			return nil, fmt.Errorf("scan collaborator by code: %w", err)
		}
		collaborators = append(collaborators, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate collaborators by code: %w", err)
	}

	return collaborators, nil
}

func (s *Service) upsertAdCollaborator(ctx context.Context, tx pgx.Tx, companyID uuid.UUID, nicheID uuid.UUID, adID uuid.UUID, collaborator collaboratorRecord, role string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO ad_collaborators (
			company_id,
			niche_id,
			ad_id,
			user_id,
			role,
			commission_pct_min,
			commission_pct_max
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (ad_id, user_id, role)
		DO UPDATE SET
			commission_pct_min = EXCLUDED.commission_pct_min,
			commission_pct_max = EXCLUDED.commission_pct_max,
			updated_at = now()
	`, companyID, nicheID, adID, collaborator.ID, role, collaborator.CommissionRateMin, collaborator.CommissionRateMax); err != nil {
		return fmt.Errorf("upsert ad collaborator %s: %w", collaborator.Code, err)
	}
	return nil
}

func (s *Service) upsertCommissionEntry(
	ctx context.Context,
	tx pgx.Tx,
	companyID uuid.UUID,
	nicheID uuid.UUID,
	adID uuid.UUID,
	snapshotDate time.Time,
	currency string,
	offerCode string,
	collaborator collaboratorRecord,
	role string,
	adObject utmify.AdObject,
	chargebackAmount decimal.Decimal,
) error {
	revenue := centsToMoney(adObject.Revenue)
	spend := centsToMoney(adObject.Spend)
	commission := s.commissionService.CalculateProfitBasedCommission(revenue, spend, chargebackAmount, collaborator.CommissionRateMin)

	metadata, err := json.Marshal(map[string]any{
		"dashboard_currency": currency,
		"offer_code":         offerCode,
		"ad_name":            adObject.Name,
		"ad_external_id":     adObject.AdID,
		"status":             adObject.Status,
		"effective_status":   adObject.EffectiveStatus,
	})
	if err != nil {
		return fmt.Errorf("marshal commission metadata: %w", err)
	}

	adjustmentReason := fmt.Sprintf("utmify_profit_sync:%s", role)
	if _, err := tx.Exec(ctx, `
		INSERT INTO commission_entries (
			company_id,
			niche_id,
			transaction_id,
			user_id,
			commission_period_id,
			role,
			base_amount,
			commission_pct,
			commission_value,
			status,
			adjustment_reason,
			ad_id,
			snapshot_date,
			revenue_amount,
			spend_amount,
			chargeback_amount,
			source_type,
			metadata
		) VALUES (
			$1, $2, NULL, $8, NULL, $3, $4, $5, $6, 'pending', $7, $9, $10, $11, $12, $13, 'ad_snapshot', $14::jsonb
		)
		ON CONFLICT (ad_id, snapshot_date, user_id, role, source_type)
			WHERE source_type = 'ad_snapshot'
			  AND ad_id IS NOT NULL
			  AND snapshot_date IS NOT NULL
			  AND user_id IS NOT NULL
		DO UPDATE SET
			base_amount = EXCLUDED.base_amount,
			commission_pct = EXCLUDED.commission_pct,
			commission_value = EXCLUDED.commission_value,
			status = EXCLUDED.status,
			adjustment_reason = EXCLUDED.adjustment_reason,
			revenue_amount = EXCLUDED.revenue_amount,
			spend_amount = EXCLUDED.spend_amount,
			chargeback_amount = EXCLUDED.chargeback_amount,
			metadata = EXCLUDED.metadata,
			updated_at = now()
	`, companyID, nicheID, role, commission.NetProfit, collaborator.CommissionRateMin, commission.Commission, adjustmentReason, collaborator.ID, adID, snapshotDate, revenue, spend, chargebackAmount, string(metadata)); err != nil {
		return fmt.Errorf("upsert commission entry for collaborator %s: %w", collaborator.Code, err)
	}

	return nil
}

func collaboratorRoleForCode(parsedName *adparser.ParsedName, code string) string {
	normalized := strings.ToUpper(strings.TrimSpace(code))
	switch normalized {
	case strings.ToUpper(strings.TrimSpace(parsedName.CopyCode)):
		return "copywriter"
	case strings.ToUpper(strings.TrimSpace(parsedName.EditorCode)):
		return "editor"
	default:
		return ""
	}
}

func mapInternalAdStatus(effectiveStatus string, status string) string {
	normalized := normalizeObjectStatus(status, effectiveStatus)
	switch normalized {
	case "active":
		return "validated"
	case "paused":
		return "pre_scale"
	case "rejected", "disabled", "archived":
		return "rejected"
	default:
		return "testing"
	}
}

func normalizeObjectStatus(status string, effectiveStatus string) string {
	value := strings.ToLower(strings.TrimSpace(firstNonEmpty(effectiveStatus, status)))
	if value == "" {
		return "unknown"
	}
	return value
}

func isActiveStatus(effectiveStatus string, status string) bool {
	return normalizeObjectStatus(status, effectiveStatus) == "active"
}

func centsToMoney(value float64) decimal.Decimal {
	return decimal.NewFromFloat(value).Div(decimal.NewFromInt(100)).Round(2)
}

func floatToDecimal(value float64) decimal.Decimal {
	return decimal.NewFromFloat(value)
}

func pointerFloatValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func stringPointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// LinkCollaboratorToAds scans all existing ads whose parsed name references the
// given collaborator code and creates the missing ad_collaborators and
// commission_entries rows. This is called right after a new collaborator is
// created so that historical data is linked without requiring a full re-sync.
func (s *Service) LinkCollaboratorToAds(ctx context.Context, companyID uuid.UUID, collabID uuid.UUID, code string, commissionRateMin decimal.Decimal, commissionRateMax decimal.Decimal) error {
	normalizedCode := strings.ToUpper(strings.TrimSpace(code))
	if normalizedCode == "" {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin link collaborator tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Create ad_collaborators for every ad that references this code.
	if _, err := tx.Exec(ctx, `
		INSERT INTO ad_collaborators (company_id, niche_id, ad_id, user_id, role, commission_pct_min, commission_pct_max)
		SELECT a.company_id, a.niche_id, a.id, $2,
			CASE
				WHEN upper(a.name_parsed->>'copy_code') = $3 THEN 'copywriter'
				WHEN upper(a.name_parsed->>'editor_code') = $3 THEN 'editor'
			END,
			$4, $5
		FROM ads a
		WHERE a.company_id = $1
		  AND (
			upper(a.name_parsed->>'copy_code') = $3
			OR upper(a.name_parsed->>'editor_code') = $3
		  )
		ON CONFLICT (ad_id, user_id, role)
		DO UPDATE SET
			commission_pct_min = EXCLUDED.commission_pct_min,
			commission_pct_max = EXCLUDED.commission_pct_max,
			updated_at = now()
	`, companyID, collabID, normalizedCode, commissionRateMin, commissionRateMax); err != nil {
		return fmt.Errorf("bulk upsert ad_collaborators for %s: %w", normalizedCode, err)
	}

	// 2. Create commission_entries for every existing snapshot of those ads.
	if _, err := tx.Exec(ctx, `
		INSERT INTO commission_entries (
			company_id, niche_id, transaction_id, user_id, commission_period_id,
			role, base_amount, commission_pct, commission_value, status,
			adjustment_reason, ad_id, snapshot_date,
			revenue_amount, spend_amount, chargeback_amount, source_type, metadata
		)
		SELECT
			a.company_id,
			a.niche_id,
			NULL,
			$2,
			NULL,
			CASE
				WHEN upper(a.name_parsed->>'copy_code') = $3 THEN 'copywriter'
				WHEN upper(a.name_parsed->>'editor_code') = $3 THEN 'editor'
			END,
			GREATEST(s.revenue - s.spend + COALESCE(s.chargeback_amount, 0), 0),
			$4,
			CASE
				WHEN (s.revenue - s.spend + COALESCE(s.chargeback_amount, 0)) > 0 AND $4 > 0
				THEN (s.revenue - s.spend + COALESCE(s.chargeback_amount, 0)) * $4 / 100
				ELSE 0
			END,
			'pending',
			'collaborator_backfill',
			a.id,
			s.snapshot_date,
			s.revenue,
			s.spend,
			COALESCE(s.chargeback_amount, 0),
			'ad_snapshot',
			jsonb_build_object(
				'offer_code', COALESCE(a.name_parsed->>'offer_code', ''),
				'ad_name', a.name,
				'backfill', true
			)
		FROM ads a
		INNER JOIN ad_metric_snapshots s
			ON s.ad_id = a.id
		WHERE a.company_id = $1
		  AND (
			upper(a.name_parsed->>'copy_code') = $3
			OR upper(a.name_parsed->>'editor_code') = $3
		  )
		ON CONFLICT (ad_id, snapshot_date, user_id, role, source_type)
			WHERE source_type = 'ad_snapshot'
			  AND ad_id IS NOT NULL
			  AND snapshot_date IS NOT NULL
			  AND user_id IS NOT NULL
		DO NOTHING
	`, companyID, collabID, normalizedCode, commissionRateMin); err != nil {
		return fmt.Errorf("bulk insert commission_entries for %s: %w", normalizedCode, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit link collaborator tx: %w", err)
	}

	s.logger.Info("linked collaborator to existing ads",
		slog.String("company_id", companyID.String()),
		slog.String("user_id", collabID.String()),
		slog.String("code", normalizedCode),
	)

	return nil
}

func extractChargebackAmount(rawPayload []byte) decimal.Decimal {
	var payload map[string]any
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return decimal.Zero
	}

	for _, key := range []string{"chargeback", "chargedbackRevenue", "chargebackRevenue", "chargebackAmount"} {
		value, exists := payload[key]
		if !exists {
			continue
		}

		switch typed := value.(type) {
		case float64:
			return centsToMoney(typed)
		case json.Number:
			parsed, err := typed.Float64()
			if err == nil {
				return centsToMoney(parsed)
			}
		}
	}

	return decimal.Zero
}
