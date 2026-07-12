package calibration

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/edge"
)

func TestBuildStrategyCalibrationReport_EmptyInput(t *testing.T) {
	t.Parallel()

	report := BuildStrategyCalibrationReport(StrategyCalibrationInput{})

	if report.SampleCount != 0 {
		t.Fatalf("SampleCount = %d, want 0", report.SampleCount)
	}
	if report.BrierScore != 0 {
		t.Fatalf("BrierScore = %v, want 0", report.BrierScore)
	}
	if report.LogLoss != 0 {
		t.Fatalf("LogLoss = %v, want 0", report.LogLoss)
	}
	if len(report.CalibrationBuckets) != defaultCalibrationBucketCount {
		t.Fatalf("len(CalibrationBuckets) = %d, want %d", len(report.CalibrationBuckets), defaultCalibrationBucketCount)
	}
	if report.AcceptedCount != 0 || report.RejectedCount != 0 {
		t.Fatalf("counts = (%d,%d), want (0,0)", report.AcceptedCount, report.RejectedCount)
	}
	if report.RejectionReasonCounts != nil {
		t.Fatalf("RejectionReasonCounts = %#v, want nil", report.RejectionReasonCounts)
	}
	if report.TimeRange != nil {
		t.Fatalf("TimeRange = %#v, want nil", report.TimeRange)
	}
	if report.RealizedPnLSummary != (RealizedPnLSummary{}) {
		t.Fatalf("RealizedPnLSummary = %#v, want zero value", report.RealizedPnLSummary)
	}
	assertFiniteReport(t, report)
}

func TestBuildSegmentedStrategyCalibrationReportsGroupsRequiredDimensions(t *testing.T) {
	reports := BuildSegmentedStrategyCalibrationReports(StrategyCalibrationInput{
		StrategyKey: "s", MarketType: domain.MarketTypePolymarket,
		Samples: []StrategyCalibrationSample{
			{Probability: .64, Outcome: true, Provider: "openai", Regime: "risk_on"},
			{Probability: .68, Outcome: false, Provider: "openai", Regime: "risk_on"},
			{Probability: .82, Outcome: true, Provider: "rules", Regime: "risk_off"},
		},
	})
	if len(reports) != 2 {
		t.Fatalf("reports = %d, want 2", len(reports))
	}
	if reports[0].Provider != "openai" || reports[0].Regime != "risk_on" || reports[0].ConfidenceBucket != "60-70%" || reports[0].SampleCount != 2 {
		t.Fatalf("first segment = %+v", reports[0])
	}
	if reports[1].Provider != "rules" || reports[1].Regime != "risk_off" || reports[1].ConfidenceBucket != "80-90%" {
		t.Fatalf("second segment = %+v", reports[1])
	}
}

func TestBuildStrategyCalibrationReport_BasicData(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	start := time.Date(2026, 6, 8, 15, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)

	report := BuildStrategyCalibrationReport(StrategyCalibrationInput{
		StrategyID:  &strategyID,
		StrategyKey: " alpha ",
		MarketType:  domain.MarketTypeOptions,
		GeneratedAt: start,
		Samples: []StrategyCalibrationSample{
			{Probability: 0.8, Outcome: true, RealizedPnL: 10, Accepted: true, Timestamp: start},
			{Probability: 0.3, Outcome: false, RealizedPnL: -4, Accepted: true, Timestamp: end},
		},
	})

	if report.StrategyID == nil || *report.StrategyID != strategyID {
		t.Fatalf("StrategyID = %#v, want %s", report.StrategyID, strategyID)
	}
	if report.StrategyKey != "alpha" {
		t.Fatalf("StrategyKey = %q, want %q", report.StrategyKey, "alpha")
	}
	if report.MarketType != domain.MarketTypeOptions {
		t.Fatalf("MarketType = %q, want %q", report.MarketType, domain.MarketTypeOptions)
	}
	if report.SampleCount != 2 {
		t.Fatalf("SampleCount = %d, want 2", report.SampleCount)
	}
	if report.AcceptedCount != 2 || report.RejectedCount != 0 {
		t.Fatalf("counts = (%d,%d), want (2,0)", report.AcceptedCount, report.RejectedCount)
	}
	if report.RealizedPnLSummary.Count != 2 || report.RealizedPnLSummary.Sum != 6 || report.RealizedPnLSummary.Mean != 3 || report.RealizedPnLSummary.Min != -4 || report.RealizedPnLSummary.Max != 10 {
		t.Fatalf("RealizedPnLSummary = %#v, want sum=6 mean=3 min=-4 max=10", report.RealizedPnLSummary)
	}
	if report.TimeRange == nil || !report.TimeRange.Start.Equal(start) || !report.TimeRange.End.Equal(end) {
		t.Fatalf("TimeRange = %#v, want [%s,%s]", report.TimeRange, start, end)
	}

	wantOutcomes := []edge.ProbabilityOutcome{{P: 0.8, Outcome: true}, {P: 0.3, Outcome: false}}
	if got, want := report.BrierScore, edge.BrierScore(wantOutcomes); !almostEqual(got, want, 1e-12) {
		t.Fatalf("BrierScore = %v, want %v", got, want)
	}
	if got, want := report.LogLoss, edge.LogLoss(wantOutcomes); !almostEqual(got, want, 1e-12) {
		t.Fatalf("LogLoss = %v, want %v", got, want)
	}
	assertFiniteReport(t, report)
}

