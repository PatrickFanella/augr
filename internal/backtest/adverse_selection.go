package backtest

import "github.com/PatrickFanella/get-rich-quick/internal/simulation"

type AdverseSelectionConfig = simulation.AdverseSelectionConfig
type AdverseModel = simulation.AdverseModel

func DefaultAdverseSelectionConfig() AdverseSelectionConfig {
	return simulation.DefaultAdverseSelectionConfig()
}

func NewAdverseModel(cfg AdverseSelectionConfig, seed int64) *AdverseModel {
	return simulation.NewAdverseModel(cfg, seed)
}
