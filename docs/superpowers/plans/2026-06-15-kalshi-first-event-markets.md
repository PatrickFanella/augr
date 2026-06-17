# Kalshi-First Event Markets Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Add Kalshi beside Polymarket as the first US-compliant event-market provider, keep Polymarket working, and extract only the shared prediction-market interfaces proven by both providers.

**Architecture:** Start additive: add `kalshi` as a concrete market type with paper/data support and no live execution by default. After Kalshi and Polymarket both run through native prediction-market flows, extract provider-neutral seams for snapshots, data loading, native decisions, sizing, and position identity. Keep provider-specific auth/order signing isolated in provider packages.

**Tech Stack:** Go, PostgreSQL migrations, existing `execution.Broker`, existing scheduler/runtime, Kalshi Trade API v2 (`https://external-api.kalshi.com/trade-api/v2`), Kalshi demo API (`https://external-api.demo.kalshi.co/trade-api/v2`), existing React dashboard patterns.

---

## Scope and Principles

### In scope for this plan

- Add `domain.MarketTypeKalshi` and DB enum support.
- Add Kalshi config/env parsing and validation.
- Add a Kalshi HTTP client with RSA request signing.
- Add Kalshi read/data support: market metadata, order book, balance/positions.
- Add Kalshi snapshot model and paper-only native execution path.
- Keep Polymarket native execution, recorder, reconciler, dashboard, and paper defaults intact.
- Extract shared prediction-market interfaces only after Kalshi proves the second implementation.
- Add dashboard grouping so “Event Markets” can include Polymarket, Kalshi, and future providers.

### Out of scope for the first implementation pass

- Kalshi live order submission.
- Kalshi WebSocket streaming.
- Shared discovery engine across all providers.
- Replacing all Polymarket-specific database tables.

### Provider constraints

- Kalshi auth uses RSA request signing, not Polymarket L2 HMAC.
- Kalshi query strings are excluded from signature string.
- Kalshi demo and production use separate hosts and credentials.
- Kalshi uses market tickers/events/series, not Polymarket slug/token IDs.
- Polymarket stays paper/data-only unless live eligibility is legally available and explicit gates are passed.

---

## File Structure

### New files

- `migrations/000050_add_kalshi_market_type.up.sql` — Add `kalshi` to `market_type` enum.
- `migrations/000050_add_kalshi_market_type.down.sql` — Down migration guard documenting enum rollback limitation.
- `internal/data/kalshi/client.go` — Kalshi HTTP client, request signing, endpoint helpers.
- `internal/data/kalshi/client_test.go` — Auth/signing and URL behavior tests.
- `internal/data/kalshi/provider.go` — Data provider and native market-data loader.
- `internal/data/kalshi/provider_test.go` — Market/orderbook mapping tests.
- `internal/data/kalshi/register.go` — Register Kalshi provider with the shared data registry.
- `internal/execution/kalshi/snapshot.go` — Kalshi native snapshot and executable-side validation.
- `internal/execution/kalshi/snapshot_test.go` — YES/NO quote validation tests.
- `internal/execution/kalshi/executor.go` — Conservative deterministic paper executor.
- `internal/execution/kalshi/executor_test.go` — Paper buy/hold safety tests.
- `internal/execution/kalshi/broker.go` — Paper-compatible broker adapter skeleton; live submit returns explicit disabled error.
- `internal/execution/kalshi/broker_test.go` — Position/balance/status mapping and disabled-live tests.
- `internal/execution/prediction/types.go` — Provider-neutral prediction-market snapshot/decision contracts after Kalshi MVP is working.
- `internal/execution/prediction/policy.go` — Provider-neutral position key/sizing/exit policy interface after Kalshi MVP is working.

### Modified files

