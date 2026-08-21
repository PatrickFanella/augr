# Phase 2 Timestamped Quote and Depth Snapshot Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Keep the public
> contract test-first, review every completed task before continuing, and retain
> the local-only deployment boundary. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Complete OVR-202 by introducing one canonical, append-only, exact-decimal quote and depth observation contract whose point-in-time execution assessment fails closed when required source provenance, active instrument eligibility, dated venue mechanics, availability, exchange age, bid, ask, market/session eligibility, or depth is missing, stale, off-tick, off-lot, or otherwise non-executable.

**Architecture:** Add a root `internal/marketdata` bounded context beside the existing provider-specific `internal/marketdata/polymarket` feed. A durable snapshot records provider, venue, a required observation namespace, observation ID and optional source revision/sequence, optional human feed/source, exchange, ingress-receive, and decision-availability times, independently present top-of-book fields, and ordered depth levels. It may bind directly to the immutable dated venue contract introduced by OVR-201. Observation validity and executable sufficiency are deliberately separate: incomplete or mechanically invalid but unambiguously attributable observations may be retained as evidence. `Assess` returns stable rejection codes for missing or stale observation facts; `AssessForExecution` additionally requires an active, unexpired matching canonical instrument and validates executable top-of-book/depth values against the exact tick/lot mechanics of the bound, effective venue contract. Migration 67 creates immutable normalized tables keyed to the OVR-201 instrument master; no legacy float cache or provider-specific snapshot is promoted without an explicit adapter and provenance-preserving identity.

**Tech Stack:** Go 1.25.8+, `shopspring/decimal`, UUIDs, PostgreSQL 17/TimescaleDB, pgx v5, golang-migrate, Task, TDD with isolated schemas.

---

## Scope and sequencing

This plan covers OVR-202 only. OVR-203 will consume the durable snapshot ID from this boundary when it builds the common intent/order/fill lifecycle. It does not need to reinterpret provider-specific quote structs or duplicate freshness arithmetic.

In scope:

- canonical provider, venue, observation namespace/ID/revision, feed/source, and optional sequence evidence;
- an immutable reference to the exact dated venue mechanics when known;
- independent presence for bid, ask, sizes, last, mark, exchange time, and availability time;
- availability-time point-in-time selection that prevents lookahead while preserving ingress receive time;
- exact decimal prices and sizes with database checks equivalent to `NUMERIC(38,12)` but without PostgreSQL typmod rounding;
- ordered bid/ask depth with database-enforced count, order, and top-level consistency;
- structured executable-assessment rejection codes, including active-instrument, venue-contract identity/window, exact tick/lot, and market/session eligibility;
- atomic persistence, identical retry replay, and mismatched-key conflicts;
- append-only database enforcement and reversible migration rehearsal.

Out of scope:

- adapting Alpaca, Kalshi, Polymarket, options, or other provider clients to write the new contract;
- changing existing market-data cache, recorder, strategy, order, fill, or UI read paths;
- guessing canonical instrument IDs for legacy ticker/slug rows;
- converting legacy `DOUBLE PRECISION` or JSON depth into exact canonical values;
- building the common execution lifecycle, simulator, reconciliation service, or dataset quality service;
- deploying or migrating a shared or production database.

## Contract decisions

1. `Provider`, `Venue`, `ObservationNamespace`, `ObservationID`, `ReceivedAt`, and canonical `InstrumentID` are required for every retained canonical observation. The namespace is the endpoint, subscription, connection/replay partition, or dataset-manifest scope in which the provider ID is unique for that instrument. A retry identity is `(instrument_id, provider, venue, observation_namespace, observation_id, source_revision)`, where an empty revision is a real normalized value rather than SQL `NULL` uniqueness behavior. Including the instrument protects multi-instrument feeds whose message IDs repeat per constituent.
2. `Source` is an optional human/provider feed or dataset label. `SourceSequence` is optional ordered source evidence. `SourceRevision` is optional but becomes part of identity: a provider-declared correction with a new revision is a new immutable observation, while a changed payload under the same full identity is an idempotency conflict.
3. `ExchangeAt` is optional because some sources do not supply provider-event time. `ReceivedAt` is mandatory and means the acquisition/ingress time recorded by Augr or a provenance-backed historical acquisition record; adapters must never copy an untrusted provider event time into it. `AvailableAt` is optional and means the earliest time the validated observation could be used by the decision engine. When present it must be at or after receive time. Unknown historical availability remains nil and non-selectable rather than being invented from import or creation time.
4. `VenueContractID` is optional for retained observations but references the exact immutable OVR-201 venue contract when known. The database validates matching instrument, venue, and effective window at `ExchangeAt`, falling back to `ReceivedAt` only when provider-event time is absent. Persistence intentionally retains attributable off-tick/off-lot provider evidence. `AssessForExecution` requires the binding, an active and unexpired matching canonical instrument, and exact tick multiples for bid, ask, and every depth price plus exact lot multiples for any displayed top size and every depth size. `Last` is historical trade evidence whose own event time may precede the current tick regime, and `Mark` may be theoretical; neither is treated as an executable price by this check. OVR-203 must use the joined execution assessment before consuming the contract's multiplier, currency, and settlement mechanics, while separately pinning fee/calendar policy versions.
5. Bid and ask are pointers, not zero sentinels. A present zero is distinguishable from a missing value; spread exists only when both sides exist.
6. Depth is normalized into immutable ordered levels with a hard generic safety ceiling of 1,000 levels per side; provider configuration may impose a lower limit. Bid prices strictly descend, ask prices strictly ascend, and a two-sided depth-only book must not cross. Sizes are nonnegative. When top-of-book values or sizes are also present, level zero must match exactly.
7. Market and session statuses normalize to trimmed lowercase provider vocabulary. Requirements separately declare whether each status is required and which normalized values are acceptable; the generic boundary does not hard-code one venue’s vocabulary. A nonempty allowlist implicitly requires a status and restricts it to that set. An explicit require flag with an empty allowlist accepts any present normalized value. With neither a require flag nor an allowlist, assessment ignores that status.
8. `LatestQuoteSnapshotAt` selects only observations with non-null `available_at <= as_of` in one explicit observation namespace, ordered by availability, source sequence where comparable, and a database-assigned monotonic ingest sequence. Exchange time never authorizes data before it became decision-available.
9. Decimal columns use unrestricted PostgreSQL `NUMERIC` plus explicit nonnegative, magnitude, and `value = round(value, 12)` constraints. This rejects material 13th-decimal input instead of allowing `NUMERIC(38,12)` to round before a check can see it.
10. `Assess` establishes observation sufficiency only. It cannot authorize a route because an attributable observation may reference an inactive, expired, or quarantined instrument. The single `AssessForExecution` boundary joins observation requirements, active/unexpired instrument eligibility, and exact dated venue mechanics so OVR-203 cannot accidentally treat mere contract-ID presence as executable proof.
11. Migration 67 starts the canonical tables empty. Provider adapters and any defensible historical import require separate, reviewed work.

