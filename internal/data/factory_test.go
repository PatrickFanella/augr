package data

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type serviceStubProvider struct {
	name              string
	ohlcv             []domain.OHLCV
	ohlcvErr          error
	ohlcvCalls        int
	fundamentals      Fundamentals
	fundamentalsErr   error
	fundamentalsCalls int
	news              []NewsArticle
	newsErr           error
	newsCalls         int
	sentiment         []SocialSentiment
	sentimentErr      error
	sentimentCalls    int
}

func (s *serviceStubProvider) GetOHLCV(_ context.Context, _ string, _ Timeframe, _, _ time.Time) ([]domain.OHLCV, error) {
	s.ohlcvCalls++
	return s.ohlcv, s.ohlcvErr
}

func (s *serviceStubProvider) GetFundamentals(_ context.Context, _ string) (Fundamentals, error) {
	s.fundamentalsCalls++
	return s.fundamentals, s.fundamentalsErr
}

func (s *serviceStubProvider) GetNews(_ context.Context, _ string, _, _ time.Time) ([]NewsArticle, error) {
	s.newsCalls++
	return s.news, s.newsErr
}

func (s *serviceStubProvider) GetSocialSentiment(_ context.Context, _ string, _, _ time.Time) ([]SocialSentiment, error) {
	s.sentimentCalls++
	return s.sentiment, s.sentimentErr
}

type fakeMarketDataCacheRepo struct {
	getResult *domain.MarketData
	getErr    error
	getCalls  int
	getKeys   []repository.MarketDataCacheKey
	setCalls  int
	setData   *domain.MarketData
}

func (f *fakeMarketDataCacheRepo) Get(_ context.Context, key repository.MarketDataCacheKey) (*domain.MarketData, error) {
	f.getCalls++
	f.getKeys = append(f.getKeys, key)
	return f.getResult, f.getErr
}

func (f *fakeMarketDataCacheRepo) Set(_ context.Context, data *domain.MarketData) error {
	f.setCalls++
	cloned := *data
	cloned.Data = append(json.RawMessage(nil), data.Data...)
	f.setData = &cloned
	return nil
}

func (f *fakeMarketDataCacheRepo) Expire(context.Context, repository.MarketDataCacheExpireFilter) error {
	return nil
}

func TestDataServiceGetOHLCVCacheHitReturnsCachedData(t *testing.T) {
	ticker := "AAPL"
	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 3, 7, 0, 0, 0, 0, time.UTC)
	want := []domain.OHLCV{
		{Timestamp: from, Open: 100, High: 110, Low: 95, Close: 105, Volume: 1000},
	}
	payload, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	provider := &serviceStubProvider{
		ohlcvErr: errors.New("provider should not be called"),
	}
	cacheRepo := &fakeMarketDataCacheRepo{
		getResult: &domain.MarketData{Data: payload},
	}
	service := &DataService{
		stockChain: provider,
		cacheRepo:  cacheRepo,
		logger:     discardLogger(),
		now:        func() time.Time { return to },
	}

	got, err := service.GetOHLCV(context.Background(), domain.MarketTypeStock, ticker, Timeframe1d, from, to)
	if err != nil {
		t.Fatalf("GetOHLCV() error = %v", err)
	}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("GetOHLCV() = %#v, want %#v", got, want)
	}
	if provider.ohlcvCalls != 0 {
		t.Fatalf("provider GetOHLCV calls = %d, want 0", provider.ohlcvCalls)
	}
	if cacheRepo.setCalls != 0 {
		t.Fatalf("cache Set() calls = %d, want 0", cacheRepo.setCalls)
	}
	if len(cacheRepo.getKeys) != 1 {
		t.Fatalf("cache Get() keys = %d, want 1", len(cacheRepo.getKeys))
	}
	if cacheRepo.getKeys[0].Timeframe != ohlcvCacheTimeframe(Timeframe1d, from, to) {
		t.Fatalf("cache key timeframe = %q, want %q", cacheRepo.getKeys[0].Timeframe, ohlcvCacheTimeframe(Timeframe1d, from, to))
	}
}

