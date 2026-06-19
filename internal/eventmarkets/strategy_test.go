package eventmarkets

import (
	"bytes"
	"testing"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestReuseByTickerOnly(t *testing.T) {
	t.Parallel()

	for _, mt := range []domain.MarketType{domain.MarketTypePolymarket, domain.MarketTypeKalshi} {
		if !ReuseByTickerOnly(mt) {
			t.Fatalf("ReuseByTickerOnly(%q) = false, want true", mt)
		}
	}

	for _, mt := range []domain.MarketType{domain.MarketTypeStock, domain.MarketTypeCrypto, domain.MarketTypeOptions, domain.MarketType("unknown")} {
		if ReuseByTickerOnly(mt) {
			t.Fatalf("ReuseByTickerOnly(%q) = true, want false", mt)
		}
	}
}

func TestBuildPaperStrategy(t *testing.T) {
	t.Parallel()

	config := []byte(`{"discovery_meta":{"source":"kalshi_discovery","market_ticker":"KX","direction":"YES","conviction":0.7}}`)
	strategy := BuildPaperStrategy(BuildPaperStrategyInput{
		Provider:     domain.MarketTypeKalshi,
		Name:         "auto: kalshi KX",
		Ticker:       "KX",
		ScheduleCron: "0 */6 * * *",
		ConfigJSON:   config,
	})

	if strategy.ID != uuid.Nil {
		t.Fatalf("strategy.ID = %v, want zero value", strategy.ID)
	}
	if strategy.Name != "auto: kalshi KX" || strategy.Ticker != "KX" || strategy.MarketType != domain.MarketTypeKalshi {
		t.Fatalf("strategy identity fields = %#v", strategy)
	}
	if !strategy.IsPaper || strategy.Status != domain.StrategyStatusActive || strategy.ScheduleCron != "0 */6 * * *" {
		t.Fatalf("strategy paper fields = %#v", strategy)
	}
	if !bytes.Equal(strategy.Config, config) {
		t.Fatalf("strategy.Config = %s, want %s", strategy.Config, config)
	}
	config[0] = '['
	if bytes.Equal(strategy.Config, config) {
		t.Fatal("strategy.Config should be a copy, not alias input slice")
	}
}
