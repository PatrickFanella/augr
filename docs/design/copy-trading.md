---
title: "Stock Leader Following and Copy Trading"
date: 2026-08-14
status: implemented-mvp
tags: [stocks, copy-trading, sec-edgar, form-4, 13f, execution, risk]
---

# Stock Leader Following and Copy Trading

## Implementation status

The institutional 13F paper-replication MVP described below is implemented in migration `000063`, the authenticated `/api/v1/copy-trading` API, the Copy Trading operator screen, and the `copy_13f_sync` automation job. Form 4 individuals, connected stock accounts, and connected Kalshi accounts remain sequenced follow-on work; their shared leader/source vocabulary is reserved, but they are not executable source types yet.

## Decision Summary

Build a stock-first feature that lets an operator follow an individual or institution and convert observed activity into a paper portfolio under Augr's existing execution and risk controls.

The product must distinguish three behaviors:

1. **Copy** an explicitly connected brokerage account from fills or positions.
2. **Follow** an individual insider's disclosed transactions from SEC Forms 3/4/5.
3. **Replicate** an institution's disclosed holdings from Form 13F.

These sources have materially different timing and completeness. The UI, database, and performance reporting must preserve the distinction instead of describing all three as real-time copy trading.

Polymarket is out of scope. Kalshi may use the same generic source/subscription framework later, but the official public trade feed does not identify the trader. An arbitrary Kalshi person or institution therefore cannot be copied from public trades; a future Kalshi source must be an explicitly connected account.

## Current-State Findings

### What Augr already has

- Alpaca stock execution, including paper mode.
- Centralized kill switch, circuit breakers, sizing, exposure checks, order/fill/position persistence, reconciliation, and audit logging.
- A rate-limited SEC EDGAR client and a provider that can list filing metadata by company ticker.
- Stock market data, market-hours handling, schedulers, automation, and frontend order/portfolio views.

### What is missing

- EDGAR discovery by reporting person or institutional manager.
- Parsing and normalizing Form 4 ownership XML and 13F information tables.
- A leader/source directory and durable subscription lifecycle.
- Deterministic source-event or portfolio-snapshot to follower-target calculations.
- Copy-specific provenance and position attribution.
- Multi-account or source-account authorization for true real-time copying.

The current EDGAR provider is useful plumbing but is not a copy-trading feed. It resolves a stock ticker to the issuer CIK and returns filing metadata; it does not ingest a manager's 13F holdings or an insider's reported transactions.

### Existing wallet code

The existing wallet feature is Polymarket-specific:

- `polymarket_accounts` and `polymarket_account_trades` store public wallet profiles and activity.
- Automation refreshes profiles and flags high-performing accounts as `tracked`.
- `internal/research/walletintel` scores research samples.
- the whale signal source emits tracked-wallet activity into signal intelligence;
- Polymarket discovery uses wallet evidence when evaluating markets.

There is no automatic copy-execution path. The wallet scoring work was explicitly designed as research-only. Because Polymarket is no longer a product venue, this code should remain isolated and be deprecated or removed in a separate cleanup. Do not migrate its address-based schema into the stock feature.

## Product Model

### Leader

A `leader` is the person or organization the operator wants to follow.

Examples:

- an officer, director, or 10% owner reporting on Form 4;
- an institutional investment manager filing Form 13F;
- a consenting trader or institution publishing activity through a connected broker account.

A leader may have multiple sources. Identity is separate from a provider-specific identifier so filings, broker connections, and later data vendors can be linked without collapsing provenance.

### Source types

| Source | Entity | Input | Typical freshness | Product term |
| --- | --- | --- | --- | --- |
| `connected_broker` | Individual or institution | Authorized fills and positions | Seconds/minutes | Copy |
| `sec_form4` | Corporate insider | Disclosed transactions | Usually after execution; filing generally due within two business days | Follow |
| `sec_13f` | Institutional manager | Quarter-end holdings snapshot | Filing may arrive up to 45 days after quarter end | Replicate |
| `kalshi_connected` | Consenting Kalshi member | Authenticated fills/positions | Seconds/minutes | Copy, later |

Every observation stores and displays:

- `effective_at`: when the source transaction occurred or snapshot was measured;
- `published_at`: when the filing/provider made it available;
- `observed_at`: when Augr ingested it;
- `age_at_decision`: the delay before Augr considered a follower action.

### Subscription lifecycle

```text
draft -> previewed -> paper_active -> paused -> live_eligible -> live_active -> stopped
```

