package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

const (
	cacheProviderStockChain   = "stock-chain"
	cacheProviderCryptoChain  = "crypto-chain"
	cacheDataTypeOHLCV        = "ohlcv"
	cacheDataTypeFundamentals = "fundamentals"
	cacheDataTypeNews         = "news"
	cacheDataTypeSocial       = "social_sentiment"
)

var ErrUnsupportedMarketType = errors.New("data: unsupported market type")

var ErrHistoricalOHLCVUnavailable = errors.New("data: historical ohlcv repository unavailable")

// ProviderRegistry holds factory functions for constructing data providers.
// Pass an explicit registry to NewDataService instead of relying on init()-time
// global registration.
type ProviderRegistry struct {
	Polygon      ProviderFactory
	AlphaVantage ProviderFactory
	Finnhub      ProviderFactory
	FMP          ProviderFactory
	NewsAPI      ProviderFactory
	Yahoo        ProviderFactory
	Binance      ProviderFactory
	Polymarket   ProviderFactory
}

// NewProviderRegistry returns an empty registry. Callers should populate the
// fields they need before passing it to NewDataService.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{}
}

// DataService wraps market-data provider chains with cache lookups and writes.
type DataService struct {
	stockChain      DataProvider
	cryptoChain     DataProvider
	polymarketChain DataProvider
	cacheRepo       repository.MarketDataCacheRepository
	historyRepo     repository.HistoricalOHLCVRepository
	logger          *slog.Logger
	nowMu           sync.RWMutex
	now             func() time.Time
}

// NewDataService constructs provider chains for each supported market type and
// wraps them with cache access. The registry parameter supplies the provider
// factory functions; pass nil to get a service with empty chains.
func NewDataService(cfg config.Config, reg *ProviderRegistry, cacheRepo repository.MarketDataCacheRepository, logger *slog.Logger) *DataService {
	if logger == nil {
		logger = slog.Default()
	}
	if reg == nil {
		reg = &ProviderRegistry{}
	}

	// Stock provider chain: Yahoo first (unlimited, free, reliable for OHLCV),
	// then Polygon (rate-limited but has snapshots/options). Finnhub, FMP, and
	// AlphaVantage are registered for non-OHLCV use (news, fundamentals) but
	// their free tiers cannot handle bulk OHLCV downloads reliably:
	//   - Finnhub free: 403 on US stock candles
	//   - FMP free: legacy endpoint deprecated
	//   - AlphaVantage free: 25 req/day, rejects outputsize=full
	stockProviders := make([]DataProvider, 0, 5)
	if reg.Yahoo != nil {
		stockProviders = append(stockProviders, reg.Yahoo(ProviderConfig{Logger: logger}))
	}
	if apiKey := strings.TrimSpace(cfg.DataProviders.Polygon.APIKey); apiKey != "" && reg.Polygon != nil {
		stockProviders = append(stockProviders, reg.Polygon(ProviderConfig{APIKey: apiKey, Logger: logger}))
	}
	if apiKey := strings.TrimSpace(cfg.DataProviders.Finnhub.APIKey); apiKey != "" && reg.Finnhub != nil {
		stockProviders = append(stockProviders, reg.Finnhub(ProviderConfig{APIKey: apiKey, RateLimitPerMinute: cfg.DataProviders.Finnhub.RateLimitPerMinute, Logger: logger}))
	}
	if apiKey := strings.TrimSpace(cfg.DataProviders.FMP.APIKey); apiKey != "" && reg.FMP != nil {
		stockProviders = append(stockProviders, reg.FMP(ProviderConfig{APIKey: apiKey, RateLimitPerMinute: cfg.DataProviders.FMP.RateLimitPerMinute, Logger: logger}))
	}
	if apiKey := strings.TrimSpace(cfg.DataProviders.AlphaVantage.APIKey); apiKey != "" && reg.AlphaVantage != nil {
		stockProviders = append(stockProviders, reg.AlphaVantage(ProviderConfig{APIKey: apiKey, RateLimitPerMinute: cfg.DataProviders.AlphaVantage.RateLimitPerMinute, Logger: logger}))
	}
	if apiKey := strings.TrimSpace(cfg.DataProviders.NewsAPI.APIKey); apiKey != "" && reg.NewsAPI != nil {
		stockProviders = append(stockProviders, reg.NewsAPI(ProviderConfig{APIKey: apiKey, Logger: logger}))
	}

	cryptoProviders := make([]DataProvider, 0, 1)
	if reg.Binance != nil {
		cryptoProviders = append(cryptoProviders, reg.Binance(ProviderConfig{Logger: logger}))
	}

	polymarketProviders := make([]DataProvider, 0, 1)
	if reg.Polymarket != nil && strings.TrimSpace(cfg.Brokers.Polymarket.CLOBURL) != "" {
		polymarketProviders = append(polymarketProviders, reg.Polymarket(ProviderConfig{BaseURL: cfg.Brokers.Polymarket.CLOBURL, Logger: logger}))
	}

	return &DataService{
		stockChain:      NewProviderChain(logger, stockProviders...),
		cryptoChain:     NewProviderChain(logger, cryptoProviders...),
		polymarketChain: NewProviderChain(logger, polymarketProviders...),
		cacheRepo:       cacheRepo,
		historyRepo:     historicalOHLCVRepo(cacheRepo),
		logger:          logger,
		now:             time.Now,
	}
}