- `internal/domain/strategy.go` — Add `MarketTypeKalshi` and normalization support.
- `internal/domain/domain_test.go` — Add Kalshi normalization tests.
- `internal/domain/validation_test.go` — Add Kalshi market-type validation case.
- `internal/repository/postgres/schema_version.go` — Bump required schema to `50`.
- `internal/config/config.go` — Add Kalshi config fields and env parsing.
- `internal/config/validate.go` — Validate live Kalshi only when all required credentials are present.
- `internal/config/validate_test.go` — Kalshi config parsing/validation tests.
- `internal/data/factory.go` — Register Kalshi provider.
- `internal/data/selection_policy.go` — Add Kalshi chain/provider selection rules.
- `internal/data/selection_policy_test.go` — Kalshi routing tests.
- `cmd/tradingagent/runtime.go` — Register Kalshi provider and wire client dependencies.
- `cmd/tradingagent/prod_strategy_runner.go` — Add Kalshi paper native route, keep Polymarket route intact.
- `cmd/tradingagent/prod_strategy_runner_test.go` — Kalshi route avoids OHLCV and stays paper by default.
- `cmd/tradingagent/sizing_policy.go` — Add generic event-market sizing hook or Kalshi-specific cap if needed.
- `internal/execution/order_manager.go` — Extract provider-neutral prediction-market position identity only after Kalshi tests exist.
- `internal/execution/order_manager_test.go` — Protect Polymarket behavior while adding Kalshi position-key tests.
- `internal/scheduler/market_hours.go` — Add Kalshi market-hours policy.
- `internal/scheduler/market_hours_test.go` — Kalshi schedule behavior tests.
- `internal/scheduler/scheduler.go` — Ensure Kalshi routes through strategy executor and never legacy OHLCV.
- `internal/scheduler/scheduler_test.go` — Kalshi native route regression.
- `web/src/components/layout/app-shell.tsx` — Rename/group nav from Polymarket Ops to Event Markets where appropriate.
- `web/src/pages/polymarket-page.tsx` — Preserve existing Polymarket page links under Event Markets.
- `web/src/pages/kalshi-page.tsx` — Add initial Kalshi page if web route exists in the same app conventions.
- `web/src/App.tsx` — Add `/kalshi` route if React app is the active dashboard.
- `web/src/lib/api/types.ts` — Add `kalshi` market type to frontend unions.
- `docs/runbooks/polymarket-live-activation.md` — Keep Polymarket paper/data-only note.
- `docs/runbooks/kalshi-paper-data.md` — Add Kalshi setup and paper/data runbook.

---

## Task 1: Add Kalshi Market Type and Schema

**Files:**
- Modify: `internal/domain/strategy.go`
- Modify: `internal/domain/domain_test.go`
- Modify: `internal/domain/validation_test.go`
- Create: `migrations/000050_add_kalshi_market_type.up.sql`
- Create: `migrations/000050_add_kalshi_market_type.down.sql`
- Modify: `internal/repository/postgres/schema_version.go`

- [x] **Step 1: Write the domain tests**

Add or extend tests so Kalshi normalizes like the other market types:

```go
func TestMarketTypeNormalize_Kalshi(t *testing.T) {
	got := domain.MarketType(" Kalshi ").Normalize()
	if got != domain.MarketTypeKalshi {
		t.Fatalf("Normalize() = %q, want %q", got, domain.MarketTypeKalshi)
	}
}
```

- [x] **Step 2: Run the failing tests**

Run:

```bash
rtk go test ./internal/domain -run 'MarketType|Validation|Kalshi' -count=1
```

Expected: fail because `MarketTypeKalshi` is not defined.

- [x] **Step 3: Add the market type**

In `internal/domain/strategy.go`, add:

```go
const (
	MarketTypeStock      MarketType = "stock"
	MarketTypeCrypto     MarketType = "crypto"
	MarketTypeOption     MarketType = "option"
	MarketTypePolymarket MarketType = "polymarket"
	MarketTypeKalshi     MarketType = "kalshi"
)
```

Ensure `Normalize()` accepts casing/whitespace and returns `MarketTypeKalshi` for `kalshi`.

