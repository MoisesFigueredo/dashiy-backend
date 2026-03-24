package commissions

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/canal/metricas-financeiro-app/backend/internal/db/sqlc"
	pgdb "github.com/canal/metricas-financeiro-app/backend/internal/platform/database"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Service struct {
	logger *slog.Logger
}

type ApplyInput struct {
	CompanyID     uuid.UUID
	NicheID       *uuid.UUID
	OfferID       *uuid.UUID
	TransactionID uuid.UUID
	Transaction   sqlc.Transaction
	Collaborators []sqlc.AdCollaborator
	Reverse       bool
}

type ProfitBasedResult struct {
	NetProfit  decimal.Decimal
	Commission decimal.Decimal
}

func NewService(logger *slog.Logger) *Service {
	return &Service{logger: logger}
}

func (s *Service) ApplyForTransaction(ctx context.Context, q sqlc.Querier, input ApplyInput) error {
	for _, collaborator := range input.Collaborators {
		rule, fallbackPct, err := s.resolveRule(ctx, q, input, collaborator)
		if err != nil {
			return err
		}

		if rule == nil && fallbackPct.IsZero() {
			s.logger.Debug("skipping collaborator without commission rule",
				slog.String("user_id", collaborator.UserID.String()),
				slog.String("role", collaborator.Role),
			)
			continue
		}

		baseAmount := input.Transaction.Amount.Add(input.Transaction.UpsellAmount)
		commissionPct := fallbackPct
		adjustmentReason := "provisional_min_rate"

		if rule != nil {
			commissionPct = rule.PercentageMin
			adjustmentReason = fmt.Sprintf("rule:%s", rule.RuleType)
		}

		commissionValue := baseAmount.Mul(commissionPct).Div(decimal.NewFromInt(100))
		status := "pending"

		if input.Reverse {
			status = "reversed"
			commissionValue = commissionValue.Neg()
			adjustmentReason = "auto_reversal"
		}

		reason := adjustmentReason
		if _, err := q.UpsertCommissionEntry(ctx, sqlc.UpsertCommissionEntryParams{
			CompanyID:        input.CompanyID,
			NicheID:          pgdb.UUIDPointer(input.NicheID),
			TransactionID:    input.TransactionID,
			UserID:           collaborator.UserID,
			Role:             collaborator.Role,
			BaseAmount:       baseAmount,
			CommissionPct:    commissionPct,
			CommissionValue:  commissionValue,
			Status:           status,
			AdjustmentReason: &reason,
		}); err != nil {
			return fmt.Errorf("upsert commission entry for user %s: %w", collaborator.UserID, err)
		}
	}

	return nil
}

func (s *Service) resolveRule(
	ctx context.Context,
	q sqlc.Querier,
	input ApplyInput,
	collaborator sqlc.AdCollaborator,
) (*sqlc.CommissionRule, decimal.Decimal, error) {
	rules, err := q.ListCommissionRuleCandidates(ctx, sqlc.ListCommissionRuleCandidatesParams{
		CompanyID: input.CompanyID,
		Role:      collaborator.Role,
		NicheID:   pgdb.UUIDPointer(input.NicheID),
		UserID:    pgdb.UUID(collaborator.UserID),
		OfferID:   pgdb.UUIDPointer(input.OfferID),
	})
	if err != nil {
		return nil, decimal.Zero, fmt.Errorf("list commission rule candidates: %w", err)
	}

	if len(rules) > 0 {
		return &rules[0], decimal.Zero, nil
	}

	if !collaborator.CommissionPctMin.IsZero() {
		return nil, collaborator.CommissionPctMin, nil
	}

	return nil, decimal.Zero, nil
}

func (s *Service) CalculateProfitBasedCommission(revenue decimal.Decimal, investment decimal.Decimal, chargeback decimal.Decimal, commissionRate decimal.Decimal) ProfitBasedResult {
	netProfit := revenue.Sub(investment).Add(chargeback)
	if netProfit.LessThanOrEqual(decimal.Zero) || commissionRate.LessThanOrEqual(decimal.Zero) {
		return ProfitBasedResult{
			NetProfit:  netProfit,
			Commission: decimal.Zero,
		}
	}

	return ProfitBasedResult{
		NetProfit:  netProfit,
		Commission: netProfit.Mul(commissionRate).Div(decimal.NewFromInt(100)),
	}
}
