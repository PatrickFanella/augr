package backtest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

const SimulationInputVersion = "backtest-input-v1"

func simulationInputHash(cfg OrchestratorConfig, bars []domain.OHLCV) (string, error) {
	payload := struct {
		Version           string                  `json:"version"`
		StrategyID        string                  `json:"strategy_id"`
		Ticker            string                  `json:"ticker"`
		StartDate         time.Time               `json:"start_date"`
		EndDate           time.Time               `json:"end_date"`
		InitialCash       float64                 `json:"initial_cash"`
		Fill              ReportFillConfiguration `json:"fill"`
		TrailingStopPct   float64                 `json:"trailing_stop_pct"`
		PromptVersion     string                  `json:"prompt_version"`
		PromptVersionHash string                  `json:"prompt_version_hash"`
		Bars              []domain.OHLCV          `json:"bars"`
	}{
		Version: SimulationInputVersion, StrategyID: cfg.StrategyID.String(), Ticker: cfg.Ticker,
		StartDate: cfg.StartDate.UTC(), EndDate: cfg.EndDate.UTC(), InitialCash: cfg.InitialCash,
		Fill: reportFillConfiguration(cfg.FillConfig), TrailingStopPct: cfg.TrailingStopPct,
		PromptVersion: cfg.PromptVersion, PromptVersionHash: cfg.PromptVersionHash, Bars: bars,
	}
	return HashInputs(payload)
}

// HashInputs returns a stable SHA-256 fingerprint for JSON-serializable
// simulation inputs.
func HashInputs(inputs any) (string, error) {
	raw, err := json.Marshal(inputs)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
