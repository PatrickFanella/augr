# Polymarket Integration & Signal Intelligence

## Overview

Four-track plan to add prediction market trading, real-time signal intelligence, short-duration quick-trading, and account shadow-trading capabilities.

- **Track A** — Polymarket Integration: full pipeline support for prediction market strategies
- **Track B** — Signal Intelligence: real-time event monitoring and thesis-triggered execution
- **Track C** — Quick-Trade Engine: fast-cycle trading on short-duration markets (design sketch)
- **Track D** — Shadow Trading: account profiling and mirror execution (design sketch)

Dependencies: Track A is foundational. Track B depends on A. Tracks C and D depend on A and B.

---

## Track A: Polymarket Integration

### Problem

The codebase has `MarketTypePolymarket` in the domain, a working Polymarket broker (`internal/execution/polymarket/`), and correct scheduler support (24/7 market). But the end-to-end pipeline is broken: no data provider chain, no broker wiring in the strategy runner, no prediction-market-aware prompts, and no way to thread prediction market metadata through the analysis pipeline.

### Architecture Decisions

#### Data Model

**Ticker = market slug** (e.g. `"will-x-happen"`). Human-readable, stable, the LLM can reason about it. TokenIDs (the hex addresses for YES/NO outcome tokens) are resolved at data-fetch time and cached on the prediction market metadata struct.

**LLM decides YES vs NO.** The trader phase outputs a `Side` field on `TradingPlan` ("YES" or "NO"). The LLM analyzes resolution criteria, news, and probabilities to determine which side is mispriced. No preconfigured direction.

**New `Side` field on `TradingPlan`.** Empty string for equities (not applicable), "YES" or "NO" for Polymarket. Backward-compatible — existing equity strategies ignore it.

#### New Type: PredictionMarketData

Separate from `Fundamentals`. Lives as a new field on `AnalysisInput` and `PipelineState`.

```go
// internal/agent/prediction_market.go (new file)

type PredictionMarketData struct {
    // Market identity
    Slug            string    // market slug (= strategy ticker)
    Question        string    // "Will X happen by Y date?"
    Description     string    // full market description
    ResolutionCriteria string // how the market resolves

    // Resolution
    EndDate         *time.Time // when the market closes
    ResolutionSource string   // who/what resolves it

    // Current state
    YesPrice        float64   // current YES token price (0-1)
    NoPrice         float64   // current NO token price (0-1)
    Volume24h       float64   // 24h volume in USDC
    Liquidity       float64   // total liquidity in USDC
    OpenInterest    float64   // total open interest

    // Token resolution (cached for execution)
    ConditionID     string
    YesTokenID      string
    NoTokenID       string

    // Order book snapshot
    BestBidYes      float64
    BestAskYes      float64
    BestBidNo       float64
    BestAskNo       float64
    SpreadYes       float64   // ask - bid for YES token
}
```

Added to `AnalysisInput`:

```go
type AnalysisInput struct {
    Ticker           string
    Market           *MarketData
    News             []NewsArticle
    Fundamentals     *Fundamentals
    Social           *SocialSentiment
    PredictionMarket *PredictionMarketData  // NEW
}
```

Also added to: `PipelineState`, `InitialStateSeed`, threaded through `loadInitialState` and `applyInitialStateSeed`.

#### Data Provider Strategy

**Polymarket CLOB chain for price/order book data.** `DataService` gets a new `polymarketChain` field. `resolveChain()` returns it for `MarketTypePolymarket`. The chain contains a new `PolymarketDataProvider` that:

- `GetOHLCV()` — fetches token price history from CLOB API, buckets into synthetic OHLCV bars (Close = YES price 0-1, Volume = USDC traded). Standard indicators (RSI, SMA, ATR) are computed on this data.
- `GetFundamentals()` — returns `Fundamentals{}` (empty). Polymarket "fundamentals" live in `PredictionMarketData`.
- `GetNews()` / `GetSocialSentiment()` — returns `ErrNotImplemented`, falls through to stock chain providers. News providers are market-agnostic; the market slug works as a search query.

**`GetPredictionMarketData` is NOT on the `DataProvider` interface.** It's a method on the `PolymarketClient` (extending the existing `internal/execution/polymarket/client.go`). The strategy runner calls it directly in `loadInitialState` when `marketType == polymarket`.

