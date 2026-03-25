package backtest

import (
	"errors"
	"fmt"
	"math"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

const (
	bpsToDecimal = 10000.0
	minFillPrice = 1e-9
)

// Errors returned by FillEngine.
var (
	// ErrNilOrder indicates a nil order was passed to SimulateFill.
	ErrNilOrder = errors.New("fill: order is required")

	// ErrInvalidQuantity indicates an order with non-positive quantity.
	ErrInvalidQuantity = errors.New("fill: order quantity must be greater than zero")

	// ErrInvalidBar indicates the bar has non-positive close price.
	ErrInvalidBar = errors.New("fill: bar close price must be greater than zero")

	// ErrUnsupportedOrderType indicates the order type is not supported.
	ErrUnsupportedOrderType = errors.New("fill: unsupported order type")

	// ErrNilSlippageModel indicates no slippage model was configured.
	ErrNilSlippageModel = errors.New("fill: slippage model is required")

	// ErrLimitPriceRequired indicates a limit order was submitted without a limit price.
	ErrLimitPriceRequired = errors.New("fill: limit order requires limit price")

	// ErrLimitPricePositive indicates a limit price was not positive.
	ErrLimitPricePositive = errors.New("fill: limit price must be greater than zero")

	// ErrNoFill indicates the order could not be filled at the current bar.
	ErrNoFill = errors.New("fill: order not filled")

	// ErrInvalidSide indicates the order has an unsupported or empty side.
	ErrInvalidSide = errors.New("fill: invalid order side")
)

// SlippageModel computes a slippage-adjusted fill price given a reference
// price, the order side, and the current market bar.
type SlippageModel interface {
	// AdjustedPrice returns the fill price after applying slippage.
	// For buy orders slippage increases the price; for sell orders it decreases it.
	AdjustedPrice(price float64, side domain.OrderSide, bar domain.OHLCV) float64
}

// FixedSlippage applies a constant absolute price adjustment as slippage.
// Buy orders pay price + Amount; sell orders receive price − Amount (floored
// at minFillPrice).
type FixedSlippage struct {
	Amount float64 // absolute price adjustment
}

// AdjustedPrice implements SlippageModel.
func (f FixedSlippage) AdjustedPrice(price float64, side domain.OrderSide, _ domain.OHLCV) float64 {
	amt := math.Abs(f.Amount)
	if side == domain.OrderSideBuy {
		return price + amt
	}
	return math.Max(price-amt, minFillPrice)
}

// ProportionalSlippage applies slippage as a fraction of the price expressed in
// basis points (1 bp = 0.01%).
type ProportionalSlippage struct {
	BasisPoints float64
}

// AdjustedPrice implements SlippageModel.
func (p ProportionalSlippage) AdjustedPrice(price float64, side domain.OrderSide, _ domain.OHLCV) float64 {
	frac := math.Abs(p.BasisPoints) / bpsToDecimal
	if side == domain.OrderSideBuy {
		return price * (1 + frac)
	}
	return math.Max(price*(1-frac), minFillPrice)
}

// VolatilityScaledSlippage derives slippage from the bar's price range,
// measured as (High − Low) / Close, multiplied by ScaleFactor.
type VolatilityScaledSlippage struct {
	ScaleFactor float64
}

// AdjustedPrice implements SlippageModel.
func (v VolatilityScaledSlippage) AdjustedPrice(price float64, side domain.OrderSide, bar domain.OHLCV) float64 {
	closePrice := bar.Close
	if closePrice <= 0 {
		closePrice = price
	}
	volatility := (bar.High - bar.Low) / closePrice
	frac := math.Abs(v.ScaleFactor) * math.Abs(volatility)
	if side == domain.OrderSideBuy {
		return price * (1 + frac)
	}
	return math.Max(price*(1-frac), minFillPrice)
}

// SpreadModel computes a bid-ask spread around the bar's close price.
type SpreadModel interface {
	// BidAsk returns the bid and ask prices derived from the bar.
	BidAsk(bar domain.OHLCV) (bid, ask float64)
}

// FixedSpread applies a fixed spread in basis points, split equally around the
// close price.
type FixedSpread struct {
	SpreadBps float64
}

// BidAsk implements SpreadModel.
func (f FixedSpread) BidAsk(bar domain.OHLCV) (bid, ask float64) {
	halfSpread := (math.Abs(f.SpreadBps) / bpsToDecimal) / 2
	mid := bar.Close
	bid = math.Max(mid*(1-halfSpread), minFillPrice)
	ask = mid * (1 + halfSpread)
	return bid, ask
}

// TransactionCosts captures commissions and exchange fees applied to each fill.
type TransactionCosts struct {
	CommissionPerOrder float64 // flat commission per fill event
	CommissionPerUnit  float64 // per-share/unit commission
	ExchangeFeePct     float64 // exchange fee as fraction of notional (0.001 = 0.1%)
}

// Compute returns the total transaction cost for a given fill.
func (tc TransactionCosts) Compute(fillQty, fillPrice float64) (commission, exchangeFee float64) {
	notional := fillPrice * fillQty
	commission = math.Abs(tc.CommissionPerOrder) + math.Abs(tc.CommissionPerUnit)*fillQty
	exchangeFee = notional * math.Abs(tc.ExchangeFeePct)
	return commission, exchangeFee
}

// FillResult describes the outcome of a simulated order fill.
type FillResult struct {
	Filled       bool    // true if any quantity was filled
	Partial      bool    // true if only part of the order was filled
	FillPrice    float64 // average fill price after slippage and spread
	FillQuantity float64 // quantity filled (may be less than order quantity)
	Slippage     float64 // total slippage cost in quote currency (|fillPrice - refPrice| * FillQuantity)
	Commission   float64 // total commission charged
	ExchangeFee  float64 // total exchange fee charged
	TotalCost    float64 // net cash impact: notional + fees for buys, notional - fees for sells
}

// FillConfig holds the configurable components of a FillEngine.
type FillConfig struct {
	Slippage     SlippageModel    // required slippage model
	Spread       SpreadModel      // optional bid-ask spread model; nil = no spread
	Costs        TransactionCosts // transaction cost configuration
	MaxVolumePct float64          // max fraction of bar volume fillable per order (0 < x ≤ 1); 0 means no volume limit
}

// FillEngine simulates realistic order fills incorporating slippage, bid-ask
// spreads, partial fills, and transaction costs.
type FillEngine struct {
	config FillConfig
}

// NewFillEngine creates a FillEngine with the given configuration.
func NewFillEngine(cfg FillConfig) (*FillEngine, error) {
	if cfg.Slippage == nil {
		return nil, ErrNilSlippageModel
	}
	if cfg.MaxVolumePct < 0 {
		cfg.MaxVolumePct = 0
	}
	if cfg.MaxVolumePct > 1 {
		cfg.MaxVolumePct = 1
	}
	return &FillEngine{config: cfg}, nil
}

// SimulateFill determines if and how an order would be filled against the
// provided OHLCV bar. It applies the configured slippage model, bid-ask
// spread, partial fill logic, and transaction costs.
//
// For market orders the reference price is the bar's close (adjusted by the
// spread model when present). For limit orders the fill only occurs when the
// limit price is marketable given the bar's price range.
func (e *FillEngine) SimulateFill(order *domain.Order, bar domain.OHLCV) (FillResult, error) {
	if order == nil {
		return FillResult{}, ErrNilOrder
	}
	if order.Quantity <= 0 {
		return FillResult{}, ErrInvalidQuantity
	}
	if !order.Side.IsValid() {
		return FillResult{}, fmt.Errorf("%w: %q", ErrInvalidSide, order.Side)
	}
	if bar.Close <= 0 {
		return FillResult{}, ErrInvalidBar
	}

	switch order.OrderType {
	case domain.OrderTypeMarket:
		return e.fillMarket(order, bar)
	case domain.OrderTypeLimit:
		return e.fillLimit(order, bar)
	default:
		return FillResult{}, fmt.Errorf("%w: %s", ErrUnsupportedOrderType, order.OrderType)
	}
}

// fillMarket processes a market order fill.
func (e *FillEngine) fillMarket(order *domain.Order, bar domain.OHLCV) (FillResult, error) {
	refPrice := e.referencePrice(order.Side, bar)
	slippedPrice := e.config.Slippage.AdjustedPrice(refPrice, order.Side, bar)
	fillQty := e.capQuantity(order.Quantity, bar)
	return e.buildResult(order.Side, refPrice, slippedPrice, order.Quantity, fillQty), nil
}

// fillLimit processes a limit order fill.
func (e *FillEngine) fillLimit(order *domain.Order, bar domain.OHLCV) (FillResult, error) {
	if order.LimitPrice == nil {
		return FillResult{}, ErrLimitPriceRequired
	}
	if *order.LimitPrice <= 0 {
		return FillResult{}, ErrLimitPricePositive
	}
	limit := *order.LimitPrice

	// Check if the limit is marketable within the bar's range.
	if !e.limitMarketable(order.Side, limit, bar) {
		return FillResult{}, ErrNoFill
	}

	refPrice := e.referencePrice(order.Side, bar)
	slippedPrice := e.config.Slippage.AdjustedPrice(refPrice, order.Side, bar)

	// Limit orders cap the fill price at the limit.
	if order.Side == domain.OrderSideBuy {
		slippedPrice = math.Min(slippedPrice, limit)
	} else {
		slippedPrice = math.Max(slippedPrice, limit)
	}

	fillQty := e.capQuantity(order.Quantity, bar)
	return e.buildResult(order.Side, refPrice, slippedPrice, order.Quantity, fillQty), nil
}

// referencePrice returns the starting price for fill simulation. When a
// spread model is configured, buy orders start at the ask and sell orders at
// the bid. Otherwise the bar's close is used.
func (e *FillEngine) referencePrice(side domain.OrderSide, bar domain.OHLCV) float64 {
	if e.config.Spread != nil {
		bid, ask := e.config.Spread.BidAsk(bar)
		if side == domain.OrderSideBuy {
			return ask
		}
		return bid
	}
	return bar.Close
}

// limitMarketable returns true if a limit order would be fillable within the
// bar's price range. When a spread model is configured, the effective bid/ask
// bounds are used instead of the raw bar low/high.
func (e *FillEngine) limitMarketable(side domain.OrderSide, limit float64, bar domain.OHLCV) bool {
	if side == domain.OrderSideBuy {
		// Buy limit fills when the effective low is at or below the limit.
		effectiveLow := bar.Low
		if e.config.Spread != nil {
			// Apply spread to the bar's low to get the effective ask at the low.
			lowBar := bar
			lowBar.Close = bar.Low
			_, ask := e.config.Spread.BidAsk(lowBar)
			effectiveLow = ask
		}
		return effectiveLow <= limit
	}
	// Sell limit fills when the effective high is at or above the limit.
	effectiveHigh := bar.High
	if e.config.Spread != nil {
		// Apply spread to the bar's high to get the effective bid at the high.
		highBar := bar
		highBar.Close = bar.High
		bid, _ := e.config.Spread.BidAsk(highBar)
		effectiveHigh = bid
	}
	return effectiveHigh >= limit
}

// capQuantity applies the MaxVolumePct partial fill logic.
func (e *FillEngine) capQuantity(requested float64, bar domain.OHLCV) float64 {
	if e.config.MaxVolumePct <= 0 || bar.Volume <= 0 {
		return requested
	}
	maxQty := bar.Volume * e.config.MaxVolumePct
	if requested > maxQty {
		return maxQty
	}
	return requested
}

// buildResult assembles a FillResult from the computed values.
func (e *FillEngine) buildResult(side domain.OrderSide, refPrice, fillPrice, orderQty, fillQty float64) FillResult {
	commission, exchangeFee := e.config.Costs.Compute(fillQty, fillPrice)
	notional := fillPrice * fillQty
	slippage := math.Abs(fillPrice-refPrice) * fillQty

	var totalCost float64
	if side == domain.OrderSideBuy {
		totalCost = notional + commission + exchangeFee
	} else {
		totalCost = notional - commission - exchangeFee
	}

	return FillResult{
		Filled:       true,
		Partial:      fillQty < orderQty,
		FillPrice:    fillPrice,
		FillQuantity: fillQty,
		Slippage:     slippage,
		Commission:   commission,
		ExchangeFee:  exchangeFee,
		TotalCost:    totalCost,
	}
}
