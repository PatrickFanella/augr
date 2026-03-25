package data

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type fakeHistoricalCacheRepo struct {
	getCalls int
	setCalls int
}

func (f *fakeHistoricalCacheRepo) Get(context.Context, repository.MarketDataCacheKey) (*domain.MarketData, error) {
	f.getCalls++
	return nil, errors.New("unexpected cache get")
}

func (f *fakeHistoricalCacheRepo) Set(context.Context, *domain.MarketData) error {
	f.setCalls++
	return errors.New("unexpected cache set")
}

func (f *fakeHistoricalCacheRepo) Expire(context.Context, repository.MarketDataCacheExpireFilter) error {
	return nil
}

type historicalProviderCall struct {
	ticker    string
	timeframe Timeframe
	from      time.Time
	to        time.Time
}

type historicalStubProvider struct {
	calls  []historicalProviderCall
	getFn  func(ticker string, timeframe Timeframe, from, to time.Time) ([]domain.OHLCV, error)
	getErr error
}

func (s *historicalStubProvider) GetOHLCV(_ context.Context, ticker string, timeframe Timeframe, from, to time.Time) ([]domain.OHLCV, error) {
	s.calls = append(s.calls, historicalProviderCall{
		ticker:    ticker,
		timeframe: timeframe,
		from:      from.UTC(),
		to:        to.UTC(),
	})
	if s.getFn != nil {
		return s.getFn(ticker, timeframe, from.UTC(), to.UTC())
	}
	return nil, s.getErr
}

func (s *historicalStubProvider) GetFundamentals(context.Context, string) (Fundamentals, error) {
	return Fundamentals{}, ErrNotImplemented
}

func (s *historicalStubProvider) GetNews(context.Context, string, time.Time, time.Time) ([]NewsArticle, error) {
	return nil, ErrNotImplemented
}

func (s *historicalStubProvider) GetSocialSentiment(context.Context, string, time.Time, time.Time) ([]SocialSentiment, error) {
	return nil, ErrNotImplemented
}

type fakeHistoricalOHLCVRepo struct {
	bars     map[string]domain.HistoricalOHLCV
	coverage map[string]domain.HistoricalOHLCVCoverage
}

func newFakeHistoricalOHLCVRepo() *fakeHistoricalOHLCVRepo {
	return &fakeHistoricalOHLCVRepo{
		bars:     make(map[string]domain.HistoricalOHLCV),
		coverage: make(map[string]domain.HistoricalOHLCVCoverage),
	}
}

func (f *fakeHistoricalOHLCVRepo) UpsertHistoricalOHLCV(_ context.Context, bars []domain.HistoricalOHLCV) error {
	for _, bar := range bars {
		f.bars[historicalBarKey(bar)] = bar
	}
	return nil
}

func (f *fakeHistoricalOHLCVRepo) ListHistoricalOHLCV(_ context.Context, filter repository.HistoricalOHLCVFilter) ([]domain.HistoricalOHLCV, error) {
	result := make([]domain.HistoricalOHLCV, 0)
	for _, bar := range f.bars {
		if filter.Ticker != "" && bar.Ticker != filter.Ticker {
			continue
		}
		if filter.Provider != "" && bar.Provider != filter.Provider {
			continue
		}
		if filter.Timeframe != "" && bar.Timeframe != filter.Timeframe {
			continue
		}
		if !filter.From.IsZero() && bar.Timestamp.Before(filter.From) {
			continue
		}
		if !filter.To.IsZero() && bar.Timestamp.After(filter.To) {
			continue
		}
		result = append(result, bar)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.Before(result[j].Timestamp)
	})

	return result, nil
}

func (f *fakeHistoricalOHLCVRepo) UpsertHistoricalOHLCVCoverage(_ context.Context, coverage domain.HistoricalOHLCVCoverage) error {
	f.coverage[historicalCoverageKey(coverage)] = coverage
	return nil
}

func (f *fakeHistoricalOHLCVRepo) ListHistoricalOHLCVCoverage(_ context.Context, filter repository.HistoricalOHLCVCoverageFilter) ([]domain.HistoricalOHLCVCoverage, error) {
	result := make([]domain.HistoricalOHLCVCoverage, 0)
	for _, item := range f.coverage {
		if filter.Ticker != "" && item.Ticker != filter.Ticker {
			continue
		}
		if filter.Provider != "" && item.Provider != filter.Provider {
			continue
		}
		if filter.Timeframe != "" && item.Timeframe != filter.Timeframe {
			continue
		}
		if !filter.From.IsZero() && item.DateTo.Before(filter.From) {
			continue
		}
		if !filter.To.IsZero() && item.DateFrom.After(filter.To) {
			continue
		}
		result = append(result, item)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].DateFrom.Equal(result[j].DateFrom) {
			return result[i].DateTo.Before(result[j].DateTo)
		}
		return result[i].DateFrom.Before(result[j].DateFrom)
	})

	return result, nil
}

