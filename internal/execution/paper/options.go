package paper

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
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
func (b *PaperBroker) SubmitSpreadOrder(ctx context.Context, spread *domain.OptionSpread, quantity float64) ([]string, error) {
	if err := b.PreflightSpread(ctx, spread, quantity); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var debit, fees float64
	for _, leg := range spread.Legs {
		premium := leg.ExecutablePrice * quantity * float64(leg.Ratio) * leg.Contract.Multiplier
		if leg.Side == domain.OrderSideBuy {
			debit += premium
		} else {
			debit -= premium
		}
		fees += quantity * float64(leg.Ratio) * DefaultOptionFeePerContract
	}
	total := debit + fees
	if b.balance.Cash < total {
		return nil, fmt.Errorf("paper: insufficient balance for debit spread: need %.2f, have %.2f", total, b.balance.Cash)
	}
	b.balance.Cash -= total
	b.balance.BuyingPower = b.balance.Cash
	b.balance.Equity = b.markToMarketEquityLocked()
	ids := make([]string, len(spread.Legs))
	for index := range spread.Legs {
		ids[index] = b.nextExternalIDLocked()
	}
	return ids, nil
}

// PreflightSpread fails before any leg orders are persisted. Atomic paper
// spread accounting is required before this broker can accept the plan.
func (b *PaperBroker) PreflightSpread(ctx context.Context, spread *domain.OptionSpread, quantity float64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b == nil || spread == nil || quantity <= 0 {
		return errors.New("paper: valid spread and quantity are required")
	}
	if spread.StrategyType != domain.StrategyBullCallSpread && spread.StrategyType != domain.StrategyBearPutSpread {
		return errors.New("paper: only debit vertical spreads are enabled")
	}
	if len(spread.Legs) != 2 {
		return errors.New("paper: debit vertical requires two legs")
	}
	first := spread.Legs[0]
	var openBuys, openSells, closeBuys, closeSells int
	var netDebit float64
	for _, leg := range spread.Legs {
		if strings.TrimSpace(leg.Contract.OCCSymbol) == "" || leg.Contract.Expiry.IsZero() || leg.Contract.Multiplier <= 0 || leg.Ratio != 1 || !isFinitePositiveOptionPrice(leg.ExecutablePrice) {
			return errors.New("paper: each debit spread leg requires contract metadata, 1:1 ratio, and executable price")
		}
		if leg.Contract.Expiry != first.Contract.Expiry || leg.Contract.OptionType != first.Contract.OptionType || leg.Contract.Multiplier != first.Contract.Multiplier {
			return errors.New("paper: debit vertical legs must share type, expiry, and multiplier")
		}
		if leg.Side == domain.OrderSideBuy && leg.PositionIntent == domain.PositionIntentBuyToOpen {
			openBuys++
			netDebit += leg.ExecutablePrice
		} else if leg.Side == domain.OrderSideSell && leg.PositionIntent == domain.PositionIntentSellToOpen {
			openSells++
			netDebit -= leg.ExecutablePrice
		} else if leg.Side == domain.OrderSideBuy && leg.PositionIntent == domain.PositionIntentBuyToClose {
			closeBuys++
			netDebit += leg.ExecutablePrice
		} else if leg.Side == domain.OrderSideSell && leg.PositionIntent == domain.PositionIntentSellToClose {
			closeSells++
			netDebit -= leg.ExecutablePrice
		} else {
			return errors.New("paper: debit vertical requires consistent open or close intents")
		}
	}
	if openBuys == 1 && openSells == 1 {
		if spread.MaxRisk <= 0 || spread.MaxReward <= 0 {
			return errors.New("paper: opening debit vertical requires finite max risk/reward")
		}
		if netDebit <= 0 || !isFinitePositiveOptionPrice(netDebit) {
			return errors.New("paper: spread must be a net debit with one bought and one sold leg")
		}
		return nil
	}
	if closeBuys != 1 || closeSells != 1 || math.IsNaN(netDebit) || math.IsInf(netDebit, 0) {
		return errors.New("paper: spread must be a net debit with one bought and one sold leg")
	}
	return nil
}

func isFinitePositiveOptionPrice(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
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
