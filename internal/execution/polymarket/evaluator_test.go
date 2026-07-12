package polymarket

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestDeterministicNativeEvaluator_GatesKnownTemplates(t *testing.T) {
	fetchedAt := time.Now().UTC()
	end := fetchedAt.Add(48 * time.Hour)
	base := Snapshot{
		Slug:       "will-example-happen",
		EndDate:    &end,
		Liquidity:  5000,
		BestBidYes: 0.40,
		BestAskYes: 0.43,
		BestBidNo:  0.56,
		BestAskNo:  0.59,
		FetchedAt:  fetchedAt,
	}

	tests := []struct {
		name     string
		strategy domain.Strategy
		snapshot Snapshot
		wantBuy  bool
	}{
		{
			name:     "microstructure enter",
			strategy: polymarketStrategyWithMeta(t, discoveryMeta{Template: "microstructure", Direction: "YES", Conviction: 0.72, EntryPriceMax: 0.50}),
			snapshot: base,
			wantBuy:  true,
		},
		{
			name:     "microstructure hold wide spread",
			strategy: polymarketStrategyWithMeta(t, discoveryMeta{Template: "microstructure", Direction: "YES", Conviction: 0.72, EntryPriceMax: 0.50}),
			snapshot: func() Snapshot { s := base; s.BestBidYes = 0.28; s.BestAskYes = 0.43; return s }(),
			wantBuy:  false,
		},
		{
			name:     "microstructure hold without net probability edge",
			strategy: polymarketStrategyWithMeta(t, discoveryMeta{Template: "microstructure", Direction: "YES", Conviction: 0.72, EntryPriceMax: 0.80}),
			snapshot: func() Snapshot { s := base; s.BestBidYes = 0.69; s.BestAskYes = 0.70; return s }(),
			wantBuy:  false,
		},
		{
			name:     "resolution edge enter",
			strategy: polymarketStrategyWithMeta(t, discoveryMeta{Template: "resolution_edge", Direction: "NO", Conviction: 0.7, EntryPriceMax: 0.60}),
			snapshot: func() Snapshot {
				s := base
				s.BestBidNo = 0.55
				s.BestAskNo = 0.58
				s.ResolutionSource = "official"
				s.ResolutionCriteria = "official result"
				return s
			}(),
			wantBuy: true,
		},
		{
			name:     "resolution edge hold without source and criteria",
			strategy: polymarketStrategyWithMeta(t, discoveryMeta{Template: "resolution_edge", Direction: "NO", Conviction: 0.7, EntryPriceMax: 0.60}),
			snapshot: func() Snapshot { s := base; s.BestBidNo = 0.55; s.BestAskNo = 0.58; return s }(),
			wantBuy:  false,
		},
		{
			name:     "resolution edge hold with source only",
			strategy: polymarketStrategyWithMeta(t, discoveryMeta{Template: "resolution_edge", Direction: "NO", Conviction: 0.7, EntryPriceMax: 0.60}),
			snapshot: func() Snapshot {
				s := base
				s.BestBidNo = 0.55
				s.BestAskNo = 0.58
				s.ResolutionSource = "official"
				return s
			}(),
			wantBuy: false,
		},
		{
			name:     "news catalyst enter",
			strategy: polymarketStrategyWithMeta(t, discoveryMeta{Template: "news_catalyst", Direction: "YES", Conviction: 0.68, EntryPriceMax: 0.50, Summary: "Official ruling expected to reprice the market."}),
			snapshot: base,
			wantBuy:  true,
		},
		{
			name:     "news catalyst hold without description",
			strategy: polymarketStrategyWithMeta(t, discoveryMeta{Template: "news_catalyst", Direction: "YES", Conviction: 0.68, EntryPriceMax: 0.50}),
			snapshot: base,
			wantBuy:  false,
		},
		{
			name:     "whale copy high conviction still holds without wallet evidence",
			strategy: polymarketStrategyWithMeta(t, discoveryMeta{Template: "whale_copy", Direction: "YES", Conviction: 0.80, EntryPriceMax: 0.50}),
			snapshot: base,
			wantBuy:  false,
		},
		{
			name:     "whale copy low conviction hold",
			strategy: polymarketStrategyWithMeta(t, discoveryMeta{Template: "whale_copy", Direction: "YES", Conviction: 0.65, EntryPriceMax: 0.50}),
			snapshot: base,
			wantBuy:  false,
		},
		{
			name:     "unknown template hold",
			strategy: polymarketStrategyWithMeta(t, discoveryMeta{Template: "arb_magic", Direction: "YES", Conviction: 0.90, EntryPriceMax: 0.50}),
			snapshot: base,
			wantBuy:  false,
		},
		{
			name:     "mean reversion holds without non OHLCV evidence",
			strategy: polymarketStrategyWithMeta(t, discoveryMeta{Template: "mean_reversion", Direction: "YES", Conviction: 0.90, EntryPriceMax: 0.50}),
			snapshot: base,
			wantBuy:  false,
		},
		{
			name:     "microstructure hold on expired market",
			strategy: polymarketStrategyWithMeta(t, discoveryMeta{Template: "microstructure", Direction: "YES", Conviction: 0.72, EntryPriceMax: 0.50}),
			snapshot: func() Snapshot { s := base; expired := fetchedAt.Add(-time.Hour); s.EndDate = &expired; return s }(),
			wantBuy:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := NewDeterministicNativeEvaluator().Evaluate(context.Background(), tc.strategy, tc.snapshot)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if tc.wantBuy {
				if decision.Signal != domain.PipelineSignalBuy || decision.Action != "enter" {
					t.Fatalf("decision = %+v, want buy/enter", decision)
				}
				if decision.EntryPrice <= 0 {
					t.Fatalf("decision entry price = %v, want executable quote", decision.EntryPrice)
				}
				if decision.FairProbability <= 0 || decision.NetEdge <= 0 || decision.Calibration == "" || len(decision.GateResults) == 0 {
					t.Fatalf("decision lacks replayable probability evidence: %+v", decision)
				}
			} else {
				if decision.Signal != domain.PipelineSignalHold || decision.Action != "hold" {
					t.Fatalf("decision = %+v, want hold", decision)
				}
			}
		})
	}
}

func polymarketStrategyWithMeta(t *testing.T, meta discoveryMeta) domain.Strategy {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"discovery_meta": meta})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return domain.Strategy{ID: uuid.New(), Ticker: "will-example-happen", MarketType: domain.MarketTypePolymarket, IsPaper: true, Config: raw}
}
