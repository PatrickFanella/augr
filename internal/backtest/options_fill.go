package backtest

import (
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/simulation"
)

var (
	ErrNilOptionsOrder        = simulation.ErrNilOptionsOrder
	ErrOptionsInvalidQty      = simulation.ErrOptionsInvalidQty
	ErrOptionsInvalidBarClose = simulation.ErrOptionsInvalidBarClose
)

type OptionsFillConfig = simulation.OptionsFillConfig
type OptionsFillResult = simulation.OptionsFillResult

func DefaultOptionsFillConfig() OptionsFillConfig {
	return simulation.DefaultOptionsFillConfig()
}

func SimulateOptionsFill(order *domain.Order, bar domain.OHLCV, cfg OptionsFillConfig) (*OptionsFillResult, error) {
	return simulation.SimulateOptionsFill(order, bar, cfg)
}