// GetOHLCV returns OHLCV data using the market-type chain and caches results by query.
func (s *DataService) GetOHLCV(ctx context.Context, marketType domain.MarketType, ticker string, timeframe Timeframe, from, to time.Time) ([]domain.OHLCV, error) {
	fromUTC := from.UTC()
	toUTC := to.UTC()

	providerName, chain, err := s.resolveChain(marketType)
	if err != nil {
		return nil, err
	}

	key := repository.MarketDataCacheKey{
		Ticker:    ticker,
		Provider:  providerName,
		DataType:  cacheDataTypeOHLCV,
		Timeframe: ohlcvCacheTimeframe(timeframe, fromUTC, toUTC),
		DateFrom:  &fromUTC,
		DateTo:    &toUTC,
	}

	if cached, ok := s.loadCachedOHLCV(ctx, key); ok {
		return cached, nil
	}

	bars, err := chain.GetOHLCV(ctx, ticker, timeframe, from, to)
	if err != nil {
		return nil, err
	}

	s.storeCached(ctx, key, bars, ttlForOHLCV(timeframe))

	return bars, nil
}

// GetFundamentals returns fundamentals using the market-type chain and caches results.
func (s *DataService) GetFundamentals(ctx context.Context, marketType domain.MarketType, ticker string) (Fundamentals, error) {
	providerName, chain, err := s.resolveChain(marketType)
	if err != nil {
		return Fundamentals{}, err
	}

	key := repository.MarketDataCacheKey{
		Ticker:   ticker,
		Provider: providerName,
		DataType: cacheDataTypeFundamentals,
	}

	if cached, ok := s.loadCachedFundamentals(ctx, key); ok {
		return cached, nil
	}

	fundamentals, err := chain.GetFundamentals(ctx, ticker)
	if err != nil {
		return Fundamentals{}, err
	}

	s.storeCached(ctx, key, fundamentals, 6*time.Hour)

	return fundamentals, nil
}

// GetNews returns news using the market-type chain and caches results by query window.
func (s *DataService) GetNews(ctx context.Context, marketType domain.MarketType, ticker string, from, to time.Time) ([]NewsArticle, error) {
	fromUTC := from.UTC()
	toUTC := to.UTC()

	providerName, chain, err := s.resolveChain(marketType)
	if err != nil {
		return nil, err
	}

	key := repository.MarketDataCacheKey{
		Ticker:    ticker,
		Provider:  providerName,
		DataType:  cacheDataTypeNews,
		Timeframe: newsCacheWindow(fromUTC, toUTC),
		DateFrom:  &fromUTC,
		DateTo:    &toUTC,
	}

	if cached, ok := s.loadCachedNews(ctx, key); ok {
		return normalizeNewsArticles(cached, fromUTC, toUTC), nil
	}

	articles, err := chain.GetNews(ctx, ticker, from, to)
	if err != nil {
		if errors.Is(err, ErrNotImplemented) {
			return nil, nil
		}
		return nil, err
	}
	articles = normalizeNewsArticles(articles, fromUTC, toUTC)

	s.storeCached(ctx, key, articles, 30*time.Minute)

	return articles, nil
}

