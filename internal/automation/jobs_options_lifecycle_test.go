package automation

import (
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
)

func TestRegisterOptionsLifecycleJobRequiresPersistenceAndMarketData(t *testing.T) {
	withoutDeps := NewJobOrchestrator(OrchestratorDeps{})
	withoutDeps.RegisterAll()
	if _, ok := withoutDeps.jobs["options_expiry_settlement"]; ok {
		t.Fatal("expiry job registered without lifecycle dependencies")
	}

	orders := newRecordingOrderRepo()
	withDeps := NewJobOrchestrator(OrchestratorDeps{PositionRepo: newRecordingPositionRepo(), TradeRepo: newRecordingTradeRepo(orders), DataService: &data.DataService{}})
	withDeps.RegisterAll()
	job, ok := withDeps.jobs["options_expiry_settlement"]
	if !ok || !job.Enabled || job.Schedule.Cron != "0 23 * * 1-5" {
		t.Fatalf("expiry job not registered correctly: %+v", job)
	}
}
