# Phase 2 Capital-Tier and Margin-Profile Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Keep public
> contracts test-first, obtain architecture approval before implementation and
> independent diff approval before phase closure, and preserve the local-only
> activation boundary.

**Goal:** Complete OVR-206 with one exact, deterministic, content-addressed
capital and margin policy that evaluates the same canonical replay at the
mandated `$500`, `$5,000`, `$25,000`, `$100,000`, `$1,000,000`, and
`$5,000,000` scored tiers plus an explicitly synthetic stress/unlimited mode.

**Architecture:** Add `internal/capital` as the sole new authority for capital
tier identity, margin assumptions, and pre-route buying-power admission. A
validated immutable policy declares the complete six-tier vocabulary and four
named profiles: cash, Reg-T-like, portfolio-style, and stress/unlimited. Its
canonical JSON bytes produce a content-addressed version and deterministic
artifact ID. Migration 74 stores those exact bytes in an immutable artifact
table and stores an append-only binding between an explicit OVR-101 account,
one declared tier, and one profile in that artifact. The Go engine consumes a
validated account, its binding, a capital state derived by one canonical
builder from an OVR-104 portfolio projection plus canonical instrument
identities, and a proposed signed exposure change for an equity or ETF. It
returns a canonical assessment containing equity,
available buying power, reserve, initial and maintenance requirements, and a
stable admit/reject reason. The engine never infers capital from a strategy,
never reads a mutable broker balance as policy, and never mutates the ledger or
positions. A thin replay matrix creates isolated account identities for every
tier, applies the same ordered scenario factory to each, and keeps stress
outcomes in a separate evidence/storage population. OVR-303 will later pin the
artifact, tier, and profile on durable experiments; OVR-404 will consume these
assessments in the common risk service. This phase provides and proves the
contract without activating a runtime writer or changing the existing order
hot path.

**Tech stack:** Go 1.25.8+, `shopspring/decimal`, UUIDs, SHA-256, canonical
JSON, PostgreSQL 17/TimescaleDB, pgx v5, golang-migrate, Task, and TDD against
an isolated schema-73 database upgraded to schema 74.

---

## Scope and sequencing

This plan covers OVR-206 only. It depends on OVR-204's common simulation venue
and consumes OVR-101 accounts/capital flows, OVR-102 ledger concepts, and
OVR-104 portfolio projection vocabulary. OVR-303 will persist reproducible
experiments using this contract. OVR-404 will make the common risk service the
runtime admission authority. OVR-405 and OVR-406 will add asset-specific
assignment, borrow, funding, and variation-margin behavior.

In scope:

- a closed canonical capital-tier vocabulary containing exactly the six
  mandate values;
- a content-addressed policy schema with no implicit economic defaults;
- exact named cash, Reg-T-like, portfolio-style, and stress/unlimited profile
  assumptions, explicitly labeled as simulation approximations rather than
  broker parity;
- separate initial-long, initial-short, maintenance-long,
  maintenance-short, cash-reserve, and maximum-gross-exposure rules;
- exact decimal arithmetic and one declared rounding boundary;
- immutable policy-artifact persistence and independent PostgreSQL
  reconstruction of schema, digest, deterministic identity, tiers, and
  profiles;
- append-only account/profile binding with exact account environment,
  starting capital, buying-power multiplier, margin profile, tier, policy
  version, evidence class, and storage namespace agreement;
- scored bindings only for the six declared finite tiers and non-unlimited
  profiles;
- stress/unlimited only for `paper_stress` plus `synthetic_stress`, a
  stress-scoped namespace, a zero account multiplier, and an explicit
  unbounded profile;
- immutable point-in-time portfolio-state input with exact cash, equity,
  existing long/short/gross exposure, and current maintenance requirement;
- one canonical state builder that derives those fields from an OVR-104
  portfolio projection and a complete matching set of OVR-201 instrument
  identities rather than accepting caller-authored exposure totals;
- v1 admission for equities and ETFs only, with every option, prediction
  contract, crypto spot, future, unknown, or mixed unsupported portfolio
  failing closed until its asset-specific collateral policy exists;
- deterministic pre-route admission of a signed proposed notional change;
- fail-closed handling of negative equity, inconsistent exposure totals,
  missing or mismatched account/binding/policy identity, unsupported profile,
  nonfinite tier, cross-currency input, scale overflow, and arithmetic that
  violates profile bounds;
