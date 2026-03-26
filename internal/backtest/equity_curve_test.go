package backtest

import (
	"testing"
	"time"
)

func TestGenerateEquityCurveReportTracksDrawdownsAndRecoveries(t *testing.T) {
	t.Parallel()

	t1 := time.Date(2026, 3, 1, 9, 30, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	t3 := t2.Add(time.Hour)
	t4 := t3.Add(time.Hour)
	t5 := t4.Add(time.Hour)
	t6 := t5.Add(time.Hour)
	t7 := t6.Add(time.Hour)
	t8 := t7.Add(time.Hour)

	report := GenerateEquityCurveReport([]EquityPoint{
		{Timestamp: t1, Equity: 100, Cash: 100, TotalPnL: 0},
		{Timestamp: t2, Equity: 110, Cash: 110, TotalPnL: 10},
		{Timestamp: t3, Equity: 105, Cash: 105, TotalPnL: 5},
		{Timestamp: t4, Equity: 90, Cash: 90, TotalPnL: -10},
		{Timestamp: t5, Equity: 100, Cash: 100, TotalPnL: 0},
		{Timestamp: t6, Equity: 110, Cash: 110, TotalPnL: 10},
		{Timestamp: t7, Equity: 120, Cash: 120, TotalPnL: 20},
		{Timestamp: t8, Equity: 108, Cash: 108, TotalPnL: 8},
	})

	if len(report.Points) != 8 {
		t.Fatalf("len(Points) = %d, want 8", len(report.Points))
	}
	if len(report.DrawdownPeriods) != 2 {
		t.Fatalf("len(DrawdownPeriods) = %d, want 2", len(report.DrawdownPeriods))
	}

	point := report.Points[3]
	if !point.Timestamp.Equal(t4) {
		t.Fatalf("Points[3].Timestamp = %v, want %v", point.Timestamp, t4)
	}
	assertFloatEqual(t, point.PortfolioValue, 90, "Points[3].PortfolioValue")
	assertFloatEqual(t, point.PeakEquity, 110, "Points[3].PeakEquity")
	assertFloatEqual(t, point.DrawdownValue, 20, "Points[3].DrawdownValue")
	assertFloatEqual(t, point.DrawdownPct, 20.0/110.0, "Points[3].DrawdownPct")
	if point.DrawdownDuration != 2*time.Hour {
		t.Fatalf("Points[3].DrawdownDuration = %s, want %s", point.DrawdownDuration, 2*time.Hour)
	}

	first := report.DrawdownPeriods[0]
	if !first.StartTimestamp.Equal(t2) {
		t.Fatalf("DrawdownPeriods[0].StartTimestamp = %v, want %v", first.StartTimestamp, t2)
	}
	if !first.TroughTimestamp.Equal(t4) {
		t.Fatalf("DrawdownPeriods[0].TroughTimestamp = %v, want %v", first.TroughTimestamp, t4)
	}
	if first.RecoveryTimestamp == nil || !first.RecoveryTimestamp.Equal(t6) {
		t.Fatalf("DrawdownPeriods[0].RecoveryTimestamp = %v, want %v", first.RecoveryTimestamp, t6)
	}
	assertFloatEqual(t, first.PeakEquity, 110, "DrawdownPeriods[0].PeakEquity")
	assertFloatEqual(t, first.TroughEquity, 90, "DrawdownPeriods[0].TroughEquity")
	if first.RecoveryEquity == nil {
		t.Fatal("DrawdownPeriods[0].RecoveryEquity = nil, want non-nil")
	}
	assertFloatEqual(t, *first.RecoveryEquity, 110, "DrawdownPeriods[0].RecoveryEquity")
	assertFloatEqual(t, first.DepthValue, 20, "DrawdownPeriods[0].DepthValue")
	assertFloatEqual(t, first.DepthPct, 20.0/110.0, "DrawdownPeriods[0].DepthPct")
	if first.Duration != 4*time.Hour {
		t.Fatalf("DrawdownPeriods[0].Duration = %s, want %s", first.Duration, 4*time.Hour)
	}

	second := report.DrawdownPeriods[1]
	if !second.StartTimestamp.Equal(t7) {
		t.Fatalf("DrawdownPeriods[1].StartTimestamp = %v, want %v", second.StartTimestamp, t7)
	}
	if !second.TroughTimestamp.Equal(t8) {
		t.Fatalf("DrawdownPeriods[1].TroughTimestamp = %v, want %v", second.TroughTimestamp, t8)
	}
	if second.RecoveryTimestamp != nil {
		t.Fatalf("DrawdownPeriods[1].RecoveryTimestamp = %v, want nil", second.RecoveryTimestamp)
	}
	assertFloatEqual(t, second.PeakEquity, 120, "DrawdownPeriods[1].PeakEquity")
	assertFloatEqual(t, second.TroughEquity, 108, "DrawdownPeriods[1].TroughEquity")
	assertFloatEqual(t, second.DepthValue, 12, "DrawdownPeriods[1].DepthValue")
	assertFloatEqual(t, second.DepthPct, 0.1, "DrawdownPeriods[1].DepthPct")
	if second.Duration != time.Hour {
		t.Fatalf("DrawdownPeriods[1].Duration = %s, want %s", second.Duration, time.Hour)
	}
}

func TestGenerateEquityCurveReportHandlesEmptyCurve(t *testing.T) {
	t.Parallel()

	report := GenerateEquityCurveReport(nil)
	if len(report.Points) != 0 {
		t.Fatalf("len(Points) = %d, want 0", len(report.Points))
	}
	if len(report.DrawdownPeriods) != 0 {
		t.Fatalf("len(DrawdownPeriods) = %d, want 0", len(report.DrawdownPeriods))
	}
}
