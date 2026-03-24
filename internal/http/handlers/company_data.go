package handlers

import (
	"time"

	"github.com/canal/metricas-financeiro-app/backend/internal/db/sqlc"
	"github.com/canal/metricas-financeiro-app/backend/internal/http/middleware"
	pgdb "github.com/canal/metricas-financeiro-app/backend/internal/platform/database"
	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"
)

type CompanyDataHandler struct {
	queries *sqlc.Queries
}

func NewCompanyDataHandler(queries *sqlc.Queries) *CompanyDataHandler {
	return &CompanyDataHandler{queries: queries}
}

func (h *CompanyDataHandler) ListNiches(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)
	niches, err := h.queries.ListNichesByCompanyID(c.Context(), scope.CompanyID)
	if err != nil {
		return err
	}

	type response struct {
		ID        string     `json:"id"`
		CompanyID string     `json:"company_id"`
		Name      string     `json:"name"`
		Slug      string     `json:"slug"`
		TaxRate   *string    `json:"tax_rate,omitempty"`
		Active    bool       `json:"active"`
		CreatedAt *time.Time `json:"created_at,omitempty"`
		UpdatedAt *time.Time `json:"updated_at,omitempty"`
	}

	items := make([]response, 0, len(niches))
	for _, niche := range niches {
		items = append(items, response{
			ID:        niche.ID.String(),
			CompanyID: niche.CompanyID.String(),
			Name:      niche.Name,
			Slug:      niche.Slug,
			TaxRate:   decimalPointerToString(niche.TaxRate),
			Active:    niche.Active,
			CreatedAt: timeFromTimestamptz(niche.CreatedAt),
			UpdatedAt: timeFromTimestamptz(niche.UpdatedAt),
		})
	}

	return c.JSON(items)
}

func (h *CompanyDataHandler) ListUsers(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)

	var (
		users []sqlc.User
		err   error
	)
	if scope.NicheID != nil {
		users, err = h.queries.ListUsersByNicheID(c.Context(), sqlc.ListUsersByNicheIDParams{
			CompanyID: scope.CompanyID,
			NicheID:   *scope.NicheID,
		})
	} else {
		users, err = h.queries.ListUsersByCompanyID(c.Context(), scope.CompanyID)
	}
	if err != nil {
		return err
	}

	type response struct {
		ID          string     `json:"id"`
		CompanyID   string     `json:"company_id"`
		Code        *string    `json:"code,omitempty"`
		FullName    string     `json:"full_name"`
		Email       string     `json:"email"`
		Role        string     `json:"role"`
		Active      bool       `json:"active"`
		LastLoginAt *time.Time `json:"last_login_at,omitempty"`
		CreatedAt   *time.Time `json:"created_at,omitempty"`
		UpdatedAt   *time.Time `json:"updated_at,omitempty"`
	}

	items := make([]response, 0, len(users))
	for _, user := range users {
		items = append(items, response{
			ID:          user.ID.String(),
			CompanyID:   user.CompanyID.String(),
			Code:        user.Code,
			FullName:    user.FullName,
			Email:       user.Email,
			Role:        user.Role,
			Active:      user.Active,
			LastLoginAt: timeFromTimestamptz(user.LastLoginAt),
			CreatedAt:   timeFromTimestamptz(user.CreatedAt),
			UpdatedAt:   timeFromTimestamptz(user.UpdatedAt),
		})
	}

	return c.JSON(items)
}

func (h *CompanyDataHandler) ListOffers(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)

	var (
		offers []sqlc.Offer
		err    error
	)
	if scope.NicheID != nil {
		offers, err = h.queries.ListOffersByNicheID(c.Context(), sqlc.ListOffersByNicheIDParams{
			CompanyID: scope.CompanyID,
			NicheID:   *scope.NicheID,
		})
	} else {
		offers, err = h.queries.ListOffersByCompanyID(c.Context(), scope.CompanyID)
	}
	if err != nil {
		return err
	}

	type response struct {
		ID        string     `json:"id"`
		CompanyID string     `json:"company_id"`
		NicheID   string     `json:"niche_id"`
		Code      string     `json:"code"`
		Name      string     `json:"name"`
		Active    bool       `json:"active"`
		CreatedAt *time.Time `json:"created_at,omitempty"`
		UpdatedAt *time.Time `json:"updated_at,omitempty"`
	}

	items := make([]response, 0, len(offers))
	for _, offer := range offers {
		items = append(items, response{
			ID:        offer.ID.String(),
			CompanyID: offer.CompanyID.String(),
			NicheID:   offer.NicheID.String(),
			Code:      offer.Code,
			Name:      offer.Name,
			Active:    offer.Active,
			CreatedAt: timeFromTimestamptz(offer.CreatedAt),
			UpdatedAt: timeFromTimestamptz(offer.UpdatedAt),
		})
	}

	return c.JSON(items)
}

