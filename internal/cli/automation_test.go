package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAutomationAlpacaReconcileCommandRunsAdminEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/automation/alpaca/reconcile" {
			t.Fatalf("path = %s, want /api/v1/automation/alpaca/reconcile", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"summary": map[string]int{
				"orders_created": 2,
				"trades_created": 3,
			},
			"verification": map[string]int{
				"orders_checked": 2,
			},
		})
	}))
	defer server.Close()

	stdout, _, err := executeCLI(t, nil, "--api-url", server.URL, "--format", "json", "automation", "alpaca-reconcile")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	summary, ok := out["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary = %#v, want object", out["summary"])
	}
	if summary["orders_created"] != float64(2) {
		t.Fatalf("orders_created = %v, want 2", summary["orders_created"])
	}
}
