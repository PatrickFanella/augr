# CLI Reference

This document describes the current Cobra command tree implemented by the
`tradingagent` binary. Flag descriptions and defaults are taken from the
command definitions in `internal/cli/root.go` and `internal/cli/dashboard.go`.

## Command hierarchy

```text
tradingagent
├── serve
├── run TICKER
├── strategies
│   ├── list
│   └── create
├── dashboard
├── portfolio
├── risk
│   ├── status
│   └── kill
└── memories
    └── search QUERY
```

## Global flags

These persistent flags are available on every command.

| Flag | Default | Description |
| --- | --- | --- |
| `--api-url string` | `http://127.0.0.1:8080` | Base URL for the local trading agent API |
| `--token string` | from `TRADINGAGENT_TOKEN` / empty | Bearer token for authenticated API requests (or set `TRADINGAGENT_TOKEN`) |
| `--api-key string` | from `TRADINGAGENT_API_KEY` / empty | API key for authenticated API requests (or set `TRADINGAGENT_API_KEY`) |
| `--format string` | `table` | Output format: `table` or `json` |
| `-v, --version` | n/a | Print the CLI version |
| `-h, --help` | n/a | Print command help |

## Shared API client environment variables

These environment variables affect every command that talks to the local API:
`run`, `strategies list`, `strategies create`, `dashboard`, `portfolio`,
`risk status`, `risk kill`, and `memories search`.

| Environment variable | Default | Notes |
| --- | --- | --- |
| `TRADINGAGENT_API_URL` | `http://127.0.0.1:8080` | Seeds the default value of `--api-url` |
| `TRADINGAGENT_TOKEN` | empty | Seeds the default value of `--token` |
| `TRADINGAGENT_API_KEY` | empty | Seeds the default value of `--api-key` |

## Commands

### `tradingagent serve`

Starts the HTTP and WebSocket API server using environment-based application
configuration.

**Usage**

```bash
tradingagent serve
```

**Command-specific flags**

This command has no command-specific flags. It inherits the global flags shown
above.

**Environment variables**

`serve` is the only command that loads the full application configuration. When
`APP_ENV=development` (the default), the server also attempts to load a local
`.env` file before parsing environment variables.

#### Bootstrap and logging

| Environment variable | Default | Notes |
| --- | --- | --- |
| `APP_ENV` | `development` | Application environment; also controls whether `.env` is auto-loaded |
| `LOG_LEVEL` | `info` | Logger level used by the `serve` command |

#### Server, auth, and storage

| Environment variable | Default | Notes |
| --- | --- | --- |
| `APP_HOST` | `0.0.0.0` | HTTP bind host |
| `APP_PORT` | `8080` | HTTP bind port |
| `JWT_SECRET` | empty | Required for authenticated API features |
| `DATABASE_URL` | empty | Database connection string |
| `DATABASE_POOL_SIZE` | `10` | PostgreSQL connection pool size |
| `DATABASE_SSL_MODE` | `disable` | PostgreSQL SSL mode |
| `REDIS_URL` | empty | Redis connection string |

#### LLM routing and provider configuration

| Environment variable | Default | Notes |
| --- | --- | --- |
| `LLM_DEFAULT_PROVIDER` | `openai` | Default LLM provider |
| `LLM_DEEP_THINK_MODEL` | `gpt-5.2` | Default deep-think model |
| `LLM_QUICK_THINK_MODEL` | `gpt-5-mini` | Default quick-think model |
| `LLM_TIMEOUT` | `30s` | Request timeout for LLM calls |
| `OPENAI_API_KEY` | empty | OpenAI credential |
| `OPENAI_BASE_URL` | empty | Optional OpenAI-compatible base URL override |
| `OPENAI_MODEL` | `gpt-5-mini` | OpenAI model name |
| `ANTHROPIC_API_KEY` | empty | Anthropic credential |
| `ANTHROPIC_MODEL` | `claude-3-7-sonnet-latest` | Anthropic model name |
| `GOOGLE_API_KEY` | empty | Google credential |
| `GOOGLE_MODEL` | `gemini-2.5-flash` | Google model name |
| `OPENROUTER_API_KEY` | empty | OpenRouter credential |
| `OPENROUTER_BASE_URL` | empty | Optional OpenRouter base URL override |
| `OPENROUTER_MODEL` | `openai/gpt-4.1-mini` | OpenRouter model name |
| `XAI_API_KEY` | empty | xAI credential |
| `XAI_BASE_URL` | empty | Optional xAI base URL override |
| `XAI_MODEL` | `grok-3-mini` | xAI model name |
| `OLLAMA_BASE_URL` | `http://localhost:11434` | Ollama endpoint |
| `OLLAMA_MODEL` | `llama3.2` | Ollama model name |