func TestDataServiceSetNowFuncOverridesCacheClock(t *testing.T) {
	now := time.Date(2026, 3, 25, 11, 0, 0, 0, time.UTC)
	service := &DataService{logger: discardLogger()}

	service.SetNowFunc(func() time.Time { return now })

	if got := service.currentTime(); !got.Equal(now) {
		t.Fatalf("currentTime() = %s, want %s", got, now)
	}
}

func TestDataServiceGetOHLCVCacheMissCallsChainAndCachesResult(t *testing.T) {
	now := time.Date(2026, 3, 22, 17, 0, 0, 0, time.UTC)
	from := now.Add(-time.Hour)
	to := now
	want := []domain.OHLCV{
		{Timestamp: from, Open: 200, High: 210, Low: 190, Close: 205, Volume: 2500},
	}

	testCases := []struct {
		name      string
		timeframe Timeframe
		wantTTL   time.Duration
	}{
		{name: "intraday", timeframe: Timeframe5m, wantTTL: 5 * time.Minute},
		{name: "historical", timeframe: Timeframe1d, wantTTL: 24 * time.Hour},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			provider := &serviceStubProvider{ohlcv: want}
			cacheRepo := &fakeMarketDataCacheRepo{
				getErr: errors.New("cache miss"),
			}
			service := &DataService{
				stockChain: provider,
				cacheRepo:  cacheRepo,
				logger:     discardLogger(),
				now:        func() time.Time { return now },
			}

			got, err := service.GetOHLCV(context.Background(), domain.MarketTypeStock, "AAPL", tc.timeframe, from, to)
			if err != nil {
				t.Fatalf("GetOHLCV() error = %v", err)
			}
			if len(got) != len(want) || got[0] != want[0] {
				t.Fatalf("GetOHLCV() = %#v, want %#v", got, want)
			}
			if provider.ohlcvCalls != 1 {
				t.Fatalf("provider GetOHLCV calls = %d, want 1", provider.ohlcvCalls)
			}
			if cacheRepo.setCalls != 1 {
				t.Fatalf("cache Set() calls = %d, want 1", cacheRepo.setCalls)
			}
			if cacheRepo.setData == nil {
				t.Fatal("cache Set() data = nil, want value")
			}
			if cacheRepo.setData.Provider != cacheProviderStockChain {
				t.Fatalf("cache provider = %q, want %q", cacheRepo.setData.Provider, cacheProviderStockChain)
			}
			if cacheRepo.setData.DataType != cacheDataTypeOHLCV {
				t.Fatalf("cache data type = %q, want %q", cacheRepo.setData.DataType, cacheDataTypeOHLCV)
			}
			if cacheRepo.setData.Timeframe != ohlcvCacheTimeframe(tc.timeframe, from, to) {
				t.Fatalf("cache timeframe = %q, want %q", cacheRepo.setData.Timeframe, ohlcvCacheTimeframe(tc.timeframe, from, to))
			}
			if !cacheRepo.setData.FetchedAt.Equal(now) {
				t.Fatalf("cache fetched_at = %s, want %s", cacheRepo.setData.FetchedAt, now)
			}
			if !cacheRepo.setData.ExpiresAt.Equal(now.Add(tc.wantTTL)) {
				t.Fatalf("cache expires_at = %s, want %s", cacheRepo.setData.ExpiresAt, now.Add(tc.wantTTL))
			}

			var cached []domain.OHLCV
			if err := json.Unmarshal(cacheRepo.setData.Data, &cached); err != nil {
				t.Fatalf("json.Unmarshal(cache data) error = %v", err)
			}
			if len(cached) != len(want) || cached[0] != want[0] {
				t.Fatalf("cached data = %#v, want %#v", cached, want)
			}
		})
	}
}

