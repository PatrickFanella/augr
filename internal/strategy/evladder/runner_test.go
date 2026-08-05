package evladder

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	polymarketexec "github.com/PatrickFanella/get-rich-quick/internal/execution/polymarket"
	marketdata "github.com/PatrickFanella/get-rich-quick/internal/marketdata/polymarket"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
	"github.com/google/uuid"
)

type fakeBroker struct {
	mu         sync.Mutex
	prep, send int
}

func (f *fakeBroker) PrepareTemplate(_ *domain.Order) (*polymarketexec.OrderTemplate, error) {
	f.mu.Lock()
	f.prep++
	f.mu.Unlock()
	return &polymarketexec.OrderTemplate{}, nil
}

func (f *fakeBroker) SendTemplate(_ context.Context, _ *polymarketexec.OrderTemplate) (any, error) {
	f.mu.Lock()
	f.send++
	f.mu.Unlock()
	return "ok", nil
}

type fakeProb struct{ p float64 }

func (f fakeProb) MarketProbability(context.Context, string) (float64, error) { return f.p, nil }

type fakeBreaker struct{ err error }

func (f fakeBreaker) Allow(context.Context, string) error        { return f.err }
func (f fakeBreaker) Trip(context.Context, string, string) error { return nil }
func (f fakeBreaker) Reset(context.Context, string) error        { return nil }

func TestRunner_PlacesRungsOnFirstPoll(t *testing.T) {
	books := make(chan marketdata.BookSnapshot, 1)
	fb := &fakeBroker{}
	sid := uuid.NewString()
	r := NewRunner(RunnerConfig{StrategyID: sid, Slug: "m", BaseSize: 10, PollInterval: time.Hour}, fb, nil, fakeProb{0.5}, nil, books, slog.Default())
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go func() { _ = r.Run(ctx) }()
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		fb.mu.Lock()
		sent := fb.send
		fb.mu.Unlock()
		if sent > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no placements")
}

func TestRunner_BreakerTrippedSkipsPlacement(t *testing.T) {
	fb := &fakeBroker{}
	sid := uuid.NewString()
	r := NewRunner(RunnerConfig{StrategyID: sid, Slug: "m", BaseSize: 10, PollInterval: 10 * time.Millisecond}, fb, fakeBreaker{err: risk.ErrBreakerTripped}, fakeProb{0.5}, nil, nil, slog.Default())
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	go func() { _ = r.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	if fb.send != 0 {
		t.Fatal("expected zero")
	}
}

func TestRunner_RePlacesOnBigBookMove(t *testing.T) {
	fb := &fakeBroker{}
	sid := uuid.NewString()
	books := make(chan marketdata.BookSnapshot, 2)
	r := NewRunner(RunnerConfig{StrategyID: sid, Slug: "m", BaseSize: 10, PollInterval: time.Hour}, fb, nil, fakeProb{0.5}, nil, books, slog.Default())
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go func() { _ = r.Run(ctx) }()
	books <- marketdata.BookSnapshot{BestBid: 0.4, BestAsk: 0.6}
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		if r.ActiveCount() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r.ActiveCount() == 0 {
		t.Fatal("expected active")
	}
	books <- marketdata.BookSnapshot{BestBid: 0.5, BestAsk: 0.7}
	deadline = time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		if r.ActiveCount() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected cleared")
}

func TestRunner_ContextCancelReturns(t *testing.T) {
	fb := &fakeBroker{}
	sid := uuid.NewString()
	r := NewRunner(RunnerConfig{StrategyID: sid, Slug: "m", BaseSize: 10, PollInterval: 10 * time.Millisecond}, fb, nil, fakeProb{0.5}, nil, nil, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Run(ctx); err == nil {
		t.Fatal("expected ctx err")
	}
}
