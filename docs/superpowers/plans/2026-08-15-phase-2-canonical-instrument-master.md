# Phase 2 Canonical Instrument Master Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Complete OVR-201 by introducing stable canonical instrument identity, append-only dated alias history, venue contracts, corporate-action facts, and explicit quarantine for ambiguous legacy symbols without cutting over existing ticker-based reads.

**Architecture:** Add an `internal/instrument` bounded context and migration 66 beside the ledger rather than extending legacy ticker structs. Canonical, fully specified instruments may be active; incomplete legacy identities are deterministic quarantined placeholders whose aliases preserve provenance but cannot authorize trading. Alias assignments and retirements are immutable effective-time events, so resolving an alias at a historical timestamp never changes when a later symbol reuse or corporate action is recorded.

**Tech Stack:** Go 1.25.8+, `shopspring/decimal`, UUIDs, PostgreSQL 17/TimescaleDB, pgx v5, golang-migrate, Task, TDD with isolated schemas.

---

## Scope and sequencing

Phase 2 contains seven dependent systems and is intentionally split into one executable plan per boundary. This plan covers OVR-201 only. OVR-202 through OVR-207 remain ordered by the accepted overhaul plan and receive their own plans after their dependencies are locally qualified:

1. OVR-201 canonical instrument and alias master — this plan.
2. OVR-202 timestamped quote/depth snapshots.
3. OVR-203 common intent/order/fill lifecycle.
4. OVR-204 common simulation venue.
5. OVR-205 Alpaca and Kalshi lifecycle adapters.
6. OVR-206 capital-tier and margin profiles.
7. OVR-207 reconciliation service, after OVR-104 also completes.

Out of scope for OVR-201:

- changing legacy strategy, order, trade, position, or market-data read paths;
- declaring a quarantined legacy ticker tradable;
- guessing currency, tick size, lot size, multiplier, venue, settlement, or corporate-action history;
- adding quote/depth storage or execution lifecycle tables;
- deploying, migrating, or backfilling a shared or production database.

## File map

- Create `internal/instrument/instrument.go`: canonical instrument aggregate, exact mechanics, quarantine rules, and validation.
- Create `internal/instrument/instrument_test.go`: public-constructor and property tests.
- Create `internal/instrument/reference.go`: alias events, venue contracts, corporate actions, and normalization.
- Create `internal/instrument/reference_test.go`: historical alias and corporate-action validation tests.
- Modify `internal/repository/interfaces.go`: add the instrument repository interface.
- Create `internal/repository/postgres/instrument.go`: atomic PostgreSQL persistence and dated alias resolution.
- Create `internal/repository/postgres/instrument_test.go`: isolated real-database repository tracer.
- Create `migrations/000066_canonical_instruments.up.sql`: immutable reference tables and provenance-preserving legacy backfill.
- Create `migrations/000066_canonical_instruments.down.sql`: narrow rollback that preserves migrations 1–65 and all legacy tables.
- Create `migrations/000066_canonical_instruments_test.go`: schema, backfill, temporal identity, immutability, and rollback tests.
- Modify `internal/repository/postgres/schema_version.go`: require schema 66.
- Modify `docs/superpowers/plans/2026-08-14-total-overhaul-plan.md`: add the OVR-201 local implementation record and retain OVR-202 as next.
- Modify `docs/development-setup.md`: list schema 66 reference tables and the quarantine inspection query.

### Task 1: Canonical instrument aggregate

**Files:**
- Create: `internal/instrument/instrument_test.go`
- Create: `internal/instrument/instrument.go`

- [x] **Step 1: Write the active-instrument tracer test**

```go
func TestNewInstrumentBuildsVerifiedEquity(t *testing.T) {
	got, err := NewInstrument(InstrumentInput{
		IdentityKey:     "figi:BBG000B9XRY4",
		AssetClass:      AssetClassEquity,
		PrimaryVenue:    "nasdaq",
		Currency:        "usd",
		TickSize:        decimal.RequireFromString("0.01"),
		LotSize:         decimal.NewFromInt(1),
		Multiplier:      decimal.NewFromInt(1),
		SettlementMethod: SettlementPhysical,
		Status:           StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Currency != "USD" || got.PrimaryVenue != "nasdaq" || got.ID == uuid.Nil {
		t.Fatalf("unexpected instrument: %+v", got)
	}
}
```

