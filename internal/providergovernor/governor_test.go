package providergovernor

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memoryCooldownStore struct{ until time.Time }

func (s *memoryCooldownStore) GetProviderCooldown(context.Context, string) (time.Time, error) {
	return s.until, nil
}

func (s *memoryCooldownStore) SetProviderCooldown(context.Context, string, time.Time) error {
	return nil
}

func (s *memoryCooldownStore) CompareAndClearProviderCooldown(context.Context, string, time.Time) (bool, error) {
	s.until = time.Time{}
	return true, nil
}

func TestProviderGovernorReserveChecksCooldownBeforeLimiter(t *testing.T) {
	t.Parallel()
	store := &memoryCooldownStore{until: time.Now().Add(time.Hour)}
	gov := &ProviderGovernor{Provider: "kalshi", Cooldown: store, Limiter: LimiterFunc(func(context.Context) error { return errors.New("should not call limiter") })}
	if err := gov.Reserve(context.Background()); err == nil {
		t.Fatal("Reserve() error = nil, want cooldown")
	}
}

func TestProviderGovernorReserveClearsExpiredCooldown(t *testing.T) {
	t.Parallel()
	store := &memoryCooldownStore{until: time.Now().Add(-time.Minute)}
	called := false
	gov := &ProviderGovernor{Provider: "kalshi", Cooldown: store, Limiter: LimiterFunc(func(context.Context) error { called = true; return nil })}
	if err := gov.Reserve(context.Background()); err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	if !called {
		t.Fatal("limiter not called")
	}
}

type LimiterFunc func(context.Context) error

func (f LimiterFunc) Wait(ctx context.Context) error { return f(ctx) }
