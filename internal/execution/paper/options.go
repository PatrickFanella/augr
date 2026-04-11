package paper

import (
	"errors"
	"fmt"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
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

	fillPrice := 1.0
	if order.LimitPrice != nil && *order.LimitPrice > 0 {
		fillPrice = *order.LimitPrice
	}

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