- [x] **Step 2: Run the tracer and verify RED**

Run:

```bash
go test -count=1 ./internal/instrument
```

Expected: FAIL because the package or `NewInstrument` does not exist.

- [x] **Step 3: Implement the public aggregate and enums**

The public surface is:

```go
type AssetClass string

const (
	AssetClassUnknown            AssetClass = "unknown"
	AssetClassEquity             AssetClass = "equity"
	AssetClassETF                AssetClass = "etf"
	AssetClassOption             AssetClass = "option"
	AssetClassCryptoSpot         AssetClass = "crypto_spot"
	AssetClassPredictionContract AssetClass = "prediction_contract"
	AssetClassFuture             AssetClass = "future"
)

type Status string

const (
	StatusActive      Status = "active"
	StatusInactive    Status = "inactive"
	StatusExpired     Status = "expired"
	StatusQuarantined Status = "quarantined"
)

type SettlementMethod string

const (
	SettlementCash     SettlementMethod = "cash"
	SettlementPhysical SettlementMethod = "physical"
	SettlementCrypto   SettlementMethod = "crypto"
	SettlementBinary   SettlementMethod = "binary"
)

type Instrument struct {
	ID               uuid.UUID
	IdentityKey      string
	AssetClass       AssetClass
	PrimaryVenue     string
	Currency         string
	TickSize         decimal.Decimal
	LotSize          decimal.Decimal
	Multiplier       decimal.Decimal
	Expiration       *time.Time
	ExerciseStyle    ExerciseStyle
	SettlementMethod SettlementMethod
	UnderlyingID     *uuid.UUID
	Status           Status
	Metadata         json.RawMessage
	CreatedAt        time.Time
}
```

`NewInstrument` normalizes strings and timestamps to PostgreSQL microseconds. `Validate` requires complete positive mechanics, currency, settlement, and a non-unknown asset class for every non-quarantined record. A quarantined record requires stable identity and provenance but permits unknown mechanics; supplied values must still be valid. Active options additionally require expiration, exercise style, and an underlying instrument.

- [x] **Step 4: Run the tracer and verify GREEN**

```bash
go test -count=1 ./internal/instrument
```

Expected: PASS.

- [x] **Step 5: Add one property at a time**

Add and individually drive RED → GREEN for:

```go
func TestInstrumentPropertyRejectsInvalidMechanics(t *testing.T)
func TestInstrumentPropertyRejectsWrongCurrency(t *testing.T)
func TestInstrumentRejectsNumericMagnitudeBeyondSchema(t *testing.T)
func TestActiveOptionRequiresTermsAndUnderlying(t *testing.T)
func TestQuarantinedInstrumentAllowsUnknownTermsButNotInventedValidity(t *testing.T)
func TestInstrumentValidateRejectsUnnormalizedManualFields(t *testing.T)
```

Run after each vertical slice:

```bash
go test -count=1 ./internal/instrument
```

Expected: the new test fails before its rule exists and the package returns to PASS after the minimal rule is added.

### Task 2: Effective-time reference facts

**Files:**
- Create: `internal/instrument/reference_test.go`
- Create: `internal/instrument/reference.go`

- [x] **Step 1: Write the alias-event tracer**

```go
func TestNewAliasEventNormalizesProviderTickerAndTime(t *testing.T) {
	effectiveAt := time.Date(2026, 8, 15, 12, 0, 0, 123456789, time.UTC)
	event, err := NewAliasEvent(AliasEventInput{
		InstrumentID: uuid.New(),
		Provider:     " Legacy_Augr_Stock ",
		AliasType:   AliasTicker,
		AliasValue:  " aapl ",
		Action:      AliasAssigned,
		EffectiveAt: effectiveAt,
		Source:      "migration-000066",
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.Provider != "legacy_augr_stock" || event.AliasValue != "AAPL" ||
		!event.EffectiveAt.Equal(effectiveAt.Truncate(time.Microsecond)) {
		t.Fatalf("unexpected alias event: %+v", event)
	}
}
```

- [x] **Step 2: Run and verify RED**

```bash
go test -count=1 ./internal/instrument
```

Expected: FAIL because alias events are undefined.

- [x] **Step 3: Implement append-only reference types**

