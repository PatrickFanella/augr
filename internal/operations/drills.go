package operations

import (
	"fmt"
	"sort"
	"strings"
)

var RequiredRecoveryDrills = []string{"restart", "dependency_outage", "stale_data", "order_rejection", "partial_fill", "reconciliation", "kill_switch", "websocket_reconnect", "prediction_settlement", "options_expiration", "options_assignment"}

type DrillResult struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Evidence string `json:"evidence"`
}

func ValidateRecoveryDrills(results []DrillResult) error {
	seen := make(map[string]DrillResult, len(results))
	for _, result := range results {
		name := strings.TrimSpace(result.Name)
		if _, exists := seen[name]; exists {
			return fmt.Errorf("recovery drills: duplicate result %q", name)
		}
		seen[name] = result
	}
	var failures []string
	for _, name := range RequiredRecoveryDrills {
		result, ok := seen[name]
		if !ok {
			failures = append(failures, name+": missing")
			continue
		}
		if !result.Passed {
			failures = append(failures, name+": failed")
		}
		if strings.TrimSpace(result.Evidence) == "" {
			failures = append(failures, name+": evidence missing")
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		return fmt.Errorf("recovery drills incomplete: %s", strings.Join(failures, ", "))
	}
	return nil
}