- canonical assessment bytes/hash and stable reason codes for admitted,
  insufficient settled cash, insufficient buying power, reserve breach,
  gross-exposure breach, maintenance breach, and stress-unbounded;
- a replay matrix that invokes the same scenario factory once for each scored
  tier and separately for stress, retaining ordered outcomes and failures
  rather than silently dropping an inapplicable tier;
- parity tests showing backtest and internal-paper callers receive identical
  assessments for the same account/policy/state/request;
- a golden replay that passes through the existing OVR-204 venue after capital
  admission and proves identical lifecycle/economic outcome within one mode;
- an over-capacity replay that is a valid small-tier rejection and larger-tier
  admission, not a harness failure;
- real PostgreSQL concurrency, exact replay, changed-payload conflict,
  migration-race, empty-only rollback, and restart/reload evidence;
- documentation, isolated qualification, independent review, reviewable
  commits, push, fetch, and hash/divergence verification.

Out of scope:

- production/shared-database migration, deployment, scheduler activation,
  account mutation, broker credentials, external provider calls, real orders,
  or runtime risk cutover;
- claiming exact Alpaca, Kalshi, FINRA, exchange, clearing, house-margin, or
  portfolio-margin parity;
- dynamic broker rule downloads, mutable “current profile” pointers, or
  interpreting an external `buying_power` field as durable policy;
- interest, borrow availability/rates, hard-to-borrow status, settlement
  calendars, pattern-day-trader counting, option strategy offsets, assignment,
  exercise, variation margin, liquidation, or cross-account netting;
- using float-based legacy `capital_ladder` strategy-promotion state as capital
  policy or migrating it into canonical evidence;
- changing account starting capital after creation; deposits and withdrawals
  remain append-only OVR-101 capital flows;
- applying generic notional haircuts to options, prediction contracts, crypto
  spot, futures, or an unsupported existing portfolio;
- making stress results promotion eligible, ranking scored and stress results
  together, or allowing their storage namespaces to overlap;
- persisting experiment runs or adding a production matrix scheduler before
  OVR-303;
- making capital rejection an order lifecycle event. Admission happens before
  route; a rejected proposal has no broker/simulation order and no fill.

## Contract decisions

### Policy, tier, and artifact identity

1. `capital-margin-policy-v1` is a fixed canonical schema. A policy contains
   exactly six sorted unique finite tiers and exactly one definition for each
   of `cash`, `reg_t`, `portfolio`, and `stress_unlimited`.
2. The tier vocabulary is exactly `500`, `5000`, `25000`, `100000`, `1000000`,
   and `5000000` USD. These values are evaluation identities, not automatic
   deposits, target allocations, or claims that a strategy works at every
   tier.
3. Canonical bytes use fixed structs, sorted arrays, lower-case enum strings,
   normalized exact decimal strings, integer scale, and booleans. Maps,
   binary floating point, NaN/infinity, process-local time, environment
   variables, and friendly labels are excluded from the authority.
4. The policy version is
   `capital-margin-policy-v1@sha256:<lowercase digest>`. The artifact ID is the
   existing length-prefixed deterministic UUID for domain
   `capital-margin-policy-artifact` and the complete version.
5. The fixed local v1 policy must be constructed from explicit source values;
   `NewPolicy` never supplies missing economic fields. Registration persists
   exact bytes before an account can bind to the policy.
6. Migration 74 independently reconstructs the fixed v1 canonical JSON and
   rejects a row unless byte length, JSON copy, SHA-256, version, deterministic
   UUID, complete tier vocabulary, complete profile vocabulary, exact decimals,
   and bounded values agree. PostgreSQL does not trust caller-supplied JSON
   labels or hashes.
7. Artifacts are append-only. An identical register retries; reused identity
   or version with changed bytes conflicts. Update/delete fails. Downgrade
   refuses while an artifact or binding exists.

### Profile semantics

