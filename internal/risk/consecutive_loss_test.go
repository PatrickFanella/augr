package risk

import (
	"context"
	"strings"
	"sync"
	"testing"
)

type fakeBreaker struct {
	mu                                                sync.Mutex
	trips, resets                                     int
	tripScopes, tripReasons, resetScopes, allowScopes []string
}

func (f *fakeBreaker) Allow(_ context.Context, scope string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.allowScopes = append(f.allowScopes, scope)
	return nil
}

func (f *fakeBreaker) Trip(_ context.Context, scope, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trips++
	f.tripScopes = append(f.tripScopes, scope)
	f.tripReasons = append(f.tripReasons, reason)
	return nil
}

func (f *fakeBreaker) Reset(_ context.Context, scope string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resets++
	f.resetScopes = append(f.resetScopes, scope)
	return nil
}

func TestConsecutiveLossBreaker_WinsResetStreak(t *testing.T) {
	fb := &fakeBreaker{}
	b := NewConsecutiveLossBreaker(ConsecutiveLossConfig{Threshold: 3, Windows: 2}, fb)
	for _, pnl := range []float64{1, -1, 1} {
		if err := b.RecordResult(context.Background(), "s1", pnl); err != nil {
			t.Fatal(err)
		}
	}
	if fb.trips != 0 {
		t.Fatalf("trips=%d", fb.trips)
	}
}

func TestConsecutiveLossBreaker_TripsOnThreshold(t *testing.T) {
	fb := &fakeBreaker{}
	b := NewConsecutiveLossBreaker(ConsecutiveLossConfig{Threshold: 3, Windows: 2}, fb)
	for i := 0; i < 3; i++ {
		_ = b.RecordResult(context.Background(), "s1", -1)
	}
	if fb.trips != 1 {
		t.Fatalf("trips=%d", fb.trips)
	}
	if fb.tripScopes[0] != "strategy:s1" {
		t.Fatalf("scope=%q", fb.tripScopes[0])
	}
	if !strings.Contains(fb.tripReasons[0], "consecutive_losses=3") {
		t.Fatalf("reason=%q", fb.tripReasons[0])
	}
}

func TestConsecutiveLossBreaker_TripsAndBlocksWindows(t *testing.T) {
	fb := &fakeBreaker{}
	b := NewConsecutiveLossBreaker(ConsecutiveLossConfig{Threshold: 1, Windows: 4}, fb)
	_ = b.RecordResult(context.Background(), "s1", -1)
	if got := b.Blocked("s1"); got != 4 {
		t.Fatalf("blocked=%d", got)
	}
}

func TestConsecutiveLossBreaker_WinAfterTripResetsCounter(t *testing.T) {
	fb := &fakeBreaker{}
	b := NewConsecutiveLossBreaker(ConsecutiveLossConfig{Threshold: 1, Windows: 4}, fb)
	_ = b.RecordResult(context.Background(), "s1", -1)
	_ = b.RecordResult(context.Background(), "s1", 1)
	if got := b.Blocked("s1"); got != 4 {
		t.Fatalf("blocked=%d", got)
	}
}

func TestConsecutiveLossBreaker_UnblockExpiredDecrementsAndResets(t *testing.T) {
	fb := &fakeBreaker{}
	b := NewConsecutiveLossBreaker(ConsecutiveLossConfig{Threshold: 1, Windows: 2}, fb)
	_ = b.RecordResult(context.Background(), "s1", -1)
	_ = b.UnblockExpired(context.Background())
	_ = b.UnblockExpired(context.Background())
	if fb.resets != 1 {
		t.Fatalf("resets=%d", fb.resets)
	}
	if got := b.Blocked("s1"); got != 0 {
		t.Fatalf("blocked=%d", got)
	}
}

func TestConsecutiveLossBreaker_ConcurrentRecordResult(*testing.T) {
	fb := &fakeBreaker{}
	b := NewConsecutiveLossBreaker(ConsecutiveLossConfig{}, fb)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = b.RecordResult(context.Background(), "s1", -1) }()
	}
	wg.Wait()
}

func TestConsecutiveLossBreaker_DefaultsAppliedWhenZero(t *testing.T) {
	b := NewConsecutiveLossBreaker(ConsecutiveLossConfig{}, &fakeBreaker{})
	if b.cfg.Threshold != 3 || b.cfg.Windows != 5 {
		t.Fatalf("cfg=%+v", b.cfg)
	}
}
