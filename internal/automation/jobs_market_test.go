package automation

import (
	"context"
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

	if err := orch.currentDataRefresh(context.Background()); err != nil {
		t.Fatalf("currentDataRefresh() error = %v", err)
	}
	status := singleJobStatus(t, orch, "current_data_refresh")
	if got := status.LastSummary["tickers"]; got != 1 {
		t.Fatalf("refreshed ticker count = %d, want only the stock position", got)
	}
}
