package portfolio

import (
	"math"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/google/uuid"
)

func TestAllocatorSelectsAndSizesHighQualityStock(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 19, 15, 0, 0, 0, time.UTC)
	cfg := DefaultAllocatorConfig()
	cfg.Now = func() time.Time { return now }
	state := PortfolioState{
		Equity:        100000,
		BuyingPower:   100000,
		GrossExposure: 1000,
		MarketExposure: map[domain.MarketType]float64{
			domain.MarketTypeStock: 1000,
		},
	}

	res := AllocateShadow([]domain.Opportunity{{
		ID:               uuid.New(),
		StrategyID:       uuid.New(),
		MarketType:       domain.MarketTypeStock,
		Ticker:           "AAPL",
		Status:           domain.OpportunityStatusQueued,
		Confidence:       0.95,
		EdgePct:          0.03,
		LiquidityUSD:     2_000_000,
		SpreadPct:        0.004,
		ProposedNotional: 500,
		MarketCapUSD:     3_000_000_000_000,
		CreatedAt:        now.Add(-30 * time.Minute),
		ExpiresAt:        now.Add(6 * time.Hour),
	}}, state, cfg)

	if len(res.Decisions) != 1 {
		t.Fatalf("decisions len = %d, want 1", len(res.Decisions))
	}
	dec := res.Decisions[0]
	if dec.Action != domain.AllocationDecisionActionShadowSelected {
		t.Fatalf("action = %q, want shadow_selected", dec.Action)
	}
	if dec.Mode != domain.AllocationDecisionModeShadow {
		t.Fatalf("mode = %q, want shadow", dec.Mode)
	}
	if math.Abs(dec.NotionalUSD-2000) > 1e-9 {
		t.Fatalf("notional = %v, want 2000", dec.NotionalUSD)
	}
	if res.Summary.Selected != 1 || res.Summary.Rejected != 0 {
		t.Fatalf("summary = %+v, want 1 selected / 0 rejected", res.Summary)
	}
	if res.Summary.SelectedByMarket[domain.MarketTypeStock.String()] != 1 {
		t.Fatalf("selected by market = %#v, want stock=1", res.Summary.SelectedByMarket)
	}
	if len(dec.Reasons) == 0 {
		t.Fatal("selected decision reasons empty")
	}
}

func TestAllocatorRejectsLowScoreEdgeLiquiditySpread(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 19, 15, 0, 0, 0, time.UTC)
	cfg := DefaultAllocatorConfig()
	cfg.Now = func() time.Time { return now }
	res := AllocateShadow([]domain.Opportunity{{
		ID:               uuid.New(),
		StrategyID:       uuid.New(),
		MarketType:       domain.MarketTypeStock,
		Ticker:           "XYZ",
		Status:           domain.OpportunityStatusQueued,
		Confidence:       0.2,
		EdgePct:          0.005,
		LiquidityUSD:     1_000,
		SpreadPct:        0.03,
		ProposedNotional: 100,
		CreatedAt:        now,
		ExpiresAt:        now.Add(1 * time.Hour),
	}}, PortfolioState{Equity: 100000, BuyingPower: 100000}, cfg)

	if len(res.Decisions) != 1 {
		t.Fatalf("decisions len = %d, want 1", len(res.Decisions))
	}
	dec := res.Decisions[0]
	if dec.Action != domain.AllocationDecisionActionShadowRejected {
		t.Fatalf("action = %q, want shadow_rejected", dec.Action)
	}
	want := map[string]bool{
		reasonBelowMinScore:     true,
		reasonBelowMinEdge:      true,
		reasonBelowMinLiquidity: true,
		reasonAboveMaxSpread:    true,
	}
	for _, reason := range dec.Reasons {
		delete(want, reason)
	}
	if len(want) != 0 {
		t.Fatalf("missing rejection reasons: %#v", want)
	}
}

func TestAllocatorPenalizesDuplicateTickerBelowThreshold(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 19, 15, 0, 0, 0, time.UTC)
	cfg := DefaultAllocatorConfig()
	cfg.Now = func() time.Time { return now }
	state := PortfolioState{
		Equity:         100000,
		BuyingPower:    100000,
		OpenTickers:    map[string]bool{"AAPL": true},
		MarketExposure: map[domain.MarketType]float64{},
	}
	res := AllocateShadow([]domain.Opportunity{{
		ID:               uuid.New(),
		StrategyID:       uuid.New(),
		MarketType:       domain.MarketTypeStock,
		Ticker:           "AAPL",
		Status:           domain.OpportunityStatusQueued,
		Confidence:       0.4,
		EdgePct:          0.015,
		LiquidityUSD:     500_000,
		SpreadPct:        0.009,
		ProposedNotional: 100,
		CreatedAt:        now,
		ExpiresAt:        now.Add(1 * time.Hour),
	}}, state, cfg)

	dec := res.Decisions[0]
	if dec.Action != domain.AllocationDecisionActionShadowRejected {
		t.Fatalf("action = %q, want shadow_rejected", dec.Action)
	}
	if dec.Score >= 65 {
		t.Fatalf("score = %v, want below threshold", dec.Score)
	}
	if !containsReason(dec.Reasons, reasonDuplicateTicker) {
		t.Fatalf("reasons = %#v, want duplicate_ticker", dec.Reasons)
	}
	if !containsReason(dec.Reasons, reasonBelowMinScore) {
		t.Fatalf("reasons = %#v, want below_min_score", dec.Reasons)
	}
}