#### Market data and broker integrations

| Environment variable | Default | Notes |
| --- | --- | --- |
| `POLYGON_API_KEY` | empty | Enables Polygon in the stock data-provider chain |
| `ALPHA_VANTAGE_API_KEY` | empty | Enables Alpha Vantage in the stock data-provider chain |
| `ALPHA_VANTAGE_RATE_LIMIT_PER_MINUTE` | `5` | Alpha Vantage client-side rate limit |
| `FINNHUB_API_KEY` | empty | Parsed into config, but the current runtime does not instantiate a Finnhub provider |
| `FINNHUB_RATE_LIMIT_PER_MINUTE` | `60` | Validated config surface for Finnhub; currently unused at runtime |
| `ALPACA_API_KEY` | empty | Alpaca broker credential |
| `ALPACA_API_SECRET` | empty | Alpaca broker secret |
| `ALPACA_PAPER_MODE` | `true` | Enables paper trading for Alpaca |
| `BINANCE_API_KEY` | empty | Binance broker credential; not required for Binance market-data reads |
| `BINANCE_API_SECRET` | empty | Binance broker secret |
| `BINANCE_PAPER_MODE` | `true` | Enables paper trading for Binance |

#### Notifications and alerting

| Environment variable | Default | Notes |
| --- | --- | --- |
| `NOTIFY_TELEGRAM_BOT_TOKEN` | empty | Telegram bot token |
| `NOTIFY_TELEGRAM_CHAT_ID` | empty | Telegram chat ID |
| `NOTIFY_SMTP_HOST` | empty | SMTP host |
| `NOTIFY_SMTP_PORT` | `587` | SMTP port |
| `NOTIFY_SMTP_USERNAME` | empty | SMTP username |
| `NOTIFY_SMTP_PASSWORD` | empty | SMTP password |
| `NOTIFY_EMAIL_FROM` | empty | Sender email address |
| `NOTIFY_EMAIL_TO` | empty | Comma-separated email recipients |
| `N8N_WEBHOOK_URL` | empty | n8n webhook destination for structured alert, signal, and decision payloads |
| `N8N_WEBHOOK_SECRET` | empty | Optional n8n shared secret sent as `X-Webhook-Secret` |
| `NOTIFY_PAGERDUTY_WEBHOOK_URL` | empty | PagerDuty webhook URL |
| `NOTIFY_PAGERDUTY_WEBHOOK_SECRET` | empty | PagerDuty webhook shared secret |
| `NOTIFY_DISCORD_SIGNAL_WEBHOOK_URL` | empty | Preferred Discord signal webhook env var; legacy aliases `DISCORD_WEBHOOK_SIGNALS` and `DISCORD_SIGNAL_WEBHOOK_URL` still load |
| `NOTIFY_DISCORD_DECISION_WEBHOOK_URL` | empty | Preferred Discord decision webhook env var; legacy aliases `DISCORD_WEBHOOK_DECISIONS` and `DISCORD_DECISION_WEBHOOK_URL` still load |
| `NOTIFY_DISCORD_ALERT_WEBHOOK_URL` | empty | Preferred Discord alert webhook env var; legacy aliases `DISCORD_WEBHOOK_ALERTS` and `DISCORD_ALERT_WEBHOOK_URL` still load |
| `ALERT_PIPELINE_FAILURE_THRESHOLD` | `3` | Consecutive pipeline failures before alerting |
| `ALERT_PIPELINE_FAILURE_CHANNELS` | `telegram,email` | Channels for pipeline-failure alerts |
| `ALERT_CIRCUIT_BREAKER_CHANNELS` | `telegram` | Channels for circuit-breaker alerts |
| `ALERT_LLM_PROVIDER_DOWN_ERROR_RATE_THRESHOLD` | `0.5` | Error-rate threshold for LLM provider alerts |
| `ALERT_LLM_PROVIDER_DOWN_WINDOW` | `5m` | Time window for LLM provider alerts |
| `ALERT_LLM_PROVIDER_DOWN_CHANNELS` | `telegram` | Channels for LLM provider alerts |
| `ALERT_HIGH_LATENCY_THRESHOLD` | `120s` | Threshold for high-latency alerts |
| `ALERT_HIGH_LATENCY_CHANNELS` | `email` | Channels for high-latency alerts |
| `ALERT_KILL_SWITCH_CHANNELS` | `telegram` | Channels for kill-switch alerts |
| `ALERT_DB_CONNECTION_CHANNELS` | `email,pagerduty` | Channels for database-connection alerts |

