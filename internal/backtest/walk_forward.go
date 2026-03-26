package backtest

import (
	"context"
	"fmt"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// WalkForwardConfig configures rolling walk-forward analysis.
type WalkForwardConfig struct {
	CalibrationMonths int
	TestMonths        int
}

// WalkForwardWindow defines one calibration/test partition.
type WalkForwardWindow struct {
	CalibrationStart        time.Time
	CalibrationEndExclusive time.Time
	TestStart               time.Time
	TestEndExclusive        time.Time
}

// WalkForwardWindowResult contains per-window run results.
type WalkForwardWindowResult struct {
	Window      WalkForwardWindow
	Calibration *OrchestratorResult
	Test        *OrchestratorResult
}

// WalkForwardResult contains all window results and aggregated out-of-sample metrics.
type WalkForwardResult struct {
	Config      WalkForwardConfig
	Windows     []WalkForwardWindowResult
	OutOfSample AggregatedMetrics
}

// RunWalkForward executes rolling walk-forward analysis by calibrating on N
// months and testing on the subsequent M months, then advancing by M months.
func (o *Orchestrator) RunWalkForward(ctx context.Context, cfg WalkForwardConfig) (*WalkForwardResult, error) {
	windows, err := generateWalkForwardWindows(o.config.StartDate, o.config.EndDate, cfg)
	if err != nil {
		return nil, err
	}

	results := make([]WalkForwardWindowResult, 0, len(windows))
	testMetrics := make([]Metrics, 0, len(windows))

	for i, window := range windows {
		calibrationResult, err := o.runWindow(ctx, window.CalibrationStart, window.CalibrationEndExclusive)
		if err != nil {
			return nil, fmt.Errorf("backtest: calibration run failed for window %d: %w", i+1, err)
		}

		testResult, err := o.runWindow(ctx, window.TestStart, window.TestEndExclusive)
		if err != nil {
			return nil, fmt.Errorf("backtest: test run failed for window %d: %w", i+1, err)
		}

		results = append(results, WalkForwardWindowResult{
			Window:      window,
			Calibration: calibrationResult,
			Test:        testResult,
		})
		testMetrics = append(testMetrics, testResult.Metrics)
	}

	return &WalkForwardResult{
		Config:      cfg,
		Windows:     results,
		OutOfSample: aggregateMetrics(testMetrics),
	}, nil
}

func (o *Orchestrator) runWindow(
	ctx context.Context,
	startInclusive time.Time,
	endExclusive time.Time,
) (*OrchestratorResult, error) {
	windowBars := filterBarsHalfOpen(o.bars, startInclusive, endExclusive)
	if len(windowBars) == 0 {
		return nil, fmt.Errorf("backtest: no bars in walk-forward window %s to %s", startInclusive, endExclusive)
	}

	windowCfg := o.config
	windowCfg.StartDate = windowBars[0].Timestamp
	windowCfg.EndDate = windowBars[len(windowBars)-1].Timestamp

	windowOrchestrator, err := NewOrchestrator(
		windowCfg,
		windowBars,
		o.pipeline,
		o.logger,
		o.clockTargets...,
	)
	if err != nil {
		return nil, fmt.Errorf("backtest: creating window orchestrator: %w", err)
	}

	return windowOrchestrator.Run(ctx)
}

func generateWalkForwardWindows(start, end time.Time, cfg WalkForwardConfig) ([]WalkForwardWindow, error) {
	if cfg.CalibrationMonths <= 0 {
		return nil, fmt.Errorf("backtest: calibration months must be > 0")
	}
	if cfg.TestMonths <= 0 {
		return nil, fmt.Errorf("backtest: test months must be > 0")
	}
	if start.IsZero() {
		return nil, fmt.Errorf("backtest: start date is required")
	}
	if end.IsZero() {
		return nil, fmt.Errorf("backtest: end date is required")
	}
	if end.Before(start) {
		return nil, fmt.Errorf("backtest: end date must not be before start date")
	}

	endExclusive := end.AddDate(0, 0, 1)
	cursor := start
	windows := make([]WalkForwardWindow, 0)

	for {
		calibrationEndExclusive := addMonthsClamped(cursor, cfg.CalibrationMonths)
		testStart := calibrationEndExclusive
		testEndExclusive := addMonthsClamped(testStart, cfg.TestMonths)

		if testEndExclusive.After(endExclusive) {
			break
		}

		windows = append(windows, WalkForwardWindow{
			CalibrationStart:        cursor,
			CalibrationEndExclusive: calibrationEndExclusive,
			TestStart:               testStart,
			TestEndExclusive:        testEndExclusive,
		})

		cursor = addMonthsClamped(cursor, cfg.TestMonths)
	}

	if len(windows) == 0 {
		return nil, fmt.Errorf(
			"backtest: no valid walk-forward windows for range %s to %s with calibration=%d months and test=%d months",
			start.Format(time.DateOnly),
			end.Format(time.DateOnly),
			cfg.CalibrationMonths,
			cfg.TestMonths,
		)
	}

	return windows, nil
}

func filterBarsHalfOpen(bars []domain.OHLCV, startInclusive, endExclusive time.Time) []domain.OHLCV {
	filtered := make([]domain.OHLCV, 0, len(bars))
	for _, bar := range bars {
		if !bar.Timestamp.Before(startInclusive) && bar.Timestamp.Before(endExclusive) {
			filtered = append(filtered, bar)
		}
	}
	return filtered
}

func addMonthsClamped(ts time.Time, months int) time.Time {
	if months == 0 {
		return ts
	}
	if months < 0 {
		return ts.AddDate(0, months, 0)
	}

	year, month, day := ts.Date()
	totalMonths := int(month) - 1 + months
	targetYear := year + totalMonths/12
	targetMonthIndex := totalMonths % 12
	if targetMonthIndex < 0 {
		targetMonthIndex += 12
		targetYear--
	}
	targetMonth := time.Month(targetMonthIndex + 1)

	lastDay := daysInMonth(targetYear, targetMonth, ts.Location())
	if day > lastDay {
		day = lastDay
	}

	return time.Date(targetYear, targetMonth, day, ts.Hour(), ts.Minute(), ts.Second(), ts.Nanosecond(), ts.Location())
}

func daysInMonth(year int, month time.Month, loc *time.Location) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()
}
