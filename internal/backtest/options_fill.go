package backtest

import (
	"errors"
	"math"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// Options fill errors.
var (
	ErrNilOptionsOrder        = errors.New("options_fill: order is required")
	ErrOptionsInvalidQty      = errors.New("options_fill: order quantity must be greater than zero")
	ErrOptionsInvalidBarClose = errors.New("options_fill: bar close price must be greater than zero")
)

// OptionsFillConfig configures options-specific fill behavior.
type OptionsFillConfig struct {
	SpreadSlippageBps float64 // bid-ask spread slippage in basis points
	FeePerContract    float64 // per-contract commission (default $0.65)
}

// DefaultOptionsFillConfig returns sensible defaults.
func DefaultOptionsFillConfig() OptionsFillConfig {
	return OptionsFillConfig{
		SpreadSlippageBps: 10,
		FeePerContract:    0.65,
	}
}

// OptionsFillResult captures the outcome of an options fill simulation.
type OptionsFillResult struct {
	FillPrice  float64 // price per contract (premium)
	Quantity   float64 // contracts filled
	TotalCost  float64 // fillPrice * quantity * multiplier +/- fees
	Fee        float64 // total fees
	Multiplier float64
}

// SimulateOptionsFill simulates filling an options order using the mid price
// from the options bar, with configurable spread slippage.
// The bar should be for the specific OCC symbol (not the underlying).
func SimulateOptionsFill(order *domain.Order, bar domain.OHLCV, cfg OptionsFillConfig) (*OptionsFillResult, error) {
	if order == nil {
		return nil, ErrNilOptionsOrder
	}
	if order.Quantity <= 0 {
		return nil, ErrOptionsInvalidQty
	}
	if bar.Close <= 0 {
		return nil, ErrOptionsInvalidBarClose
	}

	multiplier := order.ContractMultiplier
	if multiplier <= 0 {
		multiplier = 100
	}

	// Start from the bar close as the mid-price reference.
	fillPrice := bar.Close

	// Apply slippage: buys get a worse (higher) price, sells get a worse (lower) price.
	slippageFrac := math.Abs(cfg.SpreadSlippageBps) / bpsToDecimal
	if order.Side == domain.OrderSideBuy {
		fillPrice *= (1 + slippageFrac)
	} else {
		fillPrice = math.Max(fillPrice*(1-slippageFrac), minFillPrice)
	}

	fee := math.Abs(cfg.FeePerContract) * order.Quantity
	notional := fillPrice * order.Quantity * multiplier

	var totalCost float64
	if order.Side == domain.OrderSideBuy {
		totalCost = notional + fee
	} else {
		totalCost = notional - fee // premium received minus fees
	}

	return &OptionsFillResult{
		FillPrice:  fillPrice,
		Quantity:   order.Quantity,
		TotalCost:  totalCost,
		Fee:        fee,
		Multiplier: multiplier,
	}, nil
}
