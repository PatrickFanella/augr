---
title: "Venue reconciliation evidence"
description: "Read-only inspection and incident response for exact Alpaca and Kalshi reconciliation evidence."
status: "canonical"
updated: "2026-08-20"
tags: [reconciliation, ledger, alpaca, kalshi, incidents]
---

# Venue reconciliation evidence

Use this runbook to inspect OVR-207 evidence after an authorized reconciliation
writer has captured and persisted a run. The current implementation is an
additive, local-only library and schema boundary: it is not wired to a runtime
scheduler, API, UI, provider credential, or correction workflow. A healthy
application does not imply that reconciliation has run.

## Safety boundary

- Reconciliation is read-only with respect to the provider, ledger, lifecycle,
  cash, fills, positions, capital flows, and orders.
- Never make a balancing entry, synthesize a fill, edit a position, delete an
  incident, or change broker state to make a run clean.
- Preserve raw pages, observations, lifecycle events, normalizations, ledger
  transactions, projection checkpoints, snapshots, results, and incidents.
- Do not retry a provider write, submit/cancel an order, or acknowledge a
  correction from this workflow. Escalate those actions to a separately
  reviewed procedure.
- A database owner or migrator remains outside the application trust boundary.
  Treat direct owner access as privileged maintenance, not independent
  attestation.

## What one run proves

The fixed `venue-reconciliation-policy-v1` accepts only Alpaca namespace
`alpaca/account-activities/FILL` and Kalshi namespace
`kalshi/portfolio/fills`. It requires exact decimals, complete pagination,
complete fill coverage, canonical dated venue contracts, and two complete
provider captures with the same canonical state digest.

The local side is rebuilt inside one PostgreSQL `REPEATABLE READ`, read-only
transaction. It must exactly reproduce one OVR-104 checkpoint and retain every
included ledger transaction ID. Eligible fills retain their OVR-205 venue
observation, OVR-203 lifecycle identity, OVR-103 normalization and ledger
transaction, and OVR-201 canonical instrument/venue-contract identities.

A clean run means that this exact synthetic or captured evidence graph matched
under this exact policy. It does not prove provider API atomicity, external
paper fidelity, protected-environment readiness, production health, scheduler
activation, or permission to cut over accounting reads.

## Stability and comparison semantics

The provider reader performs two full reads. Receive timestamps may differ, but
account facts, cursors, page hashes, cash, equity, positions, fills, source
identities, source revisions, and source times must produce the same state
digest. Changing state is `snapshot_unstable`; unavailable, incomplete, or
unmappable evidence is also non-comparable. None of those states can be called
clean.

Result statuses are:

- `matched`: exact equality; severity `none`; no incident.
- `drift`: both sides are comparable and differ, or a required side is absent;
  severity `critical`; one immutable incident.
- `not_comparable`: evidence or semantics are insufficient; severity `high`;
  one immutable incident.

Cash, positions, and ordinary fills use exact equality with no tolerance or
netting. Missing is not zero. Equity is comparable only when mark basis and
coverage are explicitly equivalent. Corrections and busts key through the
original provider fill plus class and discriminator. Their matching presence is
still `correction_pending` or `bust_pending`, because OVR-207 intentionally has
no accounting-correction authority.

## Read-only inspection

Use a database identity with `SELECT` only. Scope every query to a known account,
run, or snapshot; do not infer the latest run across accounts.

### Run and incident inventory

```sql
SELECT r.id, r.created_at, r.policy_version, r.provider_snapshot_id,
       r.local_snapshot_id, r.clean, r.result_count, r.incident_count, r.sha256
FROM venue_reconciliation_runs AS r
JOIN venue_local_snapshots AS l ON l.id = r.local_snapshot_id
WHERE l.account_id = :'account_id'
ORDER BY r.created_at DESC, r.id DESC;

SELECT i.run_id, i.id AS incident_id, i.result_id, i.incident_key,
       i.reason, i.severity,
       result.kind, result.status, result.provider_value,
       result.local_value, result.delta
FROM venue_reconciliation_incidents AS i
JOIN venue_reconciliation_results AS result
  ON result.run_id = i.run_id AND result.id = i.result_id
WHERE i.run_id = :'run_id'
ORDER BY i.severity DESC, i.incident_key, i.reason;
```

### Provider stability evidence

```sql
SELECT id, provider, account_external_id, namespace, currency,
       horizon_start, horizon_end, state_sha256, sha256,
       first_capture_id, second_capture_id,
       first_capture_start, first_capture_end,
       second_capture_start, second_capture_end,
       page_count, position_count, fill_count
FROM venue_provider_snapshots
WHERE id = :'provider_snapshot_id';

SELECT sequence, cursor, next_cursor, terminal, sha256,
       octet_length(raw_bytes) AS raw_byte_count
FROM venue_provider_snapshot_pages
WHERE snapshot_id = :'provider_snapshot_id'
ORDER BY sequence;
```

The two capture IDs are deliberately equal only after both canonical provider
states agree. Page bytes remain evidence; inspect them under the applicable
data-handling policy and do not copy sensitive payloads into tickets.