// DownloadHistoricalOHLCV bulk downloads and persists OHLCV history for the
// provided tickers. When incremental is true, only uncovered date ranges are fetched.
func (s *DataService) DownloadHistoricalOHLCV(
	ctx context.Context,
	marketType domain.MarketType,
	tickers []string,
	timeframe Timeframe,
	from, to time.Time,
	incremental bool,
) (map[string][]domain.OHLCV, error) {
	if s == nil || s.historyRepo == nil {
		return nil, ErrHistoricalOHLCVUnavailable
	}

	fromUTC := from.UTC()
	toUTC := to.UTC()
	if toUTC.Before(fromUTC) {
		return nil, fmt.Errorf("data: invalid historical range %s > %s", fromUTC, toUTC)
	}

	providerName, chain, err := s.resolveChain(marketType)
	if err != nil {
		return nil, err
	}

	results := make(map[string][]domain.OHLCV, len(tickers))
	for _, ticker := range tickers {
		trimmedTicker := strings.TrimSpace(ticker)
		if trimmedTicker == "" {
			continue
		}

		gaps := []historicalOHLCVRange{{From: fromUTC, To: toUTC}}
		if incremental {
			coverage, err := s.historyRepo.ListHistoricalOHLCVCoverage(ctx, repository.HistoricalOHLCVCoverageFilter{
				Ticker:    trimmedTicker,
				Provider:  providerName,
				Timeframe: timeframe.String(),
				From:      fromUTC,
				To:        toUTC,
			})
			if err != nil {
				return nil, fmt.Errorf("data: list historical coverage for %s: %w", trimmedTicker, err)
			}
			gaps, err = detectHistoricalOHLCVGaps(coverage, timeframe, fromUTC, toUTC)
			if err != nil {
				return nil, err
			}
		}

		for _, gap := range gaps {
			bars, err := chain.GetOHLCV(ctx, trimmedTicker, timeframe, gap.From, gap.To)
			if err != nil {
				return nil, fmt.Errorf("data: download historical ohlcv for %s: %w", trimmedTicker, err)
			}

			if len(bars) > 0 {
				if err := s.historyRepo.UpsertHistoricalOHLCV(ctx, toHistoricalOHLCV(trimmedTicker, providerName, timeframe, bars)); err != nil {
					return nil, fmt.Errorf("data: persist historical ohlcv for %s: %w", trimmedTicker, err)
				}
			}

			if err := s.historyRepo.UpsertHistoricalOHLCVCoverage(ctx, domain.HistoricalOHLCVCoverage{
				Ticker:    trimmedTicker,
				Provider:  providerName,
				Timeframe: timeframe.String(),
				DateFrom:  gap.From,
				DateTo:    gap.To,
				FetchedAt: s.currentTime().UTC(),
			}); err != nil {
				return nil, fmt.Errorf("data: persist historical coverage for %s: %w", trimmedTicker, err)
			}
		}

		stored, err := s.ListHistoricalOHLCV(ctx, trimmedTicker, providerName, timeframe, fromUTC, toUTC)
		if err != nil {
			return nil, fmt.Errorf("data: list persisted historical ohlcv for %s: %w", trimmedTicker, err)
		}
		results[trimmedTicker] = stored
	}

	return results, nil
}

