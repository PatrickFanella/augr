package automation

import (
	"context"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type optionSettlementRepoStub struct{}

func (optionSettlementRepoStub) SettleOptionPosition(_ context.Context, input repository.OptionPositionSettlementInput) (repository.OptionPositionSettlementResult, error) {
	return repository.OptionPositionSettlementResult{PositionID: input.PositionID}, nil
}

func TestRegisterOptionsLifecycleJobRequiresPersistenceAndMarketData(t *testing.T) {
	withoutDeps := NewJobOrchestrator(OrchestratorDeps{})
	withoutDeps.RegisterAll()
	if _, ok := withoutDeps.jobs["options_expiry_settlement"]; ok {
		t.Fatal("expiry job registered without lifecycle dependencies")
	}

	orders := newRecordingOrderRepo()
	withDeps := NewJobOrchestrator(OrchestratorDeps{PositionRepo: newRecordingPositionRepo(), OrderRepo: orders, TradeRepo: newRecordingTradeRepo(orders), OptionSettlementRepo: optionSettlementRepoStub{}, DataService: &data.DataService{}})
	withDeps.RegisterAll()
	job, ok := withDeps.jobs["options_expiry_settlement"]
	if !ok || !job.Enabled || job.Schedule.Cron != "0 23 * * 1-5" {
		t.Fatalf("expiry job not registered correctly: %+v", job)
	}
	if reconcile, ok := withDeps.jobs["options_lifecycle_reconcile"]; !ok || reconcile.Schedule.Cron != "30 23 * * 1-5" {
		t.Fatalf("reconciliation job not registered: %+v", reconcile)
	}
}

func TestOptionsLifecycleReconcileJobAcceptsEmptyDurableGraph(t *testing.T) {
	orders := newRecordingOrderRepo()
	orch := NewJobOrchestrator(OrchestratorDeps{OrderRepo: orders, PositionRepo: newRecordingPositionRepo(), TradeRepo: newRecordingTradeRepo(orders)})
	orch.RegisterAll()
	if err := orch.optionsLifecycleReconcile(context.Background()); err != nil {
		t.Fatalf("optionsLifecycleReconcile() error = %v", err)
	}
	if summary := orch.jobs["options_lifecycle_reconcile"].LastSummary; summary["findings"] != 0 {
		t.Fatalf("unexpected reconciliation summary: %v", summary)
	}
}