**News/social fall through to stock chain.** The `DataService` needs a tweak: for Polymarket, `GetNews` and `GetSocialSentiment` use the stock chain (since those providers search by keyword and the slug works as a query). Implementation: `resolveChain` returns the polymarket chain for OHLCV, but `GetNews`/`GetSocialSentiment` have a fallback to stock chain for non-stock market types.

#### Analyst Strategy

**Reuse existing analyst roles with Polymarket-specific prompts.** No new agent roles in v1.

| Role                | Equity Behavior         | Polymarket Behavior                                                                                                                                                                |
| ------------------- | ----------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| MarketAnalyst       | OHLCV bars + indicators | Synthetic probability bars + indicators (RSI on probability, SMA trends)                                                                                                           |
| FundamentalsAnalyst | Company financials      | Reads `PredictionMarketData`: resolution criteria, end date, current prices, liquidity. Prompt override via strategy config. `BuildPrompt` checks `input.PredictionMarket != nil`. |
| NewsAnalyst         | Company news            | Event-related news (slug as search query). Same data, different prompt framing.                                                                                                    |
| SocialAnalyst       | Social sentiment        | Social chatter about the event. Same data flow.                                                                                                                                    |

The FundamentalsAnalyst's `BuildPrompt` function becomes:

```go
func(input agent.AnalysisInput) (string, bool) {
    if input.PredictionMarket != nil {
        return FormatPredictionMarketPrompt(input.Ticker, input.PredictionMarket), true
    }
    if input.Fundamentals == nil {
        return "", false  // skip
    }
    return FormatFundamentalsAnalystUserPrompt(input.Ticker, input.Fundamentals), true
}
```

#### Configuration

**Single `PolymarketConfig` block** in the config file. Serves both broker and data provider.

```go
// internal/config/config.go

type PolymarketConfig struct {
    KeyID          string `env:"POLYMARKET_KEY_ID"`
    SecretKey      string `env:"POLYMARKET_SECRET_KEY"`
    APIBaseURL     string `env:"POLYMARKET_API_BASE_URL" default:"https://api.polymarket.us"`
    GatewayBaseURL string `env:"POLYMARKET_GATEWAY_BASE_URL" default:"https://gateway.polymarket.us"`
    CLOBURL        string `env:"POLYMARKET_CLOB_URL" default:"https://clob.polymarket.com"`
}

// Added to BrokerConfigs (or top-level Config):
type Config struct {
    // ... existing fields
    Polymarket PolymarketConfig
}
```

#### Broker Wiring

**`newBrokerForStrategy` in `prod_strategy_runner.go`** gets a new case:

```go
case domain.MarketTypePolymarket:
    if strategy.IsPaper {
        return r.fallbackPaperBroker(), "paper", nil  // reuse existing paper broker
    }
    if !hasBrokerCredentials(r.cfg.Polymarket) {
        return nil, "", fmt.Errorf("polymarket credentials not configured")
    }
    client := polymarket.NewClient(r.cfg.Polymarket.CLOBURL, r.cfg.Polymarket.APIKey,
        r.cfg.Polymarket.Secret, r.cfg.Polymarket.Passphrase)
    return polymarket.NewBroker(client, r.logger), "polymarket", nil
```

**Paper trading uses the existing paper broker.** Limit orders, price 0-1, position tracking all work. The 0-1 price constraint is validated in the pre-processing step, not the paper broker.

#### Execution Pre-Processing

**Strategy runner translates slug → tokenID before `ProcessSignal`.** After the pipeline runs, before calling `ProcessSignal`:

```go
if strategy.MarketType == domain.MarketTypePolymarket && predictionMarketData != nil {
    // Resolve side to tokenID
    switch plan.Side {
    case "YES":
        plan.Ticker = predictionMarketData.YesTokenID
    case "NO":
        plan.Ticker = predictionMarketData.NoTokenID
    default:
        return fmt.Errorf("polymarket strategy requires Side (YES/NO), got %q", plan.Side)
    }
    // Validate price bounds
    if plan.EntryPrice < 0 || plan.EntryPrice > 1 {
        return fmt.Errorf("polymarket entry price must be 0-1, got %v", plan.EntryPrice)
    }
}
```

The order manager stays market-agnostic. The broker receives a tokenID as the ticker, which is what it already expects.

#### Custom Indicators (v2)

After v1 ships with synthetic OHLCV + standard indicators, add prediction-market-specific indicators:

