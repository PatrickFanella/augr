package data

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

const (
	cacheProviderStockChain      = "stock-chain"
	cacheProviderCryptoChain     = "crypto-chain"
	cacheProviderPolymarketChain = "polymarket-chain"
	cacheProviderKalshiChain     = "kalshi-chain"
	cacheProviderSocialAgg       = "social-agg"

	cacheDataTypeOHLCV        = "ohlcv"
	cacheDataTypeFundamentals = "fundamentals"
	cacheDataTypeNews         = "news"
	cacheDataTypeSocial       = "social_sentiment"
)

// ErrUnsupportedMarketType is returned when a request targets an unknown market.
var ErrUnsupportedMarketType = errors.New("data: unsupported market type")

// SelectionPolicy declares provider ordering, market routing, cache provider
// naming, and fallback behavior for the data package.
type SelectionPolicy struct{}

// ProviderChains captures the ordered provider lists used to assemble the data
// service chains.
type ProviderChains struct {
	Stock      []DataProvider
	Crypto     []DataProvider
	Polymarket []DataProvider
	Kalshi     []DataProvider
	Social     []DataProvider
}

// CacheSelection describes whether caching is enabled for a request and which
// logical cache provider name should be stamped into the cache key.
type CacheSelection struct {
	Provider string
	Enabled  bool
}

// FallbackPolicy controls whether a provider error is considered a retryable
// failure for the current chain.
type FallbackPolicy struct {
	IgnoreNotImplemented bool
}

var (
	providerChainFallback = FallbackPolicy{}
	optionsChainFallback  = FallbackPolicy{IgnoreNotImplemented: true}
)

// BuildProviderChains materializes the provider ordering rules for the data
// service from the registry and configuration.
func (SelectionPolicy) BuildProviderChains(cfg config.Config, reg *ProviderRegistry, logger *slog.Logger, socialTriage *SocialTriageConfig) ProviderChains {
	if logger == nil {
		logger = slog.Default()
	}
	if reg == nil {
		reg = &ProviderRegistry{}
	}

	chains := ProviderChains{
		Stock:      make([]DataProvider, 0, 6),
		Crypto:     make([]DataProvider, 0, 1),
		Polymarket: make([]DataProvider, 0, 1),
		Kalshi:     make([]DataProvider, 0, 1),
		Social:     make([]DataProvider, 0, 3),
	}

	if reg.Yahoo != nil {
		chains.Stock = append(chains.Stock, reg.Yahoo(ProviderConfig{Logger: logger}))
	}
	if apiKey := strings.TrimSpace(cfg.DataProviders.Polygon.APIKey); apiKey != "" && reg.Polygon != nil {
		chains.Stock = append(chains.Stock, reg.Polygon(ProviderConfig{APIKey: apiKey, Logger: logger}))
	}
	if apiKey := strings.TrimSpace(cfg.DataProviders.Finnhub.APIKey); apiKey != "" && reg.Finnhub != nil {
		chains.Stock = append(chains.Stock, reg.Finnhub(ProviderConfig{APIKey: apiKey, RateLimitPerMinute: cfg.DataProviders.Finnhub.RateLimitPerMinute, Logger: logger}))
	}
	if apiKey := strings.TrimSpace(cfg.DataProviders.FMP.APIKey); apiKey != "" && reg.FMP != nil {
		chains.Stock = append(chains.Stock, reg.FMP(ProviderConfig{APIKey: apiKey, RateLimitPerMinute: cfg.DataProviders.FMP.RateLimitPerMinute, Logger: logger}))
	}
	if apiKey := strings.TrimSpace(cfg.DataProviders.AlphaVantage.APIKey); apiKey != "" && reg.AlphaVantage != nil {
		chains.Stock = append(chains.Stock, reg.AlphaVantage(ProviderConfig{APIKey: apiKey, RateLimitPerMinute: cfg.DataProviders.AlphaVantage.RateLimitPerMinute, Logger: logger}))
	}
	if apiKey := strings.TrimSpace(cfg.DataProviders.NewsAPI.APIKey); apiKey != "" && reg.NewsAPI != nil {
		chains.Stock = append(chains.Stock, reg.NewsAPI(ProviderConfig{APIKey: apiKey, Logger: logger}))
	}

	if reg.Binance != nil {
		chains.Crypto = append(chains.Crypto, reg.Binance(ProviderConfig{Logger: logger}))
	}

	if reg.Polymarket != nil && strings.TrimSpace(cfg.Brokers.Polymarket.CLOBURL) != "" {
		chains.Polymarket = append(chains.Polymarket, reg.Polymarket(ProviderConfig{BaseURL: cfg.Brokers.Polymarket.CLOBURL, Logger: logger}))
	}
	if reg.Kalshi != nil {
		chains.Kalshi = append(chains.Kalshi, reg.Kalshi(ProviderConfig{BaseURL: cfg.Brokers.Kalshi.APIBaseURL, Logger: logger}))
	}

	if apiKey := strings.TrimSpace(cfg.DataProviders.Finnhub.APIKey); apiKey != "" && reg.Finnhub != nil {
		chains.Social = append(chains.Social, reg.Finnhub(ProviderConfig{APIKey: apiKey, RateLimitPerMinute: cfg.DataProviders.Finnhub.RateLimitPerMinute, Logger: logger}))
	}
	if reg.StockTwits != nil {
		chains.Social = append(chains.Social, reg.StockTwits(ProviderConfig{Logger: logger}))
	}
	if reg.Bluesky != nil && socialTriage != nil && socialTriage.Provider != nil {
		chains.Social = append(chains.Social, reg.Bluesky(ProviderConfig{Logger: logger, LLMProvider: socialTriage.Provider, LLMModel: socialTriage.Model}))
	}
	if reg.Reddit != nil && socialTriage != nil && socialTriage.Provider != nil {
		chains.Social = append(chains.Social, reg.Reddit(ProviderConfig{Logger: logger, LLMProvider: socialTriage.Provider, LLMModel: socialTriage.Model}))
	}

	return chains
}

