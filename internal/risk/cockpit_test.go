package risk

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func td(mt domain.MarketType, riskStatus domain.RiskDecisionStatus, status domain.TradeDecisionStatus, approvedSize, netEV float64) domain.TradeDecision {
	return domain.TradeDecision{
		ID:           uuid.New(),
		MarketType:   mt,
		RiskStatus:   riskStatus,
		Status:       status,
		ApprovedSize: approvedSize,
		NetEV:        netEV,
	}
}

func TestBuildCockpitSummaryEmptyState(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	got := BuildCockpitSummary(nil, nil, now)
	if !got.GeneratedAt.Equal(now) {
		t.Fatalf("generated_at = %v want %v", got.GeneratedAt, now)
	}
	if got.KillSwitchActive || got.CircuitBreaker {
		t.Fatalf("unexpected active flags: %+v", got)
	}
	if len(got.Exposures) != 5 {
		t.Fatalf("exposures len = %d want 5", len(got.Exposures))
	}
	if len(got.Warnings) != 1 || got.Warnings[0] != "no trade decisions available" {
		t.Fatalf("warnings = %+v", got.Warnings)
	}
	for i, mt := range cockpitMarketOrder {
		if got.Exposures[i].MarketType != mt {
			t.Fatalf("exposure[%d] market = %s want %s", i, got.Exposures[i].MarketType, mt)
		}
	}
}

func TestBuildCockpitSummaryMixedMarketsAndOrdering(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	decisions := []domain.TradeDecision{
		td(domain.MarketTypePolymarket, domain.RiskDecisionApproved, domain.TradeDecisionStatusLive, 8, 1.5),
		td(domain.MarketTypeOptions, domain.RiskDecisionRejected, domain.TradeDecisionStatusRejected, 0, math.NaN()),
		td(domain.MarketTypeCrypto, domain.RiskDecisionApproved, domain.TradeDecisionStatusPaper, 2, 0.75),
		td(domain.MarketTypeStock, domain.RiskDecisionApproved, domain.TradeDecisionStatusPaper, 3, 1.25),
	}
	got := BuildCockpitSummary(decisions, nil, now)
	wantOrder := []domain.MarketType{
		domain.MarketTypeStock,
		domain.MarketTypeCrypto,
		domain.MarketTypeOptions,
		domain.MarketTypePolymarket,
		domain.MarketTypeKalshi,
	}
	for i, mt := range wantOrder {
		if got.Exposures[i].MarketType != mt {
			t.Fatalf("order[%d] = %s want %s", i, got.Exposures[i].MarketType, mt)
		}
	}
	stock := got.Exposures[0]
	if stock.OpenPositions != 1 || stock.ApprovedDecisions != 1 || stock.RejectedDecisions != 0 || stock.GrossExposure != 3 || stock.NetExpectedValue != 1.25 {
		t.Fatalf("unexpected stock exposure: %+v", stock)
	}
	crypto := got.Exposures[1]
	if crypto.OpenPositions != 1 || crypto.ApprovedDecisions != 1 || crypto.GrossExposure != 2 || crypto.NetExpectedValue != 0.75 {
		t.Fatalf("unexpected crypto exposure: %+v", crypto)
	}
	options := got.Exposures[2]
	if options.RejectedDecisions != 1 || options.ApprovedDecisions != 0 {
		t.Fatalf("unexpected options exposure: %+v", options)
	}
	poly := got.Exposures[3]
	if poly.OpenPositions != 1 || poly.GrossExposure != 8 || poly.NetExpectedValue != 1.5 {
		t.Fatalf("unexpected polymarket exposure: %+v", poly)
	}
	if len(got.Warnings) != 1 || got.Warnings[0] != "market options has rejected decisions but no approved exposure" {
		t.Fatalf("warnings = %+v", got.Warnings)
	}
}