- Implied probability momentum (rate of change of YES price)
- Bid-ask spread % (liquidity signal)
- Time-to-resolution decay (how fast price should converge to 0 or 1)
- Volume-weighted probability (VWAP equivalent for prediction markets)
- Information ratio (price movement per unit of news volume)

These would be computed by a `PolymarketIndicatorSnapshot` function alongside the existing `IndicatorSnapshotFromBars`.

### Implementation Sequence

1. **Types & plumbing** — `PredictionMarketData` struct, new fields on `AnalysisInput`, `PipelineState`, `InitialStateSeed`
2. **Config** — `PolymarketConfig` in config, env vars
3. **Data provider** — `PolymarketDataProvider` implementing `DataProvider`, synthetic OHLCV, `resolveChain` update, news/social fallback
4. **Client extension** — add `GetMarketData(slug) (*PredictionMarketData, error)` to polymarket client
5. **Strategy runner wiring** — `loadInitialState` for polymarket, broker selection, pre-processing step
6. **Prompts** — Polymarket-specific system prompts for FundamentalsAnalyst, MarketAnalyst prompt adjustments
7. **UI** — Polymarket strategy creation, market type selection, prediction market data display on strategy detail page
8. **Kill switches** — per-market kill switches on risk engine, UI redesign (see Risk Management section)

### Files Changed/Created

**New files:**

- `internal/agent/prediction_market.go` — `PredictionMarketData` type
- `internal/data/polymarket/provider.go` — `PolymarketDataProvider` implementing `DataProvider`
- `internal/agent/analysts/prompts_polymarket.go` — Polymarket-specific prompt templates

**Modified files:**

- `internal/agent/phase_io.go` — add `PredictionMarket` to `AnalysisInput`
- `internal/agent/state.go` — add `PredictionMarket` to `PipelineState`
- `internal/agent/runner.go` — add `PredictionMarket` to `InitialStateSeed`, `applyInitialStateSeed`
- `internal/config/config.go` — add `PolymarketConfig`
- `internal/data/factory.go` — add `polymarketChain`, update `resolveChain`, add news/social fallback logic
- `internal/execution/polymarket/client.go` — add `GetMarketData` method
- `cmd/tradingagent/prod_strategy_runner.go` — broker wiring, `loadInitialState`, pre-processing
- `internal/risk/engine_impl.go` — per-market kill switches
- `internal/agent/analysts/fundamentals.go` — `BuildPrompt` checks `PredictionMarket`
- `internal/execution/order_manager.go` — no changes (stays market-agnostic)

---

## Track B: Signal Intelligence

### Problem

The current system is purely cron-driven. Strategies run on a schedule and miss time-sensitive opportunities. Prediction markets especially reward speed — by the time a scheduled run fires, the price has already moved. A real-time signal layer that monitors multiple data sources and triggers immediate action based on standing theses is needed.

### Architecture

#### SignalHub

In-process, channel-based event router. Starts alongside the API server, scheduler, and automation orchestrator in the main `tradingagent` process.

```go
// internal/signal/hub.go

type SignalHub struct {
    sources       []SignalSource
    evaluator     *Evaluator
    watchIndex    *WatchIndex          // in-memory inverted index: term → []strategyID
    triggerCh     chan<- TriggerEvent   // output: consumed by automation orchestrator
    strategies    StrategyProvider      // interface to load active strategies + theses
    logger        *slog.Logger
}

func (h *SignalHub) Start(ctx context.Context) error
func (h *SignalHub) RebuildWatchIndex() error  // called on strategy/thesis change
```

#### SignalSource Interface

Each input adapter owns its goroutine lifecycle. Config-driven instantiation.

```go
// internal/signal/source.go

type RawSignalEvent struct {
    Source    string            // "rss", "polymarket_clob", "reddit", "whale_tracker"
    Title    string            // headline or event description
    Body     string            // full text (may be empty for price events)
    URL      string            // source URL if applicable
    Metadata map[string]any    // source-specific data (price, volume, account, etc.)
    ReceivedAt time.Time
}

type SignalSource interface {
    Name() string
    Start(ctx context.Context) (<-chan RawSignalEvent, error)
}
```

#### v1 Source Adapters