func (h *CompanyDataHandler) ListAds(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)

	var (
		ads []sqlc.Ad
		err error
	)
	if scope.NicheID != nil {
		ads, err = h.queries.ListAdsByNicheID(c.Context(), sqlc.ListAdsByNicheIDParams{
			CompanyID: scope.CompanyID,
			NicheID:   *scope.NicheID,
		})
	} else {
		ads, err = h.queries.ListAdsByCompanyID(c.Context(), scope.CompanyID)
	}
	if err != nil {
		return err
	}

	type response struct {
		ID              string     `json:"id"`
		CompanyID       string     `json:"company_id"`
		NicheID         string     `json:"niche_id"`
		ExternalID      string     `json:"external_id"`
		Name            string     `json:"name"`
		Status          string     `json:"status"`
		RejectionReason *string    `json:"rejection_reason,omitempty"`
		ValidatedAt     *time.Time `json:"validated_at,omitempty"`
		UtmContent      *string    `json:"utm_content,omitempty"`
		UpdatedAt       *time.Time `json:"updated_at,omitempty"`
	}

	items := make([]response, 0, len(ads))
	for _, ad := range ads {
		items = append(items, response{
			ID:              ad.ID.String(),
			CompanyID:       ad.CompanyID.String(),
			NicheID:         ad.NicheID.String(),
			ExternalID:      ad.ExternalID,
			Name:            ad.Name,
			Status:          ad.Status,
			RejectionReason: ad.RejectionReason,
			ValidatedAt:     timeFromTimestamptz(ad.ValidatedAt),
			UtmContent:      ad.UtmContent,
			UpdatedAt:       timeFromTimestamptz(ad.UpdatedAt),
		})
	}

	return c.JSON(items)
}

func (h *CompanyDataHandler) ListAdCollaborators(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)

	var (
		collaborators []sqlc.AdCollaborator
		err           error
	)
	if scope.NicheID != nil {
		collaborators, err = h.queries.ListAdCollaboratorsByNicheID(c.Context(), sqlc.ListAdCollaboratorsByNicheIDParams{
			CompanyID: scope.CompanyID,
			NicheID:   *scope.NicheID,
		})
	} else {
		collaborators, err = h.queries.ListAdCollaboratorsByCompanyID(c.Context(), scope.CompanyID)
	}
	if err != nil {
		return err
	}

	type response struct {
		ID               string     `json:"id"`
		CompanyID        string     `json:"company_id"`
		NicheID          string     `json:"niche_id"`
		AdID             string     `json:"ad_id"`
		UserID           string     `json:"user_id"`
		Role             string     `json:"role"`
		CommissionPctMin string     `json:"commission_pct_min"`
		CommissionPctMax string     `json:"commission_pct_max"`
		CreatedAt        *time.Time `json:"created_at,omitempty"`
		UpdatedAt        *time.Time `json:"updated_at,omitempty"`
	}

	items := make([]response, 0, len(collaborators))
	for _, collaborator := range collaborators {
		items = append(items, response{
			ID:               collaborator.ID.String(),
			CompanyID:        collaborator.CompanyID.String(),
			NicheID:          collaborator.NicheID.String(),
			AdID:             collaborator.AdID.String(),
			UserID:           collaborator.UserID.String(),
			Role:             collaborator.Role,
			CommissionPctMin: collaborator.CommissionPctMin.String(),
			CommissionPctMax: collaborator.CommissionPctMax.String(),
			CreatedAt:        timeFromTimestamptz(collaborator.CreatedAt),
			UpdatedAt:        timeFromTimestamptz(collaborator.UpdatedAt),
		})
	}

	return c.JSON(items)
}

