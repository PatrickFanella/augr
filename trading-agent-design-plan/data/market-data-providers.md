---
title: "Market Data Providers"
date: 2026-03-20
tags: [data, providers, polygon, alphavantage, yahoo, binance]
---

# Market Data Providers

Detailed implementation notes for each data provider.

## Polygon.io (Primary — Stocks)

**Endpoints:**

| Data         | Endpoint                                                           | Notes                            |
| ------------ | ------------------------------------------------------------------ | -------------------------------- |
| OHLCV        | `GET /v2/aggs/ticker/{ticker}/range/{mult}/{timespan}/{from}/{to}` | Supports 1m to 1y timeframes     |
| Company Info | `GET /v3/reference/tickers/{ticker}`                               | Market cap, SIC code, etc.       |
| Financials   | `GET /vX/reference/financials?ticker={ticker}`                     | Income, balance sheet, cash flow |
| News         | `GET /v2/reference/news?ticker={ticker}`                           | With sentiment if available      |

**Auth:** API key via `apiKey` query parameter

**Go Implementation Notes:**

- Use `net/http` with custom transport for rate limiting
- Parse JSON responses into normalized types
- Handle pagination for financial data (multiple periods)

## Alpha Vantage (Fallback — Stocks)

**Endpoints:**

| Data         | Function                                                     | Notes                   |
| ------------ | ------------------------------------------------------------ | ----------------------- |
| Daily OHLCV  | `TIME_SERIES_DAILY`                                          | 20+ years of daily data |
| Indicators   | `RSI`, `MACD`, `BBANDS`, etc.                                | Pre-computed indicators |
| Fundamentals | `OVERVIEW`, `INCOME_STATEMENT`, `BALANCE_SHEET`, `CASH_FLOW` | Quarterly + annual      |
| News         | `NEWS_SENTIMENT`                                             | With sentiment scores   |

**Rate Limit Handling:**

```go
func (av *AlphaVantageProvider) GetOHLCV(ctx context.Context, req OHLCVRequest) ([]OHLCV, error) {
    if err := av.rateLimiter.Wait(ctx); err != nil {
        return nil, fmt.Errorf("rate limited: %w", err)
    }
    resp, err := av.fetch(ctx, map[string]string{
        "function": "TIME_SERIES_DAILY",
        "symbol":   req.Ticker,
        "outputsize": "compact",
    })
    if isRateLimited(resp) {
        return nil, ErrRateLimited // triggers fallback to next provider
    }
    return parseAlphaVantageOHLCV(resp)
}
```

## Yahoo Finance (Development)

**Endpoint:** `https://query1.finance.yahoo.com/v8/finance/chart/{ticker}`

- No API key required
- Good for development and paper trading
- Returns OHLCV, dividends, splits
- No formal SLA — may break without notice
- Financials via `https://query2.finance.yahoo.com/v10/finance/quoteSummary/{ticker}`

## Binance (Crypto)

**Endpoints:**

| Data       | Endpoint                                               |
| ---------- | ------------------------------------------------------ |
| OHLCV      | `GET /api/v3/klines?symbol={pair}&interval={interval}` |
| Ticker     | `GET /api/v3/ticker/24hr?symbol={pair}`                |
| Order book | `GET /api/v3/depth?symbol={pair}`                      |

**Notes:**

- Public endpoints — no auth needed for market data
- Testnet at `https://testnet.binance.vision`
- Symbol format: `BTCUSDT`, `ETHUSDT`

## Coinbase (Crypto — Fallback)

**Endpoint:** `GET /api/v3/brokerage/market/products/{product_id}/candles`

- Auth required (API key)
- Product format: `BTC-USD`, `ETH-USD`

## Polymarket (Prediction Markets)

Market data for Polymarket comes from their API:

- `GET /markets` — list active markets
- `GET /markets/{condition_id}` — market details with current prices
- CLOB orderbook data via Gamma API

Prediction market data is fundamentally different (binary outcomes, probability pricing) and is handled by a specialized provider.

## Provider Selection Logic

```go
func selectProviders(marketType string, config DataConfig) []MarketDataProvider {
    switch marketType {
    case "stock":
        return buildChain(config.StockProviders) // polygon → alphavantage → yahoo
    case "crypto":
        return buildChain(config.CryptoProviders) // binance → coinbase
    case "polymarket":
        return []MarketDataProvider{NewPolymarketProvider()}
    default:
        return buildChain(config.StockProviders)
    }
}
```

---

**Related:** [[data-architecture]] · [[data-ingestion-pipeline]] · [[execution-overview]]
