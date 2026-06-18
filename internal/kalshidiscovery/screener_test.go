package kalshidiscovery

import (
	"testing"
	"time"
)

func TestDefaultScreenerConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultScreenerConfig()
	if cfg.MaxCandidates != 15 || cfg.MinVolume != 1000 || cfg.MinOpenInterest != 500 || cfg.MaxSpreadPct != 12 || cfg.MinDaysToClose != 3 {
		t.Fatalf("DefaultScreenerConfig() = %#v, want conservative defaults", cfg)
	}
}

func TestScreenMarketsFiltersAndReturnsRejections(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	cfg := ScreenerConfig{
		MaxCandidates:   2,
		MinVolume:       100,
		MinOpenInterest: 50,
		MaxSpreadPct:    20,
		MinDaysToClose:  2,
		Categories:      []string{"weather", "politics"},
	}

	closedAt := now.Add(24 * time.Hour)
	soonAt := now.Add(36 * time.Hour)
	farAt := now.Add(7 * 24 * time.Hour)
	markets := []MarketCandidate{
		{Ticker: "KEEP-2", Category: "politics", Status: "active", YesBid: 0.18, YesAsk: 0.20, NoBid: 0.28, NoAsk: 0.30, Volume: 200, OpenInterest: 80, CloseTime: &farAt},
		{Ticker: "DROP-CLOSED", Category: "weather", Status: "settled", YesBid: 0.10, YesAsk: 0.20, NoBid: 0.80, NoAsk: 0.90, Volume: 500, OpenInterest: 200, CloseTime: &farAt},
		{Ticker: "DROP-SPREAD", Category: "weather", Status: "active", YesBid: 0.01, YesAsk: 0.40, NoBid: 0.01, NoAsk: 0.40, Volume: 500, OpenInterest: 200, CloseTime: &farAt},
		{Ticker: "DROP-VOLUME", Category: "weather", Status: "active", YesBid: 0.10, YesAsk: 0.20, NoBid: 0.80, NoAsk: 0.90, Volume: 99, OpenInterest: 200, CloseTime: &farAt},
		{Ticker: "DROP-OPEN", Category: "weather", Status: "active", YesBid: 0.10, YesAsk: 0.20, NoBid: 0.80, NoAsk: 0.90, Volume: 500, OpenInterest: 49, CloseTime: &farAt},
		{Ticker: "DROP-SOON", Category: "weather", Status: "active", YesBid: 0.10, YesAsk: 0.20, NoBid: 0.80, NoAsk: 0.90, Volume: 500, OpenInterest: 200, CloseTime: &soonAt},
		{Ticker: "DROP-CAT", Category: "sports", Status: "active", YesBid: 0.10, YesAsk: 0.20, NoBid: 0.80, NoAsk: 0.90, Volume: 500, OpenInterest: 200, CloseTime: &farAt},
		{Ticker: "KEEP-1", Category: "weather", Status: "open", YesBid: 0.12, YesAsk: 0.15, NoBid: 0.10, NoAsk: 0.12, Volume: 300, OpenInterest: 100, CloseTime: &farAt},
		{Ticker: "DROP-ASK", Category: "weather", Status: "active", YesBid: 0.10, YesAsk: 0, NoBid: 0.80, NoAsk: 0.90, Volume: 500, OpenInterest: 200, CloseTime: &farAt},
		{Ticker: "DROP-PAST", Category: "weather", Status: "active", YesBid: 0.10, YesAsk: 0.20, NoBid: 0.80, NoAsk: 0.90, Volume: 500, OpenInterest: 200, CloseTime: &closedAt},
	}

	accepted, rejected := ScreenMarketsDetailed(markets, cfg, now)
	if len(accepted) != 2 {
		t.Fatalf("len(accepted) = %d, want 2", len(accepted))
	}
	if accepted[0].Ticker != "KEEP-1" || accepted[1].Ticker != "KEEP-2" {
		t.Fatalf("accepted order = %#v, want volume-ranked candidates", accepted)
	}
	if len(rejected) != 8 {
		t.Fatalf("len(rejected) = %d, want 8", len(rejected))
	}
	if got := rejected[0].Reasons; len(got) == 0 {
		t.Fatal("rejected candidate has no reasons")
	}
	if got := ScreenMarkets(markets, cfg, now); len(got) != 2 {
		t.Fatalf("ScreenMarkets() len = %d, want 2", len(got))
	}
}

func TestScreenMarketsAllowsZeroBidButRequiresExecutableAsk(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	closeTime := now.Add(5 * 24 * time.Hour)
	cfg := DefaultScreenerConfig()
	cfg.MaxSpreadPct = 100
	markets := []MarketCandidate{
		{Ticker: "ZERO-BID", Status: "active", YesBid: 0, YesAsk: 0.18, NoBid: 0, NoAsk: 0.22, Volume: 1000, OpenInterest: 500, CloseTime: &closeTime},
		{Ticker: "ZERO-ASK", Status: "active", YesBid: 0, YesAsk: 0, NoBid: 0, NoAsk: 0.22, Volume: 1000, OpenInterest: 500, CloseTime: &closeTime},
	}

	accepted, rejected := ScreenMarketsDetailed(markets, cfg, now)
	if len(accepted) != 1 || accepted[0].Ticker != "ZERO-BID" {
		t.Fatalf("accepted = %#v, want only zero-bid candidate", accepted)
	}
	if len(rejected) != 1 || rejected[0].Ticker != "ZERO-ASK" {
		t.Fatalf("rejected = %#v, want zero-ask candidate rejected", rejected)
	}
}
