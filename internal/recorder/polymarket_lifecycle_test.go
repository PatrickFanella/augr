package recorder

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

type lifecycleRepo struct {
	mu          sync.Mutex
	tickBatches [][]domain.PolymarketTick
	bookBatches [][]domain.PolymarketBookSnapshot
	blockInsert chan struct{}
	insertStart chan struct{}
	startOnce   sync.Once
}

func (r *lifecycleRepo) InsertTicks(_ context.Context, ticks []domain.PolymarketTick) error {
	r.signalStart()
	if r.blockInsert != nil {
		<-r.blockInsert
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tickBatches = append(r.tickBatches, append([]domain.PolymarketTick(nil), ticks...))
	return nil
}

func (r *lifecycleRepo) InsertBookSnapshots(_ context.Context, snaps []domain.PolymarketBookSnapshot) error {
	r.signalStart()
	if r.blockInsert != nil {
		<-r.blockInsert
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bookBatches = append(r.bookBatches, append([]domain.PolymarketBookSnapshot(nil), snaps...))
	return nil
}

func (r *lifecycleRepo) QueryTicks(context.Context, string, time.Time, time.Time, int) ([]domain.PolymarketTick, error) {
	return nil, nil
}

func (r *lifecycleRepo) QueryBookAt(context.Context, string, time.Time) (*domain.PolymarketBookSnapshot, error) {
	return nil, nil
}

func (r *lifecycleRepo) signalStart() {
	r.startOnce.Do(func() {
		if r.insertStart != nil {
			close(r.insertStart)
		}
	})
}

type lifecycleMetrics struct {
	mu       sync.Mutex
	dropped  map[string]int
	inserted map[string]int
	lag      map[string]float64
}

func (m *lifecycleMetrics) IncInserted(kind string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inserted == nil {
		m.inserted = map[string]int{}
	}
	m.inserted[kind] += n
}

func (m *lifecycleMetrics) ObserveLagSeconds(kind string, sec float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lag == nil {
		m.lag = map[string]float64{}
	}
	m.lag[kind] = sec
}

func (m *lifecycleMetrics) IncDropped(kind string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dropped == nil {
		m.dropped = map[string]int{}
	}
	m.dropped[kind] += n
}

func TestPolymarketLifecycleFlushesOnBatchSize(t *testing.T) {
	repo := &lifecycleRepo{}
	metrics := &lifecycleMetrics{}
	l := newPolymarketLifecycle(repo, RecorderConfig{BatchSize: 2, FlushInterval: time.Hour}, metrics)
	l.Start()
	defer l.Close()

	if !l.SubmitTick(domain.PolymarketTick{Slug: "btc", ReceivedAt: time.Now().UTC()}) {
		t.Fatal("expected tick submit to succeed")
	}
	if !l.SubmitTick(domain.PolymarketTick{Slug: "btc", ReceivedAt: time.Now().UTC().Add(time.Millisecond)}) {
		t.Fatal("expected tick submit to succeed")
	}

	waitForCondition(t, 200*time.Millisecond, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return len(repo.tickBatches) == 1 && len(repo.tickBatches[0]) == 2
	})
}

func TestPolymarketLifecycleFlushesOnInterval(t *testing.T) {
	repo := &lifecycleRepo{}
	metrics := &lifecycleMetrics{}
	l := newPolymarketLifecycle(repo, RecorderConfig{BatchSize: 10, FlushInterval: 20 * time.Millisecond}, metrics)
	l.Start()
	defer l.Close()

	l.SubmitBook(domain.PolymarketBookSnapshot{Slug: "btc", ReceivedAt: time.Now().UTC()})

	waitForCondition(t, 200*time.Millisecond, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return len(repo.bookBatches) == 1 && len(repo.bookBatches[0]) == 1
	})
}

func TestPolymarketLifecycleFlushesOnShutdown(t *testing.T) {
	repo := &lifecycleRepo{}
	metrics := &lifecycleMetrics{}
	l := newPolymarketLifecycle(repo, RecorderConfig{BatchSize: 10, FlushInterval: time.Hour}, metrics)
	l.Start()

	l.SubmitTick(domain.PolymarketTick{Slug: "btc", ReceivedAt: time.Now().UTC()})
	l.Close()

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.tickBatches) != 1 || len(repo.tickBatches[0]) != 1 {
		t.Fatalf("tick batches = %#v, want one batch of 1", repo.tickBatches)
	}
}

func TestPolymarketLifecycleDropsWhenInputFull(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	repo := &lifecycleRepo{blockInsert: release, insertStart: started}
	metrics := &lifecycleMetrics{}
	l := newPolymarketLifecycle(repo, RecorderConfig{BatchSize: 1, FlushInterval: time.Hour}, metrics)
	l.Start()
	defer l.Close()
	defer close(release)

	if !l.SubmitTick(domain.PolymarketTick{Slug: "btc", ReceivedAt: time.Now().UTC()}) {
		t.Fatal("expected first tick submit to succeed")
	}
	waitForCondition(t, 200*time.Millisecond, func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	})

	if !l.SubmitTick(domain.PolymarketTick{Slug: "btc", ReceivedAt: time.Now().UTC().Add(time.Millisecond)}) {
		t.Fatal("expected second tick submit to succeed while queue has room")
	}
	if l.SubmitTick(domain.PolymarketTick{Slug: "btc", ReceivedAt: time.Now().UTC().Add(2 * time.Millisecond)}) {
		t.Fatal("expected third tick submit to drop while batcher is blocked")
	}

	waitForCondition(t, 200*time.Millisecond, func() bool {
		metrics.mu.Lock()
		defer metrics.mu.Unlock()
		return metrics.dropped["tick"] >= 1
	})
}

func TestPolymarketLifecycleObservesLag(t *testing.T) {
	repo := &lifecycleRepo{}
	metrics := &lifecycleMetrics{}
	l := newPolymarketLifecycle(repo, RecorderConfig{BatchSize: 10, FlushInterval: 20 * time.Millisecond}, metrics)
	l.Start()
	defer l.Close()

	l.SubmitTick(domain.PolymarketTick{Slug: "btc", ReceivedAt: time.Now().UTC()})
	time.Sleep(30 * time.Millisecond)

	waitForCondition(t, 200*time.Millisecond, func() bool {
		metrics.mu.Lock()
		defer metrics.mu.Unlock()
		return metrics.lag["tick"] > 0.01
	})
}

func waitForCondition(t *testing.T, d time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("timeout waiting for condition")
}
