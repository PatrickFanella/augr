package kalshi

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/prediction"
)

const (
	minNativeConfidence = 0.60
	minNativeLiquidity  = 1000.0
	defaultTimeHorizon  = "days"
	defaultEntryType    = "limit"
)

var supportedNativeTemplates = map[string]struct{}{
	"microstructure":  {},
	"resolution_edge": {},
	"news_catalyst":   {},
	"whale_copy":      {},
	"mean_reversion":  {},
}

// NativeDecision is the shared deterministic market-native execution decision.
type NativeDecision = prediction.NativeDecision

// DeterministicNativeExecutor converts discovery metadata into a conservative
// executable decision. It never submits orders directly.
type DeterministicNativeExecutor struct{}

// Execute builds a buy/hold decision from strategy discovery metadata and the
// current YES/NO quote. Malformed or unsupported metadata is converted to a
// safe hold decision.
func (DeterministicNativeExecutor) Execute(ctx context.Context, strategy domain.Strategy, snapshot Snapshot) (NativeDecision, error) {
	if err := ctx.Err(); err != nil {
		return NativeDecision{}, err
	}

	meta, ok := parseDiscoveryMeta(strategy.Config)
	if !ok {
		return holdDecision("kalshi native executor: missing or invalid discovery metadata"), nil
	}

	side := prediction.NormalizeOutcomeSide(meta.Direction)
	if side == "" {
		return holdDecisionWithMeta(meta, "kalshi native executor: missing YES/NO direction"), nil
	}

	template := normalizeTemplate(meta.Template)
	if _, ok := supportedNativeTemplates[template]; !ok {
		return holdDecisionWithMeta(meta, "kalshi native executor: unknown or unsupported template"), nil
	}

	if normalizeStatus(snapshot.Status) != "active" {
		return holdDecisionWithMeta(meta, "kalshi native executor: market must be active"), nil
	}

	referenceTime := snapshot.FetchedAt
	if referenceTime.IsZero() {
		referenceTime = time.Now().UTC()
	}
	if err := snapshot.ValidateExecutableSide(side, minNativeLiquidity, referenceTime); err != nil {
		return holdDecisionWithMeta(meta, "kalshi native executor: "+err.Error()), nil
	}

	entryPrice, ok := snapshot.EntryPriceForSide(side)
	if !ok || entryPrice <= 0 || entryPrice > 1 {
		return holdDecisionWithMeta(meta, "kalshi native executor: no executable quote"), nil
	}

	confidence := meta.confidence()
	if confidence <= 0 || confidence < minNativeConfidence {
		return holdDecisionWithMeta(meta, "kalshi native executor: confidence below entry threshold"), nil
	}

	maxEntryPrice := meta.priceCeiling()
	if maxEntryPrice > 0 && entryPrice > maxEntryPrice {
		return holdDecisionWithMeta(meta, "kalshi native executor: quote is above configured entry ceiling"), nil
	}

	return NativeDecision{
		Signal:        domain.PipelineSignalBuy,
		Action:        "enter",
		Side:          side,
		EntryType:     defaultEntryType,
		EntryPrice:    entryPrice,
		Confidence:    confidence,
		TimeHorizon:   normalizedTimeHorizon(meta.TimeHorizon),
		Reason:        "kalshi native executor: template passed deterministic gates",
		Rationale:     "kalshi native executor: template passed deterministic gates",
		RiskReward:    1,
		MaxEntryPrice: maxEntryPrice,
	}, nil
}

type discoveryMeta struct {
	Template      string  `json:"template"`
	Direction     string  `json:"direction"`
	Confidence    float64 `json:"confidence"`
	Conviction    float64 `json:"conviction"`
	TimeHorizon   string  `json:"time_horizon"`
	EntryPriceMax float64 `json:"entry_price_max"`
	PriceCeiling  float64 `json:"price_ceiling"`
	Summary       string  `json:"summary"`
}

func parseDiscoveryMeta(raw json.RawMessage) (discoveryMeta, bool) {
	var wrapped struct {
		DiscoveryMeta discoveryMeta `json:"discovery_meta"`
	}
	if len(raw) == 0 {
		return discoveryMeta{}, false
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return discoveryMeta{}, false
	}
	return wrapped.DiscoveryMeta, true
}

func (m discoveryMeta) confidence() float64 {
	if m.Confidence > 0 {
		return m.Confidence
	}
	return m.Conviction
}

func (m discoveryMeta) priceCeiling() float64 {
	if m.PriceCeiling > 0 {
		return m.PriceCeiling
	}
	return m.EntryPriceMax
}

func holdDecision(reason string) NativeDecision {
	return NativeDecision{
		Signal:      domain.PipelineSignalHold,
		Action:      "hold",
		Reason:      reason,
		Rationale:   reason,
		TimeHorizon: defaultTimeHorizon,
	}
}

func holdDecisionWithMeta(meta discoveryMeta, reason string) NativeDecision {
	return NativeDecision{
		Signal:        domain.PipelineSignalHold,
		Action:        "hold",
		Side:          prediction.NormalizeOutcomeSide(meta.Direction),
		Confidence:    meta.confidence(),
		TimeHorizon:   normalizedTimeHorizon(meta.TimeHorizon),
		Reason:        reason,
		Rationale:     reason,
		MaxEntryPrice: meta.priceCeiling(),
	}
}

func normalizedTimeHorizon(raw string) string {
	h := strings.ToLower(strings.TrimSpace(raw))
	if h == "" {
		return defaultTimeHorizon
	}
	return h
}

func normalizeTemplate(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func normalizeStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}
