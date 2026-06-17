package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/prediction"
)

const (
	minNativeConfidence             = 0.60
	microstructureMinLiquidity      = 1000.0
	microstructureMaxAbsoluteSpread = 0.08
)

// NativeEvaluation is the deterministic evaluation result used by the native
// Polymarket executor to decide whether a strategy should enter or hold.
type NativeEvaluation struct {
	Signal        domain.PipelineSignal
	Action        string
	Side          string
	EntryPrice    float64
	Confidence    float64
	TimeHorizon   string
	MaxEntryPrice float64
	Template      string
	Reason        string
}

// NativeEvaluator evaluates a strategy and snapshot into a deterministic native
// entry/hold decision.
type NativeEvaluator interface {
	Evaluate(ctx context.Context, strategy domain.Strategy, snapshot Snapshot) (NativeEvaluation, error)
}

// DeterministicNativeEvaluator applies conservative metadata/snapshot gates with
// no LLM or repository lookups.
type DeterministicNativeEvaluator struct{}

// NewDeterministicNativeEvaluator constructs the default evaluator.
func NewDeterministicNativeEvaluator() DeterministicNativeEvaluator {
	return DeterministicNativeEvaluator{}
}

// Evaluate applies template-aware deterministic gates and emits an enter/hold
// evaluation that is safe for the native executor to consume.
func (DeterministicNativeEvaluator) Evaluate(ctx context.Context, strategy domain.Strategy, snapshot Snapshot) (NativeEvaluation, error) {
	if err := ctx.Err(); err != nil {
		return NativeEvaluation{}, err
	}

	meta := parseDiscoveryMeta(strategy.Config)
	side := prediction.NormalizeOutcomeSide(meta.Direction)
	if side == "" {
		return holdEvaluation(meta, "polymarket native evaluator: missing YES/NO direction"), nil
	}

	template := normalizeTemplate(meta.Template)
	if !isSupportedNativeTemplate(template) {
		return holdEvaluation(meta, "polymarket native evaluator: unknown or unsupported template"), nil
	}

	entryPrice, ok := snapshot.EntryPriceForSide(side)
	if !ok || entryPrice <= 0 || entryPrice > 1 {
		return holdEvaluation(meta, fmt.Sprintf("polymarket native evaluator: no executable %s quote", side)), nil
	}

	if meta.EntryPriceMax > 0 && entryPrice > meta.EntryPriceMax {
		return NativeEvaluation{
			Signal:        domain.PipelineSignalHold,
			Action:        "hold",
			Side:          side,
			EntryPrice:    entryPrice,
			Confidence:    meta.Conviction,
			TimeHorizon:   normalizedTimeHorizon(meta.TimeHorizon),
			MaxEntryPrice: meta.EntryPriceMax,
			Template:      template,
			Reason:        "polymarket native evaluator: quote is above configured entry ceiling",
		}, nil
	}

	confidence := meta.Conviction
	if confidence <= 0 || confidence < minNativeConfidence {
		return holdEvaluation(meta, "polymarket native evaluator: confidence below entry threshold"), nil
	}
	if !snapshotHasFutureEndDate(snapshot) {
		return holdEvaluation(meta, "polymarket native evaluator: market end date must be in the future"), nil
	}

	switch template {
	case "microstructure":
		if snapshot.Liquidity < microstructureMinLiquidity {
			return holdEvaluation(meta, fmt.Sprintf("polymarket native evaluator: liquidity below %.0f minimum", microstructureMinLiquidity)), nil
		}
		if spread, ok := snapshot.SpreadForSide(side); !ok || spread <= 0 || spread > microstructureMaxAbsoluteSpread {
			return holdEvaluation(meta, fmt.Sprintf("polymarket native evaluator: %s spread too wide", side)), nil
		}
	case "resolution_edge":
		if strings.TrimSpace(snapshot.ResolutionSource) == "" || strings.TrimSpace(snapshot.ResolutionCriteria) == "" {
			return holdEvaluation(meta, "polymarket native evaluator: missing resolution source or criteria"), nil
		}
	case "news_catalyst":
		if strings.TrimSpace(strategy.Description) == "" && strings.TrimSpace(meta.Summary) == "" {
			return holdEvaluation(meta, "polymarket native evaluator: missing catalyst description"), nil
		}
	case "whale_copy":
		return holdEvaluation(meta, "polymarket native evaluator: whale copy requires wallet evidence"), nil
	case "mean_reversion":
		return holdEvaluation(meta, "polymarket native evaluator: mean reversion requires non-OHLCV reversion evidence"), nil
	default:
		return holdEvaluation(meta, "polymarket native evaluator: unsupported template"), nil
	}

	return NativeEvaluation{
		Signal:        domain.PipelineSignalBuy,
		Action:        "enter",
		Side:          side,
		EntryPrice:    entryPrice,
		Confidence:    confidence,
		TimeHorizon:   normalizedTimeHorizon(meta.TimeHorizon),
		MaxEntryPrice: meta.EntryPriceMax,
		Template:      template,
		Reason:        "polymarket native evaluator: template passed deterministic gates",
	}, nil
}

func snapshotHasFutureEndDate(snapshot Snapshot) bool {
	if snapshot.EndDate == nil {
		return false
	}
	reference := snapshot.FetchedAt
	if reference.IsZero() {
		reference = time.Now().UTC()
	}
	return snapshot.EndDate.After(reference)
}

type discoveryMeta struct {
	Template      string  `json:"template"`
	Direction     string  `json:"direction"`
	Conviction    float64 `json:"conviction"`
	TimeHorizon   string  `json:"time_horizon"`
	EntryPriceMax float64 `json:"entry_price_max"`
	Summary       string  `json:"summary"`
}

func parseDiscoveryMeta(raw json.RawMessage) discoveryMeta {
	var wrapped struct {
		DiscoveryMeta discoveryMeta `json:"discovery_meta"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &wrapped) != nil {
		return discoveryMeta{}
	}
	return wrapped.DiscoveryMeta
}

func holdEvaluation(meta discoveryMeta, reason string) NativeEvaluation {
	return NativeEvaluation{
		Signal:        domain.PipelineSignalHold,
		Action:        "hold",
		Confidence:    meta.Conviction,
		TimeHorizon:   normalizedTimeHorizon(meta.TimeHorizon),
		MaxEntryPrice: meta.EntryPriceMax,
		Template:      normalizeTemplate(meta.Template),
		Reason:        reason,
	}
}

func normalizedTimeHorizon(raw string) string {
	h := strings.ToLower(strings.TrimSpace(raw))
	if h == "" {
		return "days"
	}
	return h
}

func normalizeTemplate(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func isSupportedNativeTemplate(template string) bool {
	switch template {
	case "microstructure", "resolution_edge", "news_catalyst", "whale_copy", "mean_reversion":
		return true
	default:
		return false
	}
}
