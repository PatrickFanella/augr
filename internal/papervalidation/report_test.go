package papervalidation

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
)

func TestGenerateReportGoDecision(t *testing.T) {
	t.Parallel()

	metrics := backtest.Metrics{
		SharpeRatio: 1.5,
		MaxDrawdown: 0.10,
	}
	analytics := backtest.TradeAnalytics{ClosedTrades: 25, WinRate: 0.55, ProfitFactor: 2.0}
	th := DefaultThresholds()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reportDate := start.Add(61 * 24 * time.Hour)

	report := GenerateReport(metrics, analytics, th, start, reportDate)

	if report.Decision != "GO" {
		t.Errorf("Decision = %q, want %q", report.Decision, "GO")
	}
	if !report.GoDecision {
		t.Error("GoDecision = false, want true")
	}
	if report.TransitionPlan == nil {
		t.Fatal("TransitionPlan is nil, want non-nil for GO decision")
	}
	if report.TransitionPlan.InitialPositionPct != 25.0 {
		t.Errorf("InitialPositionPct = %f, want 25.0", report.TransitionPlan.InitialPositionPct)
	}
	if report.DaysRemaining != 0 {
		t.Errorf("DaysRemaining = %d, want 0", report.DaysRemaining)
	}
	if !report.ReportDate.Equal(reportDate) {
		t.Errorf("ReportDate = %v, want %v", report.ReportDate, reportDate)
	}
	if !report.PaperStartDate.Equal(start) {
		t.Errorf("PaperStartDate = %v, want %v", report.PaperStartDate, start)
	}
}

func TestGenerateReportNoGoDecision(t *testing.T) {
	t.Parallel()

	metrics := backtest.Metrics{
		SharpeRatio: 0.8,
		MaxDrawdown: 0.10,
	}
	analytics := backtest.TradeAnalytics{ClosedTrades: 25, WinRate: 0.55, ProfitFactor: 2.0}
	th := DefaultThresholds()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reportDate := start.Add(61 * 24 * time.Hour)

	report := GenerateReport(metrics, analytics, th, start, reportDate)

	if report.Decision != "NO-GO" {
		t.Errorf("Decision = %q, want %q", report.Decision, "NO-GO")
	}
	if report.GoDecision {
		t.Error("GoDecision = true, want false")
	}
	if report.TransitionPlan != nil {
		t.Error("TransitionPlan should be nil for NO-GO decision")
	}
}

func TestGenerateReportDaysRemaining(t *testing.T) {
	t.Parallel()

	metrics := backtest.Metrics{
		SharpeRatio: 1.5,
		MaxDrawdown: 0.10,
	}
	analytics := backtest.TradeAnalytics{ClosedTrades: 25, WinRate: 0.55, ProfitFactor: 2.0}
	th := DefaultThresholds()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reportDate := start.Add(30 * 24 * time.Hour)

	report := GenerateReport(metrics, analytics, th, start, reportDate)

	if report.DaysRemaining != 30 {
		t.Errorf("DaysRemaining = %d, want 30", report.DaysRemaining)
	}
	if report.ElapsedDays != 30 {
		t.Errorf("ElapsedDays = %d, want 30", report.ElapsedDays)
	}
}

func TestGenerateReportJSONRoundTrip(t *testing.T) {
	t.Parallel()

	metrics := backtest.Metrics{
		SharpeRatio: 1.5,
		MaxDrawdown: 0.10,
	}
	analytics := backtest.TradeAnalytics{ClosedTrades: 25, WinRate: 0.55, ProfitFactor: 2.0}
	th := DefaultThresholds()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reportDate := start.Add(61 * 24 * time.Hour)

	report := GenerateReport(metrics, analytics, th, start, reportDate)

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded ValidationReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if decoded.Decision != report.Decision {
		t.Errorf("decoded Decision = %q, want %q", decoded.Decision, report.Decision)
	}
	if decoded.GoDecision != report.GoDecision {
		t.Errorf("decoded GoDecision = %v, want %v", decoded.GoDecision, report.GoDecision)
	}
	if len(decoded.Metrics) != len(report.Metrics) {
		t.Errorf("decoded Metrics count = %d, want %d", len(decoded.Metrics), len(report.Metrics))
	}
	if decoded.TransitionPlan == nil {
		t.Fatal("decoded TransitionPlan is nil")
	}
	if decoded.TransitionPlan.InitialPositionPct != 25.0 {
		t.Errorf("decoded InitialPositionPct = %f, want 25.0", decoded.TransitionPlan.InitialPositionPct)
	}
}

func TestGenerateReportMetricDetails(t *testing.T) {
	t.Parallel()

	metrics := backtest.Metrics{
		SharpeRatio: 1.5,
		MaxDrawdown: 0.20,
	}
	analytics := backtest.TradeAnalytics{ClosedTrades: 25, WinRate: 0.55, ProfitFactor: 2.0}
	th := DefaultThresholds()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reportDate := start.Add(61 * 24 * time.Hour)

	report := GenerateReport(metrics, analytics, th, start, reportDate)

	if report.AllMetricsPassed {
		t.Error("AllMetricsPassed = true, want false (drawdown too high)")
	}

	for _, m := range report.Metrics {
		switch m.Name {
		case "max_drawdown":
			if m.Passed {
				t.Error("max_drawdown should fail when value 0.20 >= threshold 0.15")
			}
			if m.Value != 0.20 {
				t.Errorf("max_drawdown value = %f, want 0.20", m.Value)
			}
		case "sharpe_ratio":
			if !m.Passed {
				t.Error("sharpe_ratio should pass")
			}
		}
	}
}

func TestGenerateReportSerializesInfiniteProfitFactor(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	report := GenerateReport(
		backtest.Metrics{SharpeRatio: 1.5, MaxDrawdown: 0.10},
		backtest.TradeAnalytics{ClosedTrades: 25, WinRate: 0.55, ProfitFactor: math.Inf(1)},
		DefaultThresholds(),
		start,
		start.Add(61*24*time.Hour),
	)

	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, metric := range report.Metrics {
		if metric.Name != "profit_factor" {
			continue
		}
		if metric.NonFinite != "positive_infinity" || metric.Value != math.MaxFloat64 || !metric.Passed {
			t.Fatalf("profit factor metric = %+v, want serializable positive infinity", metric)
		}
		return
	}
	t.Fatal("profit_factor metric missing")
}

func TestGenerateReportFailsNaNMetricClosed(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	report := GenerateReport(
		backtest.Metrics{SharpeRatio: 1.5, MaxDrawdown: 0.10},
		backtest.TradeAnalytics{ClosedTrades: 25, WinRate: math.NaN(), ProfitFactor: 2},
		DefaultThresholds(),
		start,
		start.Add(61*24*time.Hour),
	)

	if report.GoDecision {
		t.Fatal("NaN input must fail closed")
	}
	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, metric := range report.Metrics {
		if metric.Name == "win_rate" && (metric.NonFinite != "nan" || metric.Value != 0 || metric.Passed) {
			t.Fatalf("win rate metric = %+v, want explicit failed NaN", metric)
		}
	}
}