| Source                  | Implementation                                                                                                                     | Polling Interval | Signal Type                    |
| ----------------------- | ---------------------------------------------------------------------------------------------------------------------------------- | ---------------- | ------------------------------ |
| RSS feeds               | HTTP poll, parse XML. Reuse `internal/data/rss/`. Config: list of feed URLs.                                                       | 60s              | News headlines                 |
| Polymarket CLOB monitor | Poll price/volume for watched markets. Flag moves > configurable threshold.                                                        | 10s              | Price spikes, volume surges    |
| Reddit RSS              | `reddit.com/r/{subreddit}/.rss`. Config: list of subreddits.                                                                       | 60s              | Social chatter, breaking posts |
| On-chain trade alerts   | Poll CLOB `/trades` endpoint. Flag trades above USDC size threshold. Cross-reference with account profiles for high-edge accounts. | 15s              | Whale/edge activity            |

Config-driven registration:

```yaml
signal_intelligence:
  enabled: true
  sources:
    - type: rss
      feeds:
        - https://feeds.reuters.com/reuters/topNews
        - https://feeds.apnews.com/rss/apf-topnews
      poll_interval: 60s
    - type: polymarket_clob
      price_move_threshold_pct: 5.0
      volume_spike_multiplier: 3.0
      poll_interval: 10s
    - type: reddit
      subreddits:
        - politics
        - worldnews
        - cryptocurrency
        - polymarket
      poll_interval: 60s
    - type: whale_tracker
      min_trade_usdc: 5000
      min_account_win_rate: 0.70
      poll_interval: 15s
```

#### Two-Stage Evaluator

**Stage 1: Keyword filter.** Zero latency, zero cost. The `WatchIndex` maps terms to strategy IDs. Each incoming `RawSignalEvent` is scanned for matching terms. Events with no matches are dropped.

```go
// internal/signal/watch_index.go

type WatchIndex struct {
    mu     sync.RWMutex
    terms  map[string][]uuid.UUID  // normalized term → strategy IDs
}

func (w *WatchIndex) Match(text string) []uuid.UUID  // returns affected strategy IDs
func (w *WatchIndex) Rebuild(strategies []StrategyWithThesis)
```

The index is built from:

1. Active strategy tickers and market slugs (auto-derived)
2. `WatchTerms` from each strategy's active thesis (LLM-generated)
3. Manual additions via API

**Stage 2: LLM evaluation.** Only events that pass the keyword filter. Uses the quick-think model (small/fast) with a tight prompt.

```go
// internal/signal/evaluator.go

type EvaluatedSignal struct {
    Raw              RawSignalEvent
    AffectedStrategies []uuid.UUID
    Urgency          int        // 1-5
    Summary          string     // one-line LLM summary
    RecommendedAction string   // "re-evaluate", "execute_thesis", "monitor"
}

type Evaluator struct {
    provider  llm.Provider
    model     string  // quick-think model
}

func (e *Evaluator) Evaluate(ctx context.Context, event RawSignalEvent, strategies []StrategyContext) (*EvaluatedSignal, error)
```

Prompt contract for the evaluator:

```
You are a real-time signal evaluator for a trading system.

EVENT:
{title}
{body}

ACTIVE STRATEGIES WITH THESES:
{for each matched strategy: ticker, market type, thesis summary, watch terms}

Rate urgency 1-5:
1 = background noise, no action needed
2 = mildly relevant, log for context
3 = relevant, queue a re-analysis on next available slot
4 = significant, trigger a re-analysis within 5 minutes
5 = breaking/critical, execute standing thesis immediately if one exists

Output JSON: {"urgency": N, "summary": "...", "affected_strategies": ["id1", ...], "action": "monitor|re-evaluate|execute_thesis"}
```

#### Urgency-Tiered Triggers

| Urgency | Action                                                                                                                      |
| ------- | --------------------------------------------------------------------------------------------------------------------------- |
| 1-2     | Log event, update strategy context for next scheduled run                                                                   |
| 3-4     | Queue an immediate pipeline run for affected strategies (full LLM analysis with shortened debate: 1 round)                  |
| 5       | Check for standing thesis. If exists: execute via rules engine immediately (no LLM). If no thesis: fast-track pipeline run. |

```go
// internal/signal/trigger.go

type TriggerEvent struct {
    Signal       EvaluatedSignal
    StrategyID   uuid.UUID
    Action       TriggerAction  // RunPipeline, ExecuteThesis, LogOnly
    Priority     int            // maps from urgency
}

type TriggerAction string
const (
    TriggerActionLogOnly       TriggerAction = "log_only"
    TriggerActionRunPipeline   TriggerAction = "run_pipeline"
    TriggerActionExecuteThesis TriggerAction = "execute_thesis"
)
```