func TestDataServiceDownloadHistoricalOHLCVIncrementalFetchesOnlyMissingRanges(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	providerName := cacheProviderStockChain
	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	day2 := from.Add(24 * time.Hour)
	day3 := day2.Add(24 * time.Hour)
	day4 := day3.Add(24 * time.Hour)
	day5 := day4.Add(24 * time.Hour)

	repo := newFakeHistoricalOHLCVRepo()
	_ = repo.UpsertHistoricalOHLCV(context.Background(), []domain.HistoricalOHLCV{
		{Ticker: "AAPL", Provider: providerName, Timeframe: Timeframe1d.String(), Timestamp: from, Open: 100, High: 101, Low: 99, Close: 100.5, Volume: 1000},
		{Ticker: "AAPL", Provider: providerName, Timeframe: Timeframe1d.String(), Timestamp: day2, Open: 101, High: 102, Low: 100, Close: 101.5, Volume: 1100},
	})
	_ = repo.UpsertHistoricalOHLCVCoverage(context.Background(), domain.HistoricalOHLCVCoverage{
		Ticker: "AAPL", Provider: providerName, Timeframe: Timeframe1d.String(), DateFrom: from, DateTo: day2, FetchedAt: day2,
	})

	provider := &historicalStubProvider{
		getFn: func(ticker string, timeframe Timeframe, gapFrom, gapTo time.Time) ([]domain.OHLCV, error) {
			if ticker != "AAPL" {
				return nil, errors.New("unexpected ticker")
			}
			if timeframe != Timeframe1d {
				return nil, errors.New("unexpected timeframe")
			}
			if !gapFrom.Equal(day3) || !gapTo.Equal(day5) {
				return nil, errors.New("unexpected gap request")
			}
			return []domain.OHLCV{
				{Timestamp: day3, Open: 102, High: 103, Low: 101, Close: 102.5, Volume: 1200},
				{Timestamp: day4, Open: 103, High: 104, Low: 102, Close: 103.5, Volume: 1300},
				{Timestamp: day5, Open: 104, High: 105, Low: 103, Close: 104.5, Volume: 1400},
			}, nil
		},
	}

	service := &DataService{
		stockChain:  provider,
		historyRepo: repo,
		logger:      logger,
		now:         func() time.Time { return day5.Add(time.Hour) },
	}

	got, err := service.DownloadHistoricalOHLCV(context.Background(), domain.MarketTypeStock, []string{"AAPL"}, Timeframe1d, from, day5, true)
	if err != nil {
		t.Fatalf("DownloadHistoricalOHLCV() error = %v", err)
	}

	if len(provider.calls) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(provider.calls))
	}
	if !provider.calls[0].from.Equal(day3) || !provider.calls[0].to.Equal(day5) {
		t.Fatalf("provider call range = %s..%s, want %s..%s", provider.calls[0].from, provider.calls[0].to, day3, day5)
	}

	bars := got["AAPL"]
	if len(bars) != 5 {
		t.Fatalf("len(got[\"AAPL\"]) = %d, want 5", len(bars))
	}
	if !bars[0].Timestamp.Equal(from) || !bars[4].Timestamp.Equal(day5) {
		t.Fatalf("returned range = %s..%s, want %s..%s", bars[0].Timestamp, bars[4].Timestamp, from, day5)
	}

	coverage, err := repo.ListHistoricalOHLCVCoverage(context.Background(), repository.HistoricalOHLCVCoverageFilter{
		Ticker: "AAPL", Provider: providerName, Timeframe: Timeframe1d.String(),
	})
	if err != nil {
		t.Fatalf("ListHistoricalOHLCVCoverage() error = %v", err)
	}
	if len(coverage) != 2 {
		t.Fatalf("len(coverage) = %d, want 2", len(coverage))
	}
	if !coverage[1].DateFrom.Equal(day3) || !coverage[1].DateTo.Equal(day5) {
		t.Fatalf("new coverage = %s..%s, want %s..%s", coverage[1].DateFrom, coverage[1].DateTo, day3, day5)
	}
}

