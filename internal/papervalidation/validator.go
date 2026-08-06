package papervalidation

import (
	"math"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
)

// DefaultThresholds returns the standard validation thresholds defined in the
// 60-day paper trading validation plan.
func DefaultThresholds() Thresholds {
	return Thresholds{
		MinSharpeRatio:     1.0,
		MaxDrawdown:        0.15,
		MinWinRate:         0.40,
		MinProfitFactor:    1.5,
		MinRoundTripTrades: 20,
		MinCalendarDays:    60,
	}
}

// Thresholds holds the configurable pass/fail criteria for paper trading
// validation.
type Thresholds struct {
	MinSharpeRatio     float64 `json:"min_sharpe_ratio"`
	MaxDrawdown        float64 `json:"max_drawdown"`
	MinWinRate         float64 `json:"min_win_rate"`
	MinProfitFactor    float64 `json:"min_profit_factor"`
	MinRoundTripTrades int     `json:"min_round_trip_trades"`
	MinCalendarDays    int     `json:"min_calendar_days"`
}

// MetricResult captures a single metric's current value, required threshold,
// and whether the threshold is met.
type MetricResult struct {
	Name      string  `json:"name"`
	Value     float64 `json:"value"`
	Threshold float64 `json:"threshold"`
	Passed    bool    `json:"passed"`
	// NonFinite preserves the source meaning when Value must use a finite JSON
	// sentinel. NaN always fails closed; positive/negative infinity retain the
	// comparison result while serializing as +/-MaxFloat64.
	NonFinite string `json:"non_finite,omitempty"`
}

// ValidationResult is the outcome of evaluating paper trading performance
// against the configured thresholds.
type ValidationResult struct {
	Metrics []MetricResult `json:"metrics"`
	// AllPassed indicates that all *metric thresholds* have passed. It does not
	// include the calendar-day requirement; use GoDecision to determine the final
	// overall validation decision.
	AllPassed    bool `json:"all_passed"`
	GoDecision   bool `json:"go_decision"`
	ElapsedDays  int  `json:"elapsed_days"`
	DaysRequired int  `json:"days_required"`
}

// Validate evaluates the given performance metrics and trade analytics against
// the thresholds. The paperStart time is used together with now to determine
// whether the minimum calendar-day requirement has been satisfied. A GO
// decision requires every individual metric to pass and the minimum elapsed
// days to be reached.
func Validate(metrics backtest.Metrics, analytics backtest.TradeAnalytics, thresholds Thresholds, paperStart, now time.Time) ValidationResult {
	elapsed := 0
	if !paperStart.IsZero() && !now.IsZero() && !now.Before(paperStart) {
		// Compute elapsed calendar days by normalizing to UTC midnight to avoid
		// DST/timezone effects and partial-day truncation issues.
		startUTC := paperStart.UTC()
		nowUTC := now.UTC()
		startDate := time.Date(startUTC.Year(), startUTC.Month(), startUTC.Day(), 0, 0, 0, 0, time.UTC)
		nowDate := time.Date(nowUTC.Year(), nowUTC.Month(), nowUTC.Day(), 0, 0, 0, 0, time.UTC)
		elapsed = int(nowDate.Sub(startDate).Hours() / 24)
	}

	results := []MetricResult{
		newMetricResult("sharpe_ratio", metrics.SharpeRatio, thresholds.MinSharpeRatio, metrics.SharpeRatio > thresholds.MinSharpeRatio),
		newMetricResult("max_drawdown", metrics.MaxDrawdown, thresholds.MaxDrawdown, metrics.MaxDrawdown < thresholds.MaxDrawdown),
		newMetricResult("win_rate", analytics.WinRate, thresholds.MinWinRate, analytics.WinRate > thresholds.MinWinRate),
		newMetricResult("profit_factor", analytics.ProfitFactor, thresholds.MinProfitFactor, analytics.ProfitFactor > thresholds.MinProfitFactor),
		newMetricResult("round_trip_trades", float64(analytics.ClosedTrades), float64(thresholds.MinRoundTripTrades), analytics.ClosedTrades >= thresholds.MinRoundTripTrades),
	}

	allPassed := true
	for _, r := range results {
		if !r.Passed {
			allPassed = false
			break
		}
	}

	daysOK := elapsed >= thresholds.MinCalendarDays
	goDecision := allPassed && daysOK

	return ValidationResult{
		Metrics:      results,
		AllPassed:    allPassed,
		GoDecision:   goDecision,
		ElapsedDays:  elapsed,
		DaysRequired: thresholds.MinCalendarDays,
	}
}

func newMetricResult(name string, value, threshold float64, passed bool) MetricResult {
	result := MetricResult{Name: name, Value: value, Threshold: threshold, Passed: passed}
	switch {
	case math.IsNaN(value):
		result.Value = 0
		result.Passed = false
		result.NonFinite = "nan"
	case math.IsInf(value, 1):
		result.Value = math.MaxFloat64
		result.NonFinite = "positive_infinity"
	case math.IsInf(value, -1):
		result.Value = -math.MaxFloat64
		result.NonFinite = "negative_infinity"
	}
	return result
}