func TestBuildCockpitSummaryWithPositionsIncludesKalshiAndMissingMarks(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	markedPrice := 0.3
	markedPnL := 0.5
	got := BuildCockpitSummaryWithPositions(nil, []domain.Position{
		{MarketType: domain.MarketTypeKalshi, Ticker: "KX-A", Quantity: 10, AvgEntry: 0.25, CurrentPrice: &markedPrice, UnrealizedPnL: &markedPnL},
		{MarketType: domain.MarketTypeKalshi, Ticker: "KX-B", Quantity: 20, AvgEntry: 0.10},
	}, nil, now)
	kalshi := got.Exposures[4]
	if kalshi.OpenPositions != 2 || kalshi.MarkedPositions != 1 || kalshi.UnmarkedPositions != 1 {
		t.Fatalf("unexpected Kalshi coverage: %+v", kalshi)
	}
	if kalshi.GrossExposure != 4.5 || got.GrossCostBasis != 4.5 {
		t.Fatalf("unexpected cost basis: exposure=%v total=%v", kalshi.GrossExposure, got.GrossCostBasis)
	}
	if got.ValuationStatus != "partial" || got.ReconciliationStatus != "complete" {
		t.Fatalf("unexpected statuses: %+v", got)
	}
	if len(got.Warnings) == 0 {
		t.Fatal("expected missing-mark warning")
	}
}

func TestBuildCockpitSummarySkipsNonFiniteInputs(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	decisions := []domain.TradeDecision{
		td(domain.MarketTypeStock, domain.RiskDecisionApproved, domain.TradeDecisionStatusPaper, math.NaN(), math.Inf(1)),
		td(domain.MarketTypeStock, domain.RiskDecisionApproved, domain.TradeDecisionStatusPaper, math.Inf(1), math.NaN()),
		td(domain.MarketTypeStock, domain.RiskDecisionApproved, domain.TradeDecisionStatusPaper, 5, 2.5),
	}
	got := BuildCockpitSummary(decisions, nil, now)
	stock := got.Exposures[0]
	if stock.ApprovedDecisions != 3 {
		t.Fatalf("approved decisions = %d want 3", stock.ApprovedDecisions)
	}
	if stock.OpenPositions != 1 || stock.GrossExposure != 5 || stock.NetExpectedValue != 2.5 {
		t.Fatalf("unexpected finite aggregation: %+v", stock)
	}
}

func TestBuildCockpitSummaryKillSwitchAndBreakerWarnings(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	status := &EngineStatus{
		KillSwitch:     KillSwitchStatus{Active: true},
		CircuitBreaker: CircuitBreakerStatus{State: CircuitBreakerPhaseTripped, Reason: "daily loss"},
	}
	got := BuildCockpitSummary([]domain.TradeDecision{td(domain.MarketTypeStock, domain.RiskDecisionApproved, domain.TradeDecisionStatusPaper, 1, 1)}, status, now)
	if !got.KillSwitchActive || !got.CircuitBreaker {
		t.Fatalf("active flags not set: %+v", got)
	}
	want := []string{"kill switch active", "circuit breaker tripped: daily loss"}
	if !reflect.DeepEqual(got.Warnings[:2], want) {
		t.Fatalf("warnings prefix = %+v want %+v", got.Warnings[:2], want)
	}
}

func TestBuildCockpitSummaryRejectedOnlyMarketWarning(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	got := BuildCockpitSummary([]domain.TradeDecision{
		td(domain.MarketTypeCrypto, domain.RiskDecisionRejected, domain.TradeDecisionStatusRejected, 0, 0),
	}, nil, now)
	if len(got.Warnings) != 1 || got.Warnings[0] != "market crypto has rejected decisions but no approved exposure" {
		t.Fatalf("warnings = %+v", got.Warnings)
	}
	if got.Exposures[1].RejectedDecisions != 1 {
		t.Fatalf("unexpected crypto exposure: %+v", got.Exposures[1])
	}
}
