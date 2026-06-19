package eventmarkets

import (
	"encoding/json"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestParseDiscoveryMetaKalshi(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"discovery_meta": map[string]any{
		"source": "kalshi_discovery", "market_ticker": "KXMENWORLDCUP-26-US", "direction": "YES", "conviction": 0.72,
	}})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	meta, err := ParseDiscoveryMeta(domain.MarketTypeKalshi, raw)
	if err != nil {
		t.Fatalf("ParseDiscoveryMeta() error = %v", err)
	}
	if meta.Provider != domain.MarketTypeKalshi || meta.MarketID != "KXMENWORLDCUP-26-US" || meta.Direction != "YES" || meta.Source != "kalshi_discovery" {
		t.Fatalf("meta = %#v", meta)
	}
}

func TestParseDiscoveryMetaPolymarket(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"discovery_meta": map[string]any{
		"source": "polymarket_discovery", "market_slug": "will-example-happen", "direction": "NO", "conviction": 0.66,
	}})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	meta, err := ParseDiscoveryMeta(domain.MarketTypePolymarket, raw)
	if err != nil {
		t.Fatalf("ParseDiscoveryMeta() error = %v", err)
	}
	if meta.Provider != domain.MarketTypePolymarket || meta.MarketID != "will-example-happen" || meta.Direction != "NO" || meta.Source != "polymarket_discovery" {
		t.Fatalf("meta = %#v", meta)
	}
}

func TestParseDiscoveryMetaFailsClosedInvalidDirection(t *testing.T) {
	raw := json.RawMessage(`{"discovery_meta":{"source":"kalshi_discovery","market_ticker":"KX-EXAMPLE","direction":"MAYBE","conviction":0.5}}`)
	if _, err := ParseDiscoveryMeta(domain.MarketTypeKalshi, raw); err == nil {
		t.Fatal("ParseDiscoveryMeta() error = nil, want validation failure")
	}
}

func TestParseDiscoveryMetaFailsClosedMissingMarketID(t *testing.T) {
	raw := json.RawMessage(`{"discovery_meta":{"source":"polymarket_discovery","direction":"YES","conviction":0.5}}`)
	if _, err := ParseDiscoveryMeta(domain.MarketTypePolymarket, raw); err == nil {
		t.Fatal("ParseDiscoveryMeta() error = nil, want validation failure")
	}
}

func TestParseDiscoveryMetaFailsClosedMissingSource(t *testing.T) {
	raw := json.RawMessage(`{"discovery_meta":{"market_slug":"will-example-happen","direction":"YES","conviction":0.5}}`)
	if _, err := ParseDiscoveryMeta(domain.MarketTypePolymarket, raw); err == nil {
		t.Fatal("ParseDiscoveryMeta() error = nil, want validation failure")
	}
}

func TestParseDiscoveryMetaFailsClosedMissingConviction(t *testing.T) {
	raw := json.RawMessage(`{"discovery_meta":{"source":"polymarket_discovery","market_slug":"will-example-happen","direction":"YES"}}`)
	if _, err := ParseDiscoveryMeta(domain.MarketTypePolymarket, raw); err == nil {
		t.Fatal("ParseDiscoveryMeta() error = nil, want validation failure")
	}
}
