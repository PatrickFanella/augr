package automation

import (
	"context"
	"testing"
)

func TestGapScannerSkipsWhenUniverseIsNotConfigured(t *testing.T) {
	orch := NewJobOrchestrator(OrchestratorDeps{})
	if err := orch.gapScanner(context.Background()); err != nil {
		t.Fatalf("gapScanner() error = %v, want nil", err)
	}
}