### Checkpoint and fill lineage

```sql
SELECT l.id, l.account_id, l.provider, l.namespace, l.checkpoint_id,
       l.transaction_count, l.position_count, l.fill_count, l.issue_count,
       l.sha256, p.as_of, p.input_checksum, p.checksum AS output_checksum,
       p.through_transaction_id, p.transaction_count AS checkpoint_tx_count
FROM venue_local_snapshots AS l
JOIN projection_checkpoints AS p ON p.id = l.checkpoint_id
WHERE l.id = :'local_snapshot_id';

SELECT t.transaction_id
FROM venue_local_snapshot_transactions AS t
WHERE t.snapshot_id = :'local_snapshot_id'
ORDER BY t.transaction_id;

SELECT f.sequence, f.comparison_key, f.fill_id,
       f.ledger_transaction_id, f.evidence
FROM venue_local_snapshot_fills AS f
WHERE f.snapshot_id = :'local_snapshot_id'
ORDER BY f.sequence;

SELECT issue_key, reason, evidence
FROM venue_local_snapshot_issues
WHERE snapshot_id = :'local_snapshot_id'
ORDER BY reason, issue_key;
```

For each ordinary fill, verify that `fill_id`, normalization ID, and ledger
transaction ID lead to the retained venue observation and lifecycle event. For
a correction or bust, multiple local snapshot rows may intentionally share the
original `fill_id`; their `comparison_key` and revision evidence must remain
distinct.

### Independent graph counts and hashes

```sql
SELECT r.id,
       r.result_count,
       count(DISTINCT result.id) AS stored_results,
       r.incident_count,
       count(DISTINCT incident.id) AS stored_incidents,
       r.clean,
       encode(digest(r.canonical_bytes, 'sha256'), 'hex') = r.sha256 AS run_hash_ok
FROM venue_reconciliation_runs AS r
LEFT JOIN venue_reconciliation_results AS result ON result.run_id = r.id
LEFT JOIN venue_reconciliation_incidents AS incident ON incident.run_id = r.id
WHERE r.id = :'run_id'
GROUP BY r.id;

SELECT id,
       encode(digest(canonical_bytes, 'sha256'), 'hex') = sha256 AS local_hash_ok,
       canonical_json = convert_from(canonical_bytes, 'UTF8')::jsonb AS local_json_ok
FROM venue_local_snapshots
WHERE id = :'local_snapshot_id';
```

Repository reload performs stricter canonical reconstruction than these quick
queries. Use a successful reload/recompute as the final local evidence check.

## Incident response

1. Activate or confirm the existing entry halt if a critical reconciliation
   incident can affect new exposure. Do not infer that this package activates
   the kill switch itself.
2. Record the account, provider, namespace, run ID, snapshot IDs, policy
   version, hashes, reason, and severity. Preserve the original database and
   logs before experimentation.
3. Determine whether the result is `drift` or `not_comparable`. Do not turn
   missing evidence into zero and do not offset one discrepancy with another.
4. Trace the provider page/observation, lifecycle event, normalization, ledger
   transaction, and checkpoint membership. Keep provider time, receive time,
   effective time, and checkpoint `as_of` distinct.
5. Re-read only through the reviewed read-only capture path. An identical graph
   must converge to the same deterministic IDs. Changed evidence must append a
   new graph rather than overwrite the old one.
6. Escalate correction/bust, provider mutation, ledger repair, or incident
   acknowledgement to a separately reviewed workflow. OVR-207 supplies none.
7. Resume entry only under the emergency-control runbook and after the relevant
   owner accepts independently verified clean evidence. Never clear a halt from
   a timer or from this runbook alone.

## Rollback

Migration 75 is empty-only reversible. The down migration takes the complete
schema lock set and refuses if any policy, snapshot, result, or incident evidence
exists. That refusal is the safe outcome. Do not delete evidence to force a
downgrade.

An authorized disposable empty-database rehearsal may run `75 -> 74 -> 75`.
For a database containing evidence, preserve it, take the normal backup boundary,
and escalate. Runtime rollback does not require dropping schema 75 because no
OVR-207 runtime path is activated.

## Current limits and no-cutover boundary

- Provider snapshots in qualification are synthetic captured fixtures, not
  real-account or protected-staging evidence.
- A double read detects changes but cannot claim atomicity when the provider
  offers no atomic snapshot token.
- Equity comparison requires independently proven mark-basis equivalence.
- Revision-less Alpaca lifecycle facts use the immutable raw observation SHA-256
  as reconciliation evidence revision; this does not invent a broker revision.
- Kalshi correction/bust facts are unsupported by reviewed policy v1.
- There is no runtime scheduler, API, dashboard, alert delivery,
  acknowledgement state, automatic correction, current-policy pointer, writer
  role, or deployment/cutover in OVR-207.

Evidence from local qualification should be labeled `VERIFIED_LOCAL`. Real
credentials, real-provider calls, shared/protected migration, alert ownership,
authenticated operator workflow, deployment, and runtime activation remain
`BLOCKED_EXTERNAL` until separately reviewed and authorized.