- New subscriptions are always paper subscriptions.
- Preview shows what the policy would have done against recent source data.
- `live_eligible` is earned through an evaluation gate, not directly selected.
- Pause creates no new entries or exits; source observations continue so drift is visible.
- Stop is terminal and does not silently liquidate attributed positions.

## Goals

- Search for or add an individual/institution and inspect its source, identity, history, freshness, and limitations.
- Create a capped paper subscription with explicit sizing and eligibility rules.
- Deterministically turn source disclosures/fills/snapshots into target stock positions.
- Apply all existing execution, liquidity, market-hours, risk, live-gate, reconciliation, and audit controls.
- Attribute each follower position, order, and fill to one subscription and source observation.
- Explain every action, skip, rejection, amendment, and drift condition.
- Support both transaction-oriented and portfolio-oriented sources under one coherent framework.

## Non-Goals

- Claiming delayed public disclosures are real-time trades or a complete portfolio.
- Copying an unconnected retail brokerage account from social posts or unverified claims.
- Public leader monetization, subscriptions, custody, pooled funds, or social networking.
- Options, shorting, leverage, OTC/penny stocks, fractional source reconstruction, or tax-lot matching in the MVP.
- Allowing a leader to bypass Augr risk controls.
- Polymarket ingestion or execution.
- Copying arbitrary Kalshi users from the anonymized public trade feed.

## Recommended Release Sequence

### MVP: institutional 13F replication in paper mode

Start with one institutional manager per subscription and a long-only, top-holdings target portfolio.

Why this is the best first slice:

- public, authoritative source;
- snapshot-to-target reconciliation fits the portfolio system better than attempting to reconstruct missing trades;
- deterministic and easy to replay/backtest;
- lower latency pressure than real-time copy execution;
- exercises the reusable leader, source, subscription, attribution, and drift model.

MVP behavior:

- ingest original 13F-HR and amendment filings;
- parse the information table and retain accession, reporting period, filed time, CUSIP, issuer, class, value, shares/principal, discretion, voting authority, and amendment state;
- map supported holdings to canonical Augr stock instruments with a confidence state;
- construct a target from the top N mapped long-equity holdings or holdings above a configured weight;
- normalize included holdings to the subscription's capital budget;
- rebalance in paper mode using turnover, position, sector, liquidity, and spread caps;
- retain a cash buffer for excluded/unmapped holdings rather than renormalizing silently;
- never trade options or infer shorts from omissions;
- mark the portfolio as based on a delayed quarter-end snapshot on every screen.

Default policy:

- top 10 supported long-equity holdings;
- $10,000 paper allocation;
- 10% cash buffer plus cash representing unmapped/excluded disclosure weight;
- maximum 15% follower weight per stock;
- maximum 25% one-day turnover;
- minimum price and average-dollar-volume filters;
- limit orders during regular market hours;
- no live mode.

### Phase 2: insider Form 4 following

Add individual leaders using Form 4 transaction data. Only transaction codes and ownership forms explicitly approved by policy become candidates.

Recommended initial inclusion:

- open-market or private purchase code `P`;
- open-market or private sale code `S`, limited to reducing subscription-attributed holdings.

Default exclusions:

- grants/awards, option exercises, tax withholding, gifts, conversions, transfers, and other non-discretionary or ambiguous codes;
- derivative transactions;
- indirect ownership that cannot be interpreted reliably;
- late or amended filings until amendment semantics are resolved.

Use fixed notional or conviction bands, not the insider's reported dollar amount as an exact follower size. Insider wealth and total exposure are unknown, and a reported sale may be liquidity-, tax-, or plan-driven rather than an investment view.

### Phase 3: connected stock accounts

Add true copy trading for consenting leaders through a supported broker integration.

Requirements before implementation:

- source-owner consent and revocation;
- OAuth or equivalent scoped authorization; never request a source's raw brokerage password;
- separate source credentials from follower execution credentials;
- source fills plus periodic position snapshots;
- tenant/account isolation;
- source correction/cancel semantics;
- privacy and retention policy;
- product-specific legal/compliance approval.

This phase uses event-driven copying with snapshot reconciliation. It should ship broker-by-broker, beginning with Alpaca only if its current integration and commercial terms support the source-account model.

### Phase 4: connected Kalshi accounts

Reuse the leader/source/subscription/intent model for a consenting Kalshi account. Do not call it wallet tracking.

- Public Kalshi trades can support market-flow signals but cannot identify a leader.
- Authenticated portfolio fills are scoped to the logged-in member, so the source must explicitly connect credentials or publish a signed/authorized feed.
- Kalshi-specific position targets, binary-outcome risk, contract rounding, fee, settlement, and market-closure rules remain venue-specific.
- This is independent of the stock MVP and must not delay it.

