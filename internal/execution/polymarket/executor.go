package polymarket

import (
	"context"
	"errors"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/prediction"
)

// ErrNativeExecutionDisabled is returned while Polymarket strategies are
// quarantined from legacy OHLCV execution and before the native executor is
// fully wired.
var ErrNativeExecutionDisabled = errors.New("polymarket native execution is disabled")

// NativeDecision is the shared market-native execution decision emitted from a
// prediction-market snapshot.
type NativeDecision = prediction.NativeDecision

// NativeExecutor executes a strategy against a Polymarket snapshot.
type NativeExecutor interface {
	Execute(ctx context.Context, strategy domain.Strategy, snapshot Snapshot) (NativeDecision, error)
}

// DeterministicNativeExecutor converts discovery metadata into a conservative
// executable decision. It is the default paper-trading executor until an LLM or
// rules engine native strategy evaluator is wired in front of the same broker
// path.
type DeterministicNativeExecutor struct {
	Evaluator NativeEvaluator
}

// NewDeterministicNativeExecutor constructs the default Polymarket executor.
func NewDeterministicNativeExecutor() DeterministicNativeExecutor {
	return DeterministicNativeExecutor{Evaluator: NewDeterministicNativeEvaluator()}
}

// Execute builds a buy/hold decision from strategy discovery metadata and the
// current YES/NO quote. It never submits orders directly.
func (e DeterministicNativeExecutor) Execute(ctx context.Context, strategy domain.Strategy, snapshot Snapshot) (NativeDecision, error) {
	if err := ctx.Err(); err != nil {
		return NativeDecision{}, err
	}

	evaluator := e.Evaluator
	if evaluator == nil {
		evaluator = NewDeterministicNativeEvaluator()
	}
	decision, err := evaluator.Evaluate(ctx, strategy, snapshot)
	if err != nil {
		return NativeDecision{
			Signal:    domain.PipelineSignalHold,
			Action:    "hold",
			Reason:    "polymarket native executor: evaluator failed, holding safely",
			Rationale: "polymarket native executor: evaluator failed, holding safely",
		}, nil
	}
	if decision.Signal != domain.PipelineSignalBuy {
		return NativeDecision{
			Signal:          domain.PipelineSignalHold,
			Action:          decision.Action,
			Side:            decision.Side,
			EntryPrice:      decision.EntryPrice,
			Confidence:      decision.Confidence,
			TimeHorizon:     decision.TimeHorizon,
			Reason:          decision.Reason,
			Rationale:       decision.Reason,
			MaxEntryPrice:   decision.MaxEntryPrice,
			FairProbability: decision.FairProbability,
			Spread:          decision.Spread, Depth: decision.Depth,
			GrossEdge: decision.GrossEdge, NetEdge: decision.NetEdge,
			Template: decision.Template, EvidenceSources: decision.EvidenceSources,
			Calibration: decision.Calibration, GateResults: decision.GateResults,
		}, nil
	}

	entryPrice, ok := snapshot.EntryPriceForSide(decision.Side)
	if !ok || entryPrice <= 0 || entryPrice > 1 {
		return NativeDecision{
			Signal:        domain.PipelineSignalHold,
			Action:        "hold",
			Side:          decision.Side,
			Confidence:    decision.Confidence,
			TimeHorizon:   decision.TimeHorizon,
			Reason:        "polymarket native executor: no executable quote",
			Rationale:     "polymarket native executor: no executable quote",
			MaxEntryPrice: decision.MaxEntryPrice,
		}, nil
	}

	stopLoss := entryPrice * 0.5
	if stopLoss < 0.01 {
		stopLoss = 0.01
	}
	takeProfit := entryPrice + ((1 - entryPrice) * 0.5)
	if takeProfit > 0.99 {
		takeProfit = 0.99
	}

	return NativeDecision{
		Signal:          domain.PipelineSignalBuy,
		Action:          "enter",
		Side:            decision.Side,
		EntryType:       "limit",
		EntryPrice:      entryPrice,
		StopLoss:        stopLoss,
		TakeProfit:      takeProfit,
		Confidence:      decision.Confidence,
		TimeHorizon:     decision.TimeHorizon,
		Reason:          decision.Reason,
		Rationale:       decision.Reason,
		RiskReward:      1,
		MaxEntryPrice:   decision.MaxEntryPrice,
		FairProbability: decision.FairProbability,
		Spread:          decision.Spread, Depth: decision.Depth,
		GrossEdge: decision.GrossEdge, NetEdge: decision.NetEdge,
		Template: decision.Template, EvidenceSources: decision.EvidenceSources,
		Calibration: decision.Calibration, GateResults: decision.GateResults,
	}, nil
}
