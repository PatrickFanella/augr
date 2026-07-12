package backtest

import (
	"encoding/json"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestFillConfigFromSimulationUsesPersistedAssumptions(t *testing.T) {
	cfg, err := FillConfigFromSimulation(domain.BacktestSimulationParameters{
		SlippageModel:    json.RawMessage(`{"type":"volatility_scaled","scale_factor":0.25}`),
		SpreadModel:      json.RawMessage(`{"type":"fixed","spread_bps":12}`),
		TransactionCosts: json.RawMessage(`{"commission_per_order":1.5,"commission_per_unit":0.01,"exchange_fee_pct":0.002}`),
		MaxVolumePct:     .2,
	})
	if err != nil {
		t.Fatalf("FillConfigFromSimulation() error = %v", err)
	}
	if got, ok := cfg.Slippage.(VolatilityScaledSlippage); !ok || got.ScaleFactor != .25 {
		t.Fatalf("slippage = %#v", cfg.Slippage)
	}
	if got, ok := cfg.Spread.(FixedSpread); !ok || got.SpreadBps != 12 {
		t.Fatalf("spread = %#v", cfg.Spread)
	}
	if cfg.Costs.CommissionPerOrder != 1.5 || cfg.Costs.CommissionPerUnit != .01 || cfg.Costs.ExchangeFeePct != .002 || cfg.MaxVolumePct != .2 {
		t.Fatalf("fill config = %#v", cfg)
	}
}

func TestFillConfigFromSimulationRejectsUnknownModels(t *testing.T) {
	_, err := FillConfigFromSimulation(domain.BacktestSimulationParameters{SlippageModel: json.RawMessage(`{"type":"magic"}`)})
	if err == nil {
		t.Fatal("expected unknown slippage model error")
	}
}