## File map

- Create `internal/marketdata/snapshot.go`: canonical observation, ordered depth, exact numeric validation, and fail-closed assessment.
- Create `internal/marketdata/snapshot_test.go`: public tracer, property tests, missing-versus-zero tests, freshness, and depth rules.
- Modify `internal/repository/interfaces.go`: add the quote-snapshot repository contract.
- Create `internal/repository/postgres/quote_snapshot.go`: atomic append, identical replay, point-in-time lookup, and exact scanning.
- Create `internal/repository/postgres/quote_snapshot_test.go`: isolated real-database repository tracer and concurrency/idempotency coverage.
- Create `migrations/000067_quote_depth_snapshots.up.sql`: immutable exact-decimal observations and normalized depth.
- Create `migrations/000067_quote_depth_snapshots.down.sql`: narrow rollback preserving migrations 1–66 and all legacy market data.
- Create `migrations/000067_quote_depth_snapshots_test.go`: static contract, live constraints, no-backfill boundary, immutability, and rollback tests.
- Modify `internal/repository/postgres/schema_version.go`: require schema 67.
- Modify `docs/development-setup.md`: document the schema-67 tables and inspection queries.
- Modify `docs/superpowers/plans/2026-08-14-total-overhaul-plan.md`: record local OVR-202 evidence and retain the correct dependency-ready next item.
- Update this plan’s checkboxes and self-review with actual qualification evidence.

### Task 1: Canonical observation and optional-field semantics

**Files:**

- Create: `internal/marketdata/snapshot_test.go`
- Create: `internal/marketdata/snapshot.go`

- [x] **Step 1: Write the canonical observation tracer**

```go
func TestNewQuoteSnapshotPreservesExactTopOfBook(t *testing.T) {
	exchangeAt := time.Date(2026, 8, 15, 14, 0, 0, 123456789, time.UTC)
	receivedAt := exchangeAt.Add(25 * time.Millisecond)
	availableAt := receivedAt.Add(time.Millisecond)
	bid := decimal.RequireFromString("100.125")
	ask := decimal.RequireFromString("100.375")

	got, err := NewQuoteSnapshot(QuoteSnapshotInput{
		InstrumentID: uuid.New(),
		Provider: " Polygon ", Venue: " XNAS ", Source: "sip",
		ObservationNamespace: " stocks/sip/a ", ObservationID: " quote-42 ",
		ExchangeAt: &exchangeAt, ReceivedAt: receivedAt,
		AvailableAt: &availableAt, Bid: &bid, Ask: &ask,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "polygon" || got.Venue != "xnas" ||
		got.Bid == nil || !got.Bid.Equal(bid) || got.Ask == nil || !got.Ask.Equal(ask) ||
		!got.ReceivedAt.Equal(receivedAt.Truncate(time.Microsecond)) {
		t.Fatalf("unexpected snapshot: %+v", got)
	}
}
```

- [x] **Step 2: Run the tracer and verify RED**

```bash
go test -count=1 ./internal/marketdata
```

Expected: FAIL because the root market-data package and constructor do not exist.

- [x] **Step 3: Implement the observation surface**

