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

	"golang.org/x/sync/errgroup"

	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

var ErrHistoricalOHLCVUnavailable = errors.New("data: historical ohlcv repository unavailable")

const historicalOHLCVEmptyCoverageMaxAge = 72 * time.Hour

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
	Kalshi       ProviderFactory
	Reddit       ProviderFactory
	StockTwits   ProviderFactory
}

// NewProviderRegistry returns an empty registry. Callers should populate the
// fields they need before passing it to NewDataService.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{}
}

// DataService wraps market-data provider chains with cache lookups and writes.
type DataService struct {
	selection       SelectionPolicy
	stockChain      DataProvider
	cryptoChain     DataProvider
	polymarketChain DataProvider
	kalshiChain     DataProvider
	socialProviders []DataProvider // dedicated social sentiment providers (aggregated, not first-wins)
	cacheRepo       repository.MarketDataCacheRepository
	historyRepo     repository.HistoricalOHLCVRepository
	logger          *slog.Logger
	nowMu           sync.RWMutex
	now             func() time.Time
}

// SocialTriageConfig holds optional LLM dependencies for social sentiment
// providers that require LLM-based triage (e.g. Reddit RSS).
type SocialTriageConfig struct {
	Provider llm.Provider
	Model    string
}

// NewDataService constructs provider chains for each supported market type and
// wraps them with cache access. The registry parameter supplies the provider
// factory functions; pass nil to get a service with empty chains.
// socialTriage is optional; pass nil if no LLM-backed social providers are needed.
func NewDataService(cfg config.Config, reg *ProviderRegistry, cacheRepo repository.MarketDataCacheRepository, logger *slog.Logger, socialTriage *SocialTriageConfig) *DataService {
	if logger == nil {
		logger = slog.Default()
	}
	selection := SelectionPolicy{}
	chains := selection.BuildProviderChains(cfg, reg, logger, socialTriage)

	return &DataService{
		selection:       selection,
		stockChain:      NewProviderChain(logger, chains.Stock...),
		cryptoChain:     NewProviderChain(logger, chains.Crypto...),
		polymarketChain: NewProviderChain(logger, chains.Polymarket...),
		kalshiChain:     NewProviderChain(logger, chains.Kalshi...),
		socialProviders: chains.Social,
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

	_, chain, err := s.resolveChain(marketType)
	if err != nil {
		return nil, err
	}
	cacheSelection, err := s.selection.CacheSelection(marketType, cacheDataTypeOHLCV)
	if err != nil {
		return nil, err
	}

	// Truncate date boundaries to timeframe granularity so that requests
	// arriving within the same period produce identical cache keys.
	cacheFrom := truncateForTimeframe(timeframe, fromUTC)
	cacheTo := truncateForTimeframe(timeframe, toUTC)
	key := repository.MarketDataCacheKey{
		Ticker:    ticker,
		Provider:  cacheSelection.Provider,
		DataType:  cacheDataTypeOHLCV,
		Timeframe: ohlcvCacheTimeframe(timeframe, fromUTC, toUTC),
		DateFrom:  &cacheFrom,
		DateTo:    &cacheTo,
	}

	if cacheSelection.Enabled {
		if cached, ok := s.loadCachedOHLCV(ctx, key); ok {
			return cached, nil
		}
	}

	bars, err := chain.GetOHLCV(ctx, ticker, timeframe, from, to)
	if err != nil {
		return nil, err
	}

	if cacheSelection.Enabled {
		s.storeCached(ctx, key, bars, ttlForOHLCV(timeframe))
	}

	return bars, nil
}

// GetFundamentals returns fundamentals using the market-type chain and caches results.
func (s *DataService) GetFundamentals(ctx context.Context, marketType domain.MarketType, ticker string) (Fundamentals, error) {
	_, chain, err := s.resolveChain(marketType)
	if err != nil {
		return Fundamentals{}, err
	}
	cacheSelection, err := s.selection.CacheSelection(marketType, cacheDataTypeFundamentals)
	if err != nil {
		return Fundamentals{}, err
	}

	key := repository.MarketDataCacheKey{
		Ticker:   ticker,
		Provider: cacheSelection.Provider,
		DataType: cacheDataTypeFundamentals,
	}

	if cacheSelection.Enabled {
		if cached, ok := s.loadCachedFundamentals(ctx, key); ok {
			return cached, nil
		}
	}

	fundamentals, err := chain.GetFundamentals(ctx, ticker)
	if err != nil {
		return Fundamentals{}, err
	}

	if cacheSelection.Enabled {
		s.storeCached(ctx, key, fundamentals, 6*time.Hour)
	}

	return fundamentals, nil
}

// GetNews returns news using the market-type chain and caches results by query window.
func (s *DataService) GetNews(ctx context.Context, marketType domain.MarketType, ticker string, from, to time.Time) ([]NewsArticle, error) {
	fromUTC := from.UTC()
	toUTC := to.UTC()

	_, chain, err := s.resolveChain(marketType)
	if err != nil {
		return nil, err
	}
	cacheSelection, err := s.selection.CacheSelection(marketType, cacheDataTypeNews)
	if err != nil {
		return nil, err
	}

	key := repository.MarketDataCacheKey{
		Ticker:    ticker,
		Provider:  cacheSelection.Provider,
		DataType:  cacheDataTypeNews,
		Timeframe: newsCacheWindow(fromUTC, toUTC),
		DateFrom:  &fromUTC,
		DateTo:    &toUTC,
	}

	if cacheSelection.Enabled {
		if cached, ok := s.loadCachedNews(ctx, key); ok {
			return normalizeNewsArticles(cached, fromUTC, toUTC), nil
		}
	}

	articles, err := chain.GetNews(ctx, ticker, from, to)
	if err != nil {
		if errors.Is(err, ErrNotImplemented) {
			return nil, nil
		}
		return nil, err
	}
	articles = normalizeNewsArticles(articles, fromUTC, toUTC)

	if cacheSelection.Enabled {
		s.storeCached(ctx, key, articles, 30*time.Minute)
	}

	return articles, nil
}

// HistoricalOHLCVDownload describes both the stored result and provider work
// performed by one historical download. Callers that must prove current-source
// freshness should use ProviderRequests and FreshBars rather than treating
// previously stored bars as newly downloaded.
type HistoricalOHLCVDownload struct {
	Bars             map[string][]domain.OHLCV
	ProviderRequests map[string]int
	FreshBars        map[string]int
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
	result, err := s.DownloadHistoricalOHLCVWithStats(ctx, marketType, tickers, timeframe, from, to, incremental)
	if err != nil {
		return nil, err
	}
	return result.Bars, nil
}

// DownloadHistoricalOHLCVWithStats performs the same download while exposing
// whether each ticker contacted the provider and how many fresh bars it returned.
func (s *DataService) DownloadHistoricalOHLCVWithStats(
	ctx context.Context,
	marketType domain.MarketType,
	tickers []string,
	timeframe Timeframe,
	from, to time.Time,
	incremental bool,
) (*HistoricalOHLCVDownload, error) {
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

	result := &HistoricalOHLCVDownload{
		Bars:             make(map[string][]domain.OHLCV, len(tickers)),
		ProviderRequests: make(map[string]int, len(tickers)),
		FreshBars:        make(map[string]int, len(tickers)),
	}
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
			result.ProviderRequests[trimmedTicker]++
			bars, err := chain.GetOHLCV(ctx, trimmedTicker, timeframe, gap.From, gap.To)
			if err != nil {
				return nil, fmt.Errorf("data: download historical ohlcv for %s: %w", trimmedTicker, err)
			}

			if len(bars) > 0 {
				result.FreshBars[trimmedTicker] += len(bars)
				if err := s.historyRepo.UpsertHistoricalOHLCV(ctx, toHistoricalOHLCV(trimmedTicker, providerName, timeframe, bars)); err != nil {
					return nil, fmt.Errorf("data: persist historical ohlcv for %s: %w", trimmedTicker, err)
				}
			}

			coverage, ok := historicalCoverageForBars(gap, bars, s.currentTime().UTC())
			if ok {
				coverage.Ticker = trimmedTicker
				coverage.Provider = providerName
				coverage.Timeframe = timeframe.String()
				if err := s.historyRepo.UpsertHistoricalOHLCVCoverage(ctx, coverage); err != nil {
					return nil, fmt.Errorf("data: persist historical coverage for %s: %w", trimmedTicker, err)
				}
			}
		}

		stored, err := s.ListHistoricalOHLCV(ctx, trimmedTicker, providerName, timeframe, fromUTC, toUTC)
		if err != nil {
			return nil, fmt.Errorf("data: list persisted historical ohlcv for %s: %w", trimmedTicker, err)
		}
		result.Bars[trimmedTicker] = stored
	}

	return result, nil
}