- [x] **Step 4: Add migration 50**

Create `migrations/000050_add_kalshi_market_type.up.sql`:

```sql
ALTER TYPE market_type ADD VALUE IF NOT EXISTS 'kalshi';
```

Create `migrations/000050_add_kalshi_market_type.down.sql`:

```sql
-- PostgreSQL enum values cannot be safely removed without rebuilding the enum.
-- Rollback is intentionally a no-op; strategies with market_type='kalshi'
-- must be removed or migrated before attempting a manual enum rebuild.
SELECT 1;
```

Update `internal/repository/postgres/schema_version.go`:

```go
const RequiredSchemaVersion = 50
```

- [x] **Step 5: Run tests**

Run:

```bash
rtk go test ./internal/domain ./internal/repository/postgres -count=1
```

Expected: pass.

---

## Task 2: Add Kalshi Config and Safe Defaults

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/validate.go`
- Modify: `internal/config/validate_test.go`
- Create: `docs/runbooks/kalshi-paper-data.md`

- [x] **Step 1: Add config tests**

Add tests covering parsing of:

```env
KALSHI_API_BASE_URL=https://external-api.demo.kalshi.co/trade-api/v2
KALSHI_API_KEY_ID=test-key-id
KALSHI_PRIVATE_KEY_PEM_B64=<base64-pem>
KALSHI_DEMO=true
```

Expected config shape:

```go
type KalshiConfig struct {
	APIBaseURL       string
	APIKeyID         string
	PrivateKeyPEMB64 string
	Demo             bool
}
```

- [x] **Step 2: Run failing config tests**

Run:

```bash
rtk go test ./internal/config -run 'Kalshi|Load|Validate' -count=1
```

Expected: fail because Kalshi config does not exist.

- [x] **Step 3: Implement config parsing**

Add to `internal/config/config.go`:

```go
type KalshiConfig struct {
	APIBaseURL       string
	APIKeyID         string
	PrivateKeyPEMB64 string
	Demo             bool
}
```

Add `Kalshi KalshiConfig` under `BrokerConfigs` or a dedicated provider config section. Parse env:

```go
Kalshi: KalshiConfig{
	APIBaseURL:       getEnvString("KALSHI_API_BASE_URL", "https://external-api.demo.kalshi.co/trade-api/v2"),
	APIKeyID:         os.Getenv("KALSHI_API_KEY_ID"),
	PrivateKeyPEMB64: os.Getenv("KALSHI_PRIVATE_KEY_PEM_B64"),
	Demo:             getEnvBoolDefault("KALSHI_DEMO", true),
}
```

Use the repo’s existing boolean helper style rather than adding a second inconsistent parser.

- [x] **Step 4: Validate live Kalshi only when fully configured**

In `internal/config/validate.go`, live trading should treat Kalshi as configured only when both key id and private key are present:

```go
hasKalshi := strings.TrimSpace(cfg.Brokers.Kalshi.APIKeyID) != "" &&
	strings.TrimSpace(cfg.Brokers.Kalshi.PrivateKeyPEMB64) != ""
```

Do not require Kalshi credentials for paper/data mode.

- [x] **Step 5: Add runbook**

Create `docs/runbooks/kalshi-paper-data.md` explaining:

```text
KALSHI_DEMO=true
KALSHI_API_BASE_URL=https://external-api.demo.kalshi.co/trade-api/v2
KALSHI_API_KEY_ID=<demo key id, only for authenticated demo reads/trading later>
KALSHI_PRIVATE_KEY_PEM_B64=<base64 encoded RSA private key PEM>
```

State clearly: first phase is paper/data only; live Kalshi order submission is not enabled by this plan.

- [x] **Step 6: Run tests**

Run:

```bash
rtk go test ./internal/config -count=1
```

Expected: pass.

---

## Task 3: Add Kalshi HTTP Client and RSA Signing

**Files:**
- Create: `internal/data/kalshi/client.go`
- Create: `internal/data/kalshi/client_test.go`

- [x] **Step 1: Write signing tests**

Test that authenticated requests include:

```text
KALSHI-ACCESS-KEY
KALSHI-ACCESS-TIMESTAMP
KALSHI-ACCESS-SIGNATURE
```

Test that signature input excludes query string:

```go
message := timestamp + "GET" + "/trade-api/v2/markets"
```

Do not include `?limit=10` in the signed path.

- [x] **Step 2: Run failing tests**

Run:

```bash
rtk go test ./internal/data/kalshi -run 'Client|Sign|Auth' -count=1
```

Expected: package missing or tests fail.

- [x] **Step 3: Implement client**

Create a client with this public shape:

```go
package kalshi