```go
type AliasType string
const (
	AliasTicker        AliasType = "ticker"
	AliasOCC           AliasType = "occ"
	AliasCUSIP         AliasType = "cusip"
	AliasFIGI          AliasType = "figi"
	AliasVenueContract AliasType = "venue_contract"
	AliasSlug          AliasType = "slug"
	AliasProviderID    AliasType = "provider_id"
)

type AliasAction string
const (
	AliasAssigned AliasAction = "assigned"
	AliasRetired  AliasAction = "retired"
)

type AliasEvent struct {
	ID           uuid.UUID
	InstrumentID uuid.UUID
	Provider     string
	AliasType    AliasType
	AliasValue   string
	Action       AliasAction
	EffectiveAt  time.Time
	Source       string
	Metadata     json.RawMessage
	CreatedAt    time.Time
}

type VenueContract struct {
	ID, InstrumentID uuid.UUID
	Venue, ContractID, Currency string
	TickSize, LotSize, Multiplier decimal.Decimal
	SettlementMethod SettlementMethod
	ValidFrom time.Time
	ValidTo *time.Time
	Metadata json.RawMessage
	CreatedAt time.Time
}

type CorporateAction struct {
	ID, InstrumentID uuid.UUID
	SuccessorInstrumentID *uuid.UUID
	ActionType CorporateActionType
	EffectiveAt time.Time
	RatioNumerator, RatioDenominator decimal.Decimal
	CashAmount *decimal.Decimal
	CashCurrency, Source, SourceEventID string
	Metadata json.RawMessage
	CreatedAt time.Time
}
```

Ticker, OCC, CUSIP, FIGI, and venue-contract values normalize to uppercase; slugs and opaque provider IDs preserve case after trimming. Corporate-action validation requires a successor for mergers, spinoffs, and futures rolls; a positive ratio for splits; and amount/currency for cash dividends.

- [x] **Step 4: Add temporal-identity validation tests one by one**

```go
func TestAliasRetirementRetainsInstrumentIdentity(t *testing.T)
func TestVenueContractRejectsInvalidWindow(t *testing.T)
func TestSplitRequiresPositiveRatio(t *testing.T)
func TestMergerRequiresDifferentSuccessor(t *testing.T)
func TestCashDividendRequiresAmountAndCurrency(t *testing.T)
```

Run after each slice:

```bash
go test -count=1 ./internal/instrument
```

Expected: PASS after each RED → GREEN cycle.

### Task 3: Migration 66 and legacy quarantine

**Files:**
- Create: `migrations/000066_canonical_instruments_test.go`
- Create: `migrations/000066_canonical_instruments.up.sql`
- Create: `migrations/000066_canonical_instruments.down.sql`
- Modify: `internal/repository/postgres/schema_version.go`

- [x] **Step 1: Write the static schema contract test**

The test must require these exact durable objects and invariants:

```go
for _, fragment := range []string{
	"create table instruments",
	"identity_key text not null unique",
	"status text not null check",
	"create table instrument_alias_events",
	"action text not null check (action in ('assigned', 'retired'))",
	"unique (provider, alias_type, alias_value, effective_at)",
	"create table venue_contracts",
	"create table corporate_actions",
	"create table instrument_identity_quarantine",
	"create trigger trg_instruments_immutable",
	"create trigger trg_instrument_alias_events_immutable",
	"legacy_augr_stock",
	"migration_000066_incomplete_reference_terms",
} {
	if !strings.Contains(upSQL, fragment) {
		t.Fatalf("expected migration 66 to contain %q", fragment)
	}
}
```

- [x] **Step 2: Run the contract and verify RED**

```bash
DB_URL="$AUGR_PHASE1_DB_URL" go test -count=1 -run '^TestCanonicalInstrument' ./migrations
```

Expected: FAIL because migration 66 does not exist.

- [x] **Step 3: Implement the immutable reference schema**

The schema uses nullable mechanics only for `status='quarantined'`; its check constraint requires complete terms otherwise. Alias history is event-based and append-only:

```sql
CREATE TABLE instruments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_key TEXT NOT NULL UNIQUE,
    asset_class TEXT NOT NULL,
    primary_venue TEXT NOT NULL,
    currency TEXT,
    tick_size NUMERIC(38,12),
    lot_size NUMERIC(38,12),
    multiplier NUMERIC(38,12),
    expiration TIMESTAMPTZ,
    exercise_style TEXT,
    settlement_method TEXT,
    underlying_instrument_id UUID REFERENCES instruments(id) ON DELETE RESTRICT,
    status TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (status = 'quarantined' OR (
        asset_class <> 'unknown' AND currency ~ '^[A-Z]{3}$' AND
        tick_size > 0 AND lot_size > 0 AND multiplier > 0 AND
        settlement_method IS NOT NULL
    ))
);

CREATE TABLE instrument_alias_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instrument_id UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT,
    provider TEXT NOT NULL,
    alias_type TEXT NOT NULL,
    alias_value TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('assigned', 'retired')),
    effective_at TIMESTAMPTZ NOT NULL,
    source TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider, alias_type, alias_value, effective_at)
);
```

`venue_contracts`, `corporate_actions`, and `instrument_identity_quarantine` use the same JSON-object, normalized-text, exact-decimal, source-identity, and append-only constraints.

- [x] **Step 4: Implement deterministic quarantine backfill**

Use one normalized CTE over existing symbol-bearing sources:

```sql
WITH legacy_symbols(raw_symbol, market_type, observed_at, source_table) AS (
    SELECT ticker, market_type::TEXT, created_at, 'strategies' FROM strategies
    UNION ALL SELECT ticker, market_type::TEXT, created_at, 'orders' FROM orders
    UNION ALL SELECT instrument_key, market_type::TEXT, created_at, 'trade_decisions' FROM trade_decisions
    UNION ALL SELECT ticker, 'stock', created_at, 'universe_tickers' FROM universe_tickers
    UNION ALL SELECT occ_symbol, 'option_contract', fetched_at, 'option_contracts' FROM option_contracts
    UNION ALL SELECT ticker, 'kalshi', added_at, 'kalshi_watched_markets' FROM kalshi_watched_markets
    UNION ALL SELECT slug, 'polymarket', added_at, 'polymarket_watched_markets' FROM polymarket_watched_markets
    UNION ALL SELECT ticker, 'stock', valid_from, 'copy_instrument_mappings' FROM copy_instrument_mappings
), normalized AS (
    SELECT market_type,
           CASE WHEN market_type = 'polymarket' THEN btrim(raw_symbol) ELSE upper(btrim(raw_symbol)) END AS symbol,
           MIN(observed_at) AS first_seen_at,
           jsonb_agg(DISTINCT source_table) AS sources
    FROM legacy_symbols
    WHERE btrim(raw_symbol) <> ''
    GROUP BY market_type, CASE WHEN market_type = 'polymarket' THEN btrim(raw_symbol) ELSE upper(btrim(raw_symbol)) END
)
INSERT INTO instruments (id, identity_key, asset_class, primary_venue, status, metadata, created_at)
SELECT md5('legacy-instrument:' || market_type || ':' || symbol)::UUID,
       'legacy:' || market_type || ':' || symbol,
       CASE market_type
           WHEN 'stock' THEN 'equity'
           WHEN 'crypto' THEN 'crypto_spot'
           WHEN 'kalshi' THEN 'prediction_contract'
           WHEN 'polymarket' THEN 'prediction_contract'
           WHEN 'option_contract' THEN 'option'
           ELSE 'unknown'
       END,
       CASE market_type WHEN 'kalshi' THEN 'kalshi' WHEN 'polymarket' THEN 'polymarket' ELSE 'legacy_unknown' END,
       'quarantined',
       jsonb_build_object('backfill', 'migration_000066', 'sources', sources),
       first_seen_at
FROM normalized;
```

Insert one `assigned` alias event and one quarantine finding for every normalized placeholder. Convert only `manual_verified` and `provider_verified` copy mappings into additional CUSIP/FIGI/provider-ID alias events; preserve `ambiguous` and `stale` mappings as quarantine evidence only. Do not backfill active instruments or default market mechanics.

- [x] **Step 5: Add integration tests through vertical slices**

Drive these tests individually:

```go
func TestCanonicalInstrumentMigrationQuarantinesLegacySymbols(t *testing.T)
func TestCanonicalInstrumentMigrationPreservesVerifiedCUSIPAlias(t *testing.T)
func TestCanonicalInstrumentMigrationRejectsMutation(t *testing.T)
func TestCanonicalInstrumentMigrationRejectsIncompleteActiveInstrument(t *testing.T)
func TestCanonicalInstrumentDownPreservesLegacyTables(t *testing.T)
```