The automation orchestrator (or a new trigger handler) consumes `TriggerEvent` from the channel and dispatches accordingly.

#### Durable Thesis

The LLM pipeline outputs a thesis alongside the `TradingPlan`. The thesis persists between runs and the signal intelligence layer can execute it without LLM involvement.

**Thesis structure:**

```go
// internal/agent/thesis.go (new file)

type Thesis struct {
    // Executable rules — the core of the thesis
    Rules       RulesEngineConfig `json:"rules"`

    // Watch terms for the signal intelligence keyword filter
    WatchTerms  []string          `json:"watch_terms"`

    // Human-readable summary for UI and LLM context
    Summary     string            `json:"summary"`
    Conviction  float64           `json:"conviction"`    // 0-1
    Direction   string            `json:"direction"`     // "YES", "NO", "buy", "sell", "hold"
    TimeHorizon string            `json:"time_horizon"`  // "hours", "days", "weeks"

    // Invalidation — conditions under which the thesis is no longer valid
    InvalidAfter    *time.Time    `json:"invalid_after,omitempty"`
    InvalidateIf    []string      `json:"invalidate_if,omitempty"` // natural language conditions

    // Metadata
    GeneratedAt     time.Time     `json:"generated_at"`
    PipelineRunID   uuid.UUID     `json:"pipeline_run_id"`
}
```

**Storage:** `active_thesis` JSONB column on the `strategies` table. One active thesis per strategy (latest pipeline run supersedes). Historical theses reconstructable from `pipeline_run_snapshots`.

**In-memory watch index:** On startup, the `SignalHub` loads all active strategies, extracts `WatchTerms` from each thesis, and builds the inverted index. When a pipeline run produces a new thesis, the `SignalHub.RebuildWatchIndex()` is called.

**Thesis generation:** The trader phase prompt is extended to output a `Thesis` alongside the `TradingPlan`. The trader already outputs structured JSON — the thesis fields are added to the output schema. The runner persists the thesis to the strategy row after a successful pipeline run.

### Account Profiling System

Part of Track B because the whale tracker signal source depends on it, but the full shadow-trading capability is Track D.

#### Schema

```sql
CREATE TABLE polymarket_accounts (
    address         TEXT PRIMARY KEY,
    display_name    TEXT,           -- if resolvable
    first_seen      TIMESTAMPTZ NOT NULL,
    last_active     TIMESTAMPTZ NOT NULL,
    total_trades    INT NOT NULL DEFAULT 0,
    total_volume    NUMERIC NOT NULL DEFAULT 0,
    -- Win/loss record
    markets_entered INT NOT NULL DEFAULT 0,
    markets_won     INT NOT NULL DEFAULT 0,
    markets_lost    INT NOT NULL DEFAULT 0,
    win_rate        NUMERIC,        -- computed: won / (won + lost)
    -- Category breakdown (JSONB for flexibility)
    category_stats  JSONB,          -- {"politics": {"entered": 10, "won": 8}, ...}
    -- Size patterns
    avg_position    NUMERIC,
    max_position    NUMERIC,
    -- Timing patterns
    avg_entry_time_before_resolution INTERVAL,
    early_entry_rate NUMERIC,       -- fraction of trades entered before major price moves
    -- Profile metadata
    tags            TEXT[],         -- manual labels: "whale", "insider-pattern", "bot", etc.
    tracked         BOOLEAN NOT NULL DEFAULT false,  -- actively tracked by signal source
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE polymarket_account_trades (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_address TEXT NOT NULL REFERENCES polymarket_accounts(address),
    market_slug     TEXT NOT NULL,
    side            TEXT NOT NULL,       -- "YES" or "NO"
    action          TEXT NOT NULL,       -- "buy" or "sell"
    price           NUMERIC NOT NULL,
    size_usdc       NUMERIC NOT NULL,
    timestamp       TIMESTAMPTZ NOT NULL,
    -- Resolution (populated after market resolves)
    outcome         TEXT,               -- "YES", "NO", or NULL if unresolved
    pnl             NUMERIC,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_account_trades_account ON polymarket_account_trades(account_address);
CREATE INDEX idx_account_trades_market ON polymarket_account_trades(market_slug);
CREATE INDEX idx_account_trades_timestamp ON polymarket_account_trades(timestamp);
CREATE INDEX idx_accounts_win_rate ON polymarket_accounts(win_rate) WHERE tracked = true;
```