```go
type DepthSide string

const (
	DepthSideBid DepthSide = "bid"
	DepthSideAsk DepthSide = "ask"
)

type DepthLevel struct {
	Side  DepthSide       `json:"side"`
	Level int             `json:"level"`
	Price decimal.Decimal `json:"price"`
	Size  decimal.Decimal `json:"size"`
}

type QuoteSnapshot struct {
	ID                   uuid.UUID        `json:"id"`
	IngestSequence       int64            `json:"ingest_sequence"`
	InstrumentID         uuid.UUID        `json:"instrument_id"`
	VenueContractID      *uuid.UUID       `json:"venue_contract_id,omitempty"`
	Provider             string           `json:"provider"`
	Venue                string           `json:"venue"`
	Source               string           `json:"source,omitempty"`
	ObservationNamespace string           `json:"observation_namespace"`
	ObservationID        string           `json:"observation_id"`
	SourceRevision       string           `json:"source_revision,omitempty"`
	SourceSequence       *int64           `json:"source_sequence,omitempty"`
	ExchangeAt           *time.Time       `json:"exchange_at,omitempty"`
	ReceivedAt           time.Time        `json:"received_at"`
	AvailableAt          *time.Time       `json:"available_at,omitempty"`
	Bid                  *decimal.Decimal `json:"bid,omitempty"`
	BidSize              *decimal.Decimal `json:"bid_size,omitempty"`
	Ask                  *decimal.Decimal `json:"ask,omitempty"`
	AskSize              *decimal.Decimal `json:"ask_size,omitempty"`
	Last                 *decimal.Decimal `json:"last,omitempty"`
	Mark                 *decimal.Decimal `json:"mark,omitempty"`
	MarketStatus         string           `json:"market_status,omitempty"`
	SessionStatus        string           `json:"session_status,omitempty"`
	Depth                []DepthLevel     `json:"depth,omitempty"`
	Metadata             json.RawMessage  `json:"metadata"`
	CreatedAt            time.Time        `json:"created_at"`
}

type QuoteSnapshotInput struct {
	InstrumentID         uuid.UUID
	VenueContractID      *uuid.UUID
	Provider             string
	Venue                string
	Source               string
	ObservationNamespace string
	ObservationID        string
	SourceRevision       string
	SourceSequence       *int64
	ExchangeAt           *time.Time
	ReceivedAt           time.Time
	AvailableAt          *time.Time
	Bid, BidSize         *decimal.Decimal
	Ask, AskSize         *decimal.Decimal
	Last, Mark           *decimal.Decimal
	MarketStatus         string
	SessionStatus        string
	Bids, Asks           []DepthLevelInput
	Metadata             json.RawMessage
	CreatedAt            time.Time
}

type QuoteSelector struct {
	InstrumentID         uuid.UUID
	Provider             string
	Venue                string
	ObservationNamespace string
	AsOf                 time.Time
}
```

`NewQuoteSnapshot` allocates stable row identities, trims identifiers, lowercases provider/venue/status values, normalizes UTC timestamps to PostgreSQL microseconds, clones optional UUIDs/integers/decimals/JSON, numbers depth levels from zero, and calls `Validate`. The database-assigned `IngestSequence` remains zero until persistence and is not part of retry-payload equality. `NewQuoteSelector` applies the same canonical identity normalization and rejects a nil instrument, empty namespace, or zero `AsOf`.

- [x] **Step 4: Add observation invariants one RED → GREEN slice at a time**

```go
func TestQuoteSnapshotRequiresAttributableObservationIdentity(t *testing.T)
func TestQuoteSnapshotRequiresUnambiguousObservationNamespace(t *testing.T)
func TestQuoteSnapshotAllowsMissingExecutableFieldsWithoutZeroSentinels(t *testing.T)
func TestQuoteSnapshotDistinguishesPresentZeroFromMissingPrice(t *testing.T)
func TestQuoteSnapshotRejectsCrossedTopOfBook(t *testing.T)
func TestQuoteSnapshotRejectsSizeWithoutMatchingPrice(t *testing.T)
func TestQuoteSnapshotRejectsExchangeTimeAfterReceiveTime(t *testing.T)
func TestQuoteSnapshotRejectsAvailabilityBeforeReceiveTime(t *testing.T)
func TestQuoteSnapshotRejectsNegativeSourceSequence(t *testing.T)
func TestQuoteSnapshotRejectsNumericPrecisionBeyondSchema(t *testing.T)
func TestQuoteSnapshotRejectsNumericMagnitudeBeyondSchema(t *testing.T)
func TestQuoteSnapshotRejectsNonObjectMetadata(t *testing.T)
func TestQuoteSnapshotValidateRejectsUnnormalizedManualFields(t *testing.T)
```

Prices and sizes are nonnegative when supplied. Zero is valid and present; nil is missing. Values support at most 12 fractional digits and at most 26 integer digits to match `NUMERIC(38,12)`.

Run after each slice:

```bash
go test -count=1 ./internal/marketdata
```

Expected: the new test fails first and the package returns to PASS after the smallest public-contract implementation.

### Task 2: Ordered depth and fail-closed executable assessment

**Files:**

- Modify: `internal/marketdata/snapshot_test.go`
- Modify: `internal/marketdata/snapshot.go`

- [x] **Step 1: Write depth consistency tests**

```go
func TestQuoteSnapshotAcceptsOrderedBookMatchingTopOfBook(t *testing.T)
func TestQuoteSnapshotRejectsBidDepthThatDoesNotStrictlyDescend(t *testing.T)
func TestQuoteSnapshotRejectsAskDepthThatDoesNotStrictlyAscend(t *testing.T)
func TestQuoteSnapshotRejectsNegativeDepth(t *testing.T)
func TestQuoteSnapshotRejectsMoreThanMaximumDepthLevels(t *testing.T)
func TestQuoteSnapshotRejectsCrossedDepthWithoutExplicitTopOfBook(t *testing.T)
func TestQuoteSnapshotRejectsDepthTopThatDisagreesWithQuote(t *testing.T)
func TestQuoteSnapshotRejectsTopSizeThatDisagreesWithDepth(t *testing.T)
```

Run and verify RED before adding each corresponding validation rule:

```bash
go test -count=1 ./internal/marketdata
```

- [x] **Step 2: Define stable assessment codes and requirements**

```go
type AssessmentCode string

const (
	AssessmentMissingSource        AssessmentCode = "missing_source"
	AssessmentMissingVenueContract AssessmentCode = "missing_venue_contract"
	AssessmentMissingAvailability  AssessmentCode = "missing_availability_time"
	AssessmentMissingBid           AssessmentCode = "missing_bid"
	AssessmentMissingAsk           AssessmentCode = "missing_ask"
	AssessmentMissingExchangeTime  AssessmentCode = "missing_exchange_time"
	AssessmentFutureObservation    AssessmentCode = "future_observation"
	AssessmentStaleQuote           AssessmentCode = "stale_quote"
	AssessmentMissingBidDepth      AssessmentCode = "missing_bid_depth"
	AssessmentMissingAskDepth      AssessmentCode = "missing_ask_depth"
	AssessmentMissingMarketStatus  AssessmentCode = "missing_market_status"
	AssessmentMissingSessionStatus AssessmentCode = "missing_session_status"
	AssessmentMarketNotExecutable  AssessmentCode = "market_not_executable"
	AssessmentSessionNotExecutable AssessmentCode = "session_not_executable"
	AssessmentInstrumentMismatch   AssessmentCode = "instrument_mismatch"
	AssessmentInstrumentNotExecutable AssessmentCode = "instrument_not_executable"
	AssessmentVenueContractMismatch   AssessmentCode = "venue_contract_mismatch"
	AssessmentInvalidPriceTick        AssessmentCode = "invalid_price_tick"
	AssessmentInvalidLotSize          AssessmentCode = "invalid_lot_size"
)

type QuoteRequirements struct {
	RequireSource        bool
	RequireVenueContract bool
	RequireBid           bool
	RequireAsk           bool
	RequireBidDepth      bool
	RequireAskDepth      bool
	RequireMarketStatus  bool
	RequireSessionStatus bool
	AllowedMarketStatuses  []string
	AllowedSessionStatuses []string
	MaxAge               time.Duration
}

type AssessmentError struct {
	Code AssessmentCode
	// Detail carries deterministic context without changing Code.
	Detail string
}

type QuoteAssessment struct {
	SnapshotID        uuid.UUID
	VenueContractID   *uuid.UUID
	EvaluatedAt       time.Time
	ExchangeAge       *time.Duration
	ReceiveAge        time.Duration
	AvailabilityAge   time.Duration
	TransportLatency  *time.Duration
	ValidationLatency *time.Duration
	Spread            *decimal.Decimal
}
```

`Assess(asOf, requirements)` normalizes `asOf` to UTC microseconds, validates the stored observation, rejects a negative `MaxAge`, always requires a known `AvailableAt`, rejects an observation not yet available, requires `ExchangeAt` whenever `MaxAge > 0`, calculates age from exchange time, validates requested source/contract-presence/book/status facts, and returns `AssessmentError` with stable codes. Allowed status values are normalized, deduplicated, and must be non-empty when supplied. A spread pointer is produced only when both bid and ask exist; a locked zero spread remains a present zero. It is observation-sufficiency evidence, not an execution authorization.

`AssessForExecution(asOf, requirements, instrument, venueContract)` forces contract presence, delegates the observation checks above, then requires a matching active instrument that is not expired at `asOf`. It validates contract identity and the half-open effective window at both the observation timestamp and `asOf`, then checks bid, ask, and all depth prices as exact tick multiples plus any displayed top sizes and all depth sizes as exact lot multiples. `Last` and `Mark` remain non-executable evidence and are deliberately excluded.

- [x] **Step 3: Drive the fail-closed matrix test-first**

```go
func TestQuoteAssessmentFailsClosedForRequiredSource(t *testing.T)
func TestQuoteAssessmentFailsClosedForRequiredVenueContract(t *testing.T)
func TestQuoteAssessmentFailsClosedWhenAvailabilityIsUnknown(t *testing.T)
func TestQuoteAssessmentFailsClosedForRequiredBidAndAsk(t *testing.T)
func TestQuoteAssessmentFailsClosedWhenAgeCannotBeCalculated(t *testing.T)
func TestQuoteAssessmentRejectsStaleQuoteAtIntentTime(t *testing.T)
func TestQuoteAssessmentRejectsObservationAvailableAfterDecision(t *testing.T)
func TestQuoteAssessmentRequiresRequestedDepthSide(t *testing.T)
func TestQuoteAssessmentRequiresRequestedMarketAndSessionStatus(t *testing.T)
func TestQuoteAssessmentMarketAllowlistImplicitlyRequiresStatus(t *testing.T)
func TestQuoteAssessmentSessionAllowlistImplicitlyRequiresStatus(t *testing.T)
func TestQuoteAssessmentRejectsDisallowedMarketOrSessionStatus(t *testing.T)
func TestQuoteAssessmentKeepsMissingSpreadNil(t *testing.T)
func TestQuoteAssessmentKeepsLockedSpreadPresentAndZero(t *testing.T)
func TestQuoteSnapshotValidatesExactVenueMechanics(t *testing.T)
func TestQuoteSnapshotRejectsOffTickExecutablePrices(t *testing.T)
func TestQuoteSnapshotRejectsOffLotExecutableSizes(t *testing.T)
func TestQuoteSnapshotVenueMechanicsDoNotInventLastOrMarkExecutability(t *testing.T)
func TestQuoteSnapshotRejectsMismatchedVenueContractReference(t *testing.T)
func TestQuoteSnapshotExecutionAssessmentRequiresActiveMatchingInstrument(t *testing.T)
func TestQuoteSnapshotExecutionAssessmentRequiresContractAtEvaluationTime(t *testing.T)
```