// GetSocialSentiment aggregates social sentiment from all configured social
// providers (Finnhub, StockTwits, Reddit) concurrently, merges raw counts,
// and caches the combined result.
func (s *DataService) GetSocialSentiment(ctx context.Context, marketType domain.MarketType, ticker string, from, to time.Time) ([]SocialSentiment, error) {
	fromUTC := from.UTC()
	toUTC := to.UTC()
	cacheSelection, err := s.selection.CacheSelection(marketType, cacheDataTypeSocial)
	if err != nil {
		return nil, err
	}

	key := repository.MarketDataCacheKey{
		Ticker:    ticker,
		Provider:  cacheSelection.Provider,
		DataType:  cacheDataTypeSocial,
		Timeframe: newsCacheWindow(fromUTC, toUTC),
		DateFrom:  &fromUTC,
		DateTo:    &toUTC,
	}

	if cacheSelection.Enabled {
		if cached, ok := s.loadCachedSocialSentiment(ctx, key); ok {
			return normalizeSocialSentiment(cached, fromUTC, toUTC), nil
		}
	}

	snapshots := s.aggregateSocialSentiment(ctx, ticker, from, to)
	snapshots = normalizeSocialSentiment(snapshots, fromUTC, toUTC)

	if cacheSelection.Enabled && len(snapshots) > 0 {
		s.storeCached(ctx, key, snapshots, 30*time.Minute)
	}

	return snapshots, nil
}

