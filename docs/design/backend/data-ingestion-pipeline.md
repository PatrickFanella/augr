---
title: "Data Ingestion Pipeline"
date: 2026-03-20
tags: [backend, data, market-data, ingestion, providers]
---

# Data Ingestion Pipeline

The data ingestion pipeline abstracts market data retrieval behind a provider interface, handles caching, fallback, and serves data to analyst agents.

## Provider Interface

```go
// internal/data/provider.go
type MarketDataProvider interface {
    Name() string
    GetOHLCV(ctx context.Context, req OHLCVRequest) ([]OHLCV, error)
    GetFundamentals(ctx context.Context, ticker string) (*Fundamentals, error)
    GetNews(ctx context.Context, req NewsRequest) ([]NewsItem, error)
    GetIndicators(ctx context.Context, req IndicatorRequest) (*Indicators, error)
}

type OHLCVRequest struct {
    Ticker    string
    From      time.Time
    To        time.Time
    Timeframe string // "1d", "1h", "5m"
}

type OHLCV struct {
    Date   time.Time
    Open   float64
    High   float64
    Low    float64
    Close  float64
    Volume int64
}

type Fundamentals struct {
    MarketCap       float64
    PERatio         float64
    PBRatio         float64
    RevenueGrowth   float64
    ProfitMargin    float64
    DebtToEquity    float64
    FreeCashFlow    float64
    DividendYield   float64
    EarningsDate    time.Time
    IncomeStatement []FinancialPeriod
    BalanceSheet    []FinancialPeriod
    CashFlow        []FinancialPeriod
}
```

## Provider Implementations

### Polygon.io (Primary for Stocks)

- REST API with Go HTTP client
- Requires API key (`POLYGON_API_KEY`)
- Endpoints: `/v2/aggs/ticker/{ticker}/range/1/day/{from}/{to}`, `/vX/reference/tickers/{ticker}`
- Rate limits: 5 req/min (free), unlimited (paid)
- Preferred for real-time and intraday data

### Alpha Vantage (Fallback)

- REST API: `https://www.alphavantage.co/query?function=TIME_SERIES_DAILY&symbol={ticker}`
- Requires `ALPHA_VANTAGE_API_KEY`
- Free tier: 25 requests/day — use as fallback when Polygon rate-limited
- Provides fundamentals via `OVERVIEW`, `INCOME_STATEMENT`, `BALANCE_SHEET`, `CASH_FLOW` functions

### Yahoo Finance (Free Tier)

- Unofficial API via `https://query1.finance.yahoo.com/v8/finance/chart/{ticker}`
- No API key required
- Suitable for development and paper trading
- Not recommended for production (no SLA, may break)

### Crypto Exchanges (Direct API)

For crypto tickers, use exchange REST APIs directly:

- **Binance**: `GET /api/v3/klines` for OHLCV
- **Coinbase**: `GET /api/v3/brokerage/market/products/{product_id}/candles`
- **Kraken**: `GET /0/public/OHLC`

These are separate implementations behind the same `MarketDataProvider` interface.

## Provider Chain with Fallback

```go
// internal/data/chain.go
type ProviderChain struct {
    providers []MarketDataProvider
    cache     *CacheLayer
}

func (c *ProviderChain) GetOHLCV(ctx context.Context, req OHLCVRequest) ([]OHLCV, error) {
    // Check cache first
    if cached, ok := c.cache.GetOHLCV(req); ok {
        return cached, nil
    }

    // Try providers in order
    var lastErr error
    for _, p := range c.providers {
        data, err := p.GetOHLCV(ctx, req)
        if err != nil {
            lastErr = err
            slog.Warn("provider failed, trying next",
                "provider", p.Name(),
                "ticker", req.Ticker,
                "error", err,
            )
            continue
        }
        c.cache.SetOHLCV(req, data)
        return data, nil
    }
    return nil, fmt.Errorf("all providers failed: %w", lastErr)
}
```

## Caching Strategy

| Data Type                | Cache TTL          | Storage                               |
| ------------------------ | ------------------ | ------------------------------------- |
| Daily OHLCV (historical) | 24 hours           | PostgreSQL `market_data_cache`        |
| Intraday OHLCV           | 5 minutes          | Redis (if available), else in-memory  |
| Fundamentals             | 6 hours            | PostgreSQL                            |
| News                     | 30 minutes         | PostgreSQL                            |
| Indicators               | Derived from OHLCV | Computed on demand, cached with OHLCV |

Cache lookup uses the `market_data_cache` table with composite key `(ticker, data_type, provider, date_from, date_to)`.

## Agent-to-Data Mapping

Each analyst agent has specific data requirements:

| Agent                                    | Data Needed          | Provider Method               |
| ---------------------------------------- | -------------------- | ----------------------------- |
| [[analyst-agents\|Market Analyst]]       | OHLCV + indicators   | `GetOHLCV` + `GetIndicators`  |
| [[analyst-agents\|Fundamentals Analyst]] | Financial statements | `GetFundamentals`             |
| [[analyst-agents\|News Analyst]]         | Recent news articles | `GetNews`                     |
| [[analyst-agents\|Social Media Analyst]] | Social sentiment     | `GetNews` (sentiment-focused) |

The orchestrator injects the `MarketDataProvider` into each agent, and agents call the methods they need.

## News Sources

| Source             | Coverage             | API Key           |
| ------------------ | -------------------- | ----------------- |
| NewsAPI            | 80K+ sources, global | `NEWSAPI_KEY`     |
| Polygon.io News    | Market-focused       | Same as OHLCV key |
| Alpha Vantage News | Market + macro       | Same as AV key    |

News items are normalized to a common `NewsItem` struct with title, summary, source, published time, and sentiment score (if available).

---

**Related:** [[data-architecture]] · [[market-data-providers]] · [[technical-indicators]] · [[analyst-agents]]
