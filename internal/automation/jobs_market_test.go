package automation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestCurrentDataRefreshSkipsPredictionMarketPositions(t *testing.T) {
	orch := NewJobOrchestrator(OrchestratorDeps{
		PositionRepo: newRecordingPositionRepo(
			&domain.Position{ID: uuid.New(), Ticker: "AAPL", MarketType: domain.MarketTypeStock},
			&domain.Position{ID: uuid.New(), Ticker: "KXTEST:YES", MarketType: domain.MarketTypeKalshi},
		),
	})
	orch.Register("current_data_refresh", "test", currentDataRefreshSpec, orch.currentDataRefresh)

	if err := orch.currentDataRefresh(context.Background()); err == nil {
		t.Fatal("currentDataRefresh() error = nil, want missing data service error")
	}
	status := singleJobStatus(t, orch, "current_data_refresh")
	if got := status.LastSummary["tickers"]; got != 1 {
		t.Fatalf("refreshed ticker count = %d, want only the stock position", got)
	}
}

func TestMarketJobCadenceRunsDependenciesBeforeConsumers(t *testing.T) {
	t.Parallel()

	if currentDataRefreshSpec.Cron != "*/15 * * * 1-5" {
		t.Fatalf("current refresh cron = %q", currentDataRefreshSpec.Cron)
	}
	if hotScanSpec.Cron != "5-59/15 * * * 1-5" {
		t.Fatalf("hot scan cron = %q, want five-minute dependency offset", hotScanSpec.Cron)
	}
	if deepScanSpec.Cron != "10 * * * 1-5" {
		t.Fatalf("deep scan cron = %q, want post-hot-scan offset", deepScanSpec.Cron)
	}
}

func TestCanonicalTriggeredStrategiesDeduplicatesSchedulerKeys(t *testing.T) {
	low := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	high := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	strategies := []domain.Strategy{
		{ID: high, Ticker: "AAPL", MarketType: domain.MarketTypeStock, ScheduleCron: "0 */2 * * *"},
		{ID: low, Ticker: "AAPL", MarketType: domain.MarketTypeStock, ScheduleCron: "0 */2 * * *"},
		{ID: uuid.New(), Ticker: "AAPL", MarketType: domain.MarketTypeStock, ScheduleCron: "0 9 * * 1-5"},
		{ID: uuid.New(), Ticker: "KX-A", MarketType: domain.MarketTypeKalshi, ScheduleCron: "0 */6 * * *"},
	}
	got := canonicalTriggeredStrategies(strategies)
	if len(got) != 2 {
		t.Fatalf("canonical strategies = %d, want 2 stock scheduler keys", len(got))
	}
	if got[0].ID != low {
		t.Fatalf("duplicate canonical ID = %s, want deterministic lowest %s", got[0].ID, low)
	}
}

func TestMarketScanCompletionErrorFailsVisibleOnPartialCoverage(t *testing.T) {
	t.Parallel()

	if err := marketScanCompletionError("deep_scan", map[string]int{}); err != nil {
		t.Fatalf("marketScanCompletionError(empty) = %v, want nil", err)
	}
	err := marketScanCompletionError("deep_scan", map[string]int{"fetch_errors": 1, "insufficient": 2, "stale": 3, "score_errors": 4, "strategy_list_failed": 5})
	if err == nil || !strings.Contains(err.Error(), "fetch_errors=1 insufficient=2 stale=3 score_errors=4 strategy_list_failed=5") {
		t.Fatalf("marketScanCompletionError(partial) = %v", err)
	}
}

func TestMarketJobsFailWhenCoreDependenciesAreMissing(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{})
	tests := map[string]func(context.Context) error{
		"current_data_refresh": orch.currentDataRefresh,
		"hot_scan":             orch.hotScan,
		"deep_scan":            orch.deepScan,
	}
	for name, run := range tests {
		name, run := name, run
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := run(context.Background()); err == nil {
				t.Fatalf("%s() error = nil, want missing dependency failure", name)
			}
		})
	}
}

func TestMarketBarFreshnessUsesRegularSessionAndTradingDate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 6, 10, 0, 0, 0, easternTime)
	if !intradayBarFresh(now, time.Date(2026, time.August, 6, 9, 45, 0, 0, easternTime)) {
		t.Fatal("15-minute-old same-session intraday bar should be fresh")
	}
	if intradayBarFresh(now, time.Date(2026, time.August, 6, 9, 35, 0, 0, easternTime)) {
		t.Fatal("25-minute-old intraday bar should be stale")
	}
	if intradayBarFresh(time.Date(2026, time.August, 6, 9, 20, 0, 0, easternTime), time.Date(2026, time.August, 5, 15, 55, 0, 0, easternTime)) {
		t.Fatal("premarket bar must not qualify for a regular-session refresh")
	}
	if dailyBarFresh(now, time.Date(2026, time.August, 6, 9, 30, 0, 0, easternTime)) {
		t.Fatal("incomplete current-session daily bar must not be fresh during market hours")
	}
	if !dailyBarFresh(now, time.Date(2026, time.August, 5, 9, 30, 0, 0, easternTime)) {
		t.Fatal("prior completed-session daily bar should be fresh during market hours")
	}
	preMarketMonday := time.Date(2026, time.August, 10, 8, 0, 0, 0, easternTime)
	if !dailyBarFresh(preMarketMonday, time.Date(2026, time.August, 7, 9, 30, 0, 0, easternTime)) {
		t.Fatal("Friday daily bar should be fresh before Monday open")
	}
	postClose := time.Date(2026, time.August, 6, 16, 5, 0, 0, easternTime)
	if !dailyBarFresh(postClose, time.Date(2026, time.August, 6, 16, 0, 0, 0, easternTime)) {
		t.Fatal("completed current-session daily bar should be fresh after close")
	}
}

func TestCurrentDataRefreshCompletionErrorRejectsCacheOnlyAndStale(t *testing.T) {
	t.Parallel()

	if err := currentDataRefreshCompletionError(map[string]int{}); err != nil {
		t.Fatalf("currentDataRefreshCompletionError(empty) = %v, want nil", err)
	}
	err := currentDataRefreshCompletionError(map[string]int{"cache_only": 2, "daily_stale": 1})
	if err == nil || !strings.Contains(err.Error(), "cache_only=2") || !strings.Contains(err.Error(), "stale=1") {
		t.Fatalf("currentDataRefreshCompletionError(partial) = %v", err)
	}
}
