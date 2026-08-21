package financialscheduler_test

import (
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/automation"
	"github.com/PatrickFanella/get-rich-quick/internal/financialscheduler"
)

func TestCatalogCoversEveryRegisteredAutomationJob(t *testing.T) {
	orchestrator := automation.NewJobOrchestrator(automation.OrchestratorDeps{})
	orchestrator.RegisterAll()
	keys := orchestrator.RegisteredJobKeys()
	if len(keys) < 20 {
		t.Fatalf("registered only %d jobs; coverage fixture is incomplete", len(keys))
	}
	if err := financialscheduler.ValidateCatalogCoverage(keys); err != nil {
		t.Fatal(err)
	}
	catalog := financialscheduler.Catalog()
	for _, dynamic := range []string{"strategy_execution", "backtest_execution", "options_expiry_settlement", "options_lifecycle_reconcile"} {
		if _, exists := catalog[dynamic]; !exists {
			t.Fatalf("catalog lacks conditional/dynamic job %q", dynamic)
		}
	}
}

func TestCatalogCoverageRejectsDrift(t *testing.T) {
	if err := financialscheduler.ValidateCatalogCoverage([]string{"new_financial_job"}); err == nil {
		t.Fatal("unclassified registration drift accepted")
	}
	if err := financialscheduler.ValidateCatalogCoverage([]string{"daily_review", "daily_review"}); err == nil {
		t.Fatal("duplicate registration accepted")
	}
}
