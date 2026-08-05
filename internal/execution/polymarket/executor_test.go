package polymarket

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestDeterministicNativeExecutor_BuysWhenKnownTemplatePassesGates(t *testing.T) {
	end := time.Now().UTC().Add(48 * time.Hour)
	strategy := polymarketStrategyWithMeta(t, discoveryMeta{Template: "microstructure", Direction: "YES", Conviction: 0.72, TimeHorizon: "days", EntryPriceMax: 0.50})

	decision, err := NewDeterministicNativeExecutor().Execute(context.Background(), strategy, Snapshot{
		Slug:       strategy.Ticker,
		EndDate:    &end,
		BestBidYes: 0.41,
		BestAskYes: 0.43,
		Liquidity:  10_000,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if decision.Signal != domain.PipelineSignalBuy {
		t.Fatalf("Signal = %q, want buy", decision.Signal)
	}
	if decision.Action != "enter" {
		t.Fatalf("Action = %q, want enter", decision.Action)
	}
	if decision.Side != "YES" || decision.EntryPrice != 0.43 || decision.EntryType != "limit" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestDeterministicNativeExecutor_HoldsUnknownTemplateSafely(t *testing.T) {
	strategy := polymarketStrategyWithMeta(t, discoveryMeta{Template: "unknown_template", Direction: "YES", Conviction: 0.9, EntryPriceMax: 0.50})
	decision, err := NewDeterministicNativeExecutor().Execute(context.Background(), strategy, Snapshot{Slug: strategy.Ticker, BestAskYes: 0.43, Liquidity: 10_000})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if decision.Signal != domain.PipelineSignalHold || decision.Action != "hold" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestDeterministicNativeExecutor_HoldsAboveEntryCeiling(t *testing.T) {
	strategy := polymarketStrategyWithMeta(t, discoveryMeta{Template: "mean_reversion", Direction: "NO", Conviction: 0.6, TimeHorizon: "days", EntryPriceMax: 0.40})
	decision, err := NewDeterministicNativeExecutor().Execute(context.Background(), strategy, Snapshot{
		Slug:      strategy.Ticker,
		BestAskNo: 0.55,
		Liquidity: 10_000,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if decision.Signal != domain.PipelineSignalHold {
		t.Fatalf("Signal = %q, want hold", decision.Signal)
	}
	if decision.Action != "hold" {
		t.Fatalf("Action = %q, want hold", decision.Action)
	}
	if decision.Side != "NO" || decision.EntryPrice != 0.55 {
		t.Fatalf("unexpected hold decision: %+v", decision)
	}
}

func TestDeterministicNativeExecutor_HoldsWhenNoSideAskAvailable(t *testing.T) {
	strategy := polymarketStrategyWithMeta(t, discoveryMeta{Template: "mean_reversion", Direction: "NO", Conviction: 0.6, TimeHorizon: "days", EntryPriceMax: 0.70})
	decision, err := NewDeterministicNativeExecutor().Execute(context.Background(), strategy, Snapshot{
		Slug:       strategy.Ticker,
		BestBidYes: 0.42,
		NoPrice:    0.58,
		Liquidity:  10_000,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if decision.Signal != domain.PipelineSignalHold {
		t.Fatalf("Signal = %q, want hold", decision.Signal)
	}
	if decision.Action != "hold" {
		t.Fatalf("Action = %q, want hold", decision.Action)
	}
}

func TestDeterministicNativeExecutor_HoldsWithoutDirection(t *testing.T) {
	decision, err := NewDeterministicNativeExecutor().Execute(context.Background(), domain.Strategy{Ticker: "will-example-happen"}, Snapshot{Slug: "will-example-happen", YesPrice: 0.4})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if decision.Signal != domain.PipelineSignalHold {
		t.Fatalf("Signal = %q, want hold", decision.Signal)
	}
	if decision.Action != "hold" {
		t.Fatalf("Action = %q, want hold", decision.Action)
	}
}

func TestDeterministicNativeExecutor_HoldsWhenEvaluatorErrors(t *testing.T) {
	exec := DeterministicNativeExecutor{Evaluator: errorEvaluator{err: errors.New("boom")}}
	decision, err := exec.Execute(context.Background(), domain.Strategy{Ticker: "will-example-happen"}, Snapshot{Slug: "will-example-happen", BestAskYes: 0.4})
	if err != nil {
		t.Fatalf("Execute() error = %v, want safe hold", err)
	}
	if decision.Signal != domain.PipelineSignalHold || decision.Action != "hold" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

type errorEvaluator struct{ err error }

func (e errorEvaluator) Evaluate(_ context.Context, _ domain.Strategy, _ Snapshot) (NativeEvaluation, error) {
	return NativeEvaluation{}, e.err
}
