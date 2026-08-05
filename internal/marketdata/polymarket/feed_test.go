package polymarket

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFeedLifecycleAndRouting(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WSURL = "ws://example.invalid"
	cfg.ConnectionsPerFeed = 1
	cfg.PruneInterval = 0
	cfg.WarmupDuration = 0
	cfg.WarmupMinClean = 1
	f, err := newFeedWithFactory(cfg, func(id int, _ Config, ticks chan<- Tick, books chan<- BookSnapshot, _ *atomic.Uint64) poolConnection {
		return &fakePoolConn{id: id, ticks: ticks, books: books, period: 5 * time.Millisecond, done: make(chan struct{})}
	})
	if err != nil {
		t.Fatalf("NewFeed() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := f.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	ch := f.Ticks("slug-a")
	f.cleaner.handleTick(Tick{Slug: "slug-a", Price: 1.23, Side: "yes", Size: 10, ReceivedAt: time.Now().UTC()})
	f.cleaner.handleTick(Tick{Slug: "slug-a", Price: 1.24, Side: "yes", Size: 10, ReceivedAt: time.Now().UTC().Add(time.Millisecond)})
	select {
	case tk := <-ch:
		if tk.Slug != "slug-a" {
			t.Fatalf("tick slug = %q", tk.Slug)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for tick")
	}
	if !f.Ready("slug-a") && f.Stats().Pool.Members == 0 {
		t.Fatal("expected feed to be started")
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		f.Close()
	}()
	wg.Wait()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected tick subscriber to be closed after feed shutdown")
		}
	default:
		t.Fatal("expected closed tick subscriber to be readable immediately")
	}
	if got := f.Stats().Subscribers; got != 0 {
		t.Fatalf("Subscribers = %d, want 0 after shutdown", got)
	}
	lateTick := f.Ticks("slug-b")
	select {
	case _, ok := <-lateTick:
		if ok {
			t.Fatal("expected late tick subscriber to be closed")
		}
	default:
		t.Fatal("expected late tick subscriber to be readable immediately")
	}
	lateBook := f.Books("slug-b")
	select {
	case _, ok := <-lateBook:
		if ok {
			t.Fatal("expected late book subscriber to be closed")
		}
	default:
		t.Fatal("expected late book subscriber to be readable immediately")
	}
}