#### Risk and feature flags

| Environment variable | Default | Notes |
| --- | --- | --- |
| `RISK_MAX_POSITION_SIZE_PCT` | `0.10` | Maximum position size as a fraction of equity |
| `RISK_MAX_DAILY_LOSS_PCT` | `0.02` | Maximum daily loss threshold |
| `RISK_MAX_DRAWDOWN_PCT` | `0.10` | Maximum drawdown threshold |
| `RISK_MAX_OPEN_POSITIONS` | `10` | Maximum number of open positions |
| `RISK_CIRCUIT_BREAKER_THRESHOLD` | `0.05` | Circuit breaker trigger threshold |
| `RISK_CIRCUIT_BREAKER_COOLDOWN` | `15m` | Circuit breaker cooldown duration |
| `TRADING_AGENT_KILL` | empty / unset | If set to `true`, the process starts with the environment-driven kill switch active |
| `ENABLE_SCHEDULER` | `false` | Enables scheduled strategy execution |
| `ENABLE_REDIS_CACHE` | `true` | Enables Redis-backed caching |
| `ENABLE_AGENT_MEMORY` | `true` | Enables the memory subsystem |
| `ENABLE_LIVE_TRADING` | `false` | Enables non-paper trading paths |
### `tradingagent run TICKER`

Resolves a strategy for the given ticker by first requiring a unique exact
match. If multiple exact matches exist, the CLI falls back to requiring a
single active match. If neither condition is met, the command returns an error.
Once a strategy is resolved, it requests a manual pipeline run through the
local API.

**Usage**

```bash
tradingagent run TICKER
```

**Command-specific flags**

This command has no command-specific flags. It inherits the global flags shown
above.

**Environment variables**

- `TRADINGAGENT_API_URL` — default API endpoint for the request
- `TRADINGAGENT_TOKEN` — default bearer token for authenticated requests
- `TRADINGAGENT_API_KEY` — default API key for authenticated requests

### `tradingagent strategies`

Parent command for strategy management subcommands.

**Usage**

```bash
tradingagent strategies
```

**Command-specific flags**

This parent command has no command-specific flags. It inherits the global flags
shown above.

**Environment variables**

- `TRADINGAGENT_API_URL`
- `TRADINGAGENT_TOKEN`
- `TRADINGAGENT_API_KEY`

#### `tradingagent strategies list`

Lists strategy records from the local API.

**Usage**

```bash
tradingagent strategies list
```

**Command-specific flags**

This command has no command-specific flags. It inherits the global flags shown
above.

**Environment variables**

- `TRADINGAGENT_API_URL`
- `TRADINGAGENT_TOKEN`
- `TRADINGAGENT_API_KEY`

#### `tradingagent strategies create`

Creates a new strategy record through the local API.

**Usage**

```bash
tradingagent strategies create [flags]
```

**Flags**