func TestBuildStrategyCalibrationReport_RejectionReasonCounts(t *testing.T) {
	t.Parallel()

	report := BuildStrategyCalibrationReport(StrategyCalibrationInput{
		Samples: []StrategyCalibrationSample{
			{Probability: 0.6, Outcome: true, Accepted: true},
			{Probability: 0.4, Outcome: false, Accepted: false, RejectionReason: "fill_rate_collapse"},
			{Probability: 0.2, Outcome: false, Accepted: false, RejectionReason: "fill_rate_collapse"},
			{Probability: 0.1, Outcome: false, Accepted: false, RejectionReason: "api_latency_spike"},
		},
	})

	if report.AcceptedCount != 1 || report.RejectedCount != 3 {
		t.Fatalf("counts = (%d,%d), want (1,3)", report.AcceptedCount, report.RejectedCount)
	}
	if got := report.RejectionReasonCounts["fill_rate_collapse"]; got != 2 {
		t.Fatalf("fill_rate_collapse count = %d, want 2", got)
	}
	if got := report.RejectionReasonCounts["api_latency_spike"]; got != 1 {
		t.Fatalf("api_latency_spike count = %d, want 1", got)
	}
	assertFiniteReport(t, report)
}

func TestBuildStrategyCalibrationReport_SanitizesNonFiniteInputs(t *testing.T) {
	t.Parallel()

	report := BuildStrategyCalibrationReport(StrategyCalibrationInput{
		Samples: []StrategyCalibrationSample{
			{Probability: math.NaN(), Outcome: true, RealizedPnL: math.NaN(), Accepted: true, RejectionReason: "win_rate_below_floor"},
			{Probability: math.Inf(1), Outcome: false, RealizedPnL: math.Inf(1), Accepted: false, RejectionReason: "liquidity_disappeared"},
			{Probability: math.Inf(-1), Outcome: true, RealizedPnL: math.Inf(-1), Accepted: false, RejectionReason: "liquidity_disappeared"},
			{Probability: 1.4, Outcome: true, RealizedPnL: 5, Accepted: true, RejectionReason: "api_latency_spike"},
		},
	})

	if report.SampleCount != 1 {
		t.Fatalf("SampleCount = %d, want 1", report.SampleCount)
	}
	if report.RealizedPnLSummary.Count != 1 || report.RealizedPnLSummary.Sum != 5 || report.RealizedPnLSummary.Mean != 5 {
		t.Fatalf("RealizedPnLSummary = %#v, want count=1 sum=5 mean=5", report.RealizedPnLSummary)
	}
	if math.IsNaN(report.BrierScore) || math.IsInf(report.BrierScore, 0) {
		t.Fatalf("BrierScore is not finite: %v", report.BrierScore)
	}
	if math.IsNaN(report.LogLoss) || math.IsInf(report.LogLoss, 0) {
		t.Fatalf("LogLoss is not finite: %v", report.LogLoss)
	}
	assertFiniteReport(t, report)
}

