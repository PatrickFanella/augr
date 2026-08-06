package data

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// ErrNoProviders is returned when a ProviderChain contains no providers.
var ErrNoProviders = errors.New("data: no providers in chain")

// ProviderChain tries each DataProvider in order and returns the first
// successful result. If all providers fail, the last error is returned.
type ProviderChain struct {
	providers []DataProvider
	logger    *slog.Logger
	fallback  FallbackPolicy
}

// NewProviderChain constructs a ProviderChain from an ordered list of providers.
// Providers are tried in the order they are given. If logger is nil, slog.Default() is used.
func NewProviderChain(logger *slog.Logger, providers ...DataProvider) *ProviderChain {
	if logger == nil {
		logger = slog.Default()
	}
	return &ProviderChain{
		providers: providers,
		logger:    logger,
		fallback:  providerChainFallback,
	}
}

// tryChain iterates providers calling fn for each one. It returns the result of
// the first successful call, or the last error if all providers fail.
func tryChain[T any](c *ProviderChain, method, ticker string, fn func(DataProvider) (T, error)) (T, error) {
	var zero T
	if len(c.providers) == 0 {
		return zero, ErrNoProviders
	}

	var lastErr error
	for _, p := range c.providers {
		result, err := fn(p)
		if err == nil {
			return result, nil
		}

		if c.fallback.shouldRecord(err) {
			c.logger.Warn("data provider failed, trying next",
				slog.String("method", method),
				slog.String("ticker", ticker),
				slog.Any("error", err),
			)
			lastErr = err
		}
	}

	return zero, lastErr
}

// GetOHLCV iterates providers and returns the first non-empty successful OHLCV
// result. An empty response is not authoritative while another configured
// provider may have coverage for the symbol.
func (c *ProviderChain) GetOHLCV(ctx context.Context, ticker string, timeframe Timeframe, from, to time.Time) ([]domain.OHLCV, error) {
	if len(c.providers) == 0 {
		return nil, ErrNoProviders
	}

	var lastErr error
	var sawEmpty bool
	for _, provider := range c.providers {
		bars, err := provider.GetOHLCV(ctx, ticker, timeframe, from, to)
		if err == nil {
			if len(bars) > 0 {
				return bars, nil
			}
			sawEmpty = true
			c.logger.Info("data provider returned no OHLCV, trying next",
				slog.String("ticker", ticker),
				slog.String("timeframe", timeframe.String()),
			)
			continue
		}
		if c.fallback.shouldRecord(err) {
			c.logger.Warn("data provider failed, trying next",
				slog.String("method", "GetOHLCV"),
				slog.String("ticker", ticker),
				slog.Any("error", err),
			)
			lastErr = err
		}
	}
	if sawEmpty {
		return []domain.OHLCV{}, nil
	}
	return nil, lastErr
}

// GetFundamentals iterates providers and returns fundamentals. If a provider
// succeeds but marks fields as missing, later providers are still queried to
// backfill those fields before returning.
func (c *ProviderChain) GetFundamentals(ctx context.Context, ticker string) (Fundamentals, error) {
	if len(c.providers) == 0 {
		return Fundamentals{}, ErrNoProviders
	}

	var out Fundamentals
	var haveResult bool
	var lastErr error
	for _, p := range c.providers {
		result, err := p.GetFundamentals(ctx, ticker)
		if err != nil {
			c.logger.Warn("data provider failed, trying next",
				slog.String("method", "GetFundamentals"),
				slog.String("ticker", ticker),
				slog.Any("error", err),
			)
			lastErr = err
			continue
		}

		if !haveResult {
			out = result
			haveResult = true
		} else {
			out = mergeFundamentals(out, result)
		}

		if len(out.MissingFields) == 0 {
			return out, nil
		}
	}

	if haveResult {
		return out, nil
	}
	return Fundamentals{}, lastErr
}

func mergeFundamentals(base, fill Fundamentals) Fundamentals {
	if base.Ticker == "" {
		base.Ticker = fill.Ticker
	}
	if base.FetchedAt.IsZero() || (!fill.FetchedAt.IsZero() && fill.FetchedAt.After(base.FetchedAt)) {
		base.FetchedAt = fill.FetchedAt
	}

	remaining := make([]string, 0, len(base.MissingFields))
	for _, field := range base.MissingFields {
		if IsFundamentalFieldMissing(fill, field) {
			remaining = append(remaining, field)
			continue
		}

		switch field {
		case FundamentalFieldMarketCap:
			base.MarketCap = fill.MarketCap
		case FundamentalFieldPERatio:
			base.PERatio = fill.PERatio
		case FundamentalFieldEPS:
			base.EPS = fill.EPS
		case FundamentalFieldRevenue:
			base.Revenue = fill.Revenue
		case FundamentalFieldRevenueGrowthYoY:
			base.RevenueGrowthYoY = fill.RevenueGrowthYoY
		case FundamentalFieldGrossMargin:
			base.GrossMargin = fill.GrossMargin
		case FundamentalFieldDebtToEquity:
			base.DebtToEquity = fill.DebtToEquity
		case FundamentalFieldFreeCashFlow:
			base.FreeCashFlow = fill.FreeCashFlow
		case FundamentalFieldDividendYield:
			base.DividendYield = fill.DividendYield
		default:
			remaining = append(remaining, field)
		}
	}

	base.MissingFields = remaining
	return base
}

// GetNews iterates providers and returns the first successful news result.
func (c *ProviderChain) GetNews(ctx context.Context, ticker string, from, to time.Time) ([]NewsArticle, error) {
	return tryChain(c, "GetNews", ticker, func(p DataProvider) ([]NewsArticle, error) {
		return p.GetNews(ctx, ticker, from, to)
	})
}

// GetSocialSentiment iterates providers and returns the first successful sentiment result.
func (c *ProviderChain) GetSocialSentiment(ctx context.Context, ticker string, from, to time.Time) ([]SocialSentiment, error) {
	return tryChain(c, "GetSocialSentiment", ticker, func(p DataProvider) ([]SocialSentiment, error) {
		return p.GetSocialSentiment(ctx, ticker, from, to)
	})
}