## Architecture

```text
SEC filings / connected broker fills and positions
                         |
                         v
                 Source adapters
          identity + normalize + checkpoint
                         |
                         v
             Durable source observations
          transactions or portfolio snapshots
                         |
                         v
              Subscription target engine
          sizing + mapping + attribution + delta
                         |
                         v
               Copy policy evaluator
     freshness, eligibility, liquidity, turnover, caps
                         |
                         v
                   Trade intent
                         |
                         v
        Existing risk engine and OrderManager
                         |
                         v
          Alpaca paper -> order/fill/position/audit
                         |
                         v
             Drift and amendment reconciler
```

### Boundaries

- `LeaderSourceAdapter` ingests provider data; it cannot trade.
- `InstrumentMapper` resolves CUSIP/provider instruments to Augr instruments and preserves mapping confidence/history.
- `TargetCalculator` is pure and versioned. The same observation, policy, portfolio snapshot, and price snapshot produce the same target/delta.
- `CopyPolicyEvaluator` may reject or reduce a delta but cannot bypass global risk.
- `CopyExecutor` submits approved intents through the existing order lifecycle.
- `CopyReconciler` compares target, attributed local position, open orders, and broker truth. It creates bounded intents rather than editing positions.

Do not route disclosures through the LLM agent debate before execution. Copy/replication must be deterministic. LLMs may summarize a leader or explain a filing in a separate research surface, but they cannot transform or approve the order.

## Data Model

Add a migration after `000062`.

### `leaders`

- `id`, `entity_type` (`individual`, `institution`), `display_name`;
- optional public identifiers such as SEC CIK;
- `identity_status` (`unverified`, `public_filing_verified`, `connected_verified`);
- metadata, created/updated timestamps.

### `leader_sources`

- leader relation, provider, source type, external key;
- status and freshness;
- provider metadata without credentials;
- last checkpoint and last observed time;
- unique `(provider, source_type, external_key)`.

Credentials for connected sources live in the existing encrypted secret system or a dedicated secret reference, never in source metadata.

### `source_observations`

One immutable envelope for either a transaction or snapshot:

- provider observation ID/accession and content hash;
- observation kind and schema version;
- effective, published, observed timestamps;
- original/amendment/superseded state;
- normalized payload plus a retained raw/source reference;
- unique provider identity to make retries and EDGAR reprocessing idempotent.

Use child tables for queryable data:

- `source_transactions` for Form 4 and connected fills;
- `source_portfolio_snapshots` and `source_portfolio_holdings` for 13F and connected positions.

### `instrument_mappings`

Versioned provider identifier to canonical instrument mappings:

- provider identifier type/value, ticker at mapping time, canonical instrument key;
- confidence and mapping method;
- validity interval and review status.

No intent may trade an ambiguous or stale mapping.

### `copy_subscriptions`

- leader/source relation, status, paper/live mode;
- method (`target_weight`, `fixed_notional`, later `source_ratio`);
- capital budget, cash buffer, top-N/min-weight selection;
- max position weight/notional, max turnover, min price/liquidity, max spread/slippage;
- stock/sector allowlists and blocklists;
- created by and lifecycle timestamps.

### `copy_trade_intents`

- subscription and source observation IDs;
- canonical instrument, side, target weight/value, attributed current value, requested delta;
- observed/executable price and market-data timestamp;
- calculation version and complete sizing explanation;
- policy and global-risk status/reasons;
- linked order/trade-decision IDs and terminal status;
- unique `(subscription_id, source_observation_id, instrument_key, calculation_version)`.

### Execution attribution

Add a generic execution origin rather than creating fake strategies or pipeline runs:

```text
origin_type: strategy | copy_subscription | manual | reconciliation
origin_id: UUID
source_observation_id: nullable UUID
copy_intent_id: nullable UUID
```

The current `OrderManager.ProcessSignal` requires strategy and run UUIDs even though order fields are nullable. Refactor it behind a generic execution request/origin contract, keeping the existing strategy path as an adapter. This is a prerequisite for clean copy execution.

Positions must be attributable by origin so a copied sell can never liquidate stock owned by another strategy or manual activity.

## Calculation Rules

### 13F target portfolio

```text
disclosed_weight = holding_value / total_disclosed_value
eligible_weight  = disclosed_weight when mapping and policy pass, otherwise cash
raw_target       = eligible_weight * subscription_budget
target           = clamp(raw_target, position and concentration caps)
order_delta      = target - attributed_position_value - value_of_open_orders
```