Run after every slice:

```bash
go test -count=1 ./internal/marketdata
```

Expected: PASS with deterministic codes; error assertions use `errors.As` rather than fragile full-string comparison.

### Task 3: Migration 67 and database-level truth constraints

**Files:**

- Create: `migrations/000067_quote_depth_snapshots_test.go`
- Create: `migrations/000067_quote_depth_snapshots.up.sql`
- Create: `migrations/000067_quote_depth_snapshots.down.sql`
- Modify: `internal/repository/postgres/schema_version.go`

- [x] **Step 1: Write the static schema contract test and verify RED**

Require these durable objects and invariants:

```go
for _, fragment := range []string{
	"create table quote_snapshots",
	"instrument_id uuid not null references instruments(id) on delete restrict",
	"venue_contract_id uuid references venue_contracts(id) on delete restrict",
	"ingest_sequence bigint generated always as identity unique",
	"observation_namespace text not null",
	"observation_id text not null",
	"source_revision text not null default ''",
	"available_at timestamptz",
	"bid numeric",
	"bid = round(bid, 12)",
	"bid_depth_count integer not null",
	"ask_depth_count integer not null",
	"unique (instrument_id, provider, venue, observation_namespace, observation_id, source_revision)",
	"create table quote_depth_levels",
	"unique (quote_snapshot_id, side, level_index)",
	"create trigger trg_quote_snapshots_venue_contract",
	"create constraint trigger trg_quote_snapshots_depth_consistent",
	"create trigger trg_quote_snapshots_immutable",
	"create trigger trg_quote_depth_levels_immutable",
} {
	if !strings.Contains(upSQL, fragment) {
		t.Fatalf("expected migration 67 to contain %q", fragment)
	}
}
```

Run:

```bash
DB_URL="$AUGR_PHASE1_DB_URL" go test -count=1 -run '^TestQuoteDepthSnapshotMigration' ./migrations
```

Expected: FAIL because migration 67 does not exist.

- [x] **Step 2: Implement exact immutable tables**

```sql
CREATE TABLE quote_snapshots (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ingest_sequence       BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
    instrument_id         UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT,
    venue_contract_id     UUID REFERENCES venue_contracts(id) ON DELETE RESTRICT,
    provider              TEXT NOT NULL CHECK (provider <> '' AND provider = lower(btrim(provider))),
    venue                 TEXT NOT NULL CHECK (venue <> '' AND venue = lower(btrim(venue))),
    source                TEXT CHECK (source IS NULL OR (source <> '' AND source = btrim(source))),
    observation_namespace TEXT NOT NULL
                               CHECK (observation_namespace <> '' AND observation_namespace = btrim(observation_namespace)),
    observation_id        TEXT NOT NULL
                               CHECK (observation_id <> '' AND observation_id = btrim(observation_id)),
    source_revision       TEXT NOT NULL DEFAULT '' CHECK (source_revision = btrim(source_revision)),
    source_sequence       BIGINT CHECK (source_sequence >= 0),
    exchange_at           TIMESTAMPTZ,
    received_at           TIMESTAMPTZ NOT NULL,
    available_at          TIMESTAMPTZ,
    bid                   NUMERIC,
    bid_size              NUMERIC,
    ask                   NUMERIC,
    ask_size              NUMERIC,
    last                  NUMERIC,
    mark                  NUMERIC,
    market_status         TEXT CHECK (market_status IS NULL OR (
                              market_status <> '' AND market_status = lower(btrim(market_status))
                          )),
    session_status        TEXT CHECK (session_status IS NULL OR (
                              session_status <> '' AND session_status = lower(btrim(session_status))
                          )),
    bid_depth_count       INTEGER NOT NULL CHECK (bid_depth_count BETWEEN 0 AND 1000),
    ask_depth_count       INTEGER NOT NULL CHECK (ask_depth_count BETWEEN 0 AND 1000),
    metadata              JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (instrument_id, provider, venue, observation_namespace, observation_id, source_revision),
    CHECK (exchange_at IS NULL OR exchange_at <= received_at),
    CHECK (available_at IS NULL OR available_at >= received_at),
    CHECK (bid IS NULL OR ask IS NULL OR bid <= ask),
    CHECK ((bid_size IS NULL OR bid IS NOT NULL) AND (ask_size IS NULL OR ask IS NOT NULL)),
    CHECK (bid IS NULL OR (bid >= 0 AND bid < 100000000000000000000000000 AND bid = round(bid, 12))),
    CHECK (bid_size IS NULL OR (bid_size >= 0 AND bid_size < 100000000000000000000000000 AND bid_size = round(bid_size, 12))),
    CHECK (ask IS NULL OR (ask >= 0 AND ask < 100000000000000000000000000 AND ask = round(ask, 12))),
    CHECK (ask_size IS NULL OR (ask_size >= 0 AND ask_size < 100000000000000000000000000 AND ask_size = round(ask_size, 12))),
    CHECK (last IS NULL OR (last >= 0 AND last < 100000000000000000000000000 AND last = round(last, 12))),
    CHECK (mark IS NULL OR (mark >= 0 AND mark < 100000000000000000000000000 AND mark = round(mark, 12)))
);

CREATE TABLE quote_depth_levels (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    quote_snapshot_id UUID NOT NULL REFERENCES quote_snapshots(id) ON DELETE RESTRICT,
    side              TEXT NOT NULL CHECK (side IN ('bid', 'ask')),
    level_index       INTEGER NOT NULL CHECK (level_index >= 0),
    price             NUMERIC NOT NULL CHECK (
                          price >= 0 AND price < 100000000000000000000000000 AND price = round(price, 12)
                      ),
    size              NUMERIC NOT NULL CHECK (
                          size >= 0 AND size < 100000000000000000000000000 AND size = round(size, 12)
                      ),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (quote_snapshot_id, side, level_index)
);
```

