package automation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/google/uuid"
)

type positionReviewStrategyRepo struct{ *kalshiStrategyRepoStub }

func (s *positionReviewStrategyRepo) Count(context.Context, repository.StrategyFilter) (int, error) {
	return len(s.strategies), nil
}

func TestGapScannerCompletionErrorRejectsPartialCoverage(t *testing.T) {
	t.Parallel()

	if err := gapScannerCompletionError(map[string]int{}); err != nil {
		t.Fatalf("gapScannerCompletionError(empty) = %v, want nil", err)
	}
	err := gapScannerCompletionError(map[string]int{"missing_snapshots": 3, "score_failed": 1})
	if err == nil || !strings.Contains(err.Error(), "missing_snapshots=3") || !strings.Contains(err.Error(), "score_failed=1") {
		t.Fatalf("gapScannerCompletionError(partial) = %v", err)
	}
}

func TestPreMarketSnapshotFreshRequiresCurrentExtendedSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 6, 8, 0, 0, 0, easternTime)
	if !preMarketSnapshotFresh(now, time.Date(2026, time.August, 6, 4, 1, 0, 0, easternTime)) {
		t.Fatal("current premarket snapshot should be fresh")
	}
	if preMarketSnapshotFresh(now, time.Date(2026, time.August, 5, 15, 59, 0, 0, easternTime)) {
		t.Fatal("prior-day snapshot should be stale")
	}
	if preMarketSnapshotFresh(now, time.Date(2026, time.August, 6, 3, 59, 0, 0, easternTime)) {
		t.Fatal("pre-reset snapshot should be stale")
	}
}

func TestDiscoveryRunCompletionErrorRejectsReportedErrors(t *testing.T) {
	t.Parallel()

	if err := discoveryRunCompletionError(nil); err != nil {
		t.Fatalf("discoveryRunCompletionError(nil) = %v, want nil", err)
	}
	err := discoveryRunCompletionError([]string{"generate AAPL", "persist MSFT"})
	if err == nil || !strings.Contains(err.Error(), "2 pipeline errors") {
		t.Fatalf("discoveryRunCompletionError(errors) = %v", err)
	}
}

func TestGapScannerFailsWhenProvidersAreNotConfigured(t *testing.T) {
	orch := NewJobOrchestrator(OrchestratorDeps{})
	if err := orch.gapScanner(context.Background()); err == nil || !strings.Contains(err.Error(), "providers are required") {
		t.Fatalf("gapScanner() error = %v, want missing providers", err)
	}
}

func TestTickerDiscoveryRegistersInAutomationLedger(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{TickerDiscovery: TickerDiscoveryJobConfig{
		Enabled: true, Cron: "30 10 * * 1-5", MinADV: 100000, MaxTickers: 30,
	}})
	orch.registerTickerDiscoveryJob()
	job := orch.jobs["ticker_discovery"]
	if job == nil {
		t.Fatal("ticker_discovery job not registered")
	}
	if job.Schedule.Cron != "CRON_TZ=UTC 30 10 * * 1-5" || job.Schedule.Type != "pre_market" {
		t.Fatalf("ticker_discovery schedule = %+v", job.Schedule)
	}
}

func TestTickerDiscoveryPreservesExplicitCronTimezone(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{TickerDiscovery: TickerDiscoveryJobConfig{
		Enabled: true, Cron: "CRON_TZ=America/Chicago 30 5 * * 1-5",
	}})
	orch.registerTickerDiscoveryJob()
	if got := orch.jobs["ticker_discovery"].Schedule.Cron; got != "CRON_TZ=America/Chicago 30 5 * * 1-5" {
		t.Fatalf("ticker_discovery cron = %q, want explicit timezone unchanged", got)
	}
}

func TestTickerDiscoveryFailsWhenCoreDependenciesAreMissing(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{TickerDiscovery: TickerDiscoveryJobConfig{Enabled: true}})
	orch.registerTickerDiscoveryJob()
	if err := orch.tickerDiscovery(context.Background()); err == nil || !strings.Contains(err.Error(), "dependencies are required") {
		t.Fatalf("tickerDiscovery() error = %v, want missing dependencies", err)
	}
}

