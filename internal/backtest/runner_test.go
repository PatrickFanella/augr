package backtest

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// nowFuncRecorder captures SetNowFunc calls for verifying clock wiring.
type nowFuncRecorder struct {
	called  bool
	nowFunc func() time.Time
}

func (r *nowFuncRecorder) SetNowFunc(f func() time.Time) {
	r.called = true
	r.nowFunc = f
}

func TestNewRunnerRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	cfg := RunnerConfig{
		StrategyID: uuid.New(),
		Ticker:     "AAPL",
	}

	bars := []domain.OHLCV{
		makeBar(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), 100),
	}

	engine := mustFillEngine(t)
	broker := mustBrokerAdapter(t, engine)
	tracker := mustTracker(t)
	pipeline := makePipeline()

	t.Run("nil pipeline", func(t *testing.T) {
		_, err := NewRunner(cfg, bars, nil, broker, tracker, nil)
		if err == nil {
			t.Fatal("expected error for nil pipeline")
		}
	})

	t.Run("nil broker", func(t *testing.T) {
		_, err := NewRunner(cfg, bars, pipeline, nil, tracker, nil)
		if err == nil {
			t.Fatal("expected error for nil broker")
		}
	})

	t.Run("nil tracker", func(t *testing.T) {
		_, err := NewRunner(cfg, bars, pipeline, broker, nil, nil)
		if err == nil {
			t.Fatal("expected error for nil tracker")
		}
	})

	t.Run("empty bars", func(t *testing.T) {
		_, err := NewRunner(cfg, nil, pipeline, broker, tracker, nil)
		if err == nil {
			t.Fatal("expected error for empty bars")
		}
	})
}

func TestNewRunnerWiresClockToTargets(t *testing.T) {
	t.Parallel()

	cfg := RunnerConfig{
		StrategyID: uuid.New(),
		Ticker:     "AAPL",
	}

	bars := []domain.OHLCV{
		makeBar(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), 100),
	}

	engine := mustFillEngine(t)
	broker := mustBrokerAdapter(t, engine)
	tracker := mustTracker(t)
	pipeline := makePipeline()

	rec1 := &nowFuncRecorder{}
	rec2 := &nowFuncRecorder{}

	_, err := NewRunner(cfg, bars, pipeline, broker, tracker, nil, rec1, rec2)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	if !rec1.called {
		t.Error("first clock target was not wired")
	}
	if !rec2.called {
		t.Error("second clock target was not wired")
	}
}

func TestRunnerRunProcessesAllBars(t *testing.T) {
	t.Parallel()

	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)
	t3 := t2.Add(24 * time.Hour)

	bars := []domain.OHLCV{
		makeBar(t1, 100),
		makeBar(t2, 101),
		makeBar(t3, 102),
	}

	cfg := RunnerConfig{
		StrategyID: uuid.New(),
		Ticker:     "AAPL",
	}

	engine := mustFillEngine(t)
	broker := mustBrokerAdapter(t, engine)
	tracker := mustTracker(t)
	pipeline := makePipeline()

	runner, err := NewRunner(cfg, bars, pipeline, broker, tracker,
		slog.Default())
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(result.BarResults) != 3 {
		t.Fatalf("Run() bar results = %d, want 3", len(result.BarResults))
	}

	// Verify bars are in chronological order.
	for i, br := range result.BarResults {
		if !br.Bar.Timestamp.Equal(bars[i].Timestamp) {
			t.Errorf("bar[%d] timestamp = %v, want %v", i, br.Bar.Timestamp, bars[i].Timestamp)
		}
	}

	// Verify equity curve was recorded.
	if len(result.EquityCurve) != 3 {
		t.Errorf("Run() equity curve len = %d, want 3", len(result.EquityCurve))
	}
	if len(result.EquityCurveReport.Points) != 3 {
		t.Errorf("Run() equity report points len = %d, want 3", len(result.EquityCurveReport.Points))
	}
	if len(result.EquityCurveReport.DrawdownPeriods) != 0 {
		t.Errorf("Run() drawdown periods len = %d, want 0", len(result.EquityCurveReport.DrawdownPeriods))
	}
}

func TestRunnerRunRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	bars := []domain.OHLCV{
		makeBar(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), 100),
		makeBar(time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), 101),
		makeBar(time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC), 102),
	}

	cfg := RunnerConfig{
		StrategyID: uuid.New(),
		Ticker:     "AAPL",
	}

	engine := mustFillEngine(t)
	broker := mustBrokerAdapter(t, engine)
	tracker := mustTracker(t)
	pipeline := makePipeline()

	runner, err := NewRunner(cfg, bars, pipeline, broker, tracker, nil)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	// Cancel the context immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = runner.Run(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestRunnerRunAdvancesClock(t *testing.T) {
	t.Parallel()

	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)

	bars := []domain.OHLCV{
		makeBar(t1, 100),
		makeBar(t2, 101),
	}

	cfg := RunnerConfig{
		StrategyID: uuid.New(),
		Ticker:     "AAPL",
	}

	engine := mustFillEngine(t)
	broker := mustBrokerAdapter(t, engine)
	tracker := mustTracker(t)
	pipeline := makePipeline()

	rec := &nowFuncRecorder{}

	runner, err := NewRunner(cfg, bars, pipeline, broker, tracker, nil, rec)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// After the run, the clock target's nowFunc should return the last bar's
	// timestamp.
	if rec.nowFunc == nil {
		t.Fatal("clock target nowFunc is nil")
	}
	clockTime := rec.nowFunc()
	if !clockTime.Equal(t2) {
		t.Errorf("clock time after run = %v, want %v", clockTime, t2)
	}

	// Equity curve timestamps should match bar timestamps.
	if len(result.EquityCurve) != 2 {
		t.Fatalf("equity curve len = %d, want 2", len(result.EquityCurve))
	}
	if !result.EquityCurve[0].Timestamp.Equal(t1) {
		t.Errorf("equity[0].Timestamp = %v, want %v", result.EquityCurve[0].Timestamp, t1)
	}
	if !result.EquityCurve[1].Timestamp.Equal(t2) {
		t.Errorf("equity[1].Timestamp = %v, want %v", result.EquityCurve[1].Timestamp, t2)
	}
	if len(result.EquityCurveReport.Points) != 2 {
		t.Fatalf("equity report points len = %d, want 2", len(result.EquityCurveReport.Points))
	}
	for i, point := range result.EquityCurveReport.Points {
		if !point.Timestamp.Equal(result.EquityCurve[i].Timestamp) {
			t.Errorf("equity report point[%d].Timestamp = %v, want %v", i, point.Timestamp, result.EquityCurve[i].Timestamp)
		}
		assertFloatEqual(t, point.PortfolioValue, result.EquityCurve[i].Equity, "equity report PortfolioValue")
		assertFloatEqual(t, point.PeakEquity, result.EquityCurve[i].Equity, "equity report PeakEquity")
		assertFloatEqual(t, point.DrawdownValue, 0, "equity report DrawdownValue")
		assertFloatEqual(t, point.DrawdownPct, 0, "equity report DrawdownPct")
	}
}

// ---------- helpers ----------

func mustFillEngine(t *testing.T) *FillEngine {
	t.Helper()
	engine, err := NewFillEngine(FillConfig{
		Slippage: ProportionalSlippage{BasisPoints: 0},
	})
	if err != nil {
		t.Fatalf("NewFillEngine() error = %v", err)
	}
	return engine
}

func mustBrokerAdapter(t *testing.T, engine *FillEngine) *BrokerAdapter {
	t.Helper()
	broker, err := NewBrokerAdapter(100_000, engine)
	if err != nil {
		t.Fatalf("NewBrokerAdapter() error = %v", err)
	}
	return broker
}

func mustTracker(t *testing.T) *PositionTracker {
	t.Helper()
	tracker, err := NewPositionTracker(100_000)
	if err != nil {
		t.Fatalf("NewPositionTracker() error = %v", err)
	}
	return tracker
}

func makePipeline() *agent.Pipeline {
	events := make(chan agent.PipelineEvent, 64)
	return agent.NewPipeline(
		agent.PipelineConfig{},
		agent.NoopPersister{},
		events,
		nil,
	)
}
