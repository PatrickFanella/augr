package universe

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data/polygon"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// mockUniverseRepo is a minimal in-memory implementation for testing.
type mockUniverseRepo struct {
	tickers []TrackedTicker
	scores  map[string]float64
}

func newMockRepo(tickers []TrackedTicker) *mockUniverseRepo {
	return &mockUniverseRepo{
		tickers: tickers,
		scores:  make(map[string]float64),
	}
}

func (m *mockUniverseRepo) Upsert(_ context.Context, ticker *TrackedTicker) error {
	for i, t := range m.tickers {
		if t.Ticker == ticker.Ticker {
			m.tickers[i] = *ticker
			return nil
		}
	}
	m.tickers = append(m.tickers, *ticker)
	return nil
}

func (m *mockUniverseRepo) UpsertBatch(_ context.Context, tickers []TrackedTicker) error {
	for _, t := range tickers {
		_ = m.Upsert(context.Background(), &t)
	}
	return nil
}

func (m *mockUniverseRepo) List(_ context.Context, filter ListFilter, limit, _ int) ([]TrackedTicker, error) {
	var result []TrackedTicker
	for _, t := range m.tickers {
		if filter.Active != nil && t.Active != *filter.Active {
			continue
		}
		if filter.IndexGroup != "" && t.IndexGroup != filter.IndexGroup {
			continue
		}
		if filter.Search != "" {
			search := strings.ToLower(filter.Search)
			if !strings.Contains(strings.ToLower(t.Ticker), search) &&
				!strings.Contains(strings.ToLower(t.Name), search) {
				continue
			}
		}
		result = append(result, t)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (m *mockUniverseRepo) Watchlist(_ context.Context, topN int) ([]TrackedTicker, error) {
	if topN > len(m.tickers) {
		topN = len(m.tickers)
	}
	return m.tickers[:topN], nil
}

func (m *mockUniverseRepo) UpdateScore(_ context.Context, ticker string, score float64) error {
	m.scores[ticker] = score
	return nil
}

func (m *mockUniverseRepo) Count(_ context.Context) (int, error) {
	return len(m.tickers), nil
}

func TestRunPreMarketScreen(t *testing.T) {
	t.Parallel()

	snapshots := []polygon.TickerSnapshot{
		{
			Ticker:          "AAPL",
			TodaysChangePct: 2.5,
			Day:             polygon.SnapshotBarForTest(185, 187, 184, 186, 52_000_000, 185.9),
			PrevDay:         polygon.SnapshotBarForTest(183, 186, 183, 184, 20_000_000, 184.5),
		},
		{
			Ticker:          "MSFT",
			TodaysChangePct: -1.8,
			Day:             polygon.SnapshotBarForTest(390, 395, 388, 391, 30_000_000, 391.5),
			PrevDay:         polygon.SnapshotBarForTest(395, 398, 393, 396, 25_000_000, 395.0),
		},
		{
			Ticker:          "PENNY",
			TodaysChangePct: 5.0,
			Day:             polygon.SnapshotBarForTest(2.0, 2.5, 1.9, 2.1, 100_000, 2.05),
			PrevDay:         polygon.SnapshotBarForTest(1.8, 2.0, 1.7, 2.0, 50_000, 1.9),
		},
		{
			Ticker:          "LOWVOL",
			TodaysChangePct: 0.1,
			Day:             polygon.SnapshotBarForTest(50, 51, 49, 50, 100, 50.0),
			PrevDay:         polygon.SnapshotBarForTest(50, 51, 49, 50, 200, 50.0),
		},
	}

	snapshotBody, _ := json.Marshal(map[string]any{"tickers": snapshots})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(snapshotBody)
	}))
	defer server.Close()

	client := polygon.NewClient("test-key", discardLogger())
	polygon.SetBaseURLForTest(client, server.URL)

	repo := newMockRepo([]TrackedTicker{
		{Ticker: "AAPL", Name: "Apple Inc.", Active: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{Ticker: "MSFT", Name: "Microsoft Corp.", Active: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{Ticker: "PENNY", Name: "Penny Stock", Active: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{Ticker: "LOWVOL", Name: "Low Volume", Active: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	})

	cfg := DefaultPreMarketConfig()
	cfg.TopN = 10

	results, err := RunPreMarketScreen(context.Background(), client, repo, cfg, discardLogger())
	if err != nil {
		t.Fatalf("RunPreMarketScreen() error = %v", err)
	}

	// PENNY should be filtered (price < MinPrice), LOWVOL filtered (volume < MinADV).
	// Only AAPL and MSFT should remain.
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	// AAPL should score higher: massive volume surge (52M/20M = 2.6x) + positive gap + change.
	if results[0].Ticker != "AAPL" {
		t.Errorf("top ticker = %s, want AAPL", results[0].Ticker)
	}
	if results[1].Ticker != "MSFT" {
		t.Errorf("second ticker = %s, want MSFT", results[1].Ticker)
	}

	// Verify scores were updated in repo.
	for _, r := range results {
		if score, ok := repo.scores[r.Ticker]; !ok || score <= 0 {
			t.Errorf("score for %s not updated or zero: %f", r.Ticker, score)
		}
	}

	// AAPL should have a volume surge reason (52M/20M = 2.6x >= 1.5).
	foundVolumeSurge := false
	for _, reason := range results[0].Reasons {
		if strings.Contains(reason, "Volume surge") {
			foundVolumeSurge = true
		}
	}
	if !foundVolumeSurge {
		t.Errorf("AAPL reasons %v, missing volume surge", results[0].Reasons)
	}
}

func TestRunPreMarketScreenEmptyUniverse(t *testing.T) {
	t.Parallel()

	repo := newMockRepo(nil)
	client := polygon.NewClient("test-key", discardLogger())
	cfg := DefaultPreMarketConfig()

	results, err := RunPreMarketScreen(context.Background(), client, repo, cfg, discardLogger())
	if err != nil {
		t.Fatalf("RunPreMarketScreen() error = %v", err)
	}
	if results != nil {
		t.Fatalf("got %d results, want nil", len(results))
	}
}

func TestDefaultPreMarketConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultPreMarketConfig()
	if cfg.MinADV != 500_000 {
		t.Errorf("MinADV = %f, want 500000", cfg.MinADV)
	}
	if cfg.MinPrice != 5.0 {
		t.Errorf("MinPrice = %f, want 5.0", cfg.MinPrice)
	}
	if cfg.MaxPrice != 500.0 {
		t.Errorf("MaxPrice = %f, want 500.0", cfg.MaxPrice)
	}
	if cfg.TopN != 30 {
		t.Errorf("TopN = %d, want 30", cfg.TopN)
	}
	if cfg.VolumeWeight+cfg.MomentumWeight+cfg.VolatilityWeight != 1.0 {
		t.Errorf("weights sum = %f, want 1.0", cfg.VolumeWeight+cfg.MomentumWeight+cfg.VolatilityWeight)
	}
}