#### Batch Profile Builder

Background job that runs periodically (every 15-30 minutes):

1. Fetches recent trades from CLOB API
2. Inserts into `polymarket_account_trades`
3. For resolved markets: updates `outcome` and `pnl` on trades, recomputes `win_rate`, `category_stats`, timing/size patterns on `polymarket_accounts`
4. Auto-flags accounts crossing interesting thresholds (win rate > 70% with > 20 markets resolved) by setting `tracked = true`

The signal source adapter queries `polymarket_accounts WHERE tracked = true` and cross-references incoming trades against tracked accounts.

### Implementation Sequence

1. **Signal types & interfaces** — `RawSignalEvent`, `SignalSource`, `EvaluatedSignal`, `TriggerEvent`, `Thesis`
2. **WatchIndex** — in-memory inverted index with rebuild from strategies
3. **SignalHub** — fan-in from sources, two-stage evaluation, trigger emission
4. **RSS source adapter** — reuse existing RSS code
5. **Polymarket CLOB monitor adapter** — price/volume polling
6. **Reddit RSS adapter** — subreddit RSS polling
7. **Evaluator** — LLM-based urgency rating
8. **Trigger handler** — wire into automation orchestrator or strategy runner
9. **Thesis generation** — extend trader prompt, persist to strategy row
10. **Account profiling schema + batch job** — tables, repository, background worker
11. **Whale tracker adapter** — trade polling + account profile cross-reference
12. **UI** — signal intelligence dashboard, live event feed, thesis display on strategy page

### Files Created

- `internal/signal/hub.go` — SignalHub orchestrator
- `internal/signal/source.go` — SignalSource interface, RawSignalEvent type
- `internal/signal/evaluator.go` — two-stage evaluator (keyword + LLM)
- `internal/signal/trigger.go` — TriggerEvent types, dispatch logic
- `internal/signal/watch_index.go` — in-memory term → strategy inverted index
- `internal/signal/source_rss.go` — RSS feed adapter
- `internal/signal/source_polymarket.go` — CLOB price/volume monitor
- `internal/signal/source_reddit.go` — Reddit RSS adapter
- `internal/signal/source_whale.go` — on-chain trade alert adapter
- `internal/agent/thesis.go` — Thesis type
- `internal/repository/postgres/polymarket_account.go` — account profile repository
- `internal/automation/jobs_signal.go` — account profile batch job

---

## Risk Management

### Per-Market Kill Switches

The risk engine gains per-market granularity. Two layers:

**Hard stop (risk engine level):** Blocks at the order manager. Nothing gets through regardless of strategy state.

```go
// internal/risk/engine_impl.go additions

type RiskEngineImpl struct {
    // ... existing fields
    marketKillSwitches map[domain.MarketType]bool  // per-market hard stops
    marketKsMu         sync.RWMutex
}

func (e *RiskEngineImpl) IsMarketKillSwitchActive(ctx context.Context, marketType domain.MarketType) (bool, error)
func (e *RiskEngineImpl) ActivateMarketKillSwitch(ctx context.Context, marketType domain.MarketType, reason string) error
func (e *RiskEngineImpl) DeactivateMarketKillSwitch(ctx context.Context, marketType domain.MarketType) error
```

**Soft stop (strategy level):** Existing strategy `status` field (active/paused/inactive). Per-strategy control.

**`CheckPreTrade` updated:** Checks global kill switch, then per-market kill switch, then existing circuit breaker logic.

### Kill Switch UX Redesign

Current UX uses ambiguous "activate/deactivate" language. New UX:

**Global emergency stop:** Large red button at top of dashboard.

- Active state (trading): Button reads **"STOP ALL TRADING"** (red, destructive styling)
- Stopped state: Button reads **"RESUME ALL TRADING"** (green, constructive styling)
- Status indicator: green dot + "Trading Active" or red dot + "All Trading Stopped"

**Per-market controls:** Card per market type below global stop.

- Each card shows: market type name, number of active strategies, current status
- Toggle button: **"STOP"** (red) or **"RESUME"** (green)
- When stopped: card has red border/background tint, all strategies show as blocked

**No ambiguous "activate" / "deactivate" language anywhere.** Every control is an action verb describing what will happen: stop, resume, pause, unpause.

### Polymarket-Specific Risk Controls