// GetSocialSentiment returns social sentiment snapshots using the market-type
// chain and caches results by query window.
func (s *DataService) GetSocialSentiment(ctx context.Context, marketType domain.MarketType, ticker string, from, to time.Time) ([]SocialSentiment, error) {
	fromUTC := from.UTC()
	toUTC := to.UTC()

	providerName, chain, err := s.resolveChain(marketType)
	if err != nil {
		return nil, err
	}

	key := repository.MarketDataCacheKey{
		Ticker:    ticker,
		Provider:  providerName,
		DataType:  cacheDataTypeSocial,
		Timeframe: newsCacheWindow(fromUTC, toUTC),
		DateFrom:  &fromUTC,
		DateTo:    &toUTC,
	}

	if cached, ok := s.loadCachedSocialSentiment(ctx, key); ok {
		return normalizeSocialSentiment(cached, fromUTC, toUTC), nil
	}

	snapshots, err := chain.GetSocialSentiment(ctx, ticker, from, to)
	if err != nil {
		if errors.Is(err, ErrNotImplemented) {
			return nil, nil
		}
		return nil, err
	}
	snapshots = normalizeSocialSentiment(snapshots, fromUTC, toUTC)

	s.storeCached(ctx, key, snapshots, 30*time.Minute)

	return snapshots, nil
}

// ListHistoricalOHLCV returns persisted OHLCV history for a ticker/date range.
func (s *DataService) ListHistoricalOHLCV(
	ctx context.Context,
	ticker, provider string,
	timeframe Timeframe,
	from, to time.Time,
) ([]domain.OHLCV, error) {
	if s == nil || s.historyRepo == nil {
		return nil, ErrHistoricalOHLCVUnavailable
	}

	bars, err := s.historyRepo.ListHistoricalOHLCV(ctx, repository.HistoricalOHLCVFilter{
		Ticker:    ticker,
		Provider:  provider,
		Timeframe: timeframe.String(),
		From:      from.UTC(),
		To:        to.UTC(),
	})
	if err != nil {
		return nil, err
	}

	result := make([]domain.OHLCV, 0, len(bars))
	for _, bar := range bars {
		result = append(result, domain.OHLCV{
			Timestamp: bar.Timestamp,
			Open:      bar.Open,
			High:      bar.High,
			Low:       bar.Low,
			Close:     bar.Close,
			Volume:    bar.Volume,
		})
	}

	return result, nil
}

const cacheProviderPolymarketChain = "polymarket-chain"

func (s *DataService) resolveChain(marketType domain.MarketType) (string, DataProvider, error) {
	switch normalizeMarketType(marketType) {
	case domain.MarketTypeStock:
		return cacheProviderStockChain, s.stockChain, nil
	case domain.MarketTypeCrypto:
		return cacheProviderCryptoChain, s.cryptoChain, nil
	case domain.MarketTypePolymarket:
		return cacheProviderPolymarketChain, s.polymarketChain, nil
	default:
		return "", nil, fmt.Errorf("%w: %s", ErrUnsupportedMarketType, marketType)
	}
}

func (s *DataService) loadCachedOHLCV(ctx context.Context, key repository.MarketDataCacheKey) ([]domain.OHLCV, bool) {
	var bars []domain.OHLCV
	return bars, s.loadCached(ctx, key, &bars)
}

func (s *DataService) loadCachedFundamentals(ctx context.Context, key repository.MarketDataCacheKey) (Fundamentals, bool) {
	var fundamentals Fundamentals
	return fundamentals, s.loadCached(ctx, key, &fundamentals)
}

func (s *DataService) loadCachedNews(ctx context.Context, key repository.MarketDataCacheKey) ([]NewsArticle, bool) {
	var news []NewsArticle
	return news, s.loadCached(ctx, key, &news)
}

func (s *DataService) loadCachedSocialSentiment(ctx context.Context, key repository.MarketDataCacheKey) ([]SocialSentiment, bool) {
	var snapshots []SocialSentiment
	return snapshots, s.loadCached(ctx, key, &snapshots)
}

