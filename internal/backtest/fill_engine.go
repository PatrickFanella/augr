package backtest

import (
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/simulation"
)

const (
	bpsToDecimal = 10000.0
	minFillPrice = 1e-9
)

var (
	ErrNilOrder             = simulation.ErrNilOrder
	ErrInvalidQuantity      = simulation.ErrInvalidQuantity
	ErrInvalidBar           = simulation.ErrInvalidBar
	ErrUnsupportedOrderType = simulation.ErrUnsupportedOrderType
	ErrNilSlippageModel     = simulation.ErrNilSlippageModel
	ErrLimitPriceRequired   = simulation.ErrLimitPriceRequired
	ErrLimitPricePositive   = simulation.ErrLimitPricePositive
	ErrNoFill               = simulation.ErrNoFill
	ErrInvalidSide          = simulation.ErrInvalidSide
)

type SlippageModel = simulation.SlippageModel
type FixedSlippage = simulation.FixedSlippage
type ProportionalSlippage = simulation.ProportionalSlippage
type VolatilityScaledSlippage = simulation.VolatilityScaledSlippage
type SpreadModel = simulation.SpreadModel
type FixedSpread = simulation.FixedSpread
type TransactionCosts = simulation.TransactionCosts
type FillResult = simulation.FillResult
type FillConfig = simulation.FillConfig

// FillEngine retains the legacy backtest package surface while delegating all
// fill behavior to the common simulation implementation.
type FillEngine struct {
	config FillConfig
	common *simulation.FillEngine
}

func NewFillEngine(cfg FillConfig) (*FillEngine, error) {
	if cfg.MaxVolumePct < 0 {
		cfg.MaxVolumePct = 0
	}
	if cfg.MaxVolumePct > 1 {
		cfg.MaxVolumePct = 1
	}
	common, err := simulation.NewFillEngine(cfg)
	if err != nil {
		return nil, err
	}
	return &FillEngine{config: cfg, common: common}, nil
}

func (engine *FillEngine) SimulateFill(order *domain.Order, bar domain.OHLCV) (FillResult, error) {
	return engine.common.SimulateFill(order, bar)
}
