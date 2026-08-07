package execution

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type OptionsExpirySummary struct {
	ExpiredWorthless int
	CashSettled      int
}

type optionSettlement struct {
	positionID uuid.UUID
	intrinsic  float64
	reason     string
}

// SettleExpiredOptionPositions cash-settles expired paper options. It does not
// fabricate underlying-share assignment. Every candidate is validated before
// persistence begins so missing prices or contract metadata fail the batch.
func SettleExpiredOptionPositions(ctx context.Context, positions []domain.Position, underlyingPrices map[string]float64, now time.Time, settlementRepo repository.OptionSettlementRepository) (OptionsExpirySummary, error) {
	if settlementRepo == nil {
		return OptionsExpirySummary{}, errors.New("options expiry: atomic settlement repository is required")
	}
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	settlements := make([]optionSettlement, 0)
	for index := range positions {
		position := &positions[index]
		if position.AssetClass != domain.AssetClassOption || position.ClosedAt != nil || position.Expiry == nil || position.Quantity <= 0 {
			continue
		}
		expiry := time.Date(position.Expiry.UTC().Year(), position.Expiry.UTC().Month(), position.Expiry.UTC().Day(), 0, 0, 0, 0, time.UTC)
		if expiry.After(today) {
			continue
		}
		if position.OptionType == nil || position.Strike == nil || position.UnderlyingTicker == "" {
			return OptionsExpirySummary{}, fmt.Errorf("options expiry: position %s lacks contract metadata", position.ID)
		}
		underlyingPrice, ok := underlyingPrices[position.UnderlyingTicker]
		if !ok || underlyingPrice <= 0 {
			return OptionsExpirySummary{}, fmt.Errorf("options expiry: missing underlying price for %s", position.UnderlyingTicker)
		}
		intrinsic := optionIntrinsicValue(*position.OptionType, *position.Strike, underlyingPrice)
		reason := "expired_worthless"
		if intrinsic > 0 {
			reason = "exercise_cash_settled"
		}
		settlements = append(settlements, optionSettlement{positionID: position.ID, intrinsic: intrinsic, reason: reason})
	}

	summary := OptionsExpirySummary{}
	for _, settlement := range settlements {
		if _, err := settlementRepo.SettleOptionPosition(ctx, repository.OptionPositionSettlementInput{
			PositionID: settlement.positionID, SettlementPrice: settlement.intrinsic,
			SettledAt: now.UTC(), ExitReason: settlement.reason,
		}); err != nil {
			return summary, fmt.Errorf("options expiry: settle position %s: %w", settlement.positionID, err)
		}
		if settlement.intrinsic > 0 {
			summary.CashSettled++
		} else {
			summary.ExpiredWorthless++
		}
	}
	return summary, nil
}

func optionIntrinsicValue(optionType domain.OptionType, strike, underlying float64) float64 {
	if optionType == domain.OptionTypeCall && underlying > strike {
		return underlying - strike
	}
	if optionType == domain.OptionTypePut && strike > underlying {
		return strike - underlying
	}
	return 0
}
