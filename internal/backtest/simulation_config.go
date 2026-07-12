package backtest

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// FillConfigFromSimulation converts the persisted, JSON-friendly simulation
// contract into the concrete fill models used by the engine.
func FillConfigFromSimulation(sim domain.BacktestSimulationParameters) (FillConfig, error) {
	cfg := FillConfig{Slippage: ProportionalSlippage{BasisPoints: 5}, MaxVolumePct: sim.MaxVolumePct}
	if len(sim.SlippageModel) > 0 {
		var model struct {
			Type        string  `json:"type"`
			Amount      float64 `json:"amount"`
			BasisPoints float64 `json:"basis_points"`
			ScaleFactor float64 `json:"scale_factor"`
		}
		if err := json.Unmarshal(sim.SlippageModel, &model); err != nil {
			return FillConfig{}, fmt.Errorf("backtest: parse slippage_model: %w", err)
		}
		switch strings.ToLower(strings.TrimSpace(model.Type)) {
		case "fixed":
			cfg.Slippage = FixedSlippage{Amount: model.Amount}
		case "proportional":
			cfg.Slippage = ProportionalSlippage{BasisPoints: model.BasisPoints}
		case "volatility_scaled":
			cfg.Slippage = VolatilityScaledSlippage{ScaleFactor: model.ScaleFactor}
		default:
			return FillConfig{}, fmt.Errorf("backtest: unsupported slippage model %q", model.Type)
		}
	}
	if len(sim.SpreadModel) > 0 {
		var model struct {
			Type      string  `json:"type"`
			SpreadBps float64 `json:"spread_bps"`
		}
		if err := json.Unmarshal(sim.SpreadModel, &model); err != nil {
			return FillConfig{}, fmt.Errorf("backtest: parse spread_model: %w", err)
		}
		if strings.ToLower(strings.TrimSpace(model.Type)) != "fixed" {
			return FillConfig{}, fmt.Errorf("backtest: unsupported spread model %q", model.Type)
		}
		cfg.Spread = FixedSpread{SpreadBps: model.SpreadBps}
	}
	if len(sim.TransactionCosts) > 0 {
		var costs struct {
			CommissionPerOrder float64 `json:"commission_per_order"`
			CommissionPerUnit  float64 `json:"commission_per_unit"`
			ExchangeFeePct     float64 `json:"exchange_fee_pct"`
		}
		if err := json.Unmarshal(sim.TransactionCosts, &costs); err != nil {
			return FillConfig{}, fmt.Errorf("backtest: parse transaction_costs: %w", err)
		}
		cfg.Costs = TransactionCosts{CommissionPerOrder: costs.CommissionPerOrder, CommissionPerUnit: costs.CommissionPerUnit, ExchangeFeePct: costs.ExchangeFeePct}
	}
	return cfg, nil
}