// aggregateSocialSentiment calls GetSocialSentiment on each social provider
// concurrently, collects all results, normalizes each to the time window, and
// merges them by summing raw counts.
func (s *DataService) aggregateSocialSentiment(ctx context.Context, ticker string, from, to time.Time) []SocialSentiment {
	if len(s.socialProviders) == 0 {
		return nil
	}

	fromUTC := from.UTC()
	toUTC := to.UTC()

	var (
		mu      sync.Mutex
		results [][]SocialSentiment
	)

	g, gCtx := errgroup.WithContext(ctx)
	for _, p := range s.socialProviders {
		provider := p
		g.Go(func() error {
			snapshots, err := provider.GetSocialSentiment(gCtx, ticker, from, to)
			if err != nil {
				if !errors.Is(err, ErrNotImplemented) {
					s.logger.Warn("social aggregator: provider failed",
						slog.String("ticker", ticker),
						slog.Any("error", err),
					)
				}
				return nil // continue with other providers
			}
			// Normalize to time window before merging.
			snapshots = normalizeSocialSentiment(snapshots, fromUTC, toUTC)
			if len(snapshots) > 0 {
				mu.Lock()
				results = append(results, snapshots)
				mu.Unlock()
			}
			return nil
		})
	}
	_ = g.Wait() // errors already handled per-provider

	return mergeSocialSentiment(results)
}