8. V1 definitions are conservative simulation assumptions:

   | Profile | Initial long | Initial short | Maintenance long | Maintenance short | Maximum gross | Reserve | Unlimited |
   | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
   | `cash` | `1` | unsupported | `1` | unsupported | `1` | `0` | no |
   | `reg_t` | `0.5` | `1.5` | `0.25` | `0.30` | `2` | `0` | no |
   | `portfolio` | `0.15` | `0.30` | `0.15` | `0.30` | `6` | `0` | no |
   | `stress_unlimited` | `0` | `0` | `0` | `0` | unbounded | `0` | yes |

   `portfolio` is an intentionally coarse stressable haircut model, not a
   broker portfolio-margin calculator. Asset/strategy offsets remain out of
   scope.
9. A finite profile's account `BuyingPowerMultiplier` must equal its maximum
   gross multiple. Cash is `1`, Reg-T-like is `2`, and portfolio-style is `6`.
   Stress/unlimited requires multiplier `0`; zero never means unlimited in any
   other environment/profile.
10. Short opening is rejected by cash. In other finite profiles, a proposed
    short increase uses the declared initial-short requirement; a long increase
    uses initial-long. Exposure reduction requires no new initial margin but
    must not be mislabeled as an entry.
11. Available buying power is the lesser of the exact initial-margin headroom
    and gross-exposure headroom. Cash additionally cannot spend settled cash
    below the declared reserve. No absolute value is silently substituted for
    a signed direction.
12. Maintenance headroom is checked independently from initial admission. An
    already deficient state cannot add risk even if the proposed delta is
    small. This phase reports the deficiency; it does not liquidate or mutate
    holdings.
13. All policy values and assessment amounts support at most 12 decimal places
    and 26 integer digits. Required margin rounds once upward to the policy
    scale so admission never benefits from rounding down. Display formatting
    is not authoritative.
14. V1 finite margin rules apply only to canonical equity and ETF instruments.
    The proposed instrument and every open projected position must resolve by
    stable OVR-201 instrument ID to one of those two classes. Options,
    prediction contracts, crypto spot, futures, unknown identities, missing
    identities, duplicate identity facts, or a mixed unsupported portfolio
    fail before an assessment is produced. They are not approximated through
    generic notional haircuts.

### Account binding and evidence isolation

15. A binding is deterministic from account ID and policy version and records
    the declared tier/profile plus immutable copies of account environment,
    starting capital, multiplier, evidence class, namespace, and currency.
    Repository load revalidates the account and reconstructs the policy.
16. A scored account binding requires `paper_scored`, `promotion_evidence`, a
    `paper_scored/` namespace, USD, starting capital exactly equal to one of the
    six tiers, and a finite profile matching the account profile/multiplier.
17. A stress binding requires `paper_stress`, `synthetic_stress`, a
    `paper_stress/` namespace, `stress_unlimited`, and multiplier zero. Its
    starting capital is retained as scenario context but does not create
    bounded buying power.
18. `shadow` and `live` account bindings are rejected in v1. Supporting them
    requires a later reviewed policy based on actual venue and operational
    requirements.
19. Bindings are append-only. An identical retry converges; any changed tier,
    profile, copied account fact, or policy version conflicts. The account
    foreign key is restrictive and a single account cannot silently change
    capital policy.
20. The matrix and retained schema-74 rehearsal use one separately created,
    explicit OVR-101 account per tier/profile identity. They never rewrite an
    account's starting capital, multiplier, margin profile, namespace, or
    evidence class. The retained six scored accounts use the reviewed Reg-T
    profile; the seventh uses stress/unlimited. Cash and portfolio-style
    behavior is proved with additional isolated test accounts. Migration 74
    seeds no account and binds no existing account automatically.
21. The existing OVR-101 test that adds flows from an original `$500` account
    to `$5m` remains valid capital-history evidence, but it is not reused as six
    simultaneous replay identities. Historical deposits never reinterpret that
    account's immutable starting-capital fact.

### Assessment and replay matrix

22. `StateFromProjection` is the sole v1 state constructor. It consumes one
    complete OVR-104 portfolio projection plus exactly one canonical OVR-201
    instrument identity for every open projected position. It verifies account
    ID, currency, checkpoint/payload identity when present, position IDs,
    quantities, market values, projection totals, and supported asset class.
    It derives long exposure from positive market values, short exposure as the
    absolute value of negative market values, gross as their sum, and current
    maintenance requirement from the restored profile. Callers cannot supply
    or override any of those derived values.