Run after each slice:

```bash
DB_URL="$AUGR_PHASE1_DB_URL" go test -count=1 -run '^TestCanonicalInstrument' ./migrations
```

Expected: PASS.

- [x] **Step 6: Bump and verify schema version**

```go
const RequiredSchemaVersion = 66
```

Run:

```bash
go test -count=1 -run '^TestSchemaVersionSync$' ./internal/repository/postgres
```

Expected: PASS with migration 66 as the latest up migration.

### Task 4: PostgreSQL repository and historical resolution

**Files:**
- Modify: `internal/repository/interfaces.go`
- Create: `internal/repository/postgres/instrument_test.go`
- Create: `internal/repository/postgres/instrument.go`

- [x] **Step 1: Define the narrow public repository contract**

```go
type InstrumentRepository interface {
	CreateInstrument(context.Context, *instrument.Instrument) (*instrument.Instrument, error)
	GetInstrumentByID(context.Context, uuid.UUID) (*instrument.Instrument, error)
	AppendAliasEvent(context.Context, *instrument.AliasEvent) (*instrument.AliasEvent, error)
	ResolveAlias(context.Context, string, instrument.AliasType, string, time.Time) (*instrument.Instrument, error)
	RegisterVenueContract(context.Context, *instrument.VenueContract) (*instrument.VenueContract, error)
	RecordCorporateAction(context.Context, *instrument.CorporateAction) (*instrument.CorporateAction, error)
}
```

- [x] **Step 2: Write the historical-resolution tracer**

The test creates two stable instrument IDs and appends these events for the same provider/type/value:

```go
[]instrument.AliasEventInput{
	{InstrumentID: first.ID, Provider: "test", AliasType: instrument.AliasTicker, AliasValue: "XYZ", Action: instrument.AliasAssigned, EffectiveAt: t0, Source: "test"},
	{InstrumentID: first.ID, Provider: "test", AliasType: instrument.AliasTicker, AliasValue: "XYZ", Action: instrument.AliasRetired, EffectiveAt: t1, Source: "test"},
	{InstrumentID: second.ID, Provider: "test", AliasType: instrument.AliasTicker, AliasValue: "XYZ", Action: instrument.AliasAssigned, EffectiveAt: t2, Source: "test"},
}
```

Assertions:

```go
atT0, _ := repo.ResolveAlias(ctx, "test", instrument.AliasTicker, "xyz", t0.Add(time.Hour))
between, err := repo.ResolveAlias(ctx, "test", instrument.AliasTicker, "XYZ", t1.Add(time.Hour))
atT2, _ := repo.ResolveAlias(ctx, "test", instrument.AliasTicker, "XYZ", t2.Add(time.Hour))
if atT0.ID != first.ID || !errors.Is(err, repository.ErrNotFound) || atT2.ID != second.ID {
	t.Fatalf("historical identity changed: %s / %v / %s", atT0.ID, err, atT2.ID)
}
```

- [x] **Step 3: Run and verify RED**

```bash
DB_URL="$AUGR_PHASE1_DB_URL" go test -count=1 -run '^TestInstrumentRepo' ./internal/repository/postgres
```

Expected: FAIL because `InstrumentRepo` does not exist.

- [x] **Step 4: Implement repository persistence and resolution**

`ResolveAlias` must normalize provider/value through the domain API and select the latest event at or before `asOf`:

```sql
SELECT instrument_id, action
FROM instrument_alias_events
WHERE provider = $1
  AND alias_type = $2
  AND alias_value = $3
  AND effective_at <= $4
ORDER BY effective_at DESC, created_at DESC, id DESC
LIMIT 1;
```

Return `repository.ErrNotFound` when no event exists or the latest action is `retired`. Every write validates the public domain value before SQL. Unique source/identity conflicts return `repository.ErrIdempotencyConflict`; identical retries return the original row.

- [x] **Step 5: Add repository behavior slices**

```go
func TestInstrumentRepoCreatesAndLoadsExactMechanics(t *testing.T)
func TestInstrumentRepoResolvesAliasByHistoricalTime(t *testing.T)
func TestInstrumentRepoRejectsAliasRebindWithoutRetirement(t *testing.T)
func TestInstrumentRepoReplaysIdenticalCorporateAction(t *testing.T)
func TestInstrumentRepoRejectsCorporateActionPayloadConflict(t *testing.T)
```