func TestDataServiceGetFundamentalsCacheHitReturnsCachedData(t *testing.T) {
	want := Fundamentals{
		Ticker:    "AAPL",
		PERatio:   31.2,
		FetchedAt: time.Date(2026, 3, 20, 15, 0, 0, 0, time.UTC),
	}
	payload, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	provider := &serviceStubProvider{
		fundamentalsErr: errors.New("provider should not be called"),
	}
	cacheRepo := &fakeMarketDataCacheRepo{
		getResult: &domain.MarketData{Data: payload},
	}
	service := &DataService{
		stockChain: provider,
		cacheRepo:  cacheRepo,
		logger:     discardLogger(),
		now:        func() time.Time { return want.FetchedAt },
	}

	got, err := service.GetFundamentals(context.Background(), domain.MarketTypeStock, "AAPL")
	if err != nil {
		t.Fatalf("GetFundamentals() error = %v", err)
	}
	if got != want {
		t.Fatalf("GetFundamentals() = %#v, want %#v", got, want)
	}
	if provider.fundamentalsCalls != 0 {
		t.Fatalf("provider GetFundamentals calls = %d, want 0", provider.fundamentalsCalls)
	}
	if cacheRepo.setCalls != 0 {
		t.Fatalf("cache Set() calls = %d, want 0", cacheRepo.setCalls)
	}
}

func TestDataServiceGetFundamentalsCacheMissCallsChainAndCachesResult(t *testing.T) {
	now := time.Date(2026, 3, 22, 17, 0, 0, 0, time.UTC)
	want := Fundamentals{
		Ticker:    "AAPL",
		PERatio:   28.4,
		FetchedAt: now.Add(-time.Hour),
	}

	provider := &serviceStubProvider{fundamentals: want}
	cacheRepo := &fakeMarketDataCacheRepo{}
	service := &DataService{
		stockChain: provider,
		cacheRepo:  cacheRepo,
		logger:     discardLogger(),
		now:        func() time.Time { return now },
	}

	got, err := service.GetFundamentals(context.Background(), domain.MarketTypeStock, "AAPL")
	if err != nil {
		t.Fatalf("GetFundamentals() error = %v", err)
	}
	if got != want {
		t.Fatalf("GetFundamentals() = %#v, want %#v", got, want)
	}
	if provider.fundamentalsCalls != 1 {
		t.Fatalf("provider GetFundamentals calls = %d, want 1", provider.fundamentalsCalls)
	}
	if cacheRepo.setCalls != 1 {
		t.Fatalf("cache Set() calls = %d, want 1", cacheRepo.setCalls)
	}
	if cacheRepo.setData == nil {
		t.Fatal("cache Set() data = nil, want value")
	}
	if cacheRepo.setData.DataType != cacheDataTypeFundamentals {
		t.Fatalf("cache data type = %q, want %q", cacheRepo.setData.DataType, cacheDataTypeFundamentals)
	}
	if cacheRepo.setData.Timeframe != "" {
		t.Fatalf("cache timeframe = %q, want empty", cacheRepo.setData.Timeframe)
	}
	if !cacheRepo.setData.ExpiresAt.Equal(now.Add(6 * time.Hour)) {
		t.Fatalf("cache expires_at = %s, want %s", cacheRepo.setData.ExpiresAt, now.Add(6*time.Hour))
	}
}

func TestDataServiceGetNewsCacheHitReturnsCachedData(t *testing.T) {
	from := time.Date(2026, 3, 21, 14, 30, 0, 0, time.UTC)
	to := time.Date(2026, 3, 22, 9, 45, 0, 0, time.UTC)
	want := []NewsArticle{
		{Title: "AAPL news", Source: "Example", PublishedAt: from, Sentiment: 0.4},
	}
	payload, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	provider := &serviceStubProvider{
		newsErr: errors.New("provider should not be called"),
	}
	cacheRepo := &fakeMarketDataCacheRepo{
		getResult: &domain.MarketData{Data: payload},
	}
	service := &DataService{
		stockChain: provider,
		cacheRepo:  cacheRepo,
		logger:     discardLogger(),
		now:        func() time.Time { return to },
	}

	got, err := service.GetNews(context.Background(), domain.MarketTypeStock, "AAPL", from, to)
	if err != nil {
		t.Fatalf("GetNews() error = %v", err)
	}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("GetNews() = %#v, want %#v", got, want)
	}
	if provider.newsCalls != 0 {
		t.Fatalf("provider GetNews calls = %d, want 0", provider.newsCalls)
	}
	if len(cacheRepo.getKeys) != 1 {
		t.Fatalf("cache Get() keys = %d, want 1", len(cacheRepo.getKeys))
	}
	if cacheRepo.getKeys[0].Timeframe != newsCacheWindow(from, to) {
		t.Fatalf("cache key timeframe = %q, want %q", cacheRepo.getKeys[0].Timeframe, newsCacheWindow(from, to))
	}
}

