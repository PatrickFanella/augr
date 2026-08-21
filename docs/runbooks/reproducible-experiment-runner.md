---
title: "Reproducible experiment runner"
description: "OVR-303 adapter registration, execution, replay comparison, interruption handling, inspection, and preservation boundaries."
status: "canonical"
updated: "2026-08-20"
tags: [experiments, replay, simulation, provenance, research]
---

# Reproducible experiment runner

Use this runbook for schema-78 OVR-303 experiment execution and replay
evidence. The runner consumes one exact OVR-302 declaration, executes only an
explicit adapter bound to its immutable strategy version, and records a
content-addressed program, replay plan, economic graph, result, and append-only
attempt lifecycle.

This boundary does not compile arbitrary code, select a best result, calculate
portfolio-performance statistics, approve or promote a strategy, activate a
deployment, schedule work, fetch providers, or place a live order.

## Safety boundary

- Run only against an explicit `paper_scored` or `paper_stress` account and the
  matching declared experiment mode. Never substitute a stress account for a
  scored declaration or treat stress output as promotion evidence.
- Load the exact strategy version, manifest and quality result, simulation
  policy, capital policy/binding/state, instruments, venue contracts, and
  observation bytes pinned by the declaration. Missing, stale, future,
  quarantined, or hash-mismatched evidence fails closed.
- Register adapters explicitly by exact `ProgramIdentity`: version ID/hash,
  compiler identity, source commit/tree hash, decision contract, adapter
  kind/version/hash, and `experiment-runner-v1` contract. There is no dynamic
  compiler or mutable adapter lookup.
- Treat plan and result IDs as content identities. A changed decision, order,
  observation, mode, capital state, fill, fee, or ordered child ID is a new
  identity; it is never an update.
- Keep raw simulation fill evidence before normalization. All intent, order,
  transition, fill, normalization, and ledger writes use the common OVR-103,
  OVR-203, and OVR-204 repositories.
- A completed result is inserted atomically with its ordered children and the
  attempt's completed event. A failed attempt may retain already accepted
  economic evidence, but it cannot claim or partially retain a completed
  result graph.
- Identical retries may append new attempt events but must converge on one
  program, plan, intent, order, economic graph, and result.

## Adapter registration and execution preflight

Before starting an attempt, verify all of the following:

1. The experiment exists in `declared` state and its exact version, account,
   binding, manifest, quality result, and policy artifacts reload successfully.
2. The account environment matches the experiment mode. Scored quality is not
   quarantined; stress evidence remains in its synthetic namespace.
3. Every in-window manifest observation has retained exact bytes and a valid
   normalized quote snapshot. Its byte hash, source key, availability time,
   instrument, and venue contract match the manifest row.
4. The capital state is reconstructed from the exact account, binding, policy,
   and projection checkpoint and is embedded in the replay plan.
5. The adapter `ProgramIdentity` matches every immutable version field. Reject
   missing or ambiguous adapters; do not fall back to a different program.
6. The request supplies a new nonzero attempt ID and UTC microsecond start and
   finish timestamps. Attempt timestamps do not alter the content-addressed
   result.
7. Scheduler, live-trading, and provider-fetch paths remain disabled. OVR-303
   is invoked only through a deliberately constructed local/protected runner.

The retained qualification adapter is
`internal/experimentrun/qualification`. It is test/local evidence only and is
not a production strategy registry.

## Read-only inspection

Use a read-only database identity and explicit IDs. Schema 78 deliberately has
no latest, current, best, approved, or promoted result pointer.

```sql
SELECT id, version_id, version_sha256, compiler_kind, compiler_version,
       source_commit, source_tree_sha256, decision_contract,
       adapter_kind, adapter_version, adapter_sha256, runner_contract,
       sha256, encode(digest(canonical_bytes,'sha256'),'hex') = sha256 AS hash_ok
FROM experiment_programs
WHERE id = :'program_id';

SELECT id, experiment_id, program_id, account_id, capital_state_id,
       capital_state_sha256, capital_projection_checkpoint_id,
       manifest_id, manifest_sha256, evaluation_start, evaluation_end,
       seed, mode, step_count, sha256
FROM experiment_replay_plans
WHERE id = :'plan_id';

SELECT sequence, partition_content_sha256, observation_source_key,
       observation_content_sha256, available_at, decision_sha256,
       action, rejection_code, intent_id, order_id
FROM experiment_replay_plan_steps
WHERE plan_id = :'plan_id'
ORDER BY sequence;
```

Reload with `ExperimentRunRepo.GetProgram` and `GetPlan` for the stronger
check: those methods reconstruct canonical bytes and compare every normalized
parent and ordered step column.

