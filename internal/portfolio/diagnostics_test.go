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
		TotalStrategyRuns:    3,
		TotalTradeDecisions:  2,
		TotalStrategies:      4,
		TotalOpenPositions:   1,
		SampleStrategyRuns:   3,
		SampleTradeDecisions: 2,
		SampleStrategies:     2,
		SampleOpenPositions:  1,
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
		RunCountsBySignal:      map[string]int{"hold": 1, "buy": 1, "unknown": 1},
		RunCountsByStatus:      map[string]int{"completed": 1, "failed": 1, "unknown": 1},
		DecisionCountsByStatus: map[string]int{"rejected": 1, "candidate": 1},
		NoActionReasons:        map[string]int{string(NoActionReasonHoldSignal): 2, string(NoActionReasonRiskRejected): 1, string(NoActionReasonSizingZero): 1, string(NoActionReasonSellWithoutPos): 1, string(NoActionReasonKillSwitch): 1, string(NoActionReasonLiveGateDenied): 1, string(NoActionReasonMissingData): 1, string(NoActionReasonUnknown): 1},
		BuyingPower:            50,
		Equity:                 200,
		GrossExposure:          70,
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
	if got.TotalStrategyRuns != 3 || got.TotalTradeDecisions != 2 || got.TotalStrategies != 4 || got.TotalOpenPositions != 1 {
		t.Fatalf("unexpected totals: %+v", got)
	}
	if got.SampleStrategyRuns != 3 || got.SampleTradeDecisions != 2 || got.SampleStrategies != 2 || got.SampleOpenPositions != 1 {
		t.Fatalf("unexpected samples: %+v", got)
	}

	assertNear(t, got.TargetGrossExposurePct, 0.35)
	assertNear(t, got.BuyingPowerUtilizationPct, 0.75)
	assertNear(t, got.GrossExposurePct, 0.35)
	assertNear(t, got.UtilizationGapPct, 0)
}

func TestBuildDiagnosticsSummaryUtilizationMath(t *testing.T) {
	t.Parallel()
	profile, err := domain.NewPaperEvaluationProfile(domain.PaperEvaluationModeStress, 5_000_000, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	got := BuildDiagnosticsSummary(DiagnosticsInput{
		BuyingPower:            100,
		Equity:                 400,
		GrossExposure:          50,
		TargetGrossExposurePct: 0.5,
		PaperEvaluation:        &profile,
	})

	assertNear(t, got.TargetGrossExposurePct, 0.5)
	assertNear(t, got.BuyingPowerUtilizationPct, 0.75)
	assertNear(t, got.GrossExposurePct, 0.125)
	assertNear(t, got.UtilizationGapPct, 0.375)
	if got.PaperEvaluation.Mode != string(domain.PaperEvaluationModeStress) || got.PaperEvaluation.PromotionEligible {
		t.Fatalf("paper evaluation = %+v, want non-promotable stress evidence", got.PaperEvaluation)
	}
}

func TestBuildDiagnosticsSummaryZeroEquityWarning(t *testing.T) {
	t.Parallel()

	got := BuildDiagnosticsSummary(DiagnosticsInput{Equity: 0, AccountBalanceAvailable: true, BuyingPower: 10, GrossExposure: 20})

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

func TestBuildDiagnosticsSummarySkipsEquityWarningWhenAccountBalanceUnavailable(t *testing.T) {
	t.Parallel()

	got := BuildDiagnosticsSummary(DiagnosticsInput{Equity: 0, BuyingPower: 0, GrossExposure: 0})

	if len(got.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", got.Warnings)
	}
	assertNear(t, got.TargetGrossExposurePct, 0.35)
	assertNear(t, got.UtilizationGapPct, 0.35)
}

func assertNear(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %v, want %v", got, want)
	}
}
