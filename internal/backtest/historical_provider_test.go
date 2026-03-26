package backtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestHistoricalDataProviderGetOHLCV_FiltersToSimTime(t *testing.T) {
	t.Parallel()

	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)
	t3 := t2.Add(24 * time.Hour)

	bars := []domain.OHLCV{
		makeBar(t1, 100),
		makeBar(t2, 101),
		makeBar(t3, 102),
	}

	// Build a replay iterator and advance to the second bar.
	iter, err := NewReplayIterator(bars)
	if err != nil {
		t.Fatalf("NewReplayIterator() error = %v", err)
	}
	iter.Next() // t1
	iter.Next() // t2

	prov, err := NewHistoricalDataProvider(iter.Clock(), WithBars(bars))
	if err != nil {
		t.Fatalf("NewHistoricalDataProvider() error = %v", err)
	}

	// Request all bars; only bars up to t2 (the current sim time) should be
	// returned.
	got, err := prov.GetOHLCV(context.Background(), "AAPL", data.Timeframe1d, t1, t3)
	if err != nil {
		t.Fatalf("GetOHLCV() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("GetOHLCV() len = %d, want 2", len(got))
	}
	if !got[0].Timestamp.Equal(t1) || !got[1].Timestamp.Equal(t2) {
		t.Errorf("GetOHLCV() timestamps = [%v, %v], want [%v, %v]",
			got[0].Timestamp, got[1].Timestamp, t1, t2)
	}
}

func TestHistoricalDataProviderGetOHLCV_FiltersByDateRange(t *testing.T) {
	t.Parallel()

	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)
	t3 := t2.Add(24 * time.Hour)

	bars := []domain.OHLCV{
		makeBar(t1, 100),
		makeBar(t2, 101),
		makeBar(t3, 102),
	}

	iter, err := NewReplayIterator(bars)
	if err != nil {
		t.Fatalf("NewReplayIterator() error = %v", err)
	}
	iter.Next()
	iter.Next()
	iter.Next() // advance to t3

	prov, err := NewHistoricalDataProvider(iter.Clock(), WithBars(bars))
	if err != nil {
		t.Fatalf("NewHistoricalDataProvider() error = %v", err)
	}

	// Request only bars in [t2, t3].
	got, err := prov.GetOHLCV(context.Background(), "AAPL", data.Timeframe1d, t2, t3)
	if err != nil {
		t.Fatalf("GetOHLCV() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("GetOHLCV() len = %d, want 2", len(got))
	}
	if !got[0].Timestamp.Equal(t2) {
		t.Errorf("GetOHLCV() first timestamp = %v, want %v", got[0].Timestamp, t2)
	}
}

func TestHistoricalDataProviderGetOHLCV_EmptyBars(t *testing.T) {
	t.Parallel()

	clock := newSimulatedClock()
	clock.set(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))

	prov, err := NewHistoricalDataProvider(clock)
	if err != nil {
		t.Fatalf("NewHistoricalDataProvider() error = %v", err)
	}

	got, err := prov.GetOHLCV(context.Background(), "AAPL", data.Timeframe1d,
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetOHLCV() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("GetOHLCV() len = %d, want 0", len(got))
	}
}

func TestHistoricalDataProviderGetFundamentals(t *testing.T) {
	t.Parallel()

	clock := newSimulatedClock()
	clock.set(time.Now())

	t.Run("returns fundamentals when set", func(t *testing.T) {
		f := &data.Fundamentals{
			Ticker:    "AAPL",
			MarketCap: 3e12,
			PERatio:   30,
		}
		prov, err := NewHistoricalDataProvider(clock, WithFundamentals(f))
		if err != nil {
			t.Fatalf("NewHistoricalDataProvider() error = %v", err)
		}
		got, err := prov.GetFundamentals(context.Background(), "AAPL")
		if err != nil {
			t.Fatalf("GetFundamentals() error = %v", err)
		}
		if got.MarketCap != 3e12 {
			t.Errorf("GetFundamentals().MarketCap = %v, want 3e12", got.MarketCap)
		}
	})

	t.Run("returns ErrNotImplemented when nil", func(t *testing.T) {
		prov, err := NewHistoricalDataProvider(clock)
		if err != nil {
			t.Fatalf("NewHistoricalDataProvider() error = %v", err)
		}
		_, err = prov.GetFundamentals(context.Background(), "AAPL")
		if err != data.ErrNotImplemented {
			t.Fatalf("GetFundamentals() error = %v, want %v", err, data.ErrNotImplemented)
		}
	})
}

