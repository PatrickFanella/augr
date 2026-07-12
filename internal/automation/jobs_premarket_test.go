package automation

import (
	"context"
	"errors"
	"testing"
)

func TestGapScannerSkipsWhenUniverseIsNotConfigured(t *testing.T) {
	orch := NewJobOrchestrator(OrchestratorDeps{})
	if err := orch.gapScanner(context.Background()); err != nil {
		t.Fatalf("gapScanner() error = %v, want nil", err)
	}
}

func TestIsKalshiRateLimit(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "status", err: errors.New("kalshi: request failed (status=429)"), want: true},
		{name: "provider code", err: errors.New(`{"code":"too_many_requests"}`), want: true},
		{name: "other provider error", err: errors.New("kalshi: request failed (status=500)"), want: false},
		{name: "nil", err: nil, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := isKalshiRateLimit(test.err); got != test.want {
				t.Fatalf("isKalshiRateLimit(%v) = %t, want %t", test.err, got, test.want)
			}
		})
	}
}
