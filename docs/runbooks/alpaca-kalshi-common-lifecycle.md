---
title: "Alpaca and Kalshi common-lifecycle operations"
description: "Read-only inspection, recovery, and activation boundaries for OVR-205 venue adapters."
updated: "2026-08-20"
---

# Alpaca and Kalshi common-lifecycle operations

## Safety boundary

OVR-205 is an additive, fail-closed adapter boundary. It does not activate a
worker, scheduler, route, writer grant, provider credential, or legacy cutover.
The global kill switch must remain active, live trading and scheduling must
remain disabled, and provider credentials must remain absent during local
qualification. A reviewed policy artifact and successful local rehearsal are
not authorization to place an external order.

Never repair an incident by editing an execution order, binding, fill,
observation, economic source event, normalization, ledger transaction, or
lifecycle event. Those records are immutable evidence. Record and investigate
the incident, then use a separately reviewed correction workflow when one
exists.

## Policy registration and route authorization

Each provider uses one content-addressed `venue-adapter-policy-v1` artifact.
The version contains the SHA-256 of canonical bytes. Go and PostgreSQL both
reconstruct and validate the fixed schema; a self-consistent but forged digest
does not register. A venue-policy order must reference a registered version
whose provider matches the order and dated venue contract. There is no mutable
"current policy" pointer.

Register through `venue.RegisterPolicy`; do not insert policy rows directly.
An exact retry returns the existing artifact. Any changed payload under the
same version is an idempotency conflict and must stop the route.

Kalshi contracts have an exact whole metadata object and no extension keys:

```json
{"kalshi_v2":{"outcome":"yes"}}
```

or:

```json
{"kalshi_v2":{"outcome":"no"}}
```

Case variants, misspellings, non-string outcomes, and extra nested or
top-level keys are invalid before transport and independently invalid in
PostgreSQL.

## Raw-first persistence order

For every provider fact, persistence is ordered as follows:

1. Append the byte-exact `venue_observation`, parsed object, SHA-256, provider
   token, mapping result, source identity, canonical order context, source
   time, and receive time.
2. For an authoritative fill only, append the byte-exact
   `economic_source_event` using the same provider identity and bytes.
3. Atomically append the normalization, balanced ledger transaction and
   postings, optional first external binding, canonical fill, and lifecycle
   transition.

No-change observations are durable even when they produce no lifecycle event.
Alpaca stream fill/partial-fill messages are non-economic notices; only exact
account activity `FILL` records create fills. Kalshi order status and cumulative
counts never create a fill; only cursor-retrieved `fill_id` records do.

Every persistence operation is idempotent. After a response loss, replay the
same exact plan. Never mint a new client ID, source event ID, or fill ID.

## Submission ambiguity and client-ID recovery

The canonical lifecycle order UUID string is the stable provider client order
ID for both Alpaca and Kalshi. The request body is deterministic and uses exact
decimal strings; binary floating point is not used.

- Alpaca: after a duplicate or ambiguous submit, look up exactly
  `/v2/orders:by_client_order_id`. A definitive request/auth failure and context
  cancellation remain errors.
- Kalshi: after a duplicate or ambiguous submit, scan the current order pages
  at `/portfolio/orders` and, only after a complete current miss, the reviewed
  `/historical/orders` pages.
  Exactly one matching client ID must exist. Zero unresolved matches and more
  than one match are reconciliation incidents. Rate-limit, cooldown,
  request/auth, and context-cancellation errors remain visible.

Kalshi Create Order V2 is a single YES-book request. It sends `bid`/`ask`,
fixed-point `count` and `price`, and the reviewed conservative
`self_trade_prevention_type=taker_at_cross`. Its immediate response is the
compact `order_id`, `client_order_id`, `fill_count`, `remaining_count`, optional
fill averages, and `ts_ms` object. Journal those exact bytes as a non-economic
fill notice; do not inflate it into a portfolio order or derive fills from its
counts/averages. A successful compact response and a portfolio-order recovery
after an ambiguous/duplicate submit remain distinct evidence types.

The recovered provider order must match the immutable ticker/symbol, action,
book side, outcome, quantity, price, subaccount, exchange index, and client ID.
Do not resubmit with another identity when recovery is inconclusive.

## Provider mappings

Alpaca retains every reviewed order and trade-update token. Cancellation,
expiry, and rejection remain different terminal states. `replaced`, replacement
pending states, correction, and bust evidence fail reconciliation; the adapter
does not reinterpret a replacement as the original order or rewrite economics.
Account-activity correction and bust evidence must reference an existing fill
and still stops at `failed_reconciliation`.

Kalshi accepts exactly these order states:

- `resting` — acknowledge, or durable no-change after acknowledgement/fills;
- `canceled` — cancellation only after exact local fills and provider totals
  agree;
- `executed` — a fill notice only after authoritative fill IDs have already
  brought local cumulative quantity to exact initial quantity.