func TestBuildStrategyCalibrationReport_SkipsOverflowingPnLValues(t *testing.T) {
	t.Parallel()

	report := BuildStrategyCalibrationReport(StrategyCalibrationInput{
		Samples: []StrategyCalibrationSample{
			{Probability: 0.4, Outcome: true, RealizedPnL: math.MaxFloat64, Accepted: true},
			{Probability: 0.6, Outcome: false, RealizedPnL: math.MaxFloat64, Accepted: true},
			{Probability: 0.2, Outcome: false, RealizedPnL: -math.MaxFloat64, Accepted: true},
		},
	})

	if report.RealizedPnLSummary.Count != 2 {
		t.Fatalf("RealizedPnLSummary.Count = %d, want 2", report.RealizedPnLSummary.Count)
	}
	if report.RealizedPnLSummary.Sum != 0 || report.RealizedPnLSummary.Mean != 0 {
		t.Fatalf("RealizedPnLSummary = %#v, want sum=0 mean=0", report.RealizedPnLSummary)
	}
	if report.RealizedPnLSummary.Min != -math.MaxFloat64 || report.RealizedPnLSummary.Max != math.MaxFloat64 {
		t.Fatalf("RealizedPnLSummary = %#v, want finite min/max", report.RealizedPnLSummary)
	}
	assertFiniteReport(t, report)

	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("json.Marshal(report) failed: %v", err)
	}
}

func TestBuildStrategyCalibrationReport_BucketBehavior(t *testing.T) {
	t.Parallel()

	report := BuildStrategyCalibrationReport(StrategyCalibrationInput{
		BucketCount: 4,
		Samples: []StrategyCalibrationSample{
			{Probability: 0, Outcome: false, Accepted: true},
			{Probability: 0.25, Outcome: true, Accepted: true},
			{Probability: 0.5, Outcome: false, Accepted: true},
			{Probability: 0.75, Outcome: true, Accepted: true},
			{Probability: 1, Outcome: true, Accepted: true},
		},
	})

	if len(report.CalibrationBuckets) != 4 {
		t.Fatalf("len(CalibrationBuckets) = %d, want 4", len(report.CalibrationBuckets))
	}
	wantCounts := []int{1, 1, 1, 2}
	for i, want := range wantCounts {
		if got := report.CalibrationBuckets[i].Count; got != want {
			t.Fatalf("bucket %d count = %d, want %d", i, got, want)
		}
	}
	wantBounds := []struct{ lower, upper float64 }{
		{0, 0.25},
		{0.25, 0.5},
		{0.5, 0.75},
		{0.75, 1},
	}
	for i, want := range wantBounds {
		if !almostEqual(report.CalibrationBuckets[i].Lower, want.lower, 1e-12) || !almostEqual(report.CalibrationBuckets[i].Upper, want.upper, 1e-12) {
			t.Fatalf("bucket %d bounds = (%v,%v), want (%v,%v)", i, report.CalibrationBuckets[i].Lower, report.CalibrationBuckets[i].Upper, want.lower, want.upper)
		}
	}
	assertFiniteReport(t, report)
}

func almostEqual(a, b, tolerance float64) bool {
	if math.IsNaN(a) || math.IsNaN(b) {
		return false
	}
	if math.IsInf(a, 0) || math.IsInf(b, 0) {
		return false
	}
	delta := math.Abs(a - b)
	return delta <= tolerance
}

func assertFiniteReport(t *testing.T, report StrategyCalibrationReport) {
	t.Helper()

	for _, v := range []float64{report.BrierScore, report.LogLoss, report.RealizedPnLSummary.Sum, report.RealizedPnLSummary.Mean, report.RealizedPnLSummary.Min, report.RealizedPnLSummary.Max} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("report contains non-finite value: %v", v)
		}
	}
	for _, bucket := range report.CalibrationBuckets {
		for _, v := range []float64{bucket.Lower, bucket.Upper, bucket.AvgForecast, bucket.HitRate} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Fatalf("bucket contains non-finite value: %v", v)
			}
		}
	}
	if report.RejectionReasonCounts != nil {
		for reason, count := range report.RejectionReasonCounts {
			if reason == "" || count < 0 {
				t.Fatalf("invalid rejection reason count: %q=%d", reason, count)
			}
		}
	}
}
