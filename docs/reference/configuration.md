---
title: "Configuration"
description: "Environment variables, feature flags, runtime settings, and persistence semantics."
status: "canonical"
updated: "2026-04-08"
tags: [configuration, env, reference]
---

# Configuration

All durable server configuration currently starts in the environment.

## Loading rules

- `internal/config.Load()` reads environment variables
- `.env` is auto-loaded only when `APP_ENV=development`
- validation runs after loading
- the API settings surface is separate and currently memory-backed

## Major configuration groups

### Server and auth

- `APP_ENV`
- `APP_HOST`
- `APP_PORT`
- `LOG_LEVEL`
- `JWT_SECRET`

### Database and cache

- `DATABASE_URL`
- `DATABASE_POOL_SIZE`
- `DATABASE_SSL_MODE`
- `REDIS_URL`

### LLM routing

- `LLM_DEFAULT_PROVIDER`
- `LLM_DEEP_THINK_MODEL`
- `LLM_QUICK_THINK_MODEL`
- `LLM_TIMEOUT`
- provider-specific keys and base URLs

### Data providers

| Variable | Notes |
| --- | --- |
| `POLYGON_API_KEY` | OHLCV, fundamentals, options chain |
| `ALPHA_VANTAGE_API_KEY` | OHLCV, fundamentals (free tier: 25 req/day) |
| `ALPHA_VANTAGE_RATE_LIMIT_PER_MINUTE` | Override default rate limit |
| `FINNHUB_API_KEY` | Fundamentals, news, social sentiment (Reddit+Twitter), earnings/IPO calendar |
| `FINNHUB_RATE_LIMIT_PER_MINUTE` | Override default rate limit |
| `NEWSAPI_API_KEY` | News articles for stock tickers (`newsapi.org`) |
| `FMP_API_KEY` | Financial Modeling Prep OHLCV + fundamentals |
| `FMP_RATE_LIMIT_PER_MINUTE` | Override default rate limit |

### Brokers

| Variable | Notes |
| --- | --- |
| `ALPACA_API_KEY` | Required for stock live/paper trading |
| `ALPACA_API_SECRET` | — |
| `ALPACA_PAPER_MODE` | `true` = paper account (default false) |
| `BINANCE_API_KEY` | Required for crypto live/paper trading |
| `BINANCE_API_SECRET` | — |
| `BINANCE_PAPER_MODE` | `true` = paper account (default false) |
| `POLYMARKET_KEY_ID` | Required for live Polymarket prediction-market trading |
| `POLYMARKET_SECRET_KEY` | Base64-encoded Ed25519 secret key for retail API signing |
| `POLYMARKET_API_BASE_URL` | Override authenticated retail API base URL (default: `https://api.polymarket.us`) |
| `POLYMARKET_GATEWAY_BASE_URL` | Override public gateway base URL (default: `https://gateway.polymarket.us`) |
| `POLYMARKET_CLOB_URL` | Legacy CLOB/data endpoint still used by older data/signal workflows during migration |

### Polymarket risk limits

| Variable | Notes |
| --- | --- |
| `POLYMARKET_MAX_POSITION_USDC` | Maximum USDC per single prediction market position |
| `POLYMARKET_MAX_SINGLE_EXPOSURE_PCT` | Maximum % of portfolio in one market |
| `POLYMARKET_MAX_TOTAL_EXPOSURE_PCT` | Maximum total Polymarket exposure as % of portfolio |
| `POLYMARKET_MIN_LIQUIDITY_USDC` | Minimum USDC liquidity required to enter a market |
| `POLYMARKET_MAX_SPREAD_PCT` | Maximum bid-ask spread to accept |
| `POLYMARKET_MIN_DAYS_TO_RESOLUTION` | Minimum days remaining before market resolution |

### Notifications