// ResolveMarketChain selects the correct first-wins provider chain for a market.
func (SelectionPolicy) ResolveMarketChain(marketType domain.MarketType, stock, crypto, polymarket, kalshi DataProvider) (string, DataProvider, error) {
	switch normalizeMarketType(marketType) {
	case domain.MarketTypeStock:
		return cacheProviderStockChain, stock, nil
	case domain.MarketTypeCrypto:
		return cacheProviderCryptoChain, crypto, nil
	case domain.MarketTypePolymarket:
		return cacheProviderPolymarketChain, polymarket, nil
	case domain.MarketTypeKalshi:
		return cacheProviderKalshiChain, kalshi, nil
	default:
		return "", nil, fmt.Errorf("%w: %s", ErrUnsupportedMarketType, marketType)
	}
}

// CacheSelection returns the cache provider name and whether caching is enabled
// for the requested data type.
func (SelectionPolicy) CacheSelection(marketType domain.MarketType, dataType string) (CacheSelection, error) {
	switch dataType {
	case cacheDataTypeSocial:
		return CacheSelection{Provider: cacheProviderSocialAgg, Enabled: true}, nil
	case cacheDataTypeOHLCV, cacheDataTypeFundamentals, cacheDataTypeNews:
		switch normalizeMarketType(marketType) {
		case domain.MarketTypeStock:
			return CacheSelection{Provider: cacheProviderStockChain, Enabled: true}, nil
		case domain.MarketTypeCrypto:
			return CacheSelection{Provider: cacheProviderCryptoChain, Enabled: true}, nil
		case domain.MarketTypePolymarket:
			return CacheSelection{Provider: cacheProviderPolymarketChain, Enabled: true}, nil
		case domain.MarketTypeKalshi:
			return CacheSelection{Provider: cacheProviderKalshiChain, Enabled: true}, nil
		default:
			return CacheSelection{}, fmt.Errorf("%w: %s", ErrUnsupportedMarketType, marketType)
		}
	default:
		return CacheSelection{Enabled: false}, nil
	}
}

func (p FallbackPolicy) shouldRecord(err error) bool {
	if p.IgnoreNotImplemented && errors.Is(err, ErrNotImplemented) {
		return false
	}
	return true
}

func normalizeMarketType(marketType domain.MarketType) domain.MarketType {
	return domain.MarketType(strings.ToLower(strings.TrimSpace(marketType.String())))
}
