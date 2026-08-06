package automation

import (
	"context"
	"strings"
	"testing"
)

func TestEnabledJobsFailWhenCoreDependenciesAreMissing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(*JobOrchestrator) error
		want string
	}{
		{name: "alpaca reconcile", run: func(o *JobOrchestrator) error { return o.alpacaReconcile(context.Background()) }, want: "reconciler not configured"},
		{name: "universe refresh", run: func(o *JobOrchestrator) error { return o.universeRefresh(context.Background()) }, want: "universe provider not configured"},
		{name: "earnings scanner", run: func(o *JobOrchestrator) error { return o.earningsScanner(context.Background()) }, want: "events provider not configured"},
		{name: "filing monitor", run: func(o *JobOrchestrator) error { return o.filingMonitor(context.Background()) }, want: "events provider not configured"},
		{name: "social scan", run: func(o *JobOrchestrator) error { return o.socialScan(context.Background()) }, want: "news feed repo not configured"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.run(NewJobOrchestrator(OrchestratorDeps{}))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("run() error = %v, want %q", err, test.want)
			}
		})
	}
}