Run after each slice:

```bash
DB_URL="$AUGR_PHASE1_DB_URL" go test -count=1 -run '^TestInstrumentRepo' ./internal/repository/postgres
```

Expected: PASS.

### Task 5: Reversible qualification and handoff

**Files:**
- Modify: `docs/superpowers/plans/2026-08-14-total-overhaul-plan.md`
- Modify: `docs/development-setup.md`

- [x] **Step 1: Run focused qualification**

```bash
go test -count=1 ./internal/instrument
DB_URL="$AUGR_PHASE1_DB_URL" go test -race -count=1 -run '^TestInstrumentRepo' ./internal/repository/postgres
DB_URL="$AUGR_PHASE1_DB_URL" go test -race -count=1 -run '^TestCanonicalInstrument' ./migrations
go vet ./internal/instrument ./internal/repository/postgres ./migrations
golangci-lint run ./internal/instrument ./internal/repository/postgres ./migrations
```

Expected: PASS with zero race or lint findings.

- [x] **Step 2: Rehearse migration 66 down and reapply**

```bash
migrate -path migrations -database "$AUGR_PHASE1_DB_URL" up 1
migrate -path migrations -database "$AUGR_PHASE1_DB_URL" down 1
migrate -path migrations -database "$AUGR_PHASE1_DB_URL" up 1
```

Verify after rollback that schema 65 accounts, capital flows, and ledger rows remain. Verify after reapply that schema is `66|false`, deterministic legacy placeholder IDs are restored, all backfilled records remain quarantined, and historical alias resolution has no duplicate effective-time event.

- [x] **Step 3: Run broad non-release checks**

```bash
task test:race
task build
task vet
task lint
mise exec -- npm --prefix web test -- --run --pool=threads --maxWorkers=1
mise exec -- npm --prefix web run lint
mise exec -- npm --prefix web run build
git diff --check
```

Expected: PASS. Run `task fmt:check`, the database-enabled all-package suite, and `task vulncheck` as evidence gates; keep inherited failures explicitly separate from OVR-201.

- [x] **Step 4: Smoke the compiled runtime**

Start the binary against the isolated schema-66 database with live trading disabled and `TRADING_AGENT_KILL=true`. Require `/health`, `/healthz`, and `/api/v1/health` to report database and Redis `ok`, then stop it cleanly.

- [x] **Step 5: Update the implementation record**

Record:

- OVR-201 locally complete, not deployed, and no ticker read path cut over;
- active records require complete verified mechanics;
- every legacy symbol is either a deterministic quarantined placeholder with dated provenance aliases or an explicit quarantine finding;
- corporate actions and alias changes append facts rather than mutate historical identity;
- OVR-202 is next.

- [x] **Step 6: Commit and sync the isolated slice**

```bash
git add docs/superpowers/plans/2026-08-15-phase-2-canonical-instrument-master.md \
  docs/superpowers/plans/2026-08-14-total-overhaul-plan.md \
  docs/development-setup.md internal/instrument internal/repository/interfaces.go \
  internal/repository/postgres/instrument.go internal/repository/postgres/instrument_test.go \
  internal/repository/postgres/schema_version.go migrations/000066_canonical_instruments.up.sql \
  migrations/000066_canonical_instruments.down.sql migrations/000066_canonical_instruments_test.go
git diff --cached --check
git commit -m "feat: add canonical instrument master"
git fetch origin codex/augr-overhaul
git push origin codex/augr-overhaul
```

Expected: the Phase 2 commit is separate from `b32d392`, the upstream has no unreviewed divergence, and the push updates `origin/codex/augr-overhaul` without force.

## Self-review

- Spec coverage: stable identity, mechanics, provider identifiers, dated aliases, venue contracts, corporate actions, quarantine, rollback, and no legacy cutover each map to a task above.
- Completeness scan: each implementation step specifies public types, schema shape, behavior, or an exact query.
- Type consistency: repository signatures use the `internal/instrument` types defined in Tasks 1–2; migration and domain decimal scales are both `NUMERIC(38,12)`; alias actions and resolution vocabulary are identical across domain, SQL, and tests.
- Safety boundary: local schema 66 is reversible; backfill creates no active legacy instrument and changes no legacy row.
