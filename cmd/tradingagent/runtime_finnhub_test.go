package main

import (
	"context"
	"testing"
	"time"
)

func TestNewRuntimeFinnhubLimitersPacesInsteadOfBursting(t *testing.T) {
	limiters := newRuntimeFinnhubLimiters(6000, nil)
	if len(limiters) != 1 {
		t.Fatalf("len(limiters) = %d, want 1", len(limiters))
	}
	if err := limiters[0].Wait(context.Background()); err != nil {
		t.Fatalf("first Wait() error = %v", err)
	}

	started := time.Now()
	if err := limiters[0].Wait(context.Background()); err != nil {
		t.Fatalf("second Wait() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed < 5*time.Millisecond {
		t.Fatalf("second Wait() elapsed = %v, want paced delay", elapsed)
	}
}

func TestNewRuntimePolygonLimiterPacesInsteadOfBursting(t *testing.T) {
	limiter := newRuntimePolygonLimiter()
	if err := limiter.Wait(context.Background()); err != nil {
		t.Fatalf("first Wait() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := limiter.Wait(ctx); err == nil {
		t.Fatal("second Wait() error = nil, want pacing delay beyond short context")
	}
}
