package eventmarkets

import (
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestIsEventMarket(t *testing.T) {
	for _, mt := range []domain.MarketType{domain.MarketTypePolymarket, domain.MarketTypeKalshi} {
		if !IsEventMarket(mt) {
			t.Fatalf("IsEventMarket(%q) = false, want true", mt)
		}
	}
	for _, mt := range []domain.MarketType{domain.MarketTypeStock, domain.MarketTypeCrypto, domain.MarketTypeOptions, domain.MarketType("unknown")} {
		if IsEventMarket(mt) {
			t.Fatalf("IsEventMarket(%q) = true, want false", mt)
		}
	}
}

func TestSupportsOHLCVResweep(t *testing.T) {
	for _, mt := range []domain.MarketType{domain.MarketTypeStock, domain.MarketTypeCrypto} {
		if !SupportsOHLCVResweep(mt) {
			t.Fatalf("SupportsOHLCVResweep(%q) = false, want true", mt)
		}
	}
	for _, mt := range []domain.MarketType{domain.MarketTypePolymarket, domain.MarketTypeKalshi, domain.MarketTypeOptions} {
		if SupportsOHLCVResweep(mt) {
			t.Fatalf("SupportsOHLCVResweep(%q) = true, want false", mt)
		}
	}
}