func TestIsKalshiRateLimit(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "status", err: errors.New("kalshi: request failed (status=429)"), want: true},
		{name: "provider code", err: errors.New(`{"code":"too_many_requests"}`), want: true},
		{name: "other provider error", err: errors.New("kalshi: request failed (status=500)"), want: false},
		{name: "nil", err: nil, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := isKalshiRateLimit(test.err); got != test.want {
				t.Fatalf("isKalshiRateLimit(%v) = %t, want %t", test.err, got, test.want)
			}
		})
	}
}

func TestKalshiDiscoveryCompletionErrorRejectsMissingAndPartialResults(t *testing.T) {
	t.Parallel()

	if err := kalshiDiscoveryCompletionError(true, 0); err != nil {
		t.Fatalf("kalshiDiscoveryCompletionError(success) = %v", err)
	}
	if err := kalshiDiscoveryCompletionError(false, 0); err == nil || !strings.Contains(err.Error(), "no result") {
		t.Fatalf("kalshiDiscoveryCompletionError(missing) = %v", err)
	}
	if err := kalshiDiscoveryCompletionError(true, 2); err == nil || !strings.Contains(err.Error(), "2 domain errors") {
		t.Fatalf("kalshiDiscoveryCompletionError(partial) = %v", err)
	}
}

func TestPositionReviewSummarizesActualOpenPositions(t *testing.T) {
	activeID := uuid.New()
	inactiveID := uuid.New()
	price := 101.0
	stop := 95.0
	orch := NewJobOrchestrator(OrchestratorDeps{
		StrategyRepo: &positionReviewStrategyRepo{&kalshiStrategyRepoStub{strategies: []domain.Strategy{{
			ID: activeID, Ticker: "AAPL", Status: domain.StrategyStatusActive,
		}}}},
		PositionRepo: &polymarketPositionRepoStub{positions: []domain.Position{
			{ID: uuid.New(), StrategyID: &activeID, Ticker: "AAPL", Side: domain.PositionSideLong, Quantity: 2, CurrentPrice: &price, StopLoss: &stop},
			{ID: uuid.New(), StrategyID: &inactiveID, Ticker: "MSFT", Side: domain.PositionSideLong, Quantity: 1},
			{ID: uuid.New(), Ticker: "LEGACY", Side: domain.PositionSideShort, Quantity: 1},
		}},
	})
	orch.Register("position_review", "test", schedulerSpecEveryMinute(), orch.positionReview)

	if err := orch.positionReview(context.Background()); err == nil || !strings.Contains(err.Error(), "unsafe position findings") {
		t.Fatalf("positionReview() error = %v, want unsafe findings", err)
	}
	got := orch.jobs["position_review"].LastSummary
	want := map[string]int{
		"active_strategies": 1, "open_positions": 3, "strategies_with_positions": 1,
		"unowned_positions": 1, "inactive_strategy_positions": 1,
		"positions_missing_stop_loss": 2, "positions_missing_price": 2,
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("summary[%q] = %d, want %d (summary=%v)", key, got[key], value, got)
		}
	}
}

func TestPositionReviewDoesNotRequireEquityProtectionFieldsForEventPositions(t *testing.T) {
	strategyID := uuid.New()
	orch := NewJobOrchestrator(OrchestratorDeps{
		StrategyRepo: &positionReviewStrategyRepo{&kalshiStrategyRepoStub{strategies: []domain.Strategy{{
			ID: strategyID, Ticker: "KXTEST:YES", MarketType: domain.MarketTypeKalshi, Status: domain.StrategyStatusActive,
		}}}},
		PositionRepo: &polymarketPositionRepoStub{positions: []domain.Position{{
			ID: uuid.New(), StrategyID: &strategyID, MarketType: domain.MarketTypeKalshi,
			Ticker: "KXTEST:YES", Side: domain.PositionSideLong, Quantity: 1,
		}}},
	})
	orch.Register("position_review", "test", schedulerSpecEveryMinute(), orch.positionReview)

	if err := orch.positionReview(context.Background()); err != nil {
		t.Fatalf("positionReview() error = %v, want nil", err)
	}
	got := orch.jobs["position_review"].LastSummary
	if got["event_positions"] != 1 || got["positions_requiring_protection"] != 0 {
		t.Fatalf("position market summary = %v, want one event and zero protected positions", got)
	}
	if got["positions_missing_stop_loss"] != 0 || got["positions_missing_price"] != 0 {
		t.Fatalf("event position must not require equity stop/price fields: %v", got)
	}
}