type Client struct {
	baseURL    string
	apiKeyID   string
	privateKey *rsa.PrivateKey
	httpClient *http.Client
	now        func() time.Time
	logger     *slog.Logger
}

func NewClient(baseURL, apiKeyID, privateKeyPEMB64 string, logger *slog.Logger) (*Client, error)
func (c *Client) Get(ctx context.Context, path string, query url.Values, authenticated bool) ([]byte, error)
func (c *Client) Post(ctx context.Context, path string, body any) ([]byte, error)
```

Implement RSA signing using the Kalshi documented message:

```go
message := timestampMilliseconds + strings.ToUpper(method) + pathOnly
```

Use RSA-PSS/SHA256 if Kalshi docs specify PSS in the current API examples; use the exact scheme from docs/tests, not a guessed alternative.

- [x] **Step 4: Run tests**

Run:

```bash
rtk go test ./internal/data/kalshi -count=1
```

Expected: pass.

---

## Task 4: Add Kalshi Data Provider and Snapshot

**Files:**
- Create: `internal/data/kalshi/provider.go`
- Create: `internal/data/kalshi/provider_test.go`
- Create: `internal/data/kalshi/register.go`
- Create: `internal/execution/kalshi/snapshot.go`
- Create: `internal/execution/kalshi/snapshot_test.go`
- Modify: `internal/data/factory.go`
- Modify: `internal/data/selection_policy.go`
- Modify: `internal/data/selection_policy_test.go`

- [x] **Step 1: Write provider mapping tests**

Use Kalshi market/orderbook fixtures:

```json
{
  "market": {
    "ticker": "KXTEST-YESNO",
    "title": "Will test happen?",
    "status": "active",
    "yes_bid": 45,
    "yes_ask": 47,
    "no_bid": 53,
    "no_ask": 55,
    "volume": 1000,
    "open_interest": 500,
    "close_time": "2026-12-31T23:59:59Z"
  }
}
```

Expected native snapshot:

```go
Snapshot{
	Ticker: "KXTEST-YESNO",
	BestBidYes: 0.45,
	BestAskYes: 0.47,
	BestBidNo: 0.53,
	BestAskNo: 0.55,
}
```

- [x] **Step 2: Implement Kalshi snapshot validation**

Create `internal/execution/kalshi/snapshot.go`:

```go
type Snapshot struct {
	Ticker       string
	Title        string
	Status       string
	BestBidYes   float64
	BestAskYes   float64
	BestBidNo    float64
	BestAskNo    float64
	Volume       float64
	OpenInterest float64
	CloseTime    time.Time
	FetchedAt    time.Time
}

func (s Snapshot) ValidateExecutableSide(side string, minLiquidity float64, now time.Time) error
func (s Snapshot) EntryPriceForSide(side string) (float64, bool)
func (s Snapshot) SpreadForSide(side string) (float64, bool)
```

Require explicit side-specific bid/ask. Do not derive NO from YES unless a later task adds a documented provider rule.

- [x] **Step 3: Register Kalshi data provider**

Add `internal/data/kalshi/register.go`:

```go
package kalshi

