package backtest

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestGenerateWalkForwardWindows(t *testing.T) {
	t.Parallel()

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC)

	testCases := []struct {
		name            string
		cfg             WalkForwardConfig
		wantWindowCount int
	}{
		{
			name: "calibration 2 months, test 1 month",
			cfg: WalkForwardConfig{
				CalibrationMonths: 2,
				TestMonths:        1,
			},
			wantWindowCount: 4,
		},
		{
			name: "calibration 2 months, test 2 months",
			cfg: WalkForwardConfig{
				CalibrationMonths: 2,
				TestMonths:        2,
			},
			wantWindowCount: 2,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			windows, err := generateWalkForwardWindows(start, end, tc.cfg)
			if err != nil {
				t.Fatalf("generateWalkForwardWindows() error = %v", err)
			}

			if len(windows) != tc.wantWindowCount {
				t.Fatalf("len(windows) = %d, want %d", len(windows), tc.wantWindowCount)
			}

			if !windows[0].CalibrationStart.Equal(start) {
				t.Errorf("windows[0].CalibrationStart = %v, want %v", windows[0].CalibrationStart, start)
			}

			for i := range windows {
				w := windows[i]
				if !w.CalibrationEndExclusive.Equal(w.TestStart) {
					t.Errorf("window %d calibration end exclusive (%v) must equal test start (%v)", i, w.CalibrationEndExclusive, w.TestStart)
				}
				if !w.CalibrationStart.Before(w.CalibrationEndExclusive) {
					t.Errorf("window %d calibration start (%v) must be before calibration end exclusive (%v)", i, w.CalibrationStart, w.CalibrationEndExclusive)
				}
				if !w.TestStart.Before(w.TestEndExclusive) {
					t.Errorf("window %d test start (%v) must be before test end exclusive (%v)", i, w.TestStart, w.TestEndExclusive)
				}
				if i == 0 {
					continue
				}
				wantStart := addMonthsClamped(windows[i-1].CalibrationStart, tc.cfg.TestMonths)
				if !w.CalibrationStart.Equal(wantStart) {
					t.Errorf("window %d calibration start = %v, want %v", i, w.CalibrationStart, wantStart)
				}
			}
		})
	}
}

func TestGenerateWalkForwardWindowsRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	testCases := []WalkForwardConfig{
		{CalibrationMonths: 0, TestMonths: 1},
		{CalibrationMonths: 1, TestMonths: 0},
		{CalibrationMonths: 3, TestMonths: 1}, // no full window in range
	}

	for _, tc := range testCases {
		_, err := generateWalkForwardWindows(start, end, tc)
		if err == nil {
			t.Fatalf("expected error for config %+v", tc)
		}
	}
}

func TestAddMonthsClamped_EndOfMonth(t *testing.T) {
	t.Parallel()

	start := time.Date(2024, 1, 31, 15, 0, 0, 0, time.UTC)
	got := addMonthsClamped(start, 1)
	want := time.Date(2024, 2, 29, 15, 0, 0, 0, time.UTC) // leap year clamp
	if !got.Equal(want) {
		t.Fatalf("addMonthsClamped(%v, 1) = %v, want %v", start, got, want)
	}
}

func TestOrchestratorRunWalkForwardAggregatesOutOfSample(t *testing.T) {
	t.Parallel()

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC)
	bars := makeDailyBars(start, end)

	cfg := OrchestratorConfig{
		StrategyID:  uuid.New(),
		Ticker:      "AAPL",
		StartDate:   start,
		EndDate:     end,
		InitialCash: 100_000,
		FillConfig: FillConfig{
			Slippage: ProportionalSlippage{BasisPoints: 0},
		},
	}

	testLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	orch, err := NewOrchestrator(cfg, bars, makePipelineWithLogger(testLogger), testLogger)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}

	result, err := orch.RunWalkForward(context.Background(), WalkForwardConfig{
		CalibrationMonths: 2,
		TestMonths:        1,
	})
	if err != nil {
		t.Fatalf("RunWalkForward() error = %v", err)
	}

	if len(result.Windows) != 4 {
		t.Fatalf("len(result.Windows) = %d, want 4", len(result.Windows))
	}
	if result.OutOfSample.TotalReturn.N != 4 {
		t.Errorf("OutOfSample.TotalReturn.N = %d, want 4", result.OutOfSample.TotalReturn.N)
	}
	if len(result.OutOfSample.TotalBars.Values) != 4 {
		t.Errorf("len(OutOfSample.TotalBars.Values) = %d, want 4", len(result.OutOfSample.TotalBars.Values))
	}

	for i, windowResult := range result.Windows {
		if windowResult.Calibration == nil {
			t.Fatalf("window %d calibration result is nil", i)
		}
		if windowResult.Test == nil {
			t.Fatalf("window %d test result is nil", i)
		}
		if !windowResult.Window.CalibrationEndExclusive.Equal(windowResult.Window.TestStart) {
			t.Errorf("window %d has overlapping calibration/test ranges", i)
		}
	}
}

func makeDailyBars(start, end time.Time) []domain.OHLCV {
	bars := make([]domain.OHLCV, 0)
	for ts := start; !ts.After(end); ts = ts.Add(24 * time.Hour) {
		bars = append(bars, makeBar(ts, 100))
	}
	return bars
}

func makePipelineWithLogger(logger *slog.Logger) *agent.Pipeline {
	events := make(chan agent.PipelineEvent, 64)
	return agent.NewPipeline(
		agent.PipelineConfig{},
		agent.NoopPersister{},
		events,
		logger,
	)
}
