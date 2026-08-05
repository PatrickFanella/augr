package polymarket

import (
	"context"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakePoolConn struct {
	id      int
	ticks   chan<- Tick
	books   chan<- BookSnapshot
	period  time.Duration
	done    chan struct{}
	started atomic.Bool
	closed  atomic.Bool
}

func (f *fakePoolConn) Dial(context.Context) error { return nil }

func (f *fakePoolConn) Run(ctx context.Context) error {
	if !f.started.CompareAndSwap(false, true) {
		return nil
	}
	period := f.period
	if period <= 0 {
		period = 5 * time.Millisecond
	}
	t := time.NewTicker(period)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			close(f.done)
			return ctx.Err()
		case <-t.C:
			select {
			case f.ticks <- Tick{ReceivedAt: time.Now()}:
			default:
			}
		}
	}
}

func (f *fakePoolConn) Close() error { f.closed.Store(true); return nil }

func TestPoolStartsAndStats(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WSURL = "ws://example.invalid"
	cfg.ConnectionsPerFeed = 3
	cfg.PruneInterval = 50 * time.Millisecond
	cfg.PruneFraction = 0.34
	cfg.StaggerStartup = 0
	cfg.JitterEMAAlpha = 0.5

	var mu sync.Mutex
	conns := []*fakePoolConn{}
	p := newPoolWithFactory(cfg, func(id int, _ Config, ticks chan<- Tick, books chan<- BookSnapshot, _ *atomic.Uint64) poolConnection {
		fc := &fakePoolConn{id: id, ticks: ticks, books: books, period: 10 * time.Millisecond, done: make(chan struct{})}
		mu.Lock()
		conns = append(conns, fc)
		mu.Unlock()
		return fc
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer p.Close()
	waitFor(t, time.Second, func() bool {
		st := p.Stats()
		return st.Members == 3 && st.AvgJitterMS > 0
	})
	_ = conns
}

func TestPoolPrunesSlowMember(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WSURL = "ws://example.invalid"
	cfg.ConnectionsPerFeed = 4
	cfg.PruneInterval = 20 * time.Millisecond
	cfg.PruneFraction = 0.5
	cfg.StaggerStartup = 0
	cfg.JitterEMAAlpha = 0.5

	periods := map[int]time.Duration{0: 5 * time.Millisecond, 1: 5 * time.Millisecond, 2: 40 * time.Millisecond, 3: 5 * time.Millisecond}
	p := newPoolWithFactory(cfg, func(id int, _ Config, ticks chan<- Tick, books chan<- BookSnapshot, _ *atomic.Uint64) poolConnection {
		return &fakePoolConn{id: id, ticks: ticks, books: books, period: periods[id], done: make(chan struct{})}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer p.Close()
	waitFor(t, 2*time.Second, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		ids := make([]int, 0, len(p.members))
		for id := range p.members {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		return len(ids) == 4 && !contains(ids, 2)
	})
}

func TestPoolCloseClosesChannels(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WSURL = "ws://example.invalid"
	cfg.ConnectionsPerFeed = 1
	cfg.StaggerStartup = 0
	p := newPoolWithFactory(cfg, func(id int, _ Config, ticks chan<- Tick, books chan<- BookSnapshot, _ *atomic.Uint64) poolConnection {
		return &fakePoolConn{id: id, ticks: ticks, books: books, period: 5 * time.Millisecond, done: make(chan struct{})}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	p.Close()
	if _, ok := <-p.Ticks(); ok {
		t.Fatal("ticks should be closed")
	}
	if _, ok := <-p.Books(); ok {
		t.Fatal("books should be closed")
	}
	_ = runtime.NumGoroutine()
}

func waitFor(t *testing.T, d time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout")
}

func contains(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