func TestHistoricalDataProviderGetNews_FiltersToSimTime(t *testing.T) {
	t.Parallel()

	t1 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	t3 := t2.Add(time.Hour)

	news := []data.NewsArticle{
		{Title: "A", PublishedAt: t1},
		{Title: "B", PublishedAt: t2},
		{Title: "C", PublishedAt: t3},
	}

	clock := newSimulatedClock()
	clock.set(t2) // sim time = t2

	prov, err := NewHistoricalDataProvider(clock, WithNews(news))
	if err != nil {
		t.Fatalf("NewHistoricalDataProvider() error = %v", err)
	}

	got, err := prov.GetNews(context.Background(), "AAPL", t1, t3)
	if err != nil {
		t.Fatalf("GetNews() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("GetNews() len = %d, want 2", len(got))
	}
	if got[0].Title != "A" || got[1].Title != "B" {
		t.Errorf("GetNews() titles = [%s, %s], want [A, B]", got[0].Title, got[1].Title)
	}
}

func TestHistoricalDataProviderGetSocialSentiment_FiltersToSimTime(t *testing.T) {
	t.Parallel()

	t1 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	t3 := t2.Add(time.Hour)

	social := []data.SocialSentiment{
		{Ticker: "AAPL", MeasuredAt: t1, Score: 0.5},
		{Ticker: "AAPL", MeasuredAt: t2, Score: 0.6},
		{Ticker: "AAPL", MeasuredAt: t3, Score: 0.7},
	}

	clock := newSimulatedClock()
	clock.set(t2)

	prov, err := NewHistoricalDataProvider(clock, WithSocialSentiment(social))
	if err != nil {
		t.Fatalf("NewHistoricalDataProvider() error = %v", err)
	}

	got, err := prov.GetSocialSentiment(context.Background(), "AAPL", t1, t3)
	if err != nil {
		t.Fatalf("GetSocialSentiment() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("GetSocialSentiment() len = %d, want 2", len(got))
	}
	if got[1].Score != 0.6 {
		t.Errorf("GetSocialSentiment()[1].Score = %v, want 0.6", got[1].Score)
	}
}

func TestHistoricalDataProviderSortsInputData(t *testing.T) {
	t.Parallel()

	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)
	t3 := t2.Add(24 * time.Hour)

	// Supply bars out of order.
	bars := []domain.OHLCV{
		makeBar(t3, 102),
		makeBar(t1, 100),
		makeBar(t2, 101),
	}

	iter, err := NewReplayIterator([]domain.OHLCV{makeBar(t1, 100), makeBar(t2, 101), makeBar(t3, 102)})
	if err != nil {
		t.Fatalf("NewReplayIterator() error = %v", err)
	}
	iter.Next()
	iter.Next()
	iter.Next()

	prov, err := NewHistoricalDataProvider(iter.Clock(), WithBars(bars))
	if err != nil {
		t.Fatalf("NewHistoricalDataProvider() error = %v", err)
	}

	got, err := prov.GetOHLCV(context.Background(), "AAPL", data.Timeframe1d, t1, t3)
	if err != nil {
		t.Fatalf("GetOHLCV() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("GetOHLCV() len = %d, want 3", len(got))
	}
	// Verify sorted order.
	for i := 1; i < len(got); i++ {
		if got[i].Timestamp.Before(got[i-1].Timestamp) {
			t.Fatalf("GetOHLCV() bars not sorted at index %d", i)
		}
	}
}

func TestNewHistoricalDataProviderRejectsNilClock(t *testing.T) {
	t.Parallel()

	_, err := NewHistoricalDataProvider(nil)
	if err != ErrNilClock {
		t.Fatalf("NewHistoricalDataProvider(nil) error = %v, want %v", err, ErrNilClock)
	}
}

func TestHistoricalDataProviderRejectsEmptyTicker(t *testing.T) {
	t.Parallel()

	clock := newSimulatedClock()
	clock.set(time.Now())
	prov, err := NewHistoricalDataProvider(clock)
	if err != nil {
		t.Fatalf("NewHistoricalDataProvider() error = %v", err)
	}

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	_, err = prov.GetOHLCV(context.Background(), "", data.Timeframe1d, from, to)
	if err != ErrEmptyTicker {
		t.Fatalf("GetOHLCV() error = %v, want %v", err, ErrEmptyTicker)
	}

	_, err = prov.GetNews(context.Background(), "", from, to)
	if err != ErrEmptyTicker {
		t.Fatalf("GetNews() error = %v, want %v", err, ErrEmptyTicker)
	}

	_, err = prov.GetSocialSentiment(context.Background(), "", from, to)
	if err != ErrEmptyTicker {
		t.Fatalf("GetSocialSentiment() error = %v, want %v", err, ErrEmptyTicker)
	}
}

func TestHistoricalDataProviderRejectsInvalidRange(t *testing.T) {
	t.Parallel()

	clock := newSimulatedClock()
	clock.set(time.Now())
	prov, err := NewHistoricalDataProvider(clock)
	if err != nil {
		t.Fatalf("NewHistoricalDataProvider() error = %v", err)
	}

	from := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) // before from

	_, err = prov.GetOHLCV(context.Background(), "AAPL", data.Timeframe1d, from, to)
	if !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("GetOHLCV() error = %v, want %v", err, ErrInvalidRange)
	}
}
