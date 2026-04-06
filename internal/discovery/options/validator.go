package options

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// ValidateOptionsOutOfSample runs walk-forward OOS testing on an options strategy.
// Uses the same pass/fail criteria as the stock pipeline: positive OOS Sharpe
// and OOS/in-sample ratio >= MinOOSRatio.
func ValidateOptionsOutOfSample(
	ctx context.Context,
	cfg discovery.ValidationConfig,
	bars []domain.OHLCV,
	optionsConfig rules.OptionsRulesConfig,
	startDate, endDate time.Time,
	initialCash float64,
	logger *slog.Logger,
) (*discovery.ValidationResult, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.CalibrationMonths == 0 {
		cfg.CalibrationMonths = 6
	}
	if cfg.TestMonths == 0 {
		cfg.TestMonths = 3
	}
	if cfg.MinOOSRatio == 0 {
		cfg.MinOOSRatio = 0.5
	}

	rv := realizedVol(bars, 60)
	if rv < 0.05 {
		rv = 0.20
	}

	sweepCfg := OptionsSweepConfig{
		Ticker:      optionsConfig.Underlying,
		Bars:        bars,
		InitialCash: initialCash,
	}

	// Generate walk-forward windows.
	windows := generateWindows(startDate, endDate, cfg.CalibrationMonths, cfg.TestMonths)
	if len(windows) == 0 {
		return nil, fmt.Errorf("options/validator: no walk-forward windows fit in date range")
	}

	var inSampleMetrics, oosMetrics []backtest.Metrics

	for _, w := range windows {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// In-sample (calibration).
		sweepCfg.StartDate = w.calStart
		sweepCfg.EndDate = w.calEnd
		calMetrics := runOptionsBacktest(optionsConfig, filterBars(bars, w.calStart, w.calEnd), rv, sweepCfg)
		inSampleMetrics = append(inSampleMetrics, calMetrics)

		// Out-of-sample (test).
		sweepCfg.StartDate = w.testStart
		sweepCfg.EndDate = w.testEnd
		testMetrics := runOptionsBacktest(optionsConfig, filterBars(bars, w.testStart, w.testEnd), rv, sweepCfg)
		oosMetrics = append(oosMetrics, testMetrics)
	}

	// Aggregate.
	inSampleSharpe := avgSharpe(inSampleMetrics)
	oosSharpe := avgSharpe(oosMetrics)

	result := &discovery.ValidationResult{
		InSample:    inSampleMetrics[0],
		OutOfSample: oosMetrics[0],
	}
	if len(oosMetrics) > 1 {
		result.OutOfSample.SharpeRatio = oosSharpe
	}

	// Check thresholds.
	if oosSharpe < 0 {
		result.Passed = false
		result.Reason = fmt.Sprintf("OOS Sharpe negative (%.4f)", oosSharpe)
		return result, nil
	}

	if inSampleSharpe > 0 {
		result.OOSRatio = oosSharpe / inSampleSharpe
		if result.OOSRatio < cfg.MinOOSRatio {
			result.Passed = false
			result.Reason = fmt.Sprintf("OOS ratio %.4f below minimum %.4f", result.OOSRatio, cfg.MinOOSRatio)
			return result, nil
		}
	} else {
		result.OOSRatio = 1.0
	}

	result.Passed = true
	logger.Info("options/validator: passed",
		slog.String("ticker", optionsConfig.Underlying),
		slog.Float64("oos_sharpe", oosSharpe),
		slog.Float64("oos_ratio", result.OOSRatio),
	)
	return result, nil
}

type walkWindow struct {
	calStart, calEnd   time.Time
	testStart, testEnd time.Time
}

func generateWindows(start, end time.Time, calMonths, testMonths int) []walkWindow {
	var windows []walkWindow
	cursor := start

	for {
		calEnd := cursor.AddDate(0, calMonths, 0)
		testStart := calEnd
		testEnd := testStart.AddDate(0, testMonths, 0)

		if testEnd.After(end) {
			break
		}

		windows = append(windows, walkWindow{
			calStart:  cursor,
			calEnd:    calEnd,
			testStart: testStart,
			testEnd:   testEnd,
		})

		cursor = cursor.AddDate(0, testMonths, 0) // advance by test period
	}

	return windows
}

func filterBars(bars []domain.OHLCV, start, end time.Time) []domain.OHLCV {
	var filtered []domain.OHLCV
	for _, b := range bars {
		if !b.Timestamp.Before(start) && !b.Timestamp.After(end) {
			filtered = append(filtered, b)
		}
	}
	return filtered
}

func avgSharpe(metrics []backtest.Metrics) float64 {
	if len(metrics) == 0 {
		return 0
	}
	var sum float64
	for _, m := range metrics {
		sum += m.SharpeRatio
	}
	return sum / float64(len(metrics))
}
