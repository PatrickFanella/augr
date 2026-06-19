package eventmarkets

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// DiscoveryMeta is the canonical provider-agnostic metadata extracted from a
// discovery strategy config's discovery_meta payload.
type DiscoveryMeta struct {
	Provider   domain.MarketType
	Source     string
	MarketID   string
	Direction  string
	Conviction float64
}

type discoveryEnvelope struct {
	DiscoveryMeta map[string]any `json:"discovery_meta"`
}

// ParseDiscoveryMeta extracts and validates canonical discovery metadata for a
// provider-specific strategy config.
func ParseDiscoveryMeta(marketType domain.MarketType, raw json.RawMessage) (DiscoveryMeta, error) {
	var env discoveryEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return DiscoveryMeta{}, fmt.Errorf("eventmarkets: parse config: %w", err)
	}
	if len(env.DiscoveryMeta) == 0 {
		return DiscoveryMeta{}, fmt.Errorf("eventmarkets: missing discovery_meta")
	}

	meta := DiscoveryMeta{Provider: marketType.Normalize()}
	meta.Source = stringValue(env.DiscoveryMeta, "source")
	meta.Direction = strings.ToUpper(strings.TrimSpace(stringValue(env.DiscoveryMeta, "direction")))
	conviction, ok := floatValue(env.DiscoveryMeta, "conviction")
	if !ok {
		return DiscoveryMeta{}, fmt.Errorf("eventmarkets: missing or invalid conviction")
	}
	meta.Conviction = conviction

	switch meta.Provider {
	case domain.MarketTypeKalshi:
		meta.MarketID = firstString(env.DiscoveryMeta, "market_ticker", "ticker")
	case domain.MarketTypePolymarket:
		meta.MarketID = firstString(env.DiscoveryMeta, "market_slug", "slug", "ticker")
	default:
		return DiscoveryMeta{}, fmt.Errorf("eventmarkets: unsupported market type %q", marketType)
	}
	if meta.MarketID == "" {
		return DiscoveryMeta{}, fmt.Errorf("eventmarkets: missing market id")
	}
	if meta.Source == "" {
		return DiscoveryMeta{}, fmt.Errorf("eventmarkets: missing source")
	}
	if meta.Direction != "YES" && meta.Direction != "NO" {
		return DiscoveryMeta{}, fmt.Errorf("eventmarkets: invalid direction %q", meta.Direction)
	}
	if meta.Conviction < 0 || meta.Conviction > 1 {
		return DiscoveryMeta{}, fmt.Errorf("eventmarkets: conviction %.4f outside [0,1]", meta.Conviction)
	}

	return meta, nil
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v := stringValue(m, key); v != "" {
			return v
		}
	}
	return ""
}

func stringValue(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func floatValue(m map[string]any, key string) (float64, bool) {
	switch v := m[key].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