func TestAllocatorRespectsRunAndDayCaps(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 19, 15, 0, 0, 0, time.UTC)
	cfg := DefaultAllocatorConfig()
	cfg.Now = func() time.Time { return now }
	opps := []domain.Opportunity{
		strongOpportunity("AAA", now, 0.96),
		strongOpportunity("BBB", now, 0.94),
		strongOpportunity("CCC", now, 0.92),
	}

	runLimited := AllocateShadow(opps, PortfolioState{Equity: 100000, BuyingPower: 100000}, cfg)
	if runLimited.Summary.Selected != 2 {
		t.Fatalf("selected = %d, want 2", runLimited.Summary.Selected)
	}
	if !containsReason(runLimited.Decisions[2].Reasons, reasonMaxOrdersPerRun) {
		t.Fatalf("third decision reasons = %#v, want max_orders_per_run", runLimited.Decisions[2].Reasons)
	}

	dayLimited := cfg
	dayLimited.MaxNewOrdersPerRun = 2
	dayLimited.MaxNewOrdersPerDay = 5
	dayLimited.Now = func() time.Time { return now }
	dayLimitedRes := AllocateShadow(opps[:2], PortfolioState{Equity: 100000, BuyingPower: 100000, NewOrdersToday: 4}, dayLimited)
	if dayLimitedRes.Summary.Selected != 1 {
		t.Fatalf("selected = %d, want 1", dayLimitedRes.Summary.Selected)
	}
	if !containsReason(dayLimitedRes.Decisions[1].Reasons, reasonMaxOrdersPerDay) {
		t.Fatalf("second decision reasons = %#v, want max_orders_per_day", dayLimitedRes.Decisions[1].Reasons)
	}
}

func TestAllocatorCapsEventMarketsAtTwentyFive(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 19, 15, 0, 0, 0, time.UTC)
	cfg := DefaultAllocatorConfig()
	cfg.Now = func() time.Time { return now }
	res := AllocateShadow([]domain.Opportunity{{
		ID:               uuid.New(),
		StrategyID:       uuid.New(),
		MarketType:       domain.MarketTypeKalshi,
		Ticker:           "ELECTION",
		Status:           domain.OpportunityStatusQueued,
		Confidence:       0.98,
		EdgePct:          0.12,
		LiquidityUSD:     20_000,
		SpreadPct:        0.01,
		ProposedNotional: 100,
		CreatedAt:        now,
		ExpiresAt:        now.Add(1 * time.Hour),
	}}, PortfolioState{Equity: 100000, BuyingPower: 100000}, cfg)

	if got := res.Decisions[0].NotionalUSD; math.Abs(got-25) > 1e-9 {
		t.Fatalf("notional = %v, want 25", got)
	}
}

func TestAllocatorRejectsExpiredAndNonQueuedDeterministically(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 19, 15, 0, 0, 0, time.UTC)
	cfg := DefaultAllocatorConfig()
	cfg.Now = func() time.Time { return now }
	res := AllocateShadow([]domain.Opportunity{
		strongOpportunity("ACTIVE", now, 0.97),
		{
			ID:               uuid.New(),
			StrategyID:       uuid.New(),
			MarketType:       domain.MarketTypeStock,
			Ticker:           "EXPIRED",
			Status:           domain.OpportunityStatusQueued,
			Confidence:       0.95,
			EdgePct:          0.03,
			LiquidityUSD:     2_000_000,
			SpreadPct:        0.004,
			ProposedNotional: 100,
			CreatedAt:        now.Add(-2 * time.Hour),
			ExpiresAt:        now.Add(-1 * time.Hour),
		},
		{
			ID:               uuid.New(),
			StrategyID:       uuid.New(),
			MarketType:       domain.MarketTypeStock,
			Ticker:           "DONE",
			Status:           domain.OpportunityStatusSelected,
			Confidence:       0.95,
			EdgePct:          0.03,
			LiquidityUSD:     2_000_000,
			SpreadPct:        0.004,
			ProposedNotional: 100,
			CreatedAt:        now,
			ExpiresAt:        now.Add(1 * time.Hour),
		},
	}, PortfolioState{Equity: 100000, BuyingPower: 100000}, cfg)

	if len(res.Decisions) != 3 {
		t.Fatalf("decisions len = %d, want 3", len(res.Decisions))
	}
	if res.Summary.Selected != 1 || res.Summary.Rejected != 2 {
		t.Fatalf("summary = %+v, want 1 selected / 2 rejected", res.Summary)
	}
	if !containsReason(res.Decisions[1].Reasons, reasonExpired) && !containsReason(res.Decisions[2].Reasons, reasonExpired) {
		t.Fatalf("expected one rejected decision with expired reason: %#v", res.Decisions)
	}
	if !containsReason(res.Decisions[1].Reasons, reasonNotQueued) && !containsReason(res.Decisions[2].Reasons, reasonNotQueued) {
		t.Fatalf("expected one rejected decision with not_queued reason: %#v", res.Decisions)
	}
}

func strongOpportunity(ticker string, now time.Time, confidence float64) domain.Opportunity {
	return domain.Opportunity{
		ID:               uuid.New(),
		StrategyID:       uuid.New(),
		MarketType:       domain.MarketTypeStock,
		Ticker:           ticker,
		Status:           domain.OpportunityStatusQueued,
		Confidence:       confidence,
		EdgePct:          0.03,
		LiquidityUSD:     2_000_000,
		SpreadPct:        0.004,
		ProposedNotional: 100,
		MarketCapUSD:     3_000_000_000_000,
		CreatedAt:        now,
		ExpiresAt:        now.Add(1 * time.Hour),
	}
}

func containsReason(reasons []string, reason string) bool {
	for _, r := range reasons {
		if r == reason {
			return true
		}
	}
	return false
}