**Max exposure per market:** No single prediction market should consume more than X% of the portfolio. Configurable per strategy or globally.

```go
type PolymarketRiskConfig struct {
    MaxSingleMarketExposurePct float64  // max % of portfolio in one market (default: 10%)
    MaxTotalExposurePct        float64  // max % of portfolio across all polymarket (default: 30%)
    MaxPositionUSDC            float64  // hard cap per position in USDC
    MinLiquidity               float64  // don't enter markets with less than X USDC liquidity
    MaxSpreadPct               float64  // don't enter if bid-ask spread > X%
    MinDaysToResolution        int      // don't enter markets resolving in < N days (avoid illiquid endgame)
}
```

**Correlation limits:** Multiple prediction markets may be correlated (e.g., "Will X win primary?" and "Will X win general?"). If both are held, exposure is effectively doubled. v1: manual correlation tagging in strategy config. v2: auto-detection based on market metadata similarity.

**Bankroll management:** Prediction market portfolio treated as a separate bankroll from equities/crypto. Configurable allocation: "max 20% of total portfolio in prediction markets." The position sizing step reads the polymarket-specific bankroll, not total equity.

**Token management:** Polymarket uses USDC on Polygon. The system needs to track:

- USDC balance on Polygon (available for Polymarket trading)
- Bridging needs (if USDC is on Ethereum mainnet, need to bridge to Polygon)
- Gas fees (MATIC for Polygon transactions)

v1: assume USDC is already on Polygon. Display balance in UI. v2: bridge automation.

---

## Track C: Quick-Trade Engine (Design Sketch)

### Problem

Polymarket has short-duration markets (5-minute resolution, weather events, sports quarters) where the edge is speed, not thesis quality. You're trading probability fluctuations, not waiting for resolution. Get in, capture the edge, get out.

### Key Characteristics

- **No LLM in the hot path.** Latency budget is milliseconds to seconds, not minutes.
- **WebSocket connection to CLOB.** Polling is too slow. Need real-time order book updates.
- **Rules engine only.** Entry/exit conditions evaluated on every tick.
- **Auto-exit before resolution.** Don't hold through resolution — take profit on probability movement.
- **High frequency, small size.** Many trades per day, small positions, tight stops.

### Architectural Constraints (from Track A/B)

- Uses the same Polymarket broker (limit orders only)
- Same risk engine with per-market kill switch
- Same account balance / position tracking
- Same order manager flow, just triggered by rules engine instead of LLM
- Signal intelligence (Track B) can feed quick-trade opportunities (e.g., "5-min market just opened with mispriced odds")

### Open Questions

1. **WebSocket vs fast polling?** CLOB API may not have a WebSocket endpoint. Need to verify. If polling only, what's the minimum interval before rate limiting?
2. **Position management for short-duration markets.** Auto-close all positions N seconds before resolution? What's the liquidity like in the final minutes?
3. **Strategy definition format.** Is `RulesEngineConfig` sufficient, or does quick-trade need its own config with tick-level conditions?
4. **Backtesting.** Need tick-level historical data for short-duration markets. Does the CLOB API provide this?
5. **Concurrency.** Multiple quick-trade strategies on different markets simultaneously. How does the rules engine handle this? One goroutine per market with its own WebSocket/polling loop?

### Dependencies

- Track A: Polymarket broker, data provider, config, tokenID resolution
- Track B: Signal intelligence can discover quick-trade opportunities

---

## Track D: Shadow Trading (Design Sketch)

### Problem

Some Polymarket accounts have consistently high win rates. If you can identify them, track their activity, and understand their patterns, you can derive signal from their trades. This ranges from simple "follow the smart money" to sophisticated pattern analysis and strategy reverse-engineering.

### Capabilities

#### Account Discovery

Building on Track B's account profiling system:

- **Statistical screening:** Identify accounts with win rate > X% over N+ resolved markets. Filter by category (some accounts are only good at politics, not sports).
- **Behavioral clustering:** Group accounts by trading patterns (early entrants, contrarians, momentum followers, last-minute snipers). Different clusters suggest different edge sources.
- **New account anomaly detection:** Brand new accounts making large, confident bets. Could indicate insider knowledge or a veteran using a new wallet.
- **Network analysis:** Accounts that consistently trade the same markets at similar times may be coordinated. Identify clusters.

#### Strategy Reverse-Engineering

