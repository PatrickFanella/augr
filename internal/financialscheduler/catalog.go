package financialscheduler

import (
	"fmt"
	"sort"
)

// Catalog returns the closed OVR-604 inventory. Entries describe durable side
// effects, not whether a job is enabled in a particular runtime.
func Catalog() map[string]JobDefinition {
	entries := []struct {
		key       string
		mutations []MutationClass
	}{
		{"alpaca_reconcile", []MutationClass{MutationReconciliation, MutationProvider}},
		{"current_data_refresh", []MutationClass{MutationEvidence, MutationProvider}},
		{"daily_review", []MutationClass{MutationEvidence}},
		{"deep_scan", []MutationClass{MutationEvidence, MutationProvider}},
		{"discovery_run", []MutationClass{MutationEvidence}},
		{"earnings_scanner", []MutationClass{MutationEvidence, MutationProvider}},
		{"filing_monitor", []MutationClass{MutationEvidence, MutationProvider}},
		{"gap_scanner", []MutationClass{MutationEvidence, MutationProvider}},
		{"history_refresh", []MutationClass{MutationEvidence, MutationProvider}},
		{"hot_scan", []MutationClass{MutationEvidence}},
		{"kalshi_discovery", []MutationClass{MutationEvidence, MutationProvider}},
		{"kalshi_reconcile", []MutationClass{MutationReconciliation, MutationProvider}},
		{"kalshi_settlement", []MutationClass{MutationSettlement, MutationLedger, MutationProvider}},
		{"news_scan", []MutationClass{MutationEvidence, MutationProvider}},
		{"options_discovery", []MutationClass{MutationEvidence, MutationProvider}},
		{"options_expiry_settlement", []MutationClass{MutationSettlement, MutationLedger}},
		{"options_lifecycle_reconcile", []MutationClass{MutationReconciliation}},
		{"options_scan", []MutationClass{MutationEvidence, MutationProvider}},
		{"overnight_backtest", []MutationClass{MutationEvidence}},
		{"overnight_generate", []MutationClass{MutationEvidence}},
		{"overnight_sweep", []MutationClass{MutationEvidence}},
		{"paper_validation_report", []MutationClass{MutationEvidence}},
		{"polymarket_profiles", []MutationClass{MutationEvidence, MutationProvider}},
		{"polymarket_reconcile", []MutationClass{MutationReconciliation, MutationProvider}},
		{"polymarket_resolutions", []MutationClass{MutationSettlement, MutationProvider}},
		{"polymarket_strategy_discovery", []MutationClass{MutationEvidence, MutationProvider}},
		{"portfolio_allocator", []MutationClass{MutationAllocation, MutationIntentOrder}},
		{"position_review", []MutationClass{MutationEvidence}},
		{"social_scan", []MutationClass{MutationEvidence, MutationProvider}},
		{"strategy_resweep", []MutationClass{MutationEvidence}},
		{"strategy_tournament", []MutationClass{MutationEvidence}},
		{"ticker_discovery", []MutationClass{MutationEvidence, MutationProvider}},
		{"universe_refresh", []MutationClass{MutationEvidence, MutationProvider}},
		// The legacy scheduler creates these dynamically per immutable strategy or
		// backtest configuration rather than through JobOrchestrator.Register.
		{"strategy_execution", []MutationClass{MutationIntentOrder}},
		{"backtest_execution", []MutationClass{MutationEvidence}},
	}
	result := make(map[string]JobDefinition, len(entries))
	for _, entry := range entries {
		definition, err := NewJobDefinition(entry.key, entry.mutations...)
		if err != nil {
			panic(err)
		}
		if _, exists := result[entry.key]; exists {
			panic("financial scheduler: duplicate catalog key " + entry.key)
		}
		result[entry.key] = definition
	}
	return result
}

// ValidateCatalogCoverage fails closed when a registered financial job lacks
// an explicit OVR-604 classification.
func ValidateCatalogCoverage(registeredKeys []string) error {
	catalog := Catalog()
	missing := make([]string, 0)
	seen := make(map[string]struct{}, len(registeredKeys))
	for _, key := range registeredKeys {
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("financial scheduler: duplicate registered job %q", key)
		}
		seen[key] = struct{}{}
		if _, exists := catalog[key]; !exists {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("financial scheduler: unclassified jobs: %v", missing)
	}
	return nil
}
