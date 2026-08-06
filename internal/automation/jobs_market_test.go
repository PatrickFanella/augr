package automation

import (
	"context"
	"strings"
	"testing"

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

func TestDeepScanCompletionErrorFailsVisibleOnScoreWriteErrors(t *testing.T) {
	t.Parallel()

	if err := deepScanCompletionError(0); err != nil {
		t.Fatalf("deepScanCompletionError(0) = %v, want nil", err)
	}
	err := deepScanCompletionError(2)
	if err == nil || !strings.Contains(err.Error(), "2 universe score updates failed") {
		t.Fatalf("deepScanCompletionError(2) = %v", err)
	}
}
