---
title: "Data Architecture"
date: 2026-03-20
tags: [data, architecture, providers, caching]
---

# Data Architecture

## Overview

The data layer provides a unified interface for accessing market data across all asset classes. It handles provider abstraction, caching, fallback, and rate limit management.

```
┌──────────────────────────────────────────────────────────┐
│                    Agent Layer                             │
│  Market Analyst · Fundamentals · News · Social · Trader   │
└──────────────────────┬───────────────────────────────────┘
                       │ GetOHLCV, GetFundamentals, GetNews
┌──────────────────────▼───────────────────────────────────┐
│               Provider Chain                              │
│  ┌─────────────────────────────────────────────────────┐ │
│  │              Cache Layer                             │ │
│  │  PostgreSQL (historical) · In-Memory (hot data)     │ │
│  └──────────┬──────────────────────────────────────────┘ │
│             │ cache miss                                  │
│  ┌──────────▼──────────────────────────────────────────┐ │
│  │         Provider Priority Chain                      │ │
│  │  1. Polygon.io → 2. Alpha Vantage → 3. Yahoo       │ │
│  │  1. Binance → 2. Coinbase → 3. Kraken  (crypto)    │ │
│  └─────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────┘
```

## Data Types

| Type             | Description                                 | Sources                                           |
| ---------------- | ------------------------------------------- | ------------------------------------------------- |
| OHLCV            | Price bars (open, high, low, close, volume) | Polygon, Alpha Vantage, Yahoo, Exchanges          |
| Fundamentals     | Financial statements, ratios, company info  | Polygon, Alpha Vantage                            |
| News             | Articles with titles, summaries, timestamps | NewsAPI, Polygon, Alpha Vantage                   |
| Indicators       | Computed technical indicators               | Derived from OHLCV (see [[technical-indicators]]) |
| Social Sentiment | Social media sentiment signals              | News APIs (sentiment-focused queries)             |

## Provider Configuration

Each strategy can configure its preferred provider chain:

```go
type DataConfig struct {
    StockProviders  []string // ["polygon", "alphavantage", "yahoo"]
    CryptoProviders []string // ["binance", "coinbase"]
    NewsProviders   []string // ["newsapi", "polygon"]
}
```

## Rate Limit Management

| Provider      | Free Tier    | Paid Tier    | Strategy                                   |
| ------------- | ------------ | ------------ | ------------------------------------------ |
| Polygon.io    | 5 req/min    | Unlimited    | Primary for stocks; use cache aggressively |
| Alpha Vantage | 25 req/day   | 75 req/min   | Fallback only on free tier                 |
| Yahoo Finance | Unofficial   | N/A          | Development/paper trading only             |
| Binance       | 1200 req/min | Same         | Generous; no special handling              |
| NewsAPI       | 100 req/day  | 1000 req/day | Cache news for 30 min                      |

Rate limits are tracked per-provider with a token bucket implementation:

```go
type RateLimiter struct {
    limiter *rate.Limiter
    name    string
}

func (rl *RateLimiter) Wait(ctx context.Context) error {
    return rl.limiter.Wait(ctx)
}
```

## Data Flow for a Pipeline Run

1. Orchestrator starts pipeline for `(AAPL, 2026-03-20)`
2. Analysts request data concurrently:
   - Market Analyst: `GetOHLCV(AAPL, -90d, today)` + `GetIndicators(AAPL, 14)`
   - Fundamentals: `GetFundamentals(AAPL)`
   - News: `GetNews(AAPL, -7d, today)`
   - Social: `GetNews(AAPL, -3d, today)` (sentiment filter)
3. Cache layer checks PostgreSQL for each request
4. Cache misses trigger API calls to provider chain
5. Responses are cached for future use
6. Data is passed to analysts as structured Go types

## Data Normalization

All providers return data normalized to common types (defined in [[data-ingestion-pipeline]]). Provider-specific field names and formats are mapped in each provider implementation.

---

**Related:** [[data-ingestion-pipeline]] · [[market-data-providers]] · [[technical-indicators]] · [[analyst-agents]]
