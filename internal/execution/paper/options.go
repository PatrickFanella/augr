package paper

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
)

// DefaultOptionFeePerContract is the standard per-contract option commission ($0.65).
const DefaultOptionFeePerContract = 0.65

// OptionsFillResult holds the result of a simulated options fill.
type OptionsFillResult struct {
	FillPrice float64
	Quantity  float64
	Premium   float64 // fillPrice * quantity * multiplier
	Fee       float64 // per-contract fee
}

// SimulateOptionFill calculates the fill for an options order.
// For market orders the fill price defaults to the order's LimitPrice (used as the
// mid price reference) or falls back to 1.0. The returned premium accounts for the
// contract multiplier.
func SimulateOptionFill(order *domain.Order) (*OptionsFillResult, error) {
	if order == nil {
		return nil, errors.New("paper: order is required")
	}
	if order.Quantity <= 0 {
		return nil, errors.New("paper: order quantity must be greater than zero")
	}

	if order.LimitPrice == nil || *order.LimitPrice <= 0 {
		return nil, errors.New("paper: executable option price is required")
	}
	fillPrice := *order.LimitPrice

	multiplier := order.ContractMultiplier
	if multiplier <= 0 {
		multiplier = 100
	}

	premium := fillPrice * order.Quantity * multiplier
	fee := order.Quantity * DefaultOptionFeePerContract

	return &OptionsFillResult{
		FillPrice: fillPrice,
		Quantity:  order.Quantity,
		Premium:   premium,
		Fee:       fee,
	}, nil
}

// SubmitOptionOrder fills an explicitly priced option order in the paper book.
// It never calls an external broker and never invents a price for missing quote data.
func (b *PaperBroker) SubmitOptionOrder(ctx context.Context, order *domain.Order) (string, error) {
	if b == nil {
		return "", errors.New("paper: broker is required")
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("paper: submit option order: %w", err)
	}
	if order == nil || order.AssetClass != domain.AssetClassOption {
		return "", errors.New("paper: explicit option order is required")
	}
	result, err := SimulateOptionFill(order)
	if err != nil {
		return "", err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.currentTime().UTC()
	externalID := b.nextExternalIDLocked()
	totalDebit := result.Premium + result.Fee
	if order.Side == domain.OrderSideBuy && b.balance.Cash < totalDebit {
		return "", fmt.Errorf("paper: insufficient balance: need %.2f, have %.2f", totalDebit, b.balance.Cash)
	}
	if order.Side == domain.OrderSideBuy {
		b.balance.Cash -= totalDebit
	} else {
		b.balance.Cash += result.Premium - result.Fee
	}
	if err := ApplyOptionFill(order, result); err != nil {
		return "", err
	}
	order.ExternalID = externalID
	order.SubmittedAt = timePtr(now)
	order.FilledAt = timePtr(now)
	b.balance.BuyingPower = b.balance.Cash
	b.balance.Equity = b.markToMarketEquityLocked()
	b.orders[externalID] = cloneOrder(order)
	return externalID, nil
}

// SubmitSpreadOrder remains disabled until atomic per-leg persistence and
// rollback semantics are available. Partial paper spreads would be misleading.
func (b *PaperBroker) SubmitSpreadOrder(context.Context, *domain.OptionSpread, float64) ([]string, error) {
	return nil, errors.New("paper: spread submission requires atomic leg lifecycle support")
}

// OptionFillReport returns deterministic accounting for a synchronous paper fill.
func (b *PaperBroker) OptionFillReport(_ context.Context, order *domain.Order) (execution.OptionFillReport, error) {
	result, err := SimulateOptionFill(order)
	if err != nil {
		return execution.OptionFillReport{}, err
	}
	return execution.OptionFillReport{Premium: result.Premium, Fee: result.Fee}, nil
}

var _ interface {
	SubmitOptionOrder(context.Context, *domain.Order) (string, error)
	SubmitSpreadOrder(context.Context, *domain.OptionSpread, float64) ([]string, error)
} = (*PaperBroker)(nil)

// IsExpired checks if an options position has expired at the given time.
// A position is expired when the current time is after the contract expiry date.
func IsExpired(position *domain.Position, now time.Time) bool {
	if position == nil || position.Expiry == nil {
		return false
	}
	return now.After(*position.Expiry)
}

// ExerciseValue returns the intrinsic value of an option at the given underlying
// price. Returns 0 for out-of-the-money options.
func ExerciseValue(optType domain.OptionType, strike, underlyingPrice float64) float64 {
	switch optType {
	case domain.OptionTypeCall:
		if underlyingPrice > strike {
			return underlyingPrice - strike
		}
		return 0
	case domain.OptionTypePut:
		if strike > underlyingPrice {
			return strike - underlyingPrice
		}
		return 0
	default:
		return 0
	}
}

// ApplyOptionFill applies a simulated options fill to the paper broker's position
// book. This is intended to be called from PaperBroker.SubmitOrder when the order
// has AssetClass == domain.AssetClassOption.
func ApplyOptionFill(order *domain.Order, result *OptionsFillResult) error {
	if order == nil || result == nil {
		return errors.New("paper: order and fill result are required")
	}
	if result.FillPrice <= 0 {
		return fmt.Errorf("paper: invalid fill price %.4f", result.FillPrice)
	}

	fillPrice := result.FillPrice
	order.FilledQuantity = result.Quantity
	order.FilledAvgPrice = &fillPrice
	order.Status = domain.OrderStatusFilled

	return nil
}
