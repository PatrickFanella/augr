package position

import (
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
)

func TestDefaultForMarket_StocksAndCryptoUseATR(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		market domain.MarketType
	}{
		{name: "stock", market: domain.MarketTypeStock},
		{name: "crypto", market: domain.MarketTypeCrypto},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := DefaultForMarket(tc.market, 7.5, 1.8)
			if got.Method != execution.PositionSizingMethodATR {
				t.Fatalf("Method = %q, want %q", got.Method, execution.PositionSizingMethodATR)
			}
			if got.RiskPct != 0.075 {
				t.Fatalf("RiskPct = %v, want 0.075", got.RiskPct)
			}
			if got.ATRMultiplier != 1.8 {
				t.Fatalf("ATRMultiplier = %v, want 1.8", got.ATRMultiplier)
			}
			if got.FractionPct != 0 {
				t.Fatalf("FractionPct = %v, want 0", got.FractionPct)
			}
			if got.HalfKelly {
				t.Fatalf("HalfKelly = %v, want false", got.HalfKelly)
			}
		})
	}
}

func TestDefaultForMarket_PolymarketUsesFixedFractionalTwoPercent(t *testing.T) {
	t.Parallel()

	got := DefaultForMarket(domain.MarketTypePolymarket, 9.0, 2.5)
	if got.Method != execution.PositionSizingMethodFixedFractional {
		t.Fatalf("Method = %q, want %q", got.Method, execution.PositionSizingMethodFixedFractional)
	}
	if got.FractionPct != DefaultPolymarketFractionPct {
		t.Fatalf("FractionPct = %v, want %v", got.FractionPct, DefaultPolymarketFractionPct)
	}
	if got.RiskPct != 0 || got.ATRMultiplier != 0 {
		t.Fatalf("unexpected ATR inputs in polymarket default: %+v", got)
	}
	if got.HalfKelly {
		t.Fatalf("HalfKelly = %v, want false", got.HalfKelly)
	}
}

func TestResolveForMarket_UsesHalfKellyOnlyWhenExplicitlyOptedInAndEligible(t *testing.T) {
	t.Parallel()

	defaultPolicy := DefaultForMarket(domain.MarketTypeStock, 5, 1.5)
	eligible := ResolveForMarket(domain.MarketTypeStock, 5, 1.5, true, HistoryStats{ClosedTrades: KellyHistoryThreshold, WinRate: 0.62, WinLossRatio: 1.8})
	if eligible.Method != execution.PositionSizingMethodKelly || !eligible.HalfKelly {
		t.Fatalf("eligible policy = %+v, want half-Kelly", eligible)
	}
	if eligible.WinRate != 0.62 || eligible.WinLossRatio != 1.8 {
		t.Fatalf("eligible Kelly stats = %+v, want propagated inputs", eligible)
	}

	for _, tc := range []struct {
		name     string
		useKelly bool
		stats    HistoryStats
	}{
		{name: "opt-in missing", useKelly: false, stats: HistoryStats{ClosedTrades: KellyHistoryThreshold, WinRate: 0.62, WinLossRatio: 1.8}},
		{name: "insufficient history", useKelly: true, stats: HistoryStats{ClosedTrades: KellyHistoryThreshold - 1, WinRate: 0.62, WinLossRatio: 1.8}},
		{name: "invalid win rate", useKelly: true, stats: HistoryStats{ClosedTrades: KellyHistoryThreshold, WinRate: 0, WinLossRatio: 1.8}},
		{name: "invalid win loss ratio", useKelly: true, stats: HistoryStats{ClosedTrades: KellyHistoryThreshold, WinRate: 0.62, WinLossRatio: 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ResolveForMarket(domain.MarketTypeStock, 5, 1.5, tc.useKelly, tc.stats)
			if got != defaultPolicy {
				t.Fatalf("policy = %+v, want default %+v", got, defaultPolicy)
			}
		})
	}
}

func TestHalfKellyForHistory_RejectsMissingEdgeInputs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		winRate      float64
		winLossRatio float64
	}{
		{name: "missing win rate", winRate: 0, winLossRatio: 0.55},
		{name: "missing win loss ratio", winRate: 0.62, winLossRatio: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := HalfKellyForHistory(KellyHistoryThreshold, tc.winRate, tc.winLossRatio)
			if got.Method != "" || got.HalfKelly || got.FractionPct != 0 {
				t.Fatalf("policy = %+v, want zero-value policy", got)
			}
		})
	}
}

func TestHalfKellyForHistory_RequiresEnoughClosedTrades(t *testing.T) {
	t.Parallel()

	got := HalfKellyForHistory(KellyHistoryThreshold, 0.62, 1.6)
	if got.Method != execution.PositionSizingMethodKelly {
		t.Fatalf("Method = %q, want %q", got.Method, execution.PositionSizingMethodKelly)
	}
	if !got.HalfKelly {
		t.Fatal("HalfKelly = false, want true")
	}

	insufficient := HalfKellyForHistory(KellyHistoryThreshold-1, 0.62, 1.6)
	if insufficient.Method != "" || insufficient.HalfKelly || insufficient.FractionPct != 0 {
		t.Fatalf("insufficient history policy = %+v, want zero-value policy", insufficient)
	}
}