func (s *DataService) loadCached(ctx context.Context, key repository.MarketDataCacheKey, dest any) bool {
	if s == nil || s.cacheRepo == nil {
		return false
	}

	entry, err := s.cacheRepo.Get(ctx, key)
	if err != nil {
		s.logger.Warn("failed to load market data from cache",
			slog.String("ticker", key.Ticker),
			slog.String("provider", key.Provider),
			slog.String("data_type", key.DataType),
			slog.Any("error", err),
		)
		return false
	}
	if entry == nil {
		return false
	}

	if err := json.Unmarshal(entry.Data, dest); err != nil {
		s.logger.Warn("failed to decode cached market data, refreshing",
			slog.String("ticker", key.Ticker),
			slog.String("provider", key.Provider),
			slog.String("data_type", key.DataType),
			slog.Any("error", err),
		)
		return false
	}

	return true
}

func (s *DataService) storeCached(ctx context.Context, key repository.MarketDataCacheKey, value any, ttl time.Duration) {
	if s == nil || s.cacheRepo == nil {
		return
	}

	payload, err := json.Marshal(value)
	if err != nil {
		s.logger.Warn("failed to encode market data for cache",
			slog.String("ticker", key.Ticker),
			slog.String("provider", key.Provider),
			slog.String("data_type", key.DataType),
			slog.Any("error", err),
		)
		return
	}

	fetchedAt := s.currentTime().UTC()
	if err := s.cacheRepo.Set(ctx, &domain.MarketData{
		Ticker:    key.Ticker,
		Provider:  key.Provider,
		DataType:  key.DataType,
		Timeframe: key.Timeframe,
		DateFrom:  key.DateFrom,
		DateTo:    key.DateTo,
		Data:      payload,
		FetchedAt: fetchedAt,
		ExpiresAt: fetchedAt.Add(ttl),
	}); err != nil {
		s.logger.Warn("failed to store market data in cache",
			slog.String("ticker", key.Ticker),
			slog.String("provider", key.Provider),
			slog.String("data_type", key.DataType),
			slog.Any("error", err),
		)
	}
}

func (s *DataService) currentTime() time.Time {
	if s == nil {
		return time.Now()
	}

	s.nowMu.RLock()
	defer s.nowMu.RUnlock()

	if s.now == nil {
		return time.Now()
	}

	return s.now()
}

// SetNowFunc overrides the data service time source so cache timestamps can be
// aligned with simulated backtest time.
func (s *DataService) SetNowFunc(now func() time.Time) {
	if s == nil || now == nil {
		return
	}

	s.nowMu.Lock()
	defer s.nowMu.Unlock()

	s.now = now
}

func ttlForOHLCV(timeframe Timeframe) time.Duration {
	switch timeframe {
	case Timeframe1m, Timeframe5m, Timeframe15m, Timeframe1h:
		return 5 * time.Minute
	case Timeframe1d:
		return 24 * time.Hour
	}

	return 24 * time.Hour
}

func normalizeMarketType(marketType domain.MarketType) domain.MarketType {
	return domain.MarketType(strings.ToLower(strings.TrimSpace(marketType.String())))
}

func historicalOHLCVRepo(cacheRepo repository.MarketDataCacheRepository) repository.HistoricalOHLCVRepository {
	if repo, ok := cacheRepo.(repository.HistoricalOHLCVRepository); ok {
		return repo
	}

	return nil
}

type historicalOHLCVRange struct {
	From time.Time
	To   time.Time
}