- **Entry pattern analysis:** When do they enter? What price level? How does their entry timing relate to news events?
- **Position sizing analysis:** Do they size proportionally to conviction? Do they scale in?
- **Exit pattern analysis:** Do they hold to resolution or take profit early? What triggers their exits?
- **Category specialization:** Which domains is each account best at? What's their edge per category?

#### Mirror Execution

- **Real-time alerts:** When a tracked high-edge account opens a position, fire a signal event through the Signal Intelligence hub.
- **Conditional mirroring:** Don't blindly copy. The signal evaluator (Track B) checks the trade against current market conditions, the strategy's thesis, and risk limits before executing.
- **Delay analysis:** How fast do you need to react? If a high-edge account buys YES at 0.40 and the price moves to 0.45 within 5 minutes, is following at 0.45 still +EV?

### Open Questions

1. **Data access.** Can you get per-account trade history from the CLOB API, or do you need on-chain indexing? What's the latency?
2. **Account identity.** Polymarket accounts are Polygon addresses. Can you link addresses to known entities (funds, public figures)?
3. **Gaming.** If your mirror trading becomes detectable, tracked accounts could exploit it (buy → wait for followers → sell into them). How do you mitigate?
4. **Legal/ethical boundaries.** Front-running concerns? Polymarket is unregulated but reputation matters.
5. **Scale.** How many accounts to actively track? 10? 100? 1000? Compute/storage implications.

### Dependencies

- Track A: Polymarket data infrastructure
- Track B: Account profiling, signal intelligence pipeline, trigger system

---

## UI Changes

### Strategy Management

- **Market type selector** expanded: Stock, Crypto, Polymarket, Options (existing but wired)
- **Polymarket strategy creation:** ticker field becomes market slug autocomplete (search Polymarket markets), show current YES/NO prices, volume, resolution date
- **Strategy detail page:** for Polymarket strategies, show `PredictionMarketData` (resolution criteria, current prices, liquidity, order book), thesis summary, watch terms

### Signal Intelligence Dashboard (new page)

- **Live event feed:** real-time stream of signal events with source icons (RSS, Reddit, CLOB, whale)
- **Evaluated events:** filtered list showing only events that passed keyword filter + LLM evaluation, with urgency badges
- **Trigger log:** history of triggers fired, which strategies were affected, what action was taken
- **Watchlist management:** add/remove manual watch terms, see auto-derived terms from theses

### Kill Switch Redesign

- **Top bar:** global emergency stop button (always visible)
- **Market cards:** per-market-type status + stop/resume toggle
- **Strategy list:** per-strategy pause/resume, visual indication when blocked by market-level or global stop

### Account Explorer (Track D, future)

- **Leaderboard:** top accounts by win rate, filterable by category and time period
- **Account detail:** trade history, win rate chart over time, category breakdown, behavioral profile
- **Tracked accounts:** list of actively monitored accounts, alert configuration

---

## Glossary

| Term               | Definition                                                                                                                                                                                                                    |
| ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Slug**           | Polymarket market identifier, human-readable URL slug (e.g. "will-x-happen-by-2025")                                                                                                                                          |
| **TokenID**        | Hex address of a YES or NO outcome token on Polygon. Used by the CLOB API for order submission.                                                                                                                               |
| **ConditionID**    | Polymarket's internal ID for a market condition that has YES/NO outcomes.                                                                                                                                                     |
| **Thesis**         | A durable, executable trading plan generated by the LLM pipeline. Combines a `RulesEngineConfig` (executable conditions), `WatchTerms` (signal filter keywords), and invalidation conditions. Persists between pipeline runs. |
| **Signal Event**   | A raw event from any signal source (news headline, price spike, whale trade) before evaluation.                                                                                                                               |
| **Trigger**        | An evaluated signal event that has been scored for urgency and dispatched for action (pipeline run or thesis execution).                                                                                                      |
| **Watch Index**    | In-memory inverted index mapping keywords to strategy IDs. Built from active theses + manual entries. Used by the keyword filter stage of the evaluator.                                                                      |
| **Quick-Trade**    | Fast-cycle trading on short-duration prediction markets where the goal is capturing probability movement, not waiting for resolution.                                                                                         |
| **Shadow Trading** | Identifying and optionally mirroring trades from high-edge Polymarket accounts.                                                                                                                                               |
| **Edge Account**   | A Polymarket account with statistically significant win rate, suggesting informational or analytical advantage.                                                                                                               |
