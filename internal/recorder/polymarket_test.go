package recorder

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	polymarket "github.com/PatrickFanella/get-rich-quick/internal/marketdata/polymarket"
)

type fakeRepo struct {
	mu    sync.Mutex
	ticks [][]domain.PolymarketTick
	books [][]domain.PolymarketBookSnapshot
}

func (f *fakeRepo) InsertTicks(_ context.Context, ticks []domain.PolymarketTick) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ticks = append(f.ticks, append([]domain.PolymarketTick(nil), ticks...))
	return nil
}

func (f *fakeRepo) InsertBookSnapshots(_ context.Context, snaps []domain.PolymarketBookSnapshot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.books = append(f.books, append([]domain.PolymarketBookSnapshot(nil), snaps...))
	return nil
}

func (f *fakeRepo) QueryTicks(context.Context, string, time.Time, time.Time, int) ([]domain.PolymarketTick, error) {
	return nil, nil
}

func (f *fakeRepo) QueryBookAt(context.Context, string, time.Time) (*domain.PolymarketBookSnapshot, error) {
	return nil, nil
}

type fakeMetrics struct{ dropped int }

func (f *fakeMetrics) IncInserted(string, int)           {}
func (f *fakeMetrics) ObserveLagSeconds(string, float64) {}
func (f *fakeMetrics) IncDropped(_ string, n int)        { f.dropped += n }

type fakeFeed struct {
	ticks chan polymarket.Tick
	books chan polymarket.BookSnapshot
}

func (f *fakeFeed) Ticks(string) <-chan polymarket.Tick         { return f.ticks }
func (f *fakeFeed) Books(string) <-chan polymarket.BookSnapshot { return f.books }

func TestRecorderBatchesOnSize(t *testing.T) {
	feed := &fakeFeed{ticks: make(chan polymarket.Tick, 2), books: make(chan polymarket.BookSnapshot, 2)}
	repo := &fakeRepo{}
	metrics := &fakeMetrics{}
	r := New(feed, repo, RecorderConfig{BatchSize: 2, FlushInterval: time.Hour, Slugs: []string{"btc"}}, nil, metrics)
	r.Start(context.Background())
	feed.ticks <- polymarket.Tick{Slug: "btc"}
	feed.ticks <- polymarket.Tick{Slug: "btc"}
	time.Sleep(50 * time.Millisecond)
	r.Close()
	if len(repo.ticks) != 1 || len(repo.ticks[0]) != 2 {
		t.Fatalf("batches = %#v, want one batch of 2", repo.ticks)
	}
}

func TestRecorderFlushOnIntervalAndClose(t *testing.T) {
	feed := &fakeFeed{ticks: make(chan polymarket.Tick, 1), books: make(chan polymarket.BookSnapshot, 1)}
	repo := &fakeRepo{}
	metrics := &fakeMetrics{}
	r := New(feed, repo, RecorderConfig{BatchSize: 10, FlushInterval: 20 * time.Millisecond, Slugs: []string{"btc"}}, nil, metrics)
	r.Start(context.Background())
	feed.books <- polymarket.BookSnapshot{Slug: "btc"}
	time.Sleep(50 * time.Millisecond)
	r.Close()
	if len(repo.books) == 0 {
		t.Fatal("expected interval or close flush")
	}
}