| Variable | Notes |
| --- | --- |
| `TELEGRAM_BOT_TOKEN` | Telegram bot API token |
| `TELEGRAM_CHAT_ID` | Target chat/channel ID |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD` | Email alerts |
| `SMTP_FROM`, `SMTP_TO` | Sender and recipient |
| `N8N_WEBHOOK_URL` | n8n workflow webhook |
| `PAGERDUTY_ROUTING_KEY` | PagerDuty Events API v2 key |
| `DISCORD_WEBHOOK_URL` | Default Discord webhook |
| `DISCORD_WEBHOOK_SIGNALS` | Separate webhook for signal events |
| `DISCORD_WEBHOOK_DECISIONS` | Separate webhook for agent decision events |
| `DISCORD_WEBHOOK_ALERTS` | Separate webhook for operational alerts |

#### Alert routing

| Variable | Notes |
| --- | --- |
| `ALERT_CIRCUIT_BREAKER_CHANNELS` | Comma-separated channel names (e.g. `discord,telegram`) |
| `ALERT_KILL_SWITCH_CHANNELS` | Channels notified on kill-switch activation/deactivation |
| `ALERT_DB_CONNECTION_CHANNELS` | Channels notified on DB connectivity issues |
| `ALERT_LLM_PROVIDER_DOWN_CHANNELS` | Channels notified when LLM error rate threshold is exceeded |
| `ALERT_HIGH_LATENCY_CHANNELS` | Channels notified when API/pipeline latency exceeds threshold |
| `ALERT_PIPELINE_FAILURE_CHANNELS` | Channels notified on pipeline execution failures |
| `ALERT_HIGH_LATENCY_THRESHOLD` | Duration string, e.g. `5s` |
| `ALERT_LLM_PROVIDER_DOWN_ERROR_RATE_THRESHOLD` | Float, e.g. `0.5` = 50% error rate |
| `ALERT_LLM_PROVIDER_DOWN_WINDOW` | Duration window for error rate measurement |
| `ALERT_PIPELINE_FAILURE_THRESHOLD` | Consecutive failure count before alerting |

### Risk defaults

| Variable | Notes |
| --- | --- |
| `RISK_MAX_POSITION_SIZE_PCT` | Max position as fraction of portfolio (e.g. `0.10`) |
| `RISK_MAX_DAILY_LOSS_PCT` | Circuit-breaker daily loss threshold (e.g. `0.03`) |
| `RISK_MAX_DRAWDOWN_PCT` | Circuit-breaker total drawdown threshold (e.g. `0.10`) |
| `RISK_MAX_OPEN_POSITIONS` | Maximum concurrent open positions |
| `RISK_CIRCUIT_BREAKER_THRESHOLD` | Alias for `RISK_MAX_DAILY_LOSS_PCT` |
| `RISK_CIRCUIT_BREAKER_COOLDOWN` | Duration before circuit breaker auto-resets (e.g. `15m`) |
| `TRADING_AGENT_KILL` | Set to `true` to activate the kill switch via environment variable |

### Feature flags

| Variable | Default | Notes |
| --- | --- | --- |
| `ENABLE_SCHEDULER` | `false` | Enable the cron scheduler for strategy and backtest runs |
| `ENABLE_REDIS_CACHE` | `false` | Use Redis for market-data and LLM response caching |
| `ENABLE_AGENT_MEMORY` | `false` | Enable agent memory storage and retrieval |
| `ENABLE_LIVE_TRADING` | `false` | Allow live broker order submission (paper mode otherwise) |

### Ticker discovery

| Variable | Notes |
| --- | --- |
| `TICKER_DISCOVERY` | `true` to enable scheduled ticker universe refresh |
| `TICKER_DISCOVERY_CRON` | Cron expression for discovery runs (e.g. `0 6 * * 1-5`) |
| `TICKER_DISCOVERY_MIN_ADV` | Minimum average daily volume filter |
| `TICKER_DISCOVERY_MAX_TICKERS` | Maximum universe size |

## What persists vs what does not

### Durable

- environment variables
- database records
- migration state
- strategies and run artifacts

### Not durable

- LLM provider API keys entered through the settings UI (stored in memory only; never written to DB)
- Circuit-breaker trip state (resets on restart; intentional — restarts imply manual intervention)

## Kill switch sources

The risk engine supports three independent mechanisms, any one of which will halt trading:

| Mechanism | How to activate | Survives restart? |
| --- | --- | --- |
| API toggle | `POST /api/v1/risk/killswitch` with `{"active": true}` | ✓ (persisted to `risk_state` table, migration 000025) |
| Environment variable | `TRADING_AGENT_KILL=true` | ✓ (re-evaluated from env on each check) |
| File flag | Create file at `/tmp/tradingagent_kill` | ✓ (file persists until deleted) |

When debugging a stuck kill-switch, check all three sources. `GET /api/v1/risk/status` reports the active mechanisms.

## Broker visibility in settings

The settings response currently reports broker connection summaries for:

- Alpaca
- Binance

This is a UI/control-plane summary, not proof that every execution path is production-ready.

## Recommended local minimum

```dotenv
APP_ENV=development
JWT_SECRET=replace-this-with-a-real-secret
DATABASE_URL=postgres://postgres:postgres@localhost:5432/tradingagent?sslmode=disable
REDIS_URL=redis://localhost:6379/0
OPENAI_API_KEY=...
```

## Recommended local Ollama minimum

```dotenv
APP_ENV=development
JWT_SECRET=replace-this-with-a-real-secret
DATABASE_URL=postgres://postgres:postgres@localhost:5432/tradingagent?sslmode=disable
REDIS_URL=redis://localhost:6379/0
LLM_DEFAULT_PROVIDER=ollama
OLLAMA_BASE_URL=http://localhost:11434
OLLAMA_MODEL=llama3.2
```

## Operational cautions

- A configured key in `.env` does not guarantee the runtime currently instantiates that integration.
- The UI can make settings feel persistent even though they are not.
- Secrets entered through the UI are not a substitute for durable secret management.