// mergeSocialSentiment combines multiple provider results by summing raw counts
// per day and recomputing ratios.
func mergeSocialSentiment(results [][]SocialSentiment) []SocialSentiment {
	if len(results) == 0 {
		return nil
	}

	// Group by day.
	type dayCounts struct {
		ticker       string
		postCount    int
		commentCount int
		bullishSum   float64 // weighted by post count
		bearishSum   float64
		totalWeight  int
		measuredAt   time.Time
	}

	dayMap := make(map[string]*dayCounts)

	for _, snapshots := range results {
		for _, s := range snapshots {
			key := s.MeasuredAt.UTC().Format("2006-01-02")
			d, exists := dayMap[key]
			if !exists {
				d = &dayCounts{ticker: s.Ticker, measuredAt: s.MeasuredAt}
				dayMap[key] = d
			}
			weight := s.PostCount
			if weight == 0 {
				weight = 1 // ensure providers with ratio-only data still contribute
			}
			d.postCount += s.PostCount
			d.commentCount += s.CommentCount
			d.bullishSum += s.Bullish * float64(weight)
			d.bearishSum += s.Bearish * float64(weight)
			d.totalWeight += weight
		}
	}

	merged := make([]SocialSentiment, 0, len(dayMap))
	for _, d := range dayMap {
		var score, bullish, bearish float64
		if d.totalWeight > 0 {
			bullish = d.bullishSum / float64(d.totalWeight)
			bearish = d.bearishSum / float64(d.totalWeight)
			score = bullish - bearish
		}
		merged = append(merged, SocialSentiment{
			Ticker:       d.ticker,
			Score:        score,
			Bullish:      bullish,
			Bearish:      bearish,
			PostCount:    d.postCount,
			CommentCount: d.commentCount,
			MeasuredAt:   d.measuredAt,
		})
	}

	// Sort by date ascending.
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].MeasuredAt.Before(merged[j].MeasuredAt)
	})

	return merged
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

func (s *DataService) resolveChain(marketType domain.MarketType) (string, DataProvider, error) {
	return s.selection.ResolveMarketChain(marketType, s.stockChain, s.cryptoChain, s.polymarketChain, s.kalshiChain)
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
		if errors.Is(err, repository.ErrNotFound) {
			return false
		}
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

func historicalOHLCVRepo(cacheRepo repository.MarketDataCacheRepository) repository.HistoricalOHLCVRepository {
	if repo, ok := cacheRepo.(repository.HistoricalOHLCVRepository); ok {
		return repo
	}

	return nil
}

func historicalCoverageForBars(gap historicalOHLCVRange, bars []domain.OHLCV, now time.Time) (domain.HistoricalOHLCVCoverage, bool) {
	if len(bars) == 0 {
		if !gap.To.Before(now.Add(-historicalOHLCVEmptyCoverageMaxAge)) {
			return domain.HistoricalOHLCVCoverage{}, false
		}
		return domain.HistoricalOHLCVCoverage{DateFrom: gap.From.UTC(), DateTo: gap.To.UTC(), FetchedAt: now.UTC()}, true
	}

	from := bars[0].Timestamp.UTC()
	to := from
	for _, bar := range bars[1:] {
		ts := bar.Timestamp.UTC()
		if ts.Before(from) {
			from = ts
		}
		if ts.After(to) {
			to = ts
		}
	}

	return domain.HistoricalOHLCVCoverage{DateFrom: from, DateTo: to, FetchedAt: now.UTC()}, true
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
	return timeframe.String() + "|" + truncatedCacheWindow(timeframe, from, to)
}

// truncatedCacheWindow formats from/to truncated to the timeframe's granularity
// so that requests within the same period produce identical cache keys.
func truncatedCacheWindow(timeframe Timeframe, from, to time.Time) string {
	f := truncateForTimeframe(timeframe, from.UTC())
	t := truncateForTimeframe(timeframe, to.UTC())
	return f.Format(time.RFC3339) + "|" + t.Format(time.RFC3339)
}

// truncateForTimeframe rounds a timestamp down to the granularity appropriate
// for the given timeframe so that cache keys are stable across runs.
func truncateForTimeframe(tf Timeframe, t time.Time) time.Time {
	switch tf {
	case Timeframe1m:
		return t.Truncate(time.Minute)
	case Timeframe5m:
		return t.Truncate(5 * time.Minute)
	case Timeframe15m:
		return t.Truncate(15 * time.Minute)
	case Timeframe1h:
		return t.Truncate(time.Hour)
	case Timeframe1d:
		return t.Truncate(24 * time.Hour)
	default:
		return t.Truncate(24 * time.Hour)
	}
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