func Register(reg *data.Registry) {
	reg.Register(domain.MarketTypeKalshi, "kalshi", func(cfg data.ProviderConfig) (data.Provider, error) {
		return NewProvider(cfg), nil
	})
}
```

Follow the exact existing registry factory signature in `internal/data/polymarket/register.go`; adjust code to compile with that real signature.

- [x] **Step 4: Add selection policy tests**

Kalshi chain selection should route `MarketTypeKalshi` to Kalshi provider first and not Polymarket.

Run:

```bash
rtk go test ./internal/data -run 'Kalshi|Selection|Provider' -count=1
```

Expected: pass.

---

## Task 5: Add Kalshi Paper Native Execution Path

**Files:**
- Create: `internal/execution/kalshi/executor.go`
- Create: `internal/execution/kalshi/executor_test.go`
- Create: `internal/execution/kalshi/broker.go`
- Create: `internal/execution/kalshi/broker_test.go`
- Modify: `cmd/tradingagent/prod_strategy_runner.go`
- Modify: `cmd/tradingagent/prod_strategy_runner_test.go`
- Modify: `cmd/tradingagent/sizing_policy.go`

- [x] **Step 1: Add executor tests**

Test cases:

```go
func TestDeterministicExecutor_KalshiBuyYesWhenMetadataValid(t *testing.T)
func TestDeterministicExecutor_KalshiHoldWhenMarketClosed(t *testing.T)
func TestDeterministicExecutor_KalshiHoldWhenMissingNoBook(t *testing.T)
func TestDeterministicExecutor_KalshiHoldWhenUnknownTemplate(t *testing.T)
```

Use the same conservative behavior as Polymarket: template/metadata can only produce a paper decision when side, conviction, price ceiling, future close time, and side-specific book are valid.

- [x] **Step 2: Implement paper executor**

Create `internal/execution/kalshi/executor.go`:

```go
type NativeDecision struct {
	Signal     domain.PipelineSignal
	Action     string
	Side       string
	EntryPrice float64
	Rationale  string
}

type DeterministicNativeExecutor struct{}

func (DeterministicNativeExecutor) Execute(ctx context.Context, strategy domain.Strategy, snapshot Snapshot) (NativeDecision, error)
```

On any malformed config, return hold with rationale; do not return an error that fails the run unless the context is cancelled.

- [x] **Step 3: Add Kalshi broker skeleton**

Create `internal/execution/kalshi/broker.go` implementing `execution.Broker`. For first phase:

```go
func (b *Broker) SubmitOrder(ctx context.Context, order *domain.Order) (string, error) {
	return "", errors.New("kalshi live order submission is disabled; paper trading only")
}
```

Implement read methods for balance/positions only if Task 3 client coverage exists; otherwise keep explicit unsupported errors for live broker methods and rely on paper broker for paper strategies.

- [x] **Step 4: Route Kalshi through native path before OHLCV**

In `cmd/tradingagent/prod_strategy_runner.go`, make Kalshi behave like Polymarket: branch before legacy OHLCV prep.

```go
if strategy.MarketType.Normalize() == domain.MarketTypeKalshi {
	return r.runKalshiNative(ctx, strategy)
}
```

Keep `runPolymarketNative` unchanged for now.

- [x] **Step 5: Ensure paper default**

Kalshi should force paper unless all live gates are explicitly configured later. In phase 1, if `strategy.IsPaper == false`, return a clear error:

```go
return nil, errors.New("kalshi live execution is disabled; set strategy is_paper=true")
```

- [x] **Step 6: Run tests**

Run:

```bash
rtk go test ./internal/execution/kalshi ./cmd/tradingagent -run 'Kalshi|Native|OHLCV|Paper' -count=1
```

Expected: pass.

---

## Task 6: Scheduler and Market Hours

**Files:**
- Modify: `internal/scheduler/market_hours.go`
- Modify: `internal/scheduler/market_hours_test.go`
- Modify: `internal/scheduler/schedule_spec.go`
- Modify: `internal/scheduler/scheduler.go`
- Modify: `internal/scheduler/scheduler_test.go`

- [x] **Step 1: Add market-hours tests**

Kalshi event markets should not be blindly treated as stock market hours. First phase policy:

```go
MarketTypeKalshi is schedulable whenever the strategy cron fires, but native snapshot validation must hold closed/settled markets.
```

Add tests confirming scheduler does not route Kalshi into legacy stock OHLCV execution.

- [x] **Step 2: Implement scheduler routing**

In scheduler logic, ensure Kalshi behaves like a native prediction/event market:

```go
if current.MarketType.Normalize() == domain.MarketTypeKalshi {
	if s.strategyExecution != nil {
		return s.strategyExecution(ctx, current)
	}
	return nil
}
```

Keep the existing Polymarket guard intact.

- [x] **Step 3: Run tests**

Run:

```bash
rtk go test ./internal/scheduler -run 'Kalshi|Polymarket|MarketHours|RunStrategy' -count=1
```

Expected: pass.

---

## Task 7: Extract Shared Prediction-Market Interfaces

**Files:**
- Create: `internal/execution/prediction/types.go`
- Create: `internal/execution/prediction/policy.go`
- Modify: `internal/execution/order_manager.go`
- Modify: `internal/execution/order_manager_test.go`
- Modify: `internal/execution/polymarket/snapshot.go`
- Modify: `internal/execution/kalshi/snapshot.go`
- Modify: `cmd/tradingagent/prod_strategy_runner.go`

- [x] **Step 1: Add shared types after Kalshi path is green**

Create `internal/execution/prediction/types.go`:

```go
package prediction

