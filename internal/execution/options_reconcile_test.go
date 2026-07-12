package execution

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestReconcileOptionsLifecycleHealthyAndBrokenGraphs(t *testing.T) {
	orderID, positionID, groupID := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC()
	orders := []domain.Order{{ID: orderID, AssetClass: domain.AssetClassOption, Status: domain.OrderStatusFilled}}
	positions := []domain.Position{{ID: positionID, AssetClass: domain.AssetClassOption, LegGroupID: &groupID}, {ID: uuid.New(), AssetClass: domain.AssetClassOption, LegGroupID: &groupID}}
	trades := []domain.Trade{{ID: uuid.New(), OrderID: &orderID, PositionID: &positionID, AssetClass: domain.AssetClassOption, OpenClose: "open"}, {ID: uuid.New(), PositionID: &positions[1].ID, AssetClass: domain.AssetClassOption, OpenClose: "open"}}
	healthy := ReconcileOptionsLifecycle(orders, positions, trades)
	if !healthy.Healthy() || healthy.LegGroups != 1 {
		t.Fatalf("healthy graph findings=%v", healthy.Findings)
	}
	positions[0].ClosedAt = &now
	broken := ReconcileOptionsLifecycle(orders, positions, trades[:1])
	if broken.Healthy() || len(broken.Findings) < 3 {
		t.Fatalf("broken graph not detected: %+v", broken)
	}
}