func (h *CompanyDataHandler) ListTransactions(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)

	var (
		transactions []sqlc.Transaction
		err          error
	)
	if scope.NicheID != nil {
		transactions, err = h.queries.ListTransactionsByNicheID(c.Context(), sqlc.ListTransactionsByNicheIDParams{
			CompanyID: scope.CompanyID,
			NicheID:   pgdb.UUID(*scope.NicheID),
		})
	} else {
		transactions, err = h.queries.ListTransactionsByCompanyID(c.Context(), scope.CompanyID)
	}
	if err != nil {
		return err
	}

	type response struct {
		ID                string     `json:"id"`
		CompanyID         string     `json:"company_id"`
		NicheID           *string    `json:"niche_id,omitempty"`
		Platform          string     `json:"platform"`
		PlatformTxID      string     `json:"platform_tx_id"`
		EventType         string     `json:"event_type"`
		Amount            string     `json:"amount"`
		UpsellAmount      string     `json:"upsell_amount"`
		Currency          string     `json:"currency"`
		Status            string     `json:"status"`
		AttributionStatus string     `json:"attribution_status"`
		BuyerEmail        *string    `json:"buyer_email,omitempty"`
		BuyerName         *string    `json:"buyer_name,omitempty"`
		AdID              *string    `json:"ad_id,omitempty"`
		OfferID           *string    `json:"offer_id,omitempty"`
		ProductID         *string    `json:"product_id,omitempty"`
		OccurredAt        *time.Time `json:"occurred_at,omitempty"`
	}

	items := make([]response, 0, len(transactions))
	for _, transaction := range transactions {
		items = append(items, response{
			ID:                transaction.ID.String(),
			CompanyID:         transaction.CompanyID.String(),
			NicheID:           uuidFromPgUUID(transaction.NicheID),
			Platform:          transaction.Platform,
			PlatformTxID:      transaction.PlatformTxID,
			EventType:         transaction.EventType,
			Amount:            transaction.Amount.String(),
			UpsellAmount:      transaction.UpsellAmount.String(),
			Currency:          transaction.Currency,
			Status:            transaction.Status,
			AttributionStatus: transaction.AttributionStatus,
			BuyerEmail:        transaction.BuyerEmail,
			BuyerName:         transaction.BuyerName,
			AdID:              uuidFromPgUUID(transaction.AdID),
			OfferID:           uuidFromPgUUID(transaction.OfferID),
			ProductID:         uuidFromPgUUID(transaction.ProductID),
			OccurredAt:        timeFromTimestamptz(transaction.OccurredAt),
		})
	}

	return c.JSON(items)
}

func (h *CompanyDataHandler) ListAdMetricSnapshots(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)

	var (
		snapshots []sqlc.AdMetricSnapshot
		err       error
	)
	if scope.NicheID != nil {
		snapshots, err = h.queries.ListAdMetricSnapshotsByNicheID(c.Context(), sqlc.ListAdMetricSnapshotsByNicheIDParams{
			CompanyID: scope.CompanyID,
			NicheID:   *scope.NicheID,
		})
	} else {
		snapshots, err = h.queries.ListAdMetricSnapshotsByCompanyID(c.Context(), scope.CompanyID)
	}
	if err != nil {
		return err
	}

	type response struct {
		ID               string     `json:"id"`
		CompanyID        string     `json:"company_id"`
		NicheID          string     `json:"niche_id"`
		AdID             string     `json:"ad_id"`
		SnapshotDate     string     `json:"snapshot_date"`
		Impressions      int64      `json:"impressions"`
		Clicks           int64      `json:"clicks"`
		Spend            string     `json:"spend"`
		Cpc              string     `json:"cpc"`
		Cpm              string     `json:"cpm"`
		Ctr              string     `json:"ctr"`
		Reach            int64      `json:"reach"`
		Frequency        string     `json:"frequency"`
		HookRate         string     `json:"hook_rate"`
		BodyRate         string     `json:"body_rate"`
		ViewPage         int64      `json:"view_page"`
		InitiateCheckout int64      `json:"initiate_checkout"`
		CostPerIc        string     `json:"cost_per_ic"`
		FetchedAt        *time.Time `json:"fetched_at,omitempty"`
		CreatedAt        *time.Time `json:"created_at,omitempty"`
	}

	items := make([]response, 0, len(snapshots))
	for _, snapshot := range snapshots {
		items = append(items, response{
			ID:               snapshot.ID.String(),
			CompanyID:        snapshot.CompanyID.String(),
			NicheID:          snapshot.NicheID.String(),
			AdID:             snapshot.AdID.String(),
			SnapshotDate:     dateStringFromDate(snapshot.SnapshotDate),
			Impressions:      snapshot.Impressions,
			Clicks:           snapshot.Clicks,
			Spend:            snapshot.Spend.String(),
			Cpc:              snapshot.Cpc.String(),
			Cpm:              snapshot.Cpm.String(),
			Ctr:              snapshot.Ctr.String(),
			Reach:            snapshot.Reach,
			Frequency:        snapshot.Frequency.String(),
			HookRate:         snapshot.HookRate.String(),
			BodyRate:         snapshot.BodyRate.String(),
			ViewPage:         snapshot.ViewPage,
			InitiateCheckout: snapshot.InitiateCheckout,
			CostPerIc:        snapshot.CostPerIc.String(),
			FetchedAt:        timeFromTimestamptz(snapshot.FetchedAt),
			CreatedAt:        timeFromTimestamptz(snapshot.CreatedAt),
		})
	}

	return c.JSON(items)
}

