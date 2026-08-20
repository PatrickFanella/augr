# Quality-Filtered Wheel V1 Research Runbook

## Boundary

Wheel V1 is immutable research evidence. It does not register a runtime
strategy, schedule work, call a provider, allocate capital, promote a result,
or activate a deployment. A persisted report is `VERIFIED_LOCAL` evidence only.
Licensed real inputs, independent review, a shared migration, promotion,
runtime adoption, and production activation remain `BLOCKED_EXTERNAL`.

Migration 83 owns these append-only tables:

- `wheel_v1_policies`
- `wheel_v1_scenarios`
- `wheel_v1_source_observations`
- `wheel_v1_reports`
- `wheel_v1_transitions`
- `wheel_v1_economic_effects`
- `wheel_v1_selected_contracts`

## Review a policy and its sources

1. Read `canonical_json` from `wheel_v1_policies`; verify quality thresholds,
   freshness, delta/DTE bands, liquidity, spread, deliverable, contract cap,
   fees, settlement, dividend entitlement, and decimal scale.
2. Join a scenario to `wheel_v1_source_observations` in sequence order.
3. For every candidate in `canonical_event->'candidates'`, match the exact
   `(partition_content_sha256, source_key, evidence_sha256, available_at)` to
   `dataset_manifest_observations` and its instrument to `venue_contracts`.
4. Confirm `available_at <= occurred_at`. Never substitute a later revision,
   ticker-only identity, midpoint, manufactured Greek, or inferred assignment.

Example read-only inspection:

```sql
SELECT s.id, s.sha256, s.mode, o.sequence, o.event_kind, o.occurred_at,
       o.evidence_id, o.evidence_sha256, o.canonical_event
FROM wheel_v1_scenarios s
JOIN wheel_v1_source_observations o ON o.scenario_id = s.id
WHERE s.id = $1
ORDER BY o.sequence;
```

## Replay and reconcile

Reload the exact policy and scenario through `WheelRepo.GetPolicy` and
`WheelRepo.GetScenario`, then derive a fresh `wheel.NewReport`. The replay is
equal only when report ID, SHA-256, and canonical bytes all match the stored
report.

Reconcile each transition in order:

- cash equals prior cash plus cash-changing effects;
- shares change only through put purchases or call sales;
- a put reserve equals strike times deliverable times contracts;
- a call has enough prior unencumbered shares for its deliverable;
- premiums use bid, closes use ask, and fees appear explicitly;
- option liability is not deducted twice with collateral;
- assignment/expiry releases the exact option state;
- dividends credit only held shares at the sourced event;
- capped upside is descriptive and excluded from P&L;
- final return equals `ending_net_liquidation / initial_capital - 1`.

The database performs the same structural, selection, continuity, collateral,
coverage, and terminal-accounting checks at deferred transaction commit. Go
reload independently reconstructs the canonical domain objects and normalized
rows.

## Assignment and dividend response

An early assignment is admissible only as an `assignment` event with an exact
assignment option ID and immutable evidence ID/hash. Do not infer an assignment
from spot price. At expiry, use only the policy-pinned automatic ITM convention
and exact expiry mark.

A dividend is admissible only as a sourced `dividend` event while shares are
held. Preserve the event evidence separately from option-chain observations.
If entitlement timing, amount, revision, or share state is missing, stop the
scenario; do not estimate or backfill it.

## Failure and recovery

All policy/scenario/report writes are content-addressed and append-only.
Identical concurrent writers converge. A changed retry, gap, fork, partial
graph, unknown contract, source mismatch, forged normalized row, naked state,
or accounting divergence must fail the transaction or fail reconstruction.

After interruption:

1. inspect the transaction error;
2. confirm no partial rows exist for the intended scenario/report ID;
3. reload policy and scenario parents;
4. rerun with byte-identical input;
5. compare the returned ID/SHA/canonical bytes before accepting the retry.

Never disable append-only or graph triggers as recovery. Trigger disabling is
used only in a dedicated disposable test to prove reload forgery detection.

## Local qualification

Use a dedicated empty database, migrate it from 1 through 83, and run:

```bash
WHEEL_V1_QUALIFICATION_DB_URL="$ISOLATED_DB_URL" \
  go test -race ./internal/repository/postgres \
  -run TestWheelRetainedQualification -count=1 -v
```

The retained qualification must contain exactly five scenarios/reports:
put expiry, put assignment, dividend, covered-call expiry, and call-away. Keep
their IDs/hashes and exact row counts in the OVR402 plan qualification record.

## Rollback

Migration 83 is empty-only reversible:

```bash
migrate -path migrations -database "$ISOLATED_DB_URL" down 1
migrate -path migrations -database "$ISOLATED_DB_URL" up 1
```

The down migration must refuse when any Wheel policy, scenario, or report is
present. Never delete research evidence to make rollback pass. For a shared or
production database, stop and obtain explicit migration/deployment authority;
this runbook does not grant it.