| Flag | Default | Description |
| --- | --- | --- |
| `--name string` | empty | Strategy name |
| `--description string` | empty | Strategy description |
| `--ticker string` | empty | Ticker symbol for the strategy |
| `--market-type string` | empty | Market type: `stock`, `crypto`, or `polymarket` |
| `--schedule-cron string` | empty | Optional cron expression for scheduled runs |
| `--config string` | empty | Optional JSON object for strategy-specific configuration |
| `--active` | `true` | Whether the strategy is active |
| `--paper` | `true` | Whether the strategy uses paper trading |

Required flags: `--name`, `--ticker`, and `--market-type`.

**Environment variables**

- `TRADINGAGENT_API_URL`
- `TRADINGAGENT_TOKEN`
- `TRADINGAGENT_API_KEY`

### `tradingagent dashboard`

Launches the terminal dashboard backed by the local API and `/ws` WebSocket
stream derived from the configured API URL.

**Usage**

```bash
tradingagent dashboard [flags]
```

**Flags**

| Flag | Default | Description |
| --- | --- | --- |
| `--once` | `false` | Render a single TUI frame and exit |
| `--width int` | `120` | Render width for the terminal dashboard |
| `--height int` | `34` | Render height for the terminal dashboard |

**Environment variables**

- `TRADINGAGENT_API_URL` — API base URL; the dashboard derives a `ws://` or
  `wss://` endpoint from it
- `TRADINGAGENT_TOKEN` — bearer token used for HTTP and WebSocket auth
- `TRADINGAGENT_API_KEY` — API key used for HTTP and WebSocket auth

### `tradingagent portfolio`

Fetches the portfolio summary and currently open positions from the local API.

**Usage**

```bash
tradingagent portfolio
```

**Command-specific flags**

This command has no command-specific flags. It inherits the global flags shown
above.

**Environment variables**

- `TRADINGAGENT_API_URL`
- `TRADINGAGENT_TOKEN`
- `TRADINGAGENT_API_KEY`

### `tradingagent risk`

Parent command for risk inspection and kill-switch operations.

**Usage**

```bash
tradingagent risk
```

**Command-specific flags**

This parent command has no command-specific flags. It inherits the global flags
shown above.

**Environment variables**

- `TRADINGAGENT_API_URL`
- `TRADINGAGENT_TOKEN`
- `TRADINGAGENT_API_KEY`
- `TRADING_AGENT_KILL` — affects the server-side kill-switch state that `risk`
  commands may report when the server is running

#### `tradingagent risk status`

Displays the current risk engine, circuit breaker, and kill-switch state.

**Usage**

```bash
tradingagent risk status
```

**Command-specific flags**

This command has no command-specific flags. It inherits the global flags shown
above.

**Environment variables**

- `TRADINGAGENT_API_URL`
- `TRADINGAGENT_TOKEN`
- `TRADINGAGENT_API_KEY`
- `TRADING_AGENT_KILL`

#### `tradingagent risk kill`

Activates the risk kill switch through the local API.

**Usage**

```bash
tradingagent risk kill [flags]
```

**Flags**

| Flag | Default | Description |
| --- | --- | --- |
| `--reason string` | `activated from CLI` | Reason recorded when activating the kill switch |

**Environment variables**

- `TRADINGAGENT_API_URL`
- `TRADINGAGENT_TOKEN`
- `TRADINGAGENT_API_KEY`
- `TRADING_AGENT_KILL` — the server can also report an environment-driven kill
  switch if this is set to `true`

### `tradingagent memories`

Parent command for searching the stored agent memory index.

**Usage**

```bash
tradingagent memories
```

**Command-specific flags**

This parent command has no command-specific flags. It inherits the global flags
shown above.

**Environment variables**

- `TRADINGAGENT_API_URL`
- `TRADINGAGENT_TOKEN`
- `TRADINGAGENT_API_KEY`

#### `tradingagent memories search QUERY`

Searches stored memories by natural-language query text.

**Usage**

```bash
tradingagent memories search QUERY
```

**Command-specific flags**

This command has no command-specific flags. It inherits the global flags shown
above.

**Environment variables**

- `TRADINGAGENT_API_URL`
- `TRADINGAGENT_TOKEN`
- `TRADINGAGENT_API_KEY`
