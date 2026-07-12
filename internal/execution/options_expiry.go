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
	position  *domain.Position
	intrinsic float64
	reason    string
}

// SettleExpiredOptionPositions cash-settles expired paper options. It does not
// fabricate underlying-share assignment. Every candidate is validated before
// persistence begins so missing prices or contract metadata fail the batch.
func SettleExpiredOptionPositions(ctx context.Context, positions []domain.Position, underlyingPrices map[string]float64, now time.Time, positionRepo repository.PositionRepository, tradeRepo repository.TradeRepository) (OptionsExpirySummary, error) {
	if positionRepo == nil || tradeRepo == nil {
		return OptionsExpirySummary{}, errors.New("options expiry: position and trade repositories are required")
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
		settlements = append(settlements, optionSettlement{position: position, intrinsic: intrinsic, reason: reason})
	}

	summary := OptionsExpirySummary{}
	for _, settlement := range settlements {
		position := settlement.position
		multiplier := position.ContractMultiplier
		if multiplier <= 0 {
			multiplier = 100
		}
		realized := (settlement.intrinsic - position.AvgEntry) * position.Quantity * multiplier
		side := domain.OrderSideSell
		if position.Side == domain.PositionSideShort {
			realized = (position.AvgEntry - settlement.intrinsic) * position.Quantity * multiplier
			side = domain.OrderSideBuy
		}
		closedAt, quantity := now.UTC(), position.Quantity
		position.RealizedPnL += realized
		position.CurrentPrice = &settlement.intrinsic
		position.Quantity = 0
		position.ClosedAt = &closedAt
		if err := positionRepo.Update(ctx, position); err != nil {
			return summary, fmt.Errorf("options expiry: close position %s: %w", position.ID, err)
		}
		positionID := position.ID
		trade := &domain.Trade{ID: uuid.New(), PositionID: &positionID, Ticker: position.Ticker, Side: side, Quantity: quantity, Price: settlement.intrinsic, ExecutedAt: closedAt, AssetClass: domain.AssetClassOption, OpenClose: "close", ContractMultiplier: multiplier, Premium: settlement.intrinsic * quantity * multiplier, ExitReason: settlement.reason}
		if err := tradeRepo.Create(ctx, trade); err != nil {
			return summary, fmt.Errorf("options expiry: create settlement trade for %s: %w", position.ID, err)
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