23. An assessment request contains one validated account, exact binding,
    restored policy, builder-produced point-in-time state, canonical proposed
    equity/ETF instrument, proposed signed long or short exposure
    increase/reduction, and a stable scenario ID. It does not accept share
    price and quantity as a second notional-calculation path; the caller
    supplies the exact canonical notional already derived from instrument
    mechanics and quote evidence.
24. The derived state requires cash, equity, long exposure, short exposure,
    gross exposure, and maintenance requirement to agree exactly
    (`gross = long + short`) and to be nonnegative except cash may be negative
    for finite margin profiles. State, projection, instruments, proposal, and
    account currency must agree.
25. Assessment identity/hash includes policy version, binding ID, tier,
    profile, mode/evidence/namespace, scenario ID, all state fields, direction,
    exact proposed notional, requirements, headroom, decision, and reason.
    Therefore changing capital or profile assumptions changes evidence even if
    the eventual fill is identical.
26. A rejection is a normal canonical result. Malformed/mismatched input is an
    error and produces no assessment. Callers must not conflate inapplicability
    at one tier with an engine failure.
27. The matrix runner receives one deterministic scenario factory and invokes
    it for each declared scored tier in numeric order, then once for stress.
    It returns all seven labeled outcomes in that order. The factory may size a
    proposal from the supplied tier but may not change dates, signals, market
    observations, simulation policy, or random seed across tiers.
28. Scored matrix outcomes can be compared only within the scored population.
    The stress outcome has a separate environment, evidence class, namespace,
    and assessment hash and is never included in a scored pass/fail aggregate.
29. A golden admitted result continues through the unmodified OVR-204 venue.
    Capital assessment evidence accompanies the replay result but does not
    alter quote, latency, depth, fee, fill, or OVR-203 transition rules.

## File plan

- Create `internal/capital/policy.go` and `policy_test.go`: exact v1 policy,
  canonical bytes, artifact identity, restoration, and fixed policy builder.
- Create `internal/capital/binding.go` and `binding_test.go`: account/tier/profile
  binding validation, deterministic identity, and payload equality.
- Create `internal/capital/assessment.go` and `assessment_test.go`: exact state,
  pre-route margin admission, reason codes, canonical evidence, and hashing.
- Create `internal/capital/state.go` and `state_test.go`: the sole projection and
  canonical-instrument-to-capital-state builder, including fail-closed
  unsupported-asset handling.
- Create `internal/capital/matrix.go` and `matrix_test.go`: ordered six-tier plus
  stress replay harness and isolation checks.
- Create `internal/capital/golden_replay_test.go`: capital admission followed by
  common backtest/paper simulation parity.
- Create `internal/repository/postgres/capital_policy.go` and tests: immutable
  artifact/binding register, reload, replay, conflict, and concurrency.
- Extend `internal/repository/interfaces.go` with narrow capital-policy
  artifact and binding interfaces only.
- Create migration 74 up/down SQL and migration tests. Bump the required schema
  only after isolated migration tests pass.
- Create `docs/runbooks/capital-margin-policy.md` and update ADR-018 plus
  the total overhaul plan after implementation evidence exists.

## Task 1: RED policy and artifact contract

- [x] Test exact tier/profile vocabulary, canonical ordering, fixed decimals,
  version/digest/UUID identity, defensive copies, and round-trip restoration.
- [x] Test every missing, duplicate, extra, unknown, negative, over-scale,
  inconsistent, or unbounded finite field fails closed.
- [x] Test the fixed v1 builder produces the reviewed canonical bytes and no
  hidden default can change identity.
- [x] Implement the minimum policy/artifact contract.

Run:

```bash
go test -count=1 ./internal/capital -run 'Policy|Artifact'
```

## Task 2: RED account binding contract

- [x] Test all six scored tiers against cash, Reg-T-like, and portfolio-style
  matching accounts.
- [x] Test stress/unlimited isolation and reject scored unlimited, stress finite,
  zero finite multiplier, wrong evidence/namespace/currency, and shadow/live.
- [x] Test deterministic identity, defensive copies, exact replay, and every
  changed account/policy/tier/profile fact.
- [x] Implement the minimum binding contract.

Run:

```bash
go test -count=1 ./internal/capital -run 'Binding|Tier'
```

## Task 3: RED exact margin assessment