func TestDataServiceGetNewsCacheMissCallsChainAndCachesResult(t *testing.T) {
	now := time.Date(2026, 3, 22, 17, 0, 0, 0, time.UTC)
	from := now.Add(-2 * time.Hour)
	to := now
	want := []NewsArticle{
		{Title: "Market update", Source: "Newswire", PublishedAt: from, Sentiment: 0.7},
	}

	provider := &serviceStubProvider{news: want}
	cacheRepo := &fakeMarketDataCacheRepo{}
	service := &DataService{
		stockChain: provider,
		cacheRepo:  cacheRepo,
		logger:     discardLogger(),
		now:        func() time.Time { return now },
	}

	got, err := service.GetNews(context.Background(), domain.MarketTypeStock, "AAPL", from, to)
	if err != nil {
		t.Fatalf("GetNews() error = %v", err)
	}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("GetNews() = %#v, want %#v", got, want)
	}
	if provider.newsCalls != 1 {
		t.Fatalf("provider GetNews calls = %d, want 1", provider.newsCalls)
	}
	if cacheRepo.setCalls != 1 {
		t.Fatalf("cache Set() calls = %d, want 1", cacheRepo.setCalls)
	}
	if cacheRepo.setData == nil {
		t.Fatal("cache Set() data = nil, want value")
	}
	if cacheRepo.setData.DataType != cacheDataTypeNews {
		t.Fatalf("cache data type = %q, want %q", cacheRepo.setData.DataType, cacheDataTypeNews)
	}
	if cacheRepo.setData.Timeframe != newsCacheWindow(from, to) {
		t.Fatalf("cache timeframe = %q, want %q", cacheRepo.setData.Timeframe, newsCacheWindow(from, to))
	}
	if !cacheRepo.setData.ExpiresAt.Equal(now.Add(30 * time.Minute)) {
		t.Fatalf("cache expires_at = %s, want %s", cacheRepo.setData.ExpiresAt, now.Add(30*time.Minute))
	}
}