Index point-in-time reads on `(instrument_id, provider, venue, observation_namespace, available_at DESC, source_sequence DESC, ingest_sequence DESC)` with `WHERE available_at IS NOT NULL`. The repeated explicit numeric checks are intentional: PostgreSQL must reject material scale or magnitude overflow before immutable data is accepted.

- [x] **Step 3: Enforce complete depth at commit**

`assert_quote_snapshot_depth(snapshot_id)` must verify:

- actual per-side counts equal the declared counts;
- level indexes are contiguous from zero;
- bid prices strictly descend and ask prices strictly ascend;
- two-sided level zero does not cross even when explicit bid/ask columns are nil;
- level zero agrees exactly with any present top-of-book price and size.

Attach `DEFERRABLE INITIALLY DEFERRED` constraint triggers to both the parent insert and child inserts. `validate_quote_snapshot_venue_contract()` runs before insert and rejects a referenced contract whose instrument or normalized venue does not match the snapshot. At `observation_time = COALESCE(exchange_at, received_at)`, the exact half-open validity predicate is `valid_from <= observation_time AND (valid_to IS NULL OR observation_time < valid_to)`. Add `BEFORE UPDATE OR DELETE` triggers to both tables so neither observations nor depth can be rewritten after acceptance.

- [x] **Step 4: Add live migration behavior tests**

```go
func TestQuoteDepthSnapshotMigrationAcceptsIncompleteAttributableObservation(t *testing.T)
func TestQuoteDepthSnapshotMigrationAllowsSameObservationIDInDifferentNamespaces(t *testing.T)
func TestQuoteDepthSnapshotMigrationValidatesVenueContractIdentityAndWindow(t *testing.T)
func TestQuoteDepthSnapshotMigrationUsesHalfOpenVenueContractWindow(t *testing.T)
func TestQuoteDepthSnapshotMigrationRejectsCrossedQuote(t *testing.T)
func TestQuoteDepthSnapshotMigrationRejectsCrossedDepthWithoutTopOfBook(t *testing.T)
func TestQuoteDepthSnapshotMigrationRejectsDecimalScaleAndMagnitudeOverflow(t *testing.T)
func TestQuoteDepthSnapshotMigrationRejectsMissingDeclaredDepthAtCommit(t *testing.T)
func TestQuoteDepthSnapshotMigrationRejectsOutOfOrderDepthAtCommit(t *testing.T)
func TestQuoteDepthSnapshotMigrationRejectsTopOfBookMismatchAtCommit(t *testing.T)
func TestQuoteDepthSnapshotMigrationAcceptsParentThenCompleteChildrenAtCommit(t *testing.T)
func TestQuoteDepthSnapshotMigrationRejectsLateDepthInsertWithoutMutatingBook(t *testing.T)
func TestQuoteDepthSnapshotMigrationRejectsMutation(t *testing.T)
func TestQuoteDepthSnapshotMigrationDoesNotBackfillLegacyFloatSnapshots(t *testing.T)
func TestQuoteDepthSnapshotMigrationRollsBackWithoutTouchingSchema66(t *testing.T)
```

The venue-window test accepts a quote exactly at `valid_from` and immediately before `valid_to`, then rejects one exactly at `valid_to`. The commit-time tests use explicit transactions and assert the error occurs at `Commit`, after parent and children have had a chance to form one complete aggregate. A separate second-transaction test proves a completed book accepts a no-op commit but rejects a late child insert at commit and leaves its original depth unchanged. The decimal test uses raw SQL for a material 13th decimal and an over-26-integer-digit value. The no-backfill test seeds a legacy Polymarket or Kalshi snapshot before applying migration 67 and verifies that `quote_snapshots` remains empty. It must not infer an instrument ID or decimal provenance from the legacy row.

- [x] **Step 5: Bump and verify the runtime schema contract**

Set `postgres.RequiredSchemaVersion = 67`, then run:

```bash
go test -count=1 -run '^TestSchemaVersionSync$' ./internal/repository/postgres
```

Expected: PASS with migration 67 as the latest up migration.

### Task 4: PostgreSQL repository and no-lookahead lookup

**Files:**

- Modify: `internal/repository/interfaces.go`
- Create: `internal/repository/postgres/quote_snapshot_test.go`
- Create: `internal/repository/postgres/quote_snapshot.go`

- [x] **Step 1: Add the narrow public repository contract**

```go
type QuoteSnapshotRepository interface {
	RecordQuoteSnapshot(context.Context, *marketdata.QuoteSnapshot) (*marketdata.QuoteSnapshot, error)
	GetQuoteSnapshotByID(context.Context, uuid.UUID) (*marketdata.QuoteSnapshot, error)
	LatestQuoteSnapshotAt(context.Context, marketdata.QuoteSelector) (*marketdata.QuoteSnapshot, error)
}
```