- [x] Test state derivation from an exact empty and nonempty OVR-104 projection,
  long/short/gross/maintenance calculations, and deterministic output.
- [x] Reject missing/duplicate/wrong instruments, mismatched projection/account/
  currency/payload, caller-authored totals, and every non-equity/ETF or mixed
  unsupported portfolio.
- [x] Test cash long admission/rejection, cash short rejection, Reg-T long/short,
  portfolio-style haircuts, gross limit, maintenance deficiency, reserve,
  exposure reduction, and stress-unbounded results.
- [x] Test exact boundary equality, upward rounding, very small/large decimals,
  changed cents, and no float conversion.
- [x] Test malformed state, gross mismatch, cross-currency, policy/binding/account
  mismatch, unknown direction, zero entry, and invalid reduction fail closed.
- [x] Test canonical bytes/hash and stable decision/reason vocabulary.
- [x] Implement the pure assessment engine.

Run:

```bash
go test -race -count=1 ./internal/capital -run 'Assess|Margin|BuyingPower'
```

## Task 4: RED six-tier and stress replay matrix

- [x] Prove exactly seven ordered invocations with the same scenario identity,
  dates, market input digest, simulation policy, and seed.
- [x] Prove normal rejections remain labeled results and errors stop the matrix
  without returning a partial success claim.
- [x] Prove a proportional replay runs at all six scored tiers while an absolute
  over-capacity replay rejects smaller tiers and admits larger tiers.
- [x] Prove stress is structurally excluded from scored aggregation and hashes.
- [x] Implement the matrix harness.

Run:

```bash
go test -race -count=1 ./internal/capital -run 'Matrix|Replay'
```

## Task 5: Migration 74 and PostgreSQL repository

- [x] Add source-shape RED tests for artifact/binding tables, canonical-byte
  reconstruction, append-only triggers, deterministic IDs, account agreement,
  complete lock sets, no grants/activation, and empty-only rollback.
- [x] Add direct PostgreSQL tests for forged bytes/JSON/hash/version/UUID,
  missing/extra tiers or profiles, invalid decimals, mismatched accounts,
  scored/stress contamination, changed identity reuse, mutation, and deletion.
- [x] Add repository tests for exact register/load/reconstruct/bind/reload,
  idempotency conflict, eight-writer convergence, and changed-payload races.
- [x] Create the six scored Reg-T and one stress/unlimited rehearsal accounts
  explicitly through the existing OVR-101 account repository. Prove every
  account has its own opening capital flow and immutable namespace/profile;
  migration 74 itself creates none.
- [x] Race migration up with an account/binding attempt and migration down with
  artifact/binding insert; prove serial lock behavior and no orphan facts.
- [x] Prove nonempty downgrade refuses and empty `74 -> 73 -> 74` preserves
  schema 73 exactly.
- [x] Bump `RequiredSchemaVersion` only after the isolated suite passes.

Run:

```bash
DB_URL="$AUGR_PHASE2_DB_URL" go test -race -count=1 \
  -run 'CapitalPolicy|CapitalBinding|Migration74' \
  ./internal/repository/postgres ./migrations
```

## Task 6: Common simulation golden replay

- [x] Build one canonical scenario whose capital assessment is performed before
  route, then run the existing OVR-204 backtest and paper adapters unchanged.
- [x] Prove their lifecycle transitions, fills, economics, and simulation outcome
  hashes match within the same account mode and capital profile.
- [x] Prove a rejected assessment creates no routed order, fill, normalization,
  ledger transaction, or simulation outcome.
- [x] Prove changing only tier/profile changes capital evidence while unchanged
  quote/fill inputs remain byte-identical after admission.
- [x] Persist/reload the policy, binding, and admitted lifecycle through real
  PostgreSQL; restart and replay without duplication.

Run:

```bash
go test -race -count=1 ./internal/capital ./internal/simulation \
  ./internal/backtest ./internal/execution/paper
DB_URL="$AUGR_PHASE2_DB_URL" go test -race -count=1 \
  -run 'CapitalGoldenReplay' ./internal/repository/postgres
```

## Task 7: Qualification, documentation, review, and synchronization

- [x] Document policy semantics, approximation limits, binding inspection,
  scored/stress isolation, replay matrix usage, incident reasons, empty-only
  rollback, and the no-cutover boundary.