func TestDataServiceGetNewsFiltersAndSortsPointInTimeResults(t *testing.T) {
	now := time.Date(2026, 3, 22, 17, 0, 0, 0, time.UTC)
	from := now.Add(-2 * time.Hour)
	to := now
	provider := &serviceStubProvider{
		news: []NewsArticle{
			{Title: "future", PublishedAt: to.Add(time.Minute)},
			{Title: "inside-late", PublishedAt: to.Add(-10 * time.Minute)},
			{Title: "zero"},
			{Title: "inside-early", PublishedAt: from.Add(5 * time.Minute)},
			{Title: "before", PublishedAt: from.Add(-time.Second)},
		},
	}
	cacheRepo := &fakeMarketDataCacheRepo{}
	service := &DataService{
		stockChain: provider,
		cacheRepo:  cacheRepo,
		logger:     discardLogger(),
		now:        func() time.Time { return now },
	}

	got, err := service.GetNews(context.Background(), domain.MarketTypeStock, "AAPL", from, to)
	if err != nil {
		t.Fatalf("GetNews() error = %v", err)
	}

	want := []NewsArticle{
		{Title: "inside-early", PublishedAt: from.Add(5 * time.Minute)},
		{Title: "inside-late", PublishedAt: to.Add(-10 * time.Minute)},
	}
	if len(got) != len(want) {
		t.Fatalf("GetNews() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Title != want[i].Title || !got[i].PublishedAt.Equal(want[i].PublishedAt) {
			t.Fatalf("GetNews()[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
	if cacheRepo.setData == nil {
		t.Fatal("cache Set() data = nil, want value")
	}

	var cached []NewsArticle
	if err := json.Unmarshal(cacheRepo.setData.Data, &cached); err != nil {
		t.Fatalf("json.Unmarshal(cache data) error = %v", err)
	}
	if len(cached) != len(want) {
		t.Fatalf("cached data len = %d, want %d", len(cached), len(want))
	}
	for i := range want {
		if cached[i].Title != want[i].Title || !cached[i].PublishedAt.Equal(want[i].PublishedAt) {
			t.Fatalf("cached data[%d] = %#v, want %#v", i, cached[i], want[i])
		}
	}
}

func TestDataServiceGetSocialSentimentCacheHitReturnsCachedData(t *testing.T) {
	from := time.Date(2026, 3, 21, 14, 30, 0, 0, time.UTC)
	to := time.Date(2026, 3, 22, 9, 45, 0, 0, time.UTC)
	want := []SocialSentiment{
		{Ticker: "AAPL", Score: 0.4, MeasuredAt: from},
	}
	payload, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	provider := &serviceStubProvider{
		sentimentErr: errors.New("provider should not be called"),
	}
	cacheRepo := &fakeMarketDataCacheRepo{
		getResult: &domain.MarketData{Data: payload},
	}
	service := &DataService{
		stockChain: provider,
		cacheRepo:  cacheRepo,
		logger:     discardLogger(),
		now:        func() time.Time { return to },
	}

	got, err := service.GetSocialSentiment(context.Background(), domain.MarketTypeStock, "AAPL", from, to)
	if err != nil {
		t.Fatalf("GetSocialSentiment() error = %v", err)
	}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("GetSocialSentiment() = %#v, want %#v", got, want)
	}
	if provider.sentimentCalls != 0 {
		t.Fatalf("provider GetSocialSentiment calls = %d, want 0", provider.sentimentCalls)
	}
	if len(cacheRepo.getKeys) != 1 {
		t.Fatalf("cache Get() keys = %d, want 1", len(cacheRepo.getKeys))
	}
	if cacheRepo.getKeys[0].DataType != cacheDataTypeSocial {
		t.Fatalf("cache key data type = %q, want %q", cacheRepo.getKeys[0].DataType, cacheDataTypeSocial)
	}
	if cacheRepo.getKeys[0].Timeframe != newsCacheWindow(from, to) {
		t.Fatalf("cache key timeframe = %q, want %q", cacheRepo.getKeys[0].Timeframe, newsCacheWindow(from, to))
	}
}

func TestDataServiceGetSocialSentimentCacheMissCallsChainAndCachesResult(t *testing.T) {
	now := time.Date(2026, 3, 22, 17, 0, 0, 0, time.UTC)
	from := now.Add(-2 * time.Hour)
	to := now
	provider := &serviceStubProvider{
		sentiment: []SocialSentiment{
			{Ticker: "AAPL", Score: 0.8, MeasuredAt: to.Add(time.Minute)},
			{Ticker: "AAPL", Score: 0.2, MeasuredAt: from.Add(15 * time.Minute)},
			{Ticker: "AAPL", Score: 0.6, MeasuredAt: to.Add(-10 * time.Minute)},
			{Ticker: "AAPL", Score: 0.5},
		},
	}
	cacheRepo := &fakeMarketDataCacheRepo{}
	service := &DataService{
		stockChain: provider,
		cacheRepo:  cacheRepo,
		logger:     discardLogger(),
		now:        func() time.Time { return now },
	}

	got, err := service.GetSocialSentiment(context.Background(), domain.MarketTypeStock, "AAPL", from, to)
	if err != nil {
		t.Fatalf("GetSocialSentiment() error = %v", err)
	}

	want := []SocialSentiment{
		{Ticker: "AAPL", Score: 0.2, MeasuredAt: from.Add(15 * time.Minute)},
		{Ticker: "AAPL", Score: 0.6, MeasuredAt: to.Add(-10 * time.Minute)},
	}
	if len(got) != len(want) {
		t.Fatalf("GetSocialSentiment() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("GetSocialSentiment()[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
	if provider.sentimentCalls != 1 {
		t.Fatalf("provider GetSocialSentiment calls = %d, want 1", provider.sentimentCalls)
	}
	if cacheRepo.setCalls != 1 {
		t.Fatalf("cache Set() calls = %d, want 1", cacheRepo.setCalls)
	}
	if cacheRepo.setData == nil {
		t.Fatal("cache Set() data = nil, want value")
	}
	if cacheRepo.setData.DataType != cacheDataTypeSocial {
		t.Fatalf("cache data type = %q, want %q", cacheRepo.setData.DataType, cacheDataTypeSocial)
	}
	if cacheRepo.setData.Timeframe != newsCacheWindow(from, to) {
		t.Fatalf("cache timeframe = %q, want %q", cacheRepo.setData.Timeframe, newsCacheWindow(from, to))
	}
	if !cacheRepo.setData.ExpiresAt.Equal(now.Add(30 * time.Minute)) {
		t.Fatalf("cache expires_at = %s, want %s", cacheRepo.setData.ExpiresAt, now.Add(30*time.Minute))
	}
}

func TestNewDataServiceSkipsProvidersWithoutAPIKeys(t *testing.T) {
	originalPolygonFactory := polygonProviderFactory
	originalAlphaFactory := alphaVantageProviderFactory
	originalYahooFactory := yahooProviderFactory
	originalBinanceFactory := binanceProviderFactory
	t.Cleanup(func() {
		polygonProviderFactory = originalPolygonFactory
		alphaVantageProviderFactory = originalAlphaFactory
		yahooProviderFactory = originalYahooFactory
		binanceProviderFactory = originalBinanceFactory
	})

	polygonProviderFactory = func(_ string, _ *slog.Logger) DataProvider {
		return &serviceStubProvider{name: "polygon"}
	}
	alphaVantageProviderFactory = func(_ string, _ int, _ *slog.Logger) DataProvider {
		return &serviceStubProvider{name: "alpha"}
	}
	yahooProviderFactory = func(_ *slog.Logger) DataProvider {
		return &serviceStubProvider{name: "yahoo"}
	}
	binanceProviderFactory = func(_ *slog.Logger) DataProvider {
		return &serviceStubProvider{name: "binance"}
	}

	service := NewDataService(config.Config{
		DataProviders: config.DataProviderConfigs{
			AlphaVantage: config.DataProviderConfig{
				APIKey: "alpha-key",
			},
		},
	}, nil, discardLogger())

	stockChain, ok := service.stockChain.(*ProviderChain)
	if !ok {
		t.Fatalf("stockChain type = %T, want *ProviderChain", service.stockChain)
	}
	if len(stockChain.providers) != 2 {
		t.Fatalf("len(stockChain.providers) = %d, want 2", len(stockChain.providers))
	}

	first, ok := stockChain.providers[0].(*serviceStubProvider)
	if !ok {
		t.Fatalf("stockChain.providers[0] type = %T, want *serviceStubProvider", stockChain.providers[0])
	}
	if first.name != "alpha" {
		t.Fatalf("stockChain.providers[0].name = %q, want %q", first.name, "alpha")
	}

	second, ok := stockChain.providers[1].(*serviceStubProvider)
	if !ok {
		t.Fatalf("stockChain.providers[1] type = %T, want *serviceStubProvider", stockChain.providers[1])
	}
	if second.name != "yahoo" {
		t.Fatalf("stockChain.providers[1].name = %q, want %q", second.name, "yahoo")
	}

	cryptoChain, ok := service.cryptoChain.(*ProviderChain)
	if !ok {
		t.Fatalf("cryptoChain type = %T, want *ProviderChain", service.cryptoChain)
	}
	if len(cryptoChain.providers) != 1 {
		t.Fatalf("len(cryptoChain.providers) = %d, want 1", len(cryptoChain.providers))
	}

	cryptoProvider, ok := cryptoChain.providers[0].(*serviceStubProvider)
	if !ok {
		t.Fatalf("cryptoChain.providers[0] type = %T, want *serviceStubProvider", cryptoChain.providers[0])
	}
	if cryptoProvider.name != "binance" {
		t.Fatalf("cryptoChain.providers[0].name = %q, want %q", cryptoProvider.name, "binance")
	}
}