Do not redistribute excluded/unmapped weight across remaining names without explicit policy. Otherwise a 13F with unsupported holdings can unintentionally concentrate the follower.

Treat a missing holding in the next original filing as a target of zero. Treat amendments by superseding the affected snapshot and calculating the difference; never replay the entire amended filing as new purchases.

### Form 4 transaction

```text
candidate_notional = configured fixed notional or bounded policy band
buy_delta          = min(candidate_notional, remaining subscription/position caps)
sell_delta         = min(candidate_notional, subscription-attributed holding)
```

Skip when the filing is too old, amended/ambiguous, non-discretionary, derivative-based, below the liquidity threshold, or outside the permitted transaction-code set.

### Connected broker

For source increases, calculate either a fixed-notional delta, a capped source-trade ratio, or a target weight from the source position snapshot. For source decreases, apply the source reduction ratio to only the subscription-attributed position. Snapshot reconciliation repairs missed or reordered events.

## API

Add authenticated endpoints under `/api/v1/copy-trading`:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET/POST` | `/leaders` | Search/add leaders |
| `GET` | `/leaders/{id}` | Identity, sources, history, freshness |
| `POST` | `/leaders/{id}/sources` | Add/validate a filing or connected source |
| `GET/POST` | `/subscriptions` | List/create draft subscriptions |
| `GET/PUT` | `/subscriptions/{id}` | Detail and edit while draft/paused |
| `POST` | `/subscriptions/{id}/preview` | Historical/current dry-run preview |
| `POST` | `/subscriptions/{id}/activate` | Activate paper mode |
| `POST` | `/subscriptions/{id}/pause` | Freeze new intents |
| `POST` | `/subscriptions/{id}/resume` | Reconcile then resume |
| `POST` | `/subscriptions/{id}/stop` | Terminal stop; no implicit liquidation |
| `POST` | `/subscriptions/{id}/reconcile` | Request bounded reconciliation |
| `GET` | `/subscriptions/{id}/intents` | Explain action/skip/rejection history |

WebSocket events: `source_observed`, `copy_intent`, `copy_ordered`, `copy_skipped`, `copy_drift`, and `copy_subscription_status`.

## UI

Add `/copy-trading` under Operations.

Leader directory/detail:

- person/institution identity and verification;
- source type and a plain-language freshness warning;
- filing/observation history;
- holdings or transactions;
- concentration, turnover, observed drawdown/performance when supportable;
- source limitations and amendment state.

Subscription flow:

1. Select leader and source.
2. Choose capital budget and method.
3. Configure top N, cash buffer, caps, liquidity, and allow/block rules.
4. Preview the latest observation and a historical backtest.
5. Review estimated holdings, turnover, exclusions, and worst-case limits.
6. Create and activate in paper mode.

Subscription detail shows source snapshot date, publication delay, next expected update, target versus actual holdings, cash, drift, orders/fills, P&L, and an intent timeline from filing to fill.

Use **Copy**, **Follow**, or **Replicate** based on source type. Never show “live” or “real-time” for Form 4/13F data.

## Risk and Compliance Gates

- Paper-only through the first two phases.
- Long-only supported exchange-listed equities for the initial release.
- Every order passes current kill switch, circuit breakers, position/exposure checks, and Alpaca live gate.
- Add copy-specific budget, concentration, liquidity, spread, slippage, turnover, stale-source, stale-price, and mapping-confidence gates.
- Fail closed when a filing is incomplete, amended ambiguously, mapping is uncertain, current market data is stale, audit persistence fails, or portfolio attribution is unavailable.
- Require explicit action for liquidation; pause/stop does not sell automatically.
- Record every source correction, configuration change, preview, transition, intent, risk decision, order, and reconciliation.
- Before any user-facing live securities copy feature, complete legal/compliance review for adviser/broker-dealer implications, disclosures and consent, source compensation/conflicts, communications, best execution, account-level controls, recordkeeping, privacy, and jurisdiction limits.

## Observability and Evaluation

Track:

- ingestion lag from effective to published to observed;
- filings/snapshots/fills ingested, deduplicated, amended, skipped, and failed;
- mapping coverage and ambiguous/unmapped portfolio weight;
- intent outcomes and reasons;
- target versus attributed portfolio drift;
- turnover, cash, exposure, P&L, drawdown, and tracking error;
- decision-to-order and order-to-fill latency/slippage;
- reconciliation actions and accounting mismatches.

Paper evaluation must compare:

- naive disclosure return from the effective date;
- achievable follower return from the first executable time after publication and ingestion;
- follower return after mapping exclusions, spread, slippage, and caps;
- Augr's existing strategies or a simple benchmark over the same period.

This avoids overstating results with look-ahead bias.

## Delivery Plan

### Phase 0: contracts and data proof

- Capture representative 13F-HR, 13F-HR/A, Form 4, and Form 4/A fixtures.
- Define identity, observation, amendment, mapping, target, attribution, and lifecycle contracts.
- Prove CUSIP/instrument mapping coverage on a representative manager sample.
- Build table-driven and property tests for target weights, exclusions, caps, amendments, and sells.
- Decide the Polymarket code retirement plan separately.

Exit: an agreed fixture set produces deterministic normalized observations and expected target portfolios.

### Phase 1: 13F paper MVP

- Extend EDGAR for manager CIK discovery, filing polling, document retrieval, and information-table parsing.
- Add new schema, repositories, leader/source APIs, and instrument mapping.
- Implement 13F target construction, bounded rebalance intents, generic execution origin, and position attribution.
- Execute through Alpaca paper, existing global risk, and normal fill/position/audit lifecycle.
- Add restart, deduplication, amendment, reconciliation, and integration tests.

Exit: one manager can be replicated across multiple filing periods with no duplicate orders, no look-ahead, and full filing-to-fill provenance.

### Phase 2: UI and Form 4 individuals

- Build leader directory, subscription preview/detail, target/actual portfolio, and intent timeline.
- Add Form 4 parser and strict transaction-code policy.
- Add historical simulation and paper evaluation reporting.
- Add pause/resume/stop and bounded manual reconciliation.

Exit: operators can follow eligible insider purchases/sales in paper mode and understand why every reported transaction did or did not create an order.

### Phase 3: connected stock sources and live gate

- Validate a supported consented broker-source integration.
- Add authorization, revocation, privacy, multi-account isolation, fill ingestion, and snapshot reconciliation.
- Establish paper thresholds for uptime, duplicates, tracking error, slippage, drift, accounting, drawdown, and rollback.
- Add allowlisted canary live subscriptions with conservative caps only after compliance approval.

### Phase 4: connected Kalshi sources

- Add only if a consenting-source account model is approved.
- Keep Kalshi ingestion, mapping, sizing, risk, fees, settlement, and execution venue-specific behind the common source/subscription interfaces.

## MVP Acceptance Criteria

- A leader or observed filing alone cannot create an order; only an active paper subscription can.
- Original and amended filings are idempotent and supersession-safe.
- Every 13F order uses information available only after the filing was published and ingested.
- Unmapped/excluded holdings remain cash weight unless the operator explicitly chooses another policy.
- Every target, skip, reduction, and rejection has a deterministic explanation.
- Every order, fill, and position traces to a subscription, source observation, calculation version, and market-data snapshot.
- A subscription cannot sell holdings attributed to another origin.
- All subscription and global risk caps hold under concurrent processing and restart.
- Source/mapping/data/audit degradation fails closed and is visible.
- The feature remains paper-only until a later live-readiness phase is completed.

## Open Product Decisions

1. Which first institutional managers should be in the launch directory?
2. Should the MVP be top-N holdings, minimum disclosed weight, or both? Recommended: both, with top 10 and a 1% floor.
3. Should excluded/unmapped weight remain cash? Recommended: yes.
4. Should 13F rebalancing occur at the next market open or over multiple sessions under turnover caps? Recommended: multiple sessions for larger allocations.
5. Are corporate insiders the intended meaning of “individual,” or is the goal specifically consenting retail/professional traders with connected accounts?
6. Is Augr still single-operator, or must leader subscriptions and broker destinations be user/tenant scoped from the first migration?
7. Should the legacy Polymarket/wallet surfaces be hidden, archived, or removed now that the venue is retired?

## Related

- [[execution/execution-overview]]
- [[backend/risk-management-engine]]
- [[execution/paper-trading]]
- [[api-design]]
- [[../runbooks/emergency-kill-switch]]
- [SEC Form 13F filing deadline FAQ](https://www.sec.gov/rules-regulations/staff-guidance/division-investment-management-frequently-asked-questions/frequently-asked-questions-about-form-13f)
- [SEC Form 4](https://www.sec.gov/files/form4data%2C0.pdf)
- [Kalshi public trades API](https://docs.kalshi.com/api-reference/market/get-trades)
- [Kalshi authenticated fills API](https://docs.kalshi.com/api-reference/portfolio/get-fills)