`marketdata.NewQuoteSelector(instrumentID, provider, venue, observationNamespace, asOf)` requires stable identity, normalizes provider/venue and UTC microsecond time through the public bounded context, and returns a `QuoteSelector`. Repository code does not reproduce normalization rules ad hoc.

- [x] **Step 2: Write the atomic persistence tracer and verify RED**

```go
func TestQuoteSnapshotRepoPersistsExactObservationAndDepth(t *testing.T)
```

Create an active canonical instrument and dated venue contract, record a two-sided exact-decimal snapshot with two levels per side, reload it, and compare every optional field, source namespace/revision/sequence, contract reference, timestamp, and depth level.

Run:

```bash
DB_URL="$AUGR_PHASE1_DB_URL" go test -count=1 -run '^TestQuoteSnapshotRepo' ./internal/repository/postgres
```

Expected: FAIL because `QuoteSnapshotRepo` does not exist.

- [x] **Step 3: Implement atomic append and scanning**

`RecordQuoteSnapshot` must:

1. validate the public domain value before opening a transaction;
2. insert the parent with declared depth counts using exact decimal strings;
3. insert every depth level in the same transaction;
4. let deferred database constraints validate the complete book;
5. commit before returning the fully reloaded row;
6. on a full source-identity conflict, wait for the winning transaction, load the existing row, and return it only when the full semantic payload matches; otherwise wrap `repository.ErrIdempotencyConflict`.

Do not compare generated IDs, database `IngestSequence`, or constructor/server `CreatedAt` when deciding whether a logically identical source retry matches. Do compare instrument and venue-contract IDs, provider, venue, source, namespace, observation ID, source revision/sequence, exchange/receive/availability times, every optional numeric, statuses, JSON semantics, and every ordered depth level. Preserve non-idempotency PostgreSQL errors and SQLSTATE so serialization/deadlock callers can classify retryability; only source-identity payload reuse maps to `repository.ErrIdempotencyConflict`, and absent rows map to `repository.ErrNotFound`.

- [x] **Step 4: Implement point-in-time lookup without lookahead**

```sql
SELECT id, ingest_sequence, instrument_id, venue_contract_id,
       provider, venue, source, observation_namespace, observation_id,
       source_revision, source_sequence, exchange_at, received_at, available_at,
       bid::TEXT, bid_size::TEXT,
       ask::TEXT, ask_size::TEXT, last::TEXT, mark::TEXT,
       market_status, session_status, bid_depth_count, ask_depth_count,
       metadata, created_at
FROM quote_snapshots
WHERE instrument_id = $1
  AND provider = $2
  AND venue = $3
  AND observation_namespace = $4
  AND available_at IS NOT NULL
  AND available_at <= $5
ORDER BY available_at DESC, source_sequence DESC NULLS LAST, ingest_sequence DESC
LIMIT 1;
```

Return `repository.ErrNotFound` when no already-available observation exists. The query intentionally does not select by exchange timestamp, receive time, or creation time alone.

- [x] **Step 5: Drive repository behavior slices**

```go
func TestQuoteSnapshotRepoReplaysIdenticalObservation(t *testing.T)
func TestQuoteSnapshotRepoRejectsMismatchedObservationReuse(t *testing.T)
func TestQuoteSnapshotRepoAllowsSameIDForDistinctInstrumentsNamespacesOrRevisions(t *testing.T)
func TestQuoteSnapshotRepoValidatesVenueContract(t *testing.T)
func TestQuoteSnapshotRepoRetainsNonExecutableVenueMechanicsEvidence(t *testing.T)
func TestQuoteSnapshotRepoLatestAtUsesAvailabilityNotExchangeOrImportTime(t *testing.T)
func TestQuoteSnapshotRepoLatestAtDeterministicallyOrdersSameTimeObservations(t *testing.T)
func TestQuoteSnapshotRepoLoadsMissingFieldsAsNilNotZero(t *testing.T)
func TestQuoteSnapshotRepoConcurrentIdenticalAppendCreatesOneSnapshot(t *testing.T)
func TestQuoteSnapshotRepoRejectsUnknownInstrument(t *testing.T)
```

Run after each slice:

```bash
DB_URL="$AUGR_PHASE1_DB_URL" go test -race -count=1 -run '^TestQuoteSnapshotRepo' ./internal/repository/postgres
```

Expected: PASS without race findings, duplicate rows, or imprecise numeric conversions.

### Task 5: Reversible qualification, documentation, and sync

**Files:**

- Modify: `docs/development-setup.md`
- Modify: `docs/superpowers/plans/2026-08-14-total-overhaul-plan.md`
- Modify: this plan

- [x] **Step 1: Run focused qualification**

```bash
go test -count=1 ./internal/marketdata
DB_URL="$AUGR_PHASE1_DB_URL" go test -race -count=1 -run '^TestQuoteSnapshotRepo' ./internal/repository/postgres
DB_URL="$AUGR_PHASE1_DB_URL" go test -race -count=1 -run '^TestQuoteDepthSnapshotMigration' ./migrations
go vet ./internal/marketdata ./internal/repository/postgres ./migrations
golangci-lint run ./internal/marketdata ./internal/repository/postgres ./migrations
```

Expected: PASS with zero race, vet, or lint findings.

- [x] **Step 2: Rehearse migration 67 down and reapply**

