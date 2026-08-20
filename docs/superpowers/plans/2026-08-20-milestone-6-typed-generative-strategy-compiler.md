# Milestone 6 Typed Generative Strategy Compiler Plan

**Goal:** Complete OVR-601 with a constrained, deterministic compiler that
turns a typed generated strategy specification into one immutable OVR-302
strategy version, while invalid or nondeterministic output can never become a
deployment.

**Architecture:** Add a `generativestrategy` boundary and migration 95. A
generated specification pins one existing OVR-302 family, typed/fresh inputs,
explicit universe and benchmark, a closed boolean/arithmetic rule AST,
deterministic sizing and exits, prohibited behaviors, cost/capacity assumptions,
property/example tests, retirement criteria, and authoring model/prompt/token/
cost provenance. The compiler normalizes order-insensitive sets, rejects
unknown nodes/fields and ambiguous numeric or time representations, emits one
canonical OVR-302 `strategycatalog.Version` config with fixed compiler and
decision-contract identity, and records a compilation receipt binding exact
spec/version digests. It exposes no deployment constructor, lifecycle
transition, scheduler, intent, or order path.

**Migration:** `000095_typed_generative_strategy_compiler`

## Locked contracts

1. A spec binds one nonnil OVR-302 family ID and one stable spec key. It cannot
   create, rename, or mutate a family.
2. Inputs have unique normalized names, reviewed scalar types, exact OVR-301
   dataset kinds, positive bounded freshness, and explicit missing-data policy.
   Missing or stale required inputs always abstain.
3. Universe uses one reviewed asset class, a sorted nonempty fixed instrument
   set, and one explicit benchmark instrument. Dynamic discovery, implicit
   watchlists, and future membership are not compiler inputs.
4. Entry and exit are closed typed expression trees: exact decimal literals,
   input references, arithmetic, comparisons, and boolean conjunction/
   disjunction/negation only. Unknown operators, type mismatch, divide-by-zero
   literals, unbound inputs, excessive depth/nodes, and noncanonical decimals
   fail compilation.
5. Sizing is one deterministic reviewed mode with exact positive bounds.
   Randomness, wall-clock reads, network/provider calls, reflection, source
   snippets, shell text, and arbitrary functions are unrepresentable.
6. Every spec declares maximum holding period, explicit exit, cost and capacity
   assumptions, hard prohibited behaviors, at least one property test, at least
   one example test, and measurable retirement criteria.
7. Required prohibitions include live-order submission, risk-limit mutation,
   evidence mutation, promotion, secret access, network access, and lookahead.
   The compiler may add restrictions but never remove these.
8. Authoring provenance requires normalized provider/model identity, exact
   prompt SHA-256, nonnegative input/output token counts, currency, and exact
   nonnegative monetary cost. Raw prompts are not stored in OVR-601.
9. Canonical permutation of inputs, instruments, prohibitions, and tests yields
   one spec ID/SHA-256 and byte-identical compiled config/version. Any semantic
   edit creates a new spec and version identity.
10. Compilation is all-or-nothing. A receipt is `compiled` only when the
    generated OVR-302 version reconstructs from the exact config and pins the
    expected compiler/source/decision/data contract. Invalid specs return no
    version and no receipt.
11. Persisted specs, normalized children, receipts, and version bindings are
    append-only and independently reconstructable. Exact retries converge;
    changed payload under one family/spec key conflicts.
12. No API or repository in OVR-601 can propose/activate a deployment, create an
    experiment, generate an intent, schedule work, deploy, or trade live.

## Task 1: Typed specification and expression AST

- [ ] Implement canonical inputs, universe/benchmark, typed closed expressions,
  sizing/exits, costs/capacity, prohibitions, tests, retirement, and provenance.
- [ ] Prove malformed, unbound, type-invalid, nondeterministic, lookahead-capable,
  noncanonical, oversized, or incomplete specifications fail closed.
- [ ] Prove permutation convergence, semantic-edit identity change, clone-safe
  getters, tamper rejection, and exact canonical restoration.
- [ ] Commit and push the typed-specification slice.

## Task 2: Deterministic OVR-302 compiler

- [ ] Compile one valid spec into canonical OVR-302 configuration and one exact
  immutable `strategycatalog.Version` with fixed compiler/source/decision pins.
- [ ] Bind a content-addressed receipt to family/spec/version IDs and digests;
  reconstruct compiled bytes independently and expose no deployment authority.
- [ ] Prove repeated/concurrent compilation is byte-identical, all semantic
  edits change identity, invalid inputs return no output, and output cannot claim
  active/proposed/deployed state.
- [ ] Commit and push the compiler slice.

## Task 3: Migration 95 and retained qualification

- [ ] Persist immutable spec parents/children and compilation receipts with
  exact OVR-302 family/version scope and PostgreSQL reconstruction.
- [ ] Prove eight-writer convergence, changed retry conflict, every-stage atomic
  rollback, forgery rejection, append-only evidence, nonempty rollback refusal,
  and empty `95 -> 94 -> 95`.
- [ ] Retain one valid compilation and explicit invalid/nondeterministic
  no-artifact attempts without fabricating rejected deployment records.
- [ ] Add an inspection/recovery/rollback runbook with exact IDs and digests.
- [ ] Run focused/database races, repository-wide backend/static and pinned
  frontend gates, diff review, and isolated kill-switched schema-95 health/API/
  rollback/backup/restore/reapply.
- [ ] Commit/push verified slices, fetch, and prove `0 0` before OVR-602.

## Acceptance evidence to record

- [ ] One generated spec deterministically reconstructs its exact typed config,
  OVR-302 version, and compilation receipt under repeated and concurrent use.
- [ ] Invalid or nondeterministic output yields no spec/version/receipt and no
  path can create or activate a deployment.
- [ ] Local qualification is `VERIFIED_LOCAL`; model invocation, source/search
  lineage, independent review, experiment declaration/execution, shared
  migration, scheduling, deployment, broker routing, and live trading remain
  `BLOCKED_EXTERNAL`.