func (h *CompanyDataHandler) ListCommissionEntries(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)

	var (
		entries []sqlc.CommissionEntry
		err     error
	)
	if scope.NicheID != nil {
		entries, err = h.queries.ListCommissionEntriesByNicheID(c.Context(), sqlc.ListCommissionEntriesByNicheIDParams{
			CompanyID: scope.CompanyID,
			NicheID:   pgdb.UUID(*scope.NicheID),
		})
	} else {
		entries, err = h.queries.ListCommissionEntriesByCompanyID(c.Context(), scope.CompanyID)
	}
	if err != nil {
		return err
	}

	type response struct {
		ID                 string     `json:"id"`
		CompanyID          string     `json:"company_id"`
		NicheID            *string    `json:"niche_id,omitempty"`
		TransactionID      string     `json:"transaction_id"`
		UserID             string     `json:"user_id"`
		CommissionPeriodID *string    `json:"commission_period_id,omitempty"`
		Role               string     `json:"role"`
		BaseAmount         string     `json:"base_amount"`
		CommissionPct      string     `json:"commission_pct"`
		CommissionValue    string     `json:"commission_value"`
		Status             string     `json:"status"`
		AdjustmentReason   *string    `json:"adjustment_reason,omitempty"`
		CreatedAt          *time.Time `json:"created_at,omitempty"`
		UpdatedAt          *time.Time `json:"updated_at,omitempty"`
	}

	items := make([]response, 0, len(entries))
	for _, entry := range entries {
		items = append(items, response{
			ID:                 entry.ID.String(),
			CompanyID:          entry.CompanyID.String(),
			NicheID:            uuidFromPgUUID(entry.NicheID),
			TransactionID:      entry.TransactionID.String(),
			UserID:             entry.UserID.String(),
			CommissionPeriodID: uuidFromPgUUID(entry.CommissionPeriodID),
			Role:               entry.Role,
			BaseAmount:         entry.BaseAmount.String(),
			CommissionPct:      entry.CommissionPct.String(),
			CommissionValue:    entry.CommissionValue.String(),
			Status:             entry.Status,
			AdjustmentReason:   entry.AdjustmentReason,
			CreatedAt:          timeFromTimestamptz(entry.CreatedAt),
			UpdatedAt:          timeFromTimestamptz(entry.UpdatedAt),
		})
	}

	return c.JSON(items)
}

func timeFromTimestamptz(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}

	result := value.Time.UTC()
	return &result
}

func dateStringFromDate(value pgtype.Date) string {
	if !value.Valid {
		return ""
	}

	return value.Time.UTC().Format("2006-01-02")
}

func decimalPointerToString(value *decimal.Decimal) *string {
	if value == nil {
		return nil
	}
	result := value.String()
	return &result
}

func uuidFromPgUUID(value pgtype.UUID) *string {
	if !value.Valid {
		return nil
	}
	result := pgdb.UUIDFromNullable(value).String()
	return &result
}