- [x] Apply migrations `1 -> 74` to a dedicated loopback-only database, create
  six explicit scored Reg-T accounts and one explicit stress/unlimited account,
  retain one policy plus all seven bindings, reload all
  evidence, prove nonempty rollback refusal, and prove empty `74 -> 73 -> 74`
  separately.
- [x] Run focused races and repository-wide gates:

  ```bash
  go test -race -count=1 ./internal/capital ./internal/simulation \
    ./internal/backtest ./internal/execution/paper
  DB_URL="$AUGR_PHASE2_DB_URL" go test -race -count=1 \
    -run 'CapitalPolicy|CapitalBinding|CapitalGoldenReplay|Migration74' \
    ./internal/repository/postgres ./migrations
  task test:race
  task build
  task vet
  task lint
  task fmt:check
  govulncheck ./...
  ```

- [x] Run pinned Node 22 frontend install/test/lint/build gates; classify inherited
  dependency advisories separately.
- [x] Start the rebuilt binary only with the global kill switch active, live
  trading and scheduler false, no provider credentials, isolated schema-74
  PostgreSQL, and isolated Redis. Check all health routes, stop cleanly, and
  prove retained evidence unchanged.
- [x] Obtain independent final diff approval with no unresolved P0/P1.
- [x] Commit verified slices, push `codex/augr-overhaul`, fetch, prove local and
  remote hashes equal with `0 0` divergence, then begin OVR-207.

## Acceptance evidence to record after implementation

- [x] One exact content-addressed policy defines all six tiers and four profile
  semantics and reconstructs independently in Go and PostgreSQL.
- [x] Account bindings are immutable, replay-safe, and cannot mix scored and
  stress evidence or reinterpret zero buying power.
- [x] Cash, Reg-T-like, portfolio-style, and stress/unlimited assessments use
  exact decimals and stable admitted/rejected evidence without claiming broker
  parity.
- [x] The same deterministic replay scenario runs at `$500` through `$5m` plus
  stress, retaining valid tier-specific capacity outcomes.
- [x] Backtest and internal paper produce identical downstream OVR-204 outcomes
  after the same admitted assessment; rejection produces no order/economics.
- [x] Migration 74 is additive, immutable, empty-only reversible, and activates
  no runtime path or grant.
- [x] Focused races, real PostgreSQL, full backend/frontend gates, kill-switched
  startup, independent review, commits, push, and synchronization are recorded
  honestly with inherited failures separate.

## Closure evidence (2026-08-20)

- Implementation is split across commits `1d09985` through `d7f03e1`, following
  the reviewed plan in `14fc20c`. The runtime fixes ensure both the normal and
  smoke schedulers, as well as automation, remain stopped when
  `ENABLE_SCHEDULER=false`.
- A dedicated loopback PostgreSQL database applied migrations `1 -> 74` and
  retained one exact policy, seven explicit accounts, seven bindings, and seven
  opening-capital flows. Nonempty rollback refused with evidence intact. A
  separate empty database completed `74 -> 73 -> 74`.
- Focused capital/simulation/backtest/paper races passed 522 tests. Focused real
  PostgreSQL race suites passed 10 tests. Repository-wide `task test:race`,
  `task build`, `task vet`, `task lint`, and `task fmt:check` passed.
- `govulncheck ./...` reports no called vulnerabilities after updating the Go
  dependency graph. Pinned Node `v22.23.2` install, 162 frontend tests, lint,
  and production build passed. Non-breaking lockfile updates removed all seven
  high-severity npm findings; one low-severity transitive `esbuild` Windows
  development-server advisory remains because the compatible dependency graph
  still selects `0.27.4`. The qualified Linux production build does not invoke
  that development server.
- The rebuilt app started only against isolated schema-74 PostgreSQL and Redis,
  with the global kill switch active, live trading and schedulers disabled,
  paper/dry-run modes enabled, and no provider credentials. `/health`,
  `/healthz`, and `/api/v1/health` all reported database and Redis healthy. A
  clean interrupt completed shutdown and retained row counts remained
  `1|7|7|7`.
- Independent final review found no unresolved P0/P1 findings.
- Evidence status: **VERIFIED_LOCAL**. No shared or production database was
  migrated, no provider or broker was called, and no deployment or cutover was
  performed.
