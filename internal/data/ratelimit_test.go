package data_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
)

func TestRateLimiterTryAcquireRespectsRate(t *testing.T) {
	const refillInterval = 200 * time.Millisecond

	limiter := data.NewRateLimiter(2, refillInterval)

	if !limiter.TryAcquire() {
		t.Fatal("TryAcquire() = false, want true for first token")
	}
	if !limiter.TryAcquire() {
		t.Fatal("TryAcquire() = false, want true for second token")
	}
	if limiter.TryAcquire() {
		t.Fatal("TryAcquire() = true, want false when bucket is exhausted")
	}

	time.Sleep(refillInterval + 50*time.Millisecond)

	if !limiter.TryAcquire() {
		t.Fatal("TryAcquire() = false, want true after refill interval")
	}
}

func TestRateLimiterWaitBlocksUntilTokenAvailable(t *testing.T) {
	const refillInterval = 150 * time.Millisecond
	const minimumBlock = refillInterval - 40*time.Millisecond

	limiter := data.NewRateLimiter(1, refillInterval)
	if !limiter.TryAcquire() {
		t.Fatal("TryAcquire() = false, want true for initial token")
	}

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		done <- limiter.Wait(context.Background())
	}()

	select {
	case err := <-done:
		t.Fatalf("Wait() returned early with err = %v, want it to block", err)
	case <-time.After(50 * time.Millisecond):
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Wait() error = %v, want nil", err)
		}
	case <-time.After(750 * time.Millisecond):
		t.Fatal("Wait() did not return after token should have refilled")
	}

	if elapsed := time.Since(start); elapsed < minimumBlock {
		t.Fatalf("Wait() blocked for %v, want at least %v", elapsed, minimumBlock)
	}
}

func TestRateLimiterWaitHonorsContextCancellation(t *testing.T) {
	const waitTimeout = 150 * time.Millisecond
	const cancellationMargin = 250 * time.Millisecond

	limiter := data.NewRateLimiter(1, 500*time.Millisecond)
	if !limiter.TryAcquire() {
		t.Fatal("TryAcquire() = false, want true for initial token")
	}

	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()

	start := time.Now()
	err := limiter.Wait(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait() error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > waitTimeout+cancellationMargin {
		t.Fatalf("Wait() returned after %v, want prompt cancellation", elapsed)
	}
}
