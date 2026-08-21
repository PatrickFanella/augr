package backtest

import "github.com/PatrickFanella/get-rich-quick/internal/simulation"

type LatencyModel = simulation.LatencyModel

func NewLatencyModel(mu, sigma float64, seed int64) *LatencyModel {
	return simulation.NewLatencyModel(mu, sigma, seed)
}

func DefaultLatencyModel(seed int64) *LatencyModel {
	return simulation.DefaultLatencyModel(seed)
}
