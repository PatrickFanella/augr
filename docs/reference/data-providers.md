# Data providers reference

This document describes the provider wiring that exists in the current runtime.
Code is the source of truth.

## Provider-chain model

`internal/data.ProviderChain` tries providers in order and returns the first successful result for the requested method. Providers that do not implement a method return `data.ErrNotImplemented`, which allows the chain to fall through to the next provider.

`internal/data.DataService` builds two chains:

- `stock-chain`: Polygon -> Alpha Vantage -> Yahoo
- `crypto-chain`: Binance

Market type selection is strict:

- `stock` uses `stock-chain`
- `crypto` uses `crypto-chain`
- any other market type returns `data: unsupported market type`

## What is actually wired today

| Provider | Runtime status | Market types | Order in chain | Notes |
| --- | --- | --- | --- | --- |
| Polygon | live | `stock` | 1 | Requires `POLYGON_API_KEY` |
| Alpha Vantage | live | `stock` | 2 | Requires `ALPHA_VANTAGE_API_KEY` |
| Yahoo Finance | live | `stock` | 3 | Public fallback; no provider env vars |
| Binance | live | `crypto` | 1 | Public market-data endpoints; broker creds are separate |
| NewsAPI | code exists, not wired | none | n/a | Package exists under `internal/data/newsapi`, but the runtime does not register or instantiate it |
| Finnhub | config accepted, not wired | none | n/a | `FINNHUB_*` env vars load and validate, but there is no provider implementation/factory wiring |

## Supported methods by provider

| Provider | OHLCV | Fundamentals | News | Social sentiment |
| --- | --- | --- | --- | --- |
| Polygon | yes | no | yes | no |
| Alpha Vantage | yes | yes | yes | no |
| Yahoo Finance | yes | no | no | no |
| Binance | yes | no | no | no |
| NewsAPI package | no | no | yes | no |

No provider currently implements `GetSocialSentiment` in the runtime.

## Configuration surface

### Live provider env vars

| Env var | Default | Used by runtime | Notes |
| --- | --- | --- | --- |
| `POLYGON_API_KEY` | empty | Polygon stock provider | Required to enable Polygon in the stock chain |
| `ALPHA_VANTAGE_API_KEY` | empty | Alpha Vantage stock provider | Required to enable Alpha Vantage in the stock chain |
| `ALPHA_VANTAGE_RATE_LIMIT_PER_MINUTE` | `5` | Alpha Vantage stock provider | Applied by the registered Alpha Vantage client rate limiter |

### Accepted by config, but not instantiated

| Env var | Default | Current behavior |
| --- | --- | --- |
| `FINNHUB_API_KEY` | empty | Parsed into config, but no runtime provider uses it |
| `FINNHUB_RATE_LIMIT_PER_MINUTE` | `60` | Validated, but no runtime provider uses it |

### Not used for market-data providers

| Env var(s) | Why not |
| --- | --- |
| `BINANCE_API_KEY`, `BINANCE_API_SECRET` | Broker execution credentials, not Binance market-data provider settings |
| any `NEWSAPI_*` variable | The current config loader does not expose NewsAPI settings |
| any `YAHOO_*` variable | Yahoo provider is always a public fallback and has no config surface |

## Code-truth rate limits

These are runtime limits visible in code, not promises about vendor-side quotas.

| Provider | Code-truth rate limit |
| --- | --- |
| Polygon | no custom limiter in current provider code |
| Alpha Vantage | `ALPHA_VANTAGE_RATE_LIMIT_PER_MINUTE`, default `5/min` |
| Yahoo Finance | no custom limiter in current provider code |
| Binance | internal limiter at `1200/min` |
| NewsAPI package | internal limiter at `100 requests / 24h` if you instantiate the package manually |
| Finnhub | config field exists with default `60/min`, but runtime does not instantiate a Finnhub provider |

## Method-level behavior

### OHLCV

Supported timeframes in the live providers are `1m`, `5m`, `15m`, `1h`, and `1d`.

- Polygon: stocks, paginated aggregates endpoint
- Alpha Vantage: stocks, daily/intraday time-series endpoints
- Yahoo Finance: stocks, public chart endpoint
- Binance: crypto, public klines endpoint

### Fundamentals

Only Alpha Vantage currently returns fundamentals. The runtime combines Alpha Vantage `OVERVIEW`, `INCOME_STATEMENT`, and `BALANCE_SHEET` responses into one `data.Fundamentals` result.

### News

- Polygon returns stock news and maps ticker-level sentiment when Polygon supplies it.
- Alpha Vantage returns stock news from `NEWS_SENTIMENT`.
- The NewsAPI package can return news if manually instantiated, but the runtime does not currently wire it.

### Social sentiment

No live provider is wired for social-sentiment reads today.

## Example `.env`

```dotenv
POLYGON_API_KEY=your-polygon-key
ALPHA_VANTAGE_API_KEY=your-alpha-vantage-key
ALPHA_VANTAGE_RATE_LIMIT_PER_MINUTE=5

# Accepted by config validation, but not used by the runtime provider factory yet.
FINNHUB_API_KEY=
FINNHUB_RATE_LIMIT_PER_MINUTE=60
```

## Important gaps

- NewsAPI is present in the repository but is not imported by `cmd/tradingagent/main.go` and is not part of `DataService` factory wiring.
- Finnhub has config/validation surface but no provider implementation in the current runtime.
- Yahoo and Binance are live fallbacks/providers even though they do not expose dedicated provider env vars.
- Binance market data is public; do not confuse it with Binance broker execution credentials.