```sql
SELECT a.id AS attempt_id, a.experiment_id, e.sequence, e.type,
       e.occurred_at, e.result_id, e.error_code, e.error_sha256, e.sha256
FROM experiment_run_attempts a
JOIN experiment_run_attempt_events e ON e.attempt_id=a.id
WHERE a.id = :'attempt_id'
ORDER BY e.sequence;

SELECT id, experiment_id, program_id, plan_id, account_id, manifest_id,
       quality_result_id, simulation_policy_version, capital_policy_version,
       mode, step_count, noop_count, rejected_count, intent_count,
       order_count, transition_count, fill_count,
       trim_scale(filled_quantity) AS filled_quantity,
       trim_scale(fee_total) AS fee_total, sha256, created_at
FROM experiment_run_results
WHERE experiment_id = :'experiment_id'
ORDER BY created_at, id;

SELECT sequence, action, decision_sha256, intent_id, order_id,
       transition_count, fill_count, trim_scale(filled_quantity),
       trim_scale(fee_total), aggregate_sha256, outcome_sha256
FROM experiment_run_step_outcomes
WHERE result_id = :'result_id'
ORDER BY sequence;
```

Use `GetResult` or `ListExperimentResults`; they reconstruct the canonical
result, compare normalized metrics/outcomes and ordered transition/fill IDs,
and recheck the result-to-plan experiment/program/account/manifest/mode pins.

## Replay comparison

For two attempts of the same declaration and program, compare all levels:

1. program ID and SHA-256;
2. plan ID/SHA-256 and every ordered decision SHA-256;
3. deterministic intent/order IDs and idempotency keys;
4. ordered lifecycle transition and fill IDs;
5. per-step filled quantity, fee, aggregate SHA-256, and outcome SHA-256;
6. result metrics, ID, SHA-256, and canonical bytes.

Different attempt IDs/timestamps are expected. Any difference in the six
levels above is a replay failure even if a high-level status says completed.
Do not explain away divergence by selecting a newer result: there is no current
selection contract.

The retained database qualification is:

```bash
DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/DEDICATED_DB?sslmode=disable' \
  go test ./internal/repository/postgres \
  -run 'TestExperimentRunnerGolden' -count=1 -race
```

Use only a dedicated loopback disposable database. The test creates isolated
temporary databases/schemas and never targets a shared environment.

## Interruption and failure response

1. Stop automatic retries if the declaration, program identity, canonical
   bytes, or accepted idempotency payload differs.
2. Inspect the attempt stream. A normal attempt has `started` at sequence 0 and
   exactly one `completed` or `failed` event at sequence 1.
3. If no terminal event exists because the process died, retain the incomplete
   attempt and start a new attempt ID after verifying the exact parents. Do not
   mutate or delete the old attempt.
4. If a failed attempt already wrote intent/order/fill evidence, reload the
   common lifecycle. The next attempt must converge on the same deterministic
   IDs and append no duplicate economic effects.
5. If result persistence failed, confirm that result parent, outcomes,
   transition IDs, fill IDs, and completed event are all absent. The transaction
   is one atomic boundary.
6. If a terminal event conflicts under the same attempt ID, stop. One attempt
   cannot change from failed to completed or vice versa.
7. Preserve PostgreSQL logs, canonical bytes, IDs/digests, and row counts for
   investigation. Append-only triggers are guardrails, not proof against a
   database owner.

## No-promotion response

An OVR-303 result is execution evidence only. If asked to mark it best,
approved, promoted, active, retired, scheduled, or deployed:

1. refuse the state change at this boundary;
2. retain the result and exact evidence links;
3. route evaluation statistics to OVR-304, robustness/search correction to
   OVR-305, and policy-governed promotion/retirement to OVR-306;
4. require separate authority for deployment activation or production cutover.

## Rollback and preservation

Migration 78 is empty-only reversible. The down migration locks every
experiment-run table and refuses if any program, plan, attempt, event, result,
outcome, transition ID, or fill ID exists. Never delete evidence to force a
downgrade.

An authorized disposable empty database may rehearse `78 -> 77 -> 78`. A
database containing OVR-303 evidence must retain schema 78 even if older
application code is restored. Schema 78 adds no scheduler, writer grant,
current pointer, promotion authority, provider call, or runtime cutover.

Label successful isolated qualification `VERIFIED_LOCAL`. Real strategy
adapters, licensed datasets, protected runner infrastructure, independent
review, shared migration, promotion, deployment, and production cutover remain
`BLOCKED_EXTERNAL` until separately authorized and evidenced.