func TestDataServiceDownloadHistoricalOHLCVTracksEmptyCoverageForIncrementalUpdates(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	from := time.Date(2026, 3, 7, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)

	repo := newFakeHistoricalOHLCVRepo()
	provider := &historicalStubProvider{
		getFn: func(_ string, _ Timeframe, gapFrom, gapTo time.Time) ([]domain.OHLCV, error) {
			if !gapFrom.Equal(from) || !gapTo.Equal(to) {
				return nil, errors.New("unexpected gap request")
			}
			return []domain.OHLCV{}, nil
		},
	}

	service := &DataService{
		stockChain:  provider,
		historyRepo: repo,
		logger:      logger,
		now:         func() time.Time { return to.Add(time.Hour) },
	}

	for i := 0; i < 2; i++ {
		got, err := service.DownloadHistoricalOHLCV(context.Background(), domain.MarketTypeStock, []string{"AAPL"}, Timeframe1d, from, to, true)
		if err != nil {
			t.Fatalf("DownloadHistoricalOHLCV() run %d error = %v", i+1, err)
		}
		if len(got["AAPL"]) != 0 {
			t.Fatalf("len(got[\"AAPL\"]) run %d = %d, want 0", i+1, len(got["AAPL"]))
		}
	}

	if len(provider.calls) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(provider.calls))
	}
}

func TestDataServiceDownloadHistoricalOHLCVIncrementalFetchesOnlyMissingSubRanges(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	providerName := cacheProviderStockChain
	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	day2 := from.Add(24 * time.Hour)
	day3 := day2.Add(24 * time.Hour)
	day4 := day3.Add(24 * time.Hour)
	day5 := day4.Add(24 * time.Hour)
	day6 := day5.Add(24 * time.Hour)
	day7 := day6.Add(24 * time.Hour)

	repo := newFakeHistoricalOHLCVRepo()
	_ = repo.UpsertHistoricalOHLCV(context.Background(), []domain.HistoricalOHLCV{
		{Ticker: "AAPL", Provider: providerName, Timeframe: Timeframe1d.String(), Timestamp: day3, Open: 102, High: 103, Low: 101, Close: 102.5, Volume: 1200},
		{Ticker: "AAPL", Provider: providerName, Timeframe: Timeframe1d.String(), Timestamp: day4, Open: 103, High: 104, Low: 102, Close: 103.5, Volume: 1300},
		{Ticker: "AAPL", Provider: providerName, Timeframe: Timeframe1d.String(), Timestamp: day5, Open: 104, High: 105, Low: 103, Close: 104.5, Volume: 1400},
	})
	_ = repo.UpsertHistoricalOHLCVCoverage(context.Background(), domain.HistoricalOHLCVCoverage{
		Ticker: "AAPL", Provider: providerName, Timeframe: Timeframe1d.String(), DateFrom: day3, DateTo: day5, FetchedAt: day5,
	})

	provider := &historicalStubProvider{
		getFn: func(ticker string, timeframe Timeframe, gapFrom, gapTo time.Time) ([]domain.OHLCV, error) {
			if ticker != "AAPL" {
				return nil, errors.New("unexpected ticker")
			}
			if timeframe != Timeframe1d {
				return nil, errors.New("unexpected timeframe")
			}

			switch {
			case gapFrom.Equal(from) && gapTo.Equal(day2):
				return []domain.OHLCV{
					{Timestamp: from, Open: 100, High: 101, Low: 99, Close: 100.5, Volume: 1000},
					{Timestamp: day2, Open: 101, High: 102, Low: 100, Close: 101.5, Volume: 1100},
				}, nil
			case gapFrom.Equal(day6) && gapTo.Equal(day7):
				return []domain.OHLCV{
					{Timestamp: day6, Open: 105, High: 106, Low: 104, Close: 105.5, Volume: 1500},
					{Timestamp: day7, Open: 106, High: 107, Low: 105, Close: 106.5, Volume: 1600},
				}, nil
			default:
				return nil, errors.New("unexpected gap request")
			}
		},
	}

	service := &DataService{
		stockChain:  provider,
		historyRepo: repo,
		logger:      logger,
		now:         func() time.Time { return day7.Add(time.Hour) },
	}

	got, err := service.DownloadHistoricalOHLCV(context.Background(), domain.MarketTypeStock, []string{"AAPL"}, Timeframe1d, from, day7, true)
	if err != nil {
		t.Fatalf("DownloadHistoricalOHLCV() error = %v", err)
	}

	if len(provider.calls) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(provider.calls))
	}
	if !provider.calls[0].from.Equal(from) || !provider.calls[0].to.Equal(day2) {
		t.Fatalf("provider call[0] range = %s..%s, want %s..%s", provider.calls[0].from, provider.calls[0].to, from, day2)
	}
	if !provider.calls[1].from.Equal(day6) || !provider.calls[1].to.Equal(day7) {
		t.Fatalf("provider call[1] range = %s..%s, want %s..%s", provider.calls[1].from, provider.calls[1].to, day6, day7)
	}

	bars := got["AAPL"]
	if len(bars) != 7 {
		t.Fatalf("len(got[\"AAPL\"]) = %d, want 7", len(bars))
	}
	if !bars[0].Timestamp.Equal(from) || !bars[6].Timestamp.Equal(day7) {
		t.Fatalf("returned range = %s..%s, want %s..%s", bars[0].Timestamp, bars[6].Timestamp, from, day7)
	}
}

