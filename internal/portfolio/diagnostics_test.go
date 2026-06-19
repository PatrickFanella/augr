package portfolio

import (
	"math"
	"reflect"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestBuildDiagnosticsSummaryCountsAndClassification(t *testing.T) {
	t.Parallel()

	got := BuildDiagnosticsSummary(DiagnosticsInput{
		StrategyRuns: []RunDiagnostic{
			{Status: "Completed", Signal: "Hold", MarketType: domain.MarketTypeStock},
			{Status: "failed", Signal: "BUY", MarketType: domain.MarketTypeCrypto},
			{Status: "", Signal: "", MarketType: domain.MarketTypePolymarket},
		},
		TradeDecisions: []DecisionDiagnostic{
			{
				Status:      "Rejected",
				Signal:      "hold",
				RiskReasons: []string{"Risk_Rejected", "Sizing_Zero", "SELL_WITHOUT_POSITION"},
				Evidence: map[string]any{
					"Kill_Switch":  false,
					"Live_Gate":    false,
					"missing_data": true,
				},
			},
			{
				Status: "candidate",
				Signal: "",
			},
		},
		ActiveStrategiesByMarket: map[domain.MarketType]int{
			domain.MarketTypeStock:  2,
			domain.MarketTypeKalshi: 1,
		},
		OpenPositionsByMarket: map[domain.MarketType]int{
			domain.MarketTypeCrypto: 3,
		},
		BuyingPower:   50,
		Equity:        200,
		GrossExposure: 70,
	})

	wantRunSignals := map[string]int{"hold": 1, "buy": 1, "unknown": 1}
	if !reflect.DeepEqual(got.RunCountsBySignal, wantRunSignals) {
		t.Fatalf("run signal counts = %#v, want %#v", got.RunCountsBySignal, wantRunSignals)
	}

	wantRunStatus := map[string]int{"completed": 1, "failed": 1, "unknown": 1}
	if !reflect.DeepEqual(got.RunCountsByStatus, wantRunStatus) {
		t.Fatalf("run status counts = %#v, want %#v", got.RunCountsByStatus, wantRunStatus)
	}

	wantDecisionStatus := map[string]int{"rejected": 1, "candidate": 1}
	if !reflect.DeepEqual(got.DecisionCountsByStatus, wantDecisionStatus) {
		t.Fatalf("decision status counts = %#v, want %#v", got.DecisionCountsByStatus, wantDecisionStatus)
	}

	wantReasons := map[string]int{
		string(NoActionReasonHoldSignal):     2,
		string(NoActionReasonRiskRejected):   1,
		string(NoActionReasonSizingZero):     1,
		string(NoActionReasonSellWithoutPos): 1,
		string(NoActionReasonKillSwitch):     1,
		string(NoActionReasonLiveGateDenied): 1,
		string(NoActionReasonMissingData):    1,
		string(NoActionReasonUnknown):        1,
	}
	if !reflect.DeepEqual(got.NoActionReasons, wantReasons) {
		t.Fatalf("no-action reasons = %#v, want %#v", got.NoActionReasons, wantReasons)
	}

	wantActive := map[string]int{"stock": 2, "kalshi": 1}
	if !reflect.DeepEqual(got.ActiveStrategiesByMarket, wantActive) {
		t.Fatalf("active strategies = %#v, want %#v", got.ActiveStrategiesByMarket, wantActive)
	}

	wantOpen := map[string]int{"crypto": 3}
	if !reflect.DeepEqual(got.OpenPositionsByMarket, wantOpen) {
		t.Fatalf("open positions = %#v, want %#v", got.OpenPositionsByMarket, wantOpen)
	}

	assertNear(t, got.TargetGrossExposurePct, 0.35)
	assertNear(t, got.BuyingPowerUtilizationPct, 0.75)
	assertNear(t, got.GrossExposurePct, 0.35)
	assertNear(t, got.UtilizationGapPct, 0)
}

func TestBuildDiagnosticsSummaryUtilizationMath(t *testing.T) {
	t.Parallel()

	got := BuildDiagnosticsSummary(DiagnosticsInput{
		BuyingPower:            100,
		Equity:                 400,
		GrossExposure:          50,
		TargetGrossExposurePct: 0.5,
	})

	assertNear(t, got.TargetGrossExposurePct, 0.5)
	assertNear(t, got.BuyingPowerUtilizationPct, 0.75)
	assertNear(t, got.GrossExposurePct, 0.125)
	assertNear(t, got.UtilizationGapPct, 0.375)
}

func TestBuildDiagnosticsSummaryZeroEquityWarning(t *testing.T) {
	t.Parallel()

	got := BuildDiagnosticsSummary(DiagnosticsInput{Equity: 0, BuyingPower: 10, GrossExposure: 20})

	if len(got.Warnings) != 1 || got.Warnings[0] != "equity_non_positive" {
		t.Fatalf("warnings = %#v, want [equity_non_positive]", got.Warnings)
	}
	if got.BuyingPowerUtilizationPct != 0 {
		t.Fatalf("buying power utilization = %v, want 0", got.BuyingPowerUtilizationPct)
	}
	if got.GrossExposurePct != 0 {
		t.Fatalf("gross exposure pct = %v, want 0", got.GrossExposurePct)
	}
}

func assertNear(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %v, want %v", got, want)
	}
}