```bash
migrate -path migrations -database "$AUGR_PHASE1_DB_URL" up 1
migrate -path migrations -database "$AUGR_PHASE1_DB_URL" down 1
migrate -path migrations -database "$AUGR_PHASE1_DB_URL" up 1
```

Verify before rollback that exact quote/depth fixtures and no legacy backfill exist. Verify after rollback that schema-66 instruments, aliases, venue contracts, corporate actions, quarantine, accounts, and ledger rows remain. Verify after reapply that the schema reports `67|false` and canonical quote tables are empty until explicitly written.

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

Run `task fmt:check`, the database-enabled all-package suite, and `task vulncheck` as evidence gates. Classify inherited failures separately from OVR-202 and verify every touched Go file with the repository formatter.

- [x] **Step 4: Smoke the compiled runtime**

Start the compiled binary against isolated schema 67 with live trading disabled and `TRADING_AGENT_KILL=true`. Require `/health`, `/healthz`, and `/api/v1/health` to report PostgreSQL and Redis `ok`, then stop it cleanly.

- [x] **Step 5: Update durable documentation**

Record:

- OVR-202 is locally complete, not deployed, and no provider/read path is cut over;
- exact canonical observations are append-only, keyed to stable instruments and unambiguous source namespaces, and bind dated venue mechanics when known;
- missing source, venue contract, availability, exchange age, bid, ask, market/session eligibility, or depth remains explicitly missing and fails the requested assessment;
- execution additionally requires a matching active/unexpired instrument, a contract effective at observation and route time, and exact venue tick/lot mechanics; attributable off-tick/off-lot observations remain evidence but cannot authorize execution;
- point-in-time reads use authoritative availability time within one source namespace and cannot look ahead;
- legacy float snapshots remain legacy and were not upgraded by assertion;
- the next dependency-ready work returns to OVR-103 before OVR-203 so economic fill events have a ledger adapter when the lifecycle arrives.

Qualification evidence on 2026-08-15:

- focused domain tests, repository and migration suites under the race detector, vet, and lint passed;
- the isolated development database rehearsed schema `66 → 67 → 66 → 67`, preserving one account, one capital flow, one ledger transaction, and two postings while leaving canonical quote/depth tables empty;
- repository-wide short race tests, build, vet, lint, all 162 frontend tests, frontend lint, frontend build, touched-file `gofumpt`, and `git diff --check` passed;
- the compiled kill-switched binary returned `{"status":"ok","db":"ok","redis":"ok"}` from `/health`, `/healthz`, and `/api/v1/health`, then shut down cleanly;
- independent post-implementation review approved the final revision after exact tick/lot mechanics, active instrument eligibility, late-depth mutation, and observation-time plus route-time contract windows were proven;
- inherited gates remained unchanged: nine untouched files fail repository-wide `task fmt:check`; the database-enabled all-package suite retains its existing shared-harness and legacy schema/JSON assumptions; and `govulncheck` reports the same five reachable advisories in existing pgx, gRPC, x/net, and x/text dependencies.

- [x] **Step 6: Commit and sync the isolated slice**

```bash
git add docs/superpowers/plans/2026-08-15-phase-2-quote-depth-snapshots.md \
  docs/superpowers/plans/2026-08-14-total-overhaul-plan.md docs/development-setup.md \
  internal/marketdata/snapshot.go internal/marketdata/snapshot_test.go \
  internal/repository/interfaces.go internal/repository/postgres/quote_snapshot.go \
  internal/repository/postgres/quote_snapshot_test.go internal/repository/postgres/schema_version.go \
  migrations/000067_quote_depth_snapshots.up.sql migrations/000067_quote_depth_snapshots.down.sql \
  migrations/000067_quote_depth_snapshots_test.go
git diff --cached --check
git commit -m "feat: add timestamped quote snapshots"
git fetch origin codex/augr-overhaul
git push origin codex/augr-overhaul
```

Expected: one reviewed OVR-202 commit, no force push, and local/remote branch alignment before OVR-103 begins.

## Planned review gates

- Initial architecture review: verify optional observation fields do not weaken executable fail-closed behavior and that source identity, dated mechanics, availability, session eligibility, depth, and decimal constraints are sufficient.
- Final implementation-plan review: verify the domain, SQL, repository, rollback, and qualification steps cover the accepted OVR-202 exit condition without absorbing OVR-203 or OVR-301.
- Post-implementation review: inspect the actual diff for precision loss, lookahead, weak immutability, race/idempotency errors, and accidental legacy cutover.

## Self-review

- Spec coverage: provider/venue/source namespace/revision identity, active instrument eligibility, dated venue mechanics at observation and route time, executable tick/lot multiples, exchange/receive/availability time, optional bid/ask/last/mark, quote age, market/session eligibility, bounded depth, exact decimals, missing-versus-zero semantics, and fail-closed requirements each map to a public test or database constraint.
- Completeness scan: every new public type, repository method, SQL table, constraint trigger, rollback object, documentation update, and verification command has a named task.
- Type consistency: domain and SQL use 12-decimal exact values; optional Go pointers map to nullable SQL; side/status vocabulary remains identical across types, SQL, and tests.
- Point-in-time safety: only source-namespace-scoped, availability-bounded rows are selectable; exchange time drives staleness only after the observation is available, and ingress/import/creation time cannot silently substitute for unknown availability.
- Migration safety: migration 67 writes no legacy row, promotes no legacy float, and can roll back without touching schema 66.
- Scope control: provider adapters and execution-path cutover remain explicit work for later slices.