func detectHistoricalOHLCVGaps(coverage []domain.HistoricalOHLCVCoverage, timeframe Timeframe, from, to time.Time) ([]historicalOHLCVRange, error) {
	step, err := timeframeDuration(timeframe)
	if err != nil {
		return nil, err
	}

	if to.Before(from) {
		return nil, fmt.Errorf("data: invalid historical range %s > %s", from, to)
	}

	if len(coverage) == 0 {
		return []historicalOHLCVRange{{From: from, To: to}}, nil
	}

	sortedCoverage := append([]domain.HistoricalOHLCVCoverage(nil), coverage...)
	sort.Slice(sortedCoverage, func(i, j int) bool {
		if sortedCoverage[i].DateFrom.Equal(sortedCoverage[j].DateFrom) {
			return sortedCoverage[i].DateTo.Before(sortedCoverage[j].DateTo)
		}
		return sortedCoverage[i].DateFrom.Before(sortedCoverage[j].DateFrom)
	})

	cursor := from
	gaps := make([]historicalOHLCVRange, 0)
	for _, item := range sortedCoverage {
		coverageFrom := item.DateFrom.UTC()
		coverageTo := item.DateTo.UTC()
		if coverageTo.Before(from) || coverageFrom.After(to) {
			continue
		}
		if coverageFrom.Before(from) {
			coverageFrom = from
		}
		if coverageTo.After(to) {
			coverageTo = to
		}

		if coverageFrom.After(cursor) {
			gapTo := coverageFrom.Add(-step)
			if gapTo.After(to) {
				gapTo = to
			}
			if !gapTo.Before(cursor) {
				gaps = append(gaps, historicalOHLCVRange{From: cursor, To: gapTo})
			}
		}

		nextCursor := coverageTo.Add(step)
		if nextCursor.After(cursor) {
			cursor = nextCursor
		}
		if cursor.After(to) {
			return gaps, nil
		}
	}

	if !cursor.After(to) {
		gaps = append(gaps, historicalOHLCVRange{From: cursor, To: to})
	}

	return gaps, nil
}

func timeframeDuration(timeframe Timeframe) (time.Duration, error) {
	switch timeframe {
	case Timeframe1m:
		return time.Minute, nil
	case Timeframe5m:
		return 5 * time.Minute, nil
	case Timeframe15m:
		return 15 * time.Minute, nil
	case Timeframe1h:
		return time.Hour, nil
	case Timeframe1d:
		return 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("data: unsupported timeframe %q", timeframe)
	}
}

func toHistoricalOHLCV(ticker, provider string, timeframe Timeframe, bars []domain.OHLCV) []domain.HistoricalOHLCV {
	historicalBars := make([]domain.HistoricalOHLCV, 0, len(bars))
	for _, bar := range bars {
		historicalBars = append(historicalBars, domain.HistoricalOHLCV{
			Ticker:    ticker,
			Provider:  provider,
			Timeframe: timeframe.String(),
			Timestamp: bar.Timestamp.UTC(),
			Open:      bar.Open,
			High:      bar.High,
			Low:       bar.Low,
			Close:     bar.Close,
			Volume:    bar.Volume,
		})
	}

	return historicalBars
}

func ohlcvCacheTimeframe(timeframe Timeframe, from, to time.Time) string {
	return timeframe.String() + "|" + newsCacheWindow(from, to)
}

func newsCacheWindow(from, to time.Time) string {
	return from.UTC().Format(time.RFC3339Nano) + "|" + to.UTC().Format(time.RFC3339Nano)
}

func normalizeNewsArticles(articles []NewsArticle, from, to time.Time) []NewsArticle {
	return filterAndSortByWindow(articles, from, to,
		func(article NewsArticle) time.Time { return article.PublishedAt },
		func(article *NewsArticle, timestamp time.Time) { article.PublishedAt = timestamp },
	)
}

func normalizeSocialSentiment(snapshots []SocialSentiment, from, to time.Time) []SocialSentiment {
	return filterAndSortByWindow(snapshots, from, to,
		func(snapshot SocialSentiment) time.Time { return snapshot.MeasuredAt },
		func(snapshot *SocialSentiment, timestamp time.Time) { snapshot.MeasuredAt = timestamp },
	)
}

func filterAndSortByWindow[T any](items []T, from, to time.Time, timestamp func(T) time.Time, setTimestamp func(*T, time.Time)) []T {
	if len(items) == 0 {
		return nil
	}

	fromUTC := from.UTC()
	toUTC := to.UTC()
	filtered := make([]T, 0, len(items))
	for _, item := range items {
		at := timestamp(item)
		if at.IsZero() {
			continue
		}

		at = at.UTC()
		if at.Before(fromUTC) || at.After(toUTC) {
			continue
		}

		setTimestamp(&item, at)
		filtered = append(filtered, item)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		return timestamp(filtered[i]).Before(timestamp(filtered[j]))
	})

	if len(filtered) == 0 {
		return nil
	}

	return filtered
}