func TestDataServiceDownloadHistoricalOHLCVBypassesCache(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	from := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)

	historyRepo := newFakeHistoricalOHLCVRepo()
	cacheRepo := &fakeHistoricalCacheRepo{}
	provider := &historicalStubProvider{
		getFn: func(_ string, _ Timeframe, gapFrom, gapTo time.Time) ([]domain.OHLCV, error) {
			if !gapFrom.Equal(from) || !gapTo.Equal(to) {
				return nil, errors.New("unexpected gap request")
			}
			return []domain.OHLCV{
				{Timestamp: from, Open: 1, High: 2, Low: 0.5, Close: 1.5, Volume: 10},
				{Timestamp: to, Open: 2, High: 3, Low: 1.5, Close: 2.5, Volume: 20},
			}, nil
		},
	}

	service := &DataService{
		stockChain:  provider,
		cacheRepo:   cacheRepo,
		historyRepo: historyRepo,
		logger:      logger,
		now:         func() time.Time { return to.Add(time.Hour) },
	}

	got, err := service.DownloadHistoricalOHLCV(context.Background(), domain.MarketTypeStock, []string{"AAPL"}, Timeframe1d, from, to, false)
	if err != nil {
		t.Fatalf("DownloadHistoricalOHLCV() error = %v", err)
	}
	if len(got["AAPL"]) != 2 {
		t.Fatalf("len(got[\"AAPL\"]) = %d, want 2", len(got["AAPL"]))
	}
	if cacheRepo.getCalls != 0 {
		t.Fatalf("cache get calls = %d, want 0", cacheRepo.getCalls)
	}
	if cacheRepo.setCalls != 0 {
		t.Fatalf("cache set calls = %d, want 0", cacheRepo.setCalls)
	}
}

func TestDetectHistoricalOHLCVGapsMergesOverlappingCoverage(t *testing.T) {
	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(5 * 24 * time.Hour)

	gaps, err := detectHistoricalOHLCVGaps([]domain.HistoricalOHLCVCoverage{
		{DateFrom: from, DateTo: from.Add(24 * time.Hour)},
		{DateFrom: from.Add(24 * time.Hour), DateTo: from.Add(2 * 24 * time.Hour)},
		{DateFrom: from.Add(4 * 24 * time.Hour), DateTo: to},
	}, Timeframe1d, from, to)
	if err != nil {
		t.Fatalf("detectHistoricalOHLCVGaps() error = %v", err)
	}

	if len(gaps) != 1 {
		t.Fatalf("len(gaps) = %d, want 1", len(gaps))
	}
	if !gaps[0].From.Equal(from.Add(3*24*time.Hour)) || !gaps[0].To.Equal(from.Add(3*24*time.Hour)) {
		t.Fatalf("gap = %s..%s, want %s..%s", gaps[0].From, gaps[0].To, from.Add(3*24*time.Hour), from.Add(3*24*time.Hour))
	}
}

func historicalBarKey(bar domain.HistoricalOHLCV) string {
	return bar.Ticker + "|" + bar.Provider + "|" + bar.Timeframe + "|" + bar.Timestamp.UTC().Format(time.RFC3339Nano)
}

func historicalCoverageKey(coverage domain.HistoricalOHLCVCoverage) string {
	return coverage.Ticker + "|" + coverage.Provider + "|" + coverage.Timeframe + "|" +
		coverage.DateFrom.UTC().Format(time.RFC3339Nano) + "|" + coverage.DateTo.UTC().Format(time.RFC3339Nano)
}
