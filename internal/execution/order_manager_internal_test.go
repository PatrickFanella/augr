package execution

import (
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestPolymarketPositionTickerIdempotent(t *testing.T) {
	t.Parallel()
	if got := polymarketPositionTicker(" MARKET ", "yes"); got != "MARKET:YES" {
		t.Fatalf("first normalization = %q, want MARKET:YES", got)
	}
	if got := polymarketPositionTicker("MARKET:YES", "yes"); got != "MARKET:YES" {
		t.Fatalf("already-qualified normalization = %q, want MARKET:YES", got)
	}
	if got := polymarketPositionTicker("market:no", "YES"); got != "market:NO" {
		t.Fatalf("conflicting side should preserve existing qualification, got %q", got)
	}
}

func TestNormalizePredictionOrderTicker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		marketType     domain.MarketType
		ticker         string
		predictionSide string
		wantTicker     string
		wantSide       string
		wantErr        bool
	}{
		{name: "non prediction market preserved", marketType: domain.MarketTypeStock, ticker: " aapl ", predictionSide: "yes", wantTicker: "aapl", wantSide: "YES"},
		{name: "explicit yes side", marketType: domain.MarketTypeKalshi, ticker: "market", predictionSide: "yes", wantTicker: "market", wantSide: "YES"},
		{name: "infer yes side from suffix", marketType: domain.MarketTypePolymarket, ticker: "market:yes", predictionSide: "", wantTicker: "MARKET", wantSide: "YES"},
		{name: "infer no side from suffix", marketType: domain.MarketTypeKalshi, ticker: " market : no ", predictionSide: "invalid", wantTicker: "MARKET", wantSide: "NO"},
		{name: "conflicting suffix", marketType: domain.MarketTypePolymarket, ticker: "market:no", predictionSide: "yes", wantErr: true},
		{name: "missing side and suffix", marketType: domain.MarketTypePolymarket, ticker: "market", predictionSide: "", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotTicker, gotSide, err := NormalizePredictionOrderTicker(tt.marketType, tt.ticker, tt.predictionSide)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizePredictionOrderTicker() error = %v", err)
			}
			if gotTicker != tt.wantTicker || gotSide != tt.wantSide {
				t.Fatalf("NormalizePredictionOrderTicker() = (%q, %q), want (%q, %q)", gotTicker, gotSide, tt.wantTicker, tt.wantSide)
			}
		})
	}
}