type OutcomeSide string

const (
	OutcomeYes OutcomeSide = "YES"
	OutcomeNo  OutcomeSide = "NO"
)

type ExecutableSnapshot interface {
	Provider() string
	MarketKey() string
	ValidateExecutableSide(side string, minLiquidity float64, now time.Time) error
	EntryPriceForSide(side string) (float64, bool)
	SpreadForSide(side string) (float64, bool)
}
```

- [ ] **Step 2: Add shared policy interface**

Create `internal/execution/prediction/policy.go`:

```go
package prediction

type MarketPolicy interface {
	MarketType() domain.MarketType
	PositionTicker(marketKey string, side OutcomeSide) string
	NormalizeSide(side string) (OutcomeSide, error)
	IsPredictionMarket() bool
}
```

- [ ] **Step 3: Move side-qualified ticker rules behind policy**

Replace direct Polymarket-only ticker helpers in `internal/execution/order_manager.go` with provider-policy calls. Preserve existing `slug:YES` / `slug:NO` for Polymarket.

Kalshi should use:

```text
<kalshi-market-ticker>:YES
<kalshi-market-ticker>:NO
```

- [ ] **Step 4: Add regression tests**

Tests must prove:

- Polymarket position keys remain unchanged.
- Kalshi YES/NO positions do not collide.
- Stock/crypto positions are not treated as prediction-market positions.

Run:

```bash
rtk go test ./internal/execution -run 'Polymarket|Kalshi|PositionTicker|Prediction' -count=1
```

Expected: pass.

---

## Task 8: Dashboard Grouping for Event Markets

**Files:**
- Modify: `web/src/components/layout/app-shell.tsx`
- Modify: `web/src/components/layout/app-shell.test.tsx`
- Modify: `web/src/App.tsx`
- Create: `web/src/pages/kalshi-page.tsx`
- Create: `web/src/pages/kalshi-page.test.tsx`
- Modify: `web/src/pages/polymarket-page.tsx`
- Modify: `web/src/lib/api/types.ts`

- [x] **Step 1: Add navigation tests**

Test that the shell groups:

```text
Event Markets
  Polymarket
  Kalshi
  Surfers Ops
