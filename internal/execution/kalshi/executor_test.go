package kalshi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestDeterministicNativeExecutor_KalshiBuyYesWhenMetadataValid(t *testing.T) {
	t.Parallel()

	strategy := kalshiStrategyWithMeta(t, discoveryMeta{
		Template:      "microstructure",
		Direction:     "YES",
		Confidence:    0.72,
		TimeHorizon:   "days",
		EntryPriceMax: 0.50,
	})

	decision, err := DeterministicNativeExecutor{}.Execute(context.Background(), strategy, Snapshot{
		Ticker:     strategy.Ticker,
		Title:      "Will test happen?",
		Status:     "active",
		BestBidYes: 0.45,
		BestAskYes: 0.47,
		BestBidNo:  0.53,
		BestAskNo:  0.55,
		Volume:     1500,
		CloseTime:  time.Now().UTC().Add(48 * time.Hour),
		FetchedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if decision.Signal != domain.PipelineSignalBuy || decision.Action != "enter" {
		t.Fatalf("decision = %+v", decision)
	}
	if decision.Side != "YES" || decision.EntryPrice != 0.47 || decision.EntryType != "limit" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestDeterministicNativeExecutor_KalshiHoldWhenMarketClosed(t *testing.T) {
	t.Parallel()

	strategy := kalshiStrategyWithMeta(t, discoveryMeta{Template: "microstructure", Direction: "YES", Confidence: 0.72, EntryPriceMax: 0.50})
	decision, err := DeterministicNativeExecutor{}.Execute(context.Background(), strategy, Snapshot{
		Ticker:     strategy.Ticker,
		Title:      "Will test happen?",
		Status:     "closed",
		BestBidYes: 0.45,
		BestAskYes: 0.47,
		Volume:     1500,
		CloseTime:  time.Now().UTC().Add(2 * time.Hour),
		FetchedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if decision.Signal != domain.PipelineSignalHold || decision.Action != "hold" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestDeterministicNativeExecutor_KalshiHoldWhenMissingNoBook(t *testing.T) {
	t.Parallel()

	strategy := kalshiStrategyWithMeta(t, discoveryMeta{Template: "microstructure", Direction: "NO", Confidence: 0.72, EntryPriceMax: 0.60})
	decision, err := DeterministicNativeExecutor{}.Execute(context.Background(), strategy, Snapshot{
		Ticker:     strategy.Ticker,
		Title:      "Will test happen?",
		Status:     "active",
		BestBidYes: 0.45,
		BestAskYes: 0.47,
		Volume:     1500,
		CloseTime:  time.Now().UTC().Add(2 * time.Hour),
		FetchedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if decision.Signal != domain.PipelineSignalHold || decision.Action != "hold" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestDeterministicNativeExecutor_KalshiHoldWhenUnknownTemplate(t *testing.T) {
	t.Parallel()

	strategy := kalshiStrategyWithMeta(t, discoveryMeta{Template: "unknown_template", Direction: "YES", Confidence: 0.9, EntryPriceMax: 0.50})
	decision, err := DeterministicNativeExecutor{}.Execute(context.Background(), strategy, Snapshot{
		Ticker:     strategy.Ticker,
		Title:      "Will test happen?",
		Status:     "active",
		BestBidYes: 0.45,
		BestAskYes: 0.47,
		BestBidNo:  0.53,
		BestAskNo:  0.55,
		Volume:     1500,
		CloseTime:  time.Now().UTC().Add(2 * time.Hour),
		FetchedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if decision.Signal != domain.PipelineSignalHold || decision.Action != "hold" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestDeterministicNativeExecutor_KalshiHoldOnMalformedConfig(t *testing.T) {
	t.Parallel()

	decision, err := DeterministicNativeExecutor{}.Execute(context.Background(), domain.Strategy{Ticker: "KXTEST-YESNO", Config: json.RawMessage(`{"discovery_meta":`)}, Snapshot{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if decision.Signal != domain.PipelineSignalHold || decision.Action != "hold" {
		t.Fatalf("decision = %+v", decision)
	}
}

func kalshiStrategyWithMeta(t *testing.T, meta discoveryMeta) domain.Strategy {
	t.Helper()

	raw, err := json.Marshal(map[string]any{"discovery_meta": meta})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	return domain.Strategy{Ticker: "KXTEST-YESNO", MarketType: domain.MarketTypeKalshi, Config: raw}
}