Legacy synonyms such as `filled`, `open`, `new`, `pending`, `cancelled`,
`complete`, and `partially_filled` are unknown states and fail reconciliation.
For NO contracts, buy/sell map to ask/bid and the V2 single-book request price
is exactly `1 - outcome_price`; no float rounding is permitted. Raw order/fill
observations retain the provider's YES-book `yes_price_dollars`. Canonical NO
fill economics use `no_price_dollars`; PostgreSQL enforces the complement
between those two price domains.

## Fill pagination and historical cutoffs

Alpaca account activities are requested for the external order ID in ascending
order with the previous final activity ID as page token. A repeated token,
malformed page, wrong order/client/symbol/side, impossible cumulative/leaves
total, correction, or bust stops recovery.

Kalshi scans every `/portfolio/fills` page and then every `/historical/fills`
page using order ID, ticker, and subaccount. An empty cursor terminates a family;
a repeated cursor is an error. `fill_id`, not `trade_id`, is the economic source
identity. Each fill must match order ID, ticker, outcome, action, book side,
subaccount, exchange index, exact quantity, YES/NO complement, fee, and provider
timestamp before economics are constructed.

Never treat a current-endpoint miss as authoritative absence. Never infer a
missing fill from executed status, cumulative quantity, average price, or a
zero/missing sentinel.

## Cancellation semantics

Persist the local `cancel_requested` command before calling provider DELETE.
An identical retry converges; changed command evidence conflicts. A DELETE
success only means the request was accepted and must not create
`order_cancelled`. Cancellation requires a later exact provider observation.
If DELETE times out after the command commits, reload the aggregate and retry
the same command/request identity.

Alpaca cancellation uses the immutable external order ID. Kalshi V2
cancellation additionally carries the immutable client ID, subaccount, and
exchange index. The compact Kalshi DELETE response (`order_id`,
`client_order_id`, `reduced_by`, `ts_ms`) is journaled as no-change evidence;
only a later exact portfolio order with status `canceled` is terminal.

## Incident states

The lifecycle enters `failed_reconciliation` for an unknown, malformed, or
contradictory provider fact, and for Alpaca corrections/busts. Existing fills,
normalizations, and ledger entries remain unchanged. Typical causes include:

- multiple or missing client-ID recovery matches;
- external/client/ticker/outcome/action/book-side/subaccount/index mismatch;
- impossible initial/fill/remaining totals or missing fill IDs;
- unrecognized provider status/event token;
- changed raw bytes under an existing provider identity;
- provider time after receive time;
- Kalshi metadata or YES/NO price projection disagreement;
- Alpaca replacement, correction, or bust evidence.

Do not resume automated execution for that order. Preserve the raw evidence and
escalate with the policy version, observation ID, provider identity, and exact
bytes/hash.

## Read-only inspection

Use a read-only transaction or replica. Replace the placeholders; never paste
credentials into a transcript.

```sql
BEGIN TRANSACTION READ ONLY;

SELECT id, provider, kind, provider_state, mapped_outcome,
       external_order_id, client_order_id, source_namespace,
       source_event_id, source_revision, source_at, received_at,
       raw_sha256
FROM venue_observations
WHERE order_id = :order_id
ORDER BY received_at, id;

SELECT id, kind, prior_state, next_state, source_namespace,
       source_event_id, source_revision, source_at, received_at
FROM execution_lifecycle_events
WHERE order_id = :order_id
ORDER BY ingest_sequence;

SELECT f.id, f.source_event_id, f.quantity, f.price,
       f.normalization_id, f.ledger_transaction_id,
       e.payload_sha256
FROM execution_fills AS f
JOIN economic_source_events AS e ON e.id = f.economic_source_event_id
WHERE f.order_id = :order_id
ORDER BY f.received_at, f.id;

ROLLBACK;
```

Confirm one observation per provider identity, one economic source and
canonical fill per authoritative fill identity, matching normalization and
ledger IDs, and no economic row for a notice, unknown, malformed, or
contradictory observation.

## Migration and rollback

Migration 73 is additive, starts empty, creates no writer grant, and does not
activate a runtime. Apply it only to a dedicated disposable or separately
authorized database after migrations 1–72. Its down migration locks the graph
and refuses while a policy artifact, venue order, observation, or dependent
fact exists. This refusal is the rollback boundary; do not delete evidence to
force a downgrade.

Qualification requires two separate databases:

- a retained schema-73 database with complete Alpaca and Kalshi rehearsal
  graphs, fresh-process replay, and a proven nonempty rollback refusal;
- an empty database proving `73 -> 72 -> 73` from the reviewed source.

## Activation checklist (future, separate change)

Activation is out of scope until a new reviewed change supplies all of the
following: protected environment and database authorization; credential
provisioning/rotation/revocation; least-privilege writer identity; scheduler and
worker ownership; global/provider/account kill switches; rate-limit and retry
budgets; reconciliation service and incident alerts; real external-paper
qualification; deployment/rollback rehearsal; and explicit owner approval.

Until then, keep live trading and scheduling disabled, omit provider
credentials, use only loopback fakes and disposable databases, and make no
external provider calls.