```

- [x] **Step 2: Add Kalshi placeholder hub**

Create a simple `/kalshi` page with sections:

```text
Overview
Markets
Paper Strategies
Operations
Setup
```

Each section should make current phase clear: paper/data first, live disabled.

- [x] **Step 3: Link Polymarket and Kalshi together**

Add cross-links:

- Polymarket hub → Kalshi hub
- Kalshi hub → Polymarket hub
- Both → Surfers Ops / operations where relevant

- [x] **Step 4: Run frontend tests**

Run:

```bash
cd web && npm test -- --run src/components/layout/app-shell.test.tsx src/pages/polymarket-page.test.tsx src/pages/kalshi-page.test.tsx
```

Expected: pass.

---

## Task 9: Validation, Review, and Deployment Readiness

**Files:**
- Modify: `docs/superpowers/plans/2026-06-15-kalshi-first-event-markets.md`
- Modify: `docs/runbooks/kalshi-paper-data.md`

- [ ] **Step 1: Run backend test suite**

Run:

```bash
rtk go test ./internal/domain ./internal/config ./internal/data ./internal/data/kalshi ./internal/execution ./internal/execution/polymarket ./internal/execution/kalshi ./cmd/tradingagent ./internal/scheduler ./internal/repository/postgres -count=1
```

Expected: pass.

- [ ] **Step 2: Run frontend test suite**

Run:

```bash
cd web && npm test -- --run src/components/layout/app-shell.test.tsx src/pages/polymarket-page.test.tsx src/pages/kalshi-page.test.tsx
```

Expected: pass.

- [ ] **Step 3: Review gate**

Ask reviewer to check:

- Polymarket behavior unchanged.
- Kalshi paper path cannot live trade.
- Kalshi never enters OHLCV stock pipeline.
- Shared interfaces do not force Polymarket token assumptions onto Kalshi.
- Future ForecastEx/IBKR support is not blocked by names or interfaces.

- [ ] **Step 4: Deployment checklist**

Before deploy:

```bash
rtk git diff --check
rtk git status --short
```

After deploy:

```bash
docker compose --project-name augr-prod -f docker-compose.prod.yml ps
curl -sf http://127.0.0.1:8080/healthz
```

Expected: app, postgres, redis healthy; schema version `50`; Polymarket existing automation remains healthy.

---

## Future Provider Guardrails

To keep ForecastEx, IBKR, CME event contracts, or Robinhood-style event contracts possible:

- Use `MarketTypeKalshi` only for provider-specific code.
- Use `prediction` package names only for provider-neutral abstractions.
- Do not put Kalshi RSA auth or Polymarket L2 auth into shared interfaces.
- Use `MarketKey` for provider-native market identifier; do not call it `slug` outside Polymarket.
- Use `OutcomeSide` for YES/NO; do not encode provider order intent into the shared side type.
- Keep discovery provider-specific until two non-Polymarket discovery implementations exist.
- Keep live trading disabled by default for every event-market provider.

---

## Complexity Assessment

- **Logic depth:** High — native execution, snapshots, side-aware decisions, and provider-specific auth.
- **Contract sensitivity:** High — DB enum migration, config/env contracts, broker/data API contracts.
- **Context span:** High — multiple sequential layers across backend, scheduler, dashboard, docs.
- **Discovery need:** Medium — Kalshi API docs are known enough for the first pass; live order details remain later.
- **Failure cost:** High — live trading safety, scheduler errors, data correctness.
- **Concern coupling:** High — domain, config, data, execution, scheduler, dashboard, deployment.

Route: full written plan + review gate.

---

## Recommended Execution Order

1. Task 1 — market type/schema.
2. Task 2 — config and runbook.
3. Task 3 — Kalshi client/auth.
4. Task 4 — Kalshi read provider/snapshot.
5. Task 5 — Kalshi paper native execution.
6. Task 6 — scheduler routing.
7. Task 7 — shared prediction interfaces.
8. Task 8 — dashboard grouping.
9. Task 9 — full validation/review.

This order keeps Polymarket stable, gets Kalshi visible early, and delays abstraction until the second provider proves what should be shared.
