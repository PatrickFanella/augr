---
title: "Immutable strategy catalog and experiment declarations"
description: "Registration, evidence admission, inspection, preservation, and rollback boundaries for OVR-302."
status: "canonical"
updated: "2026-08-20"
tags: [strategies, experiments, deployments, provenance, research]
---

# Immutable strategy catalog and experiment declarations

Use this runbook for OVR-302 strategy families, content-addressed versions,
research experiment declarations, inert deployment proposals, and explicit
legacy mappings. This boundary records identity and intent. It does not run an
experiment, generate a signal, approve a strategy, activate a deployment,
schedule work, place an order, or change a legacy runtime path.

## Safety boundary

- Construct and restore catalog objects through `internal/strategycatalog`;
  persist them through `StrategyCatalogRepo` so the canonical and relational
  graphs are checked together in one transaction.
- Treat a family as one durable thesis. A change to its slug, name, thesis, or
  asset-class declaration is a new product decision, not an update operation.
- Treat every code, compiler, configuration, decision-contract, or required-
  dataset change as a new immutable version. Never rewrite an old version.
- A scored experiment requires a nonquarantined quality result, a manifest
  satisfying every version-required dataset kind, an exact simulation policy,
  and an account binding with finite `promotion_evidence` capital policy.
- A stress experiment requires an isolated `paper_stress` binding with
  `synthetic_stress` evidence. Stress results cannot become promotion evidence.
- A deployment row is only a `proposed` assignment. Its fixed activation
  authority names a future evaluator; schema 77 exposes no approval or
  activation transition.
- A legacy mapping is only `legacy_unvalidated`. It captures the exact mutable
  strategy-row snapshot hash and never creates a version or experiment.
- Preserve old canonical bytes, normalized children, lifecycle evidence, and
  all pinned OVR-301/206/204 artifacts. Corrections append new identities.

## Registration sequence

1. Register a family with a normalized stable slug, bounded name and thesis,
   and sorted unique supported asset classes.
2. Register a version under that family. Pin the compiler kind/version, exact
   source commit and source-tree SHA-256, canonical JSON configuration, config
   schema, decision-contract version, and sorted unique dataset kinds.
3. Declare an experiment only after the version, dataset manifest and matching
   quality result, simulation policy, account, and capital binding exist.
   Supply an exact UTC microsecond evaluation window and deterministic seed.
4. Optionally propose a deployment by pinning the exact version, account and
   binding, positive budget, cron text, canonical timezone, risk-policy
   reference, and scored/stress mode. Proposal is not activation.
5. Map a legacy strategy only after reading its current database snapshot hash.
   If the row changes before commit, registration fails closed.

Identical retries converge. A changed family payload under the same stable slug
returns `ErrIdempotencyConflict`. Concurrent registration leaves one complete
parent/children/lifecycle graph.

## Read-only inspection

Use a read-only database identity and explicit IDs. Schema 77 has no
current/latest version pointer.

```sql
SELECT id, slug, name, thesis, asset_classes, sha256,
       encode(digest(canonical_bytes,'sha256'),'hex') = sha256 AS hash_ok,
       created_at
FROM strategy_families
WHERE id = :'family_id';

SELECT id, family_id, compiler_kind, compiler_version, source_commit,
       source_tree_sha256, config_schema, config, decision_contract,
       required_kind_count, sha256, created_at
FROM strategy_versions
WHERE family_id = :'family_id'
ORDER BY created_at, id;

SELECT version_id, sequence, kind
FROM strategy_version_dataset_kinds
WHERE version_id = :'version_id'
ORDER BY sequence;
```

Configuration edits must appear as distinct version IDs while the old version
remains present. Reload through `GetStrategyVersion` for stronger evidence: it
reconstructs both canonical bytes and the normalized ordered kind rows.

```sql
SELECT id, state, version_id, account_id, capital_binding_id, manifest_id,
       quality_result_id, simulation_policy_version, capital_policy_version,
       mode, evaluation_start, evaluation_end, seed, dataset_quarantined,
       sha256, created_at
FROM research_experiments
WHERE id = :'experiment_id';

SELECT e.id, e.mode, e.dataset_quarantined, q.quarantined,
       b.environment, b.evidence_class, b.policy_version,
       m.decision_cutoff >= e.evaluation_end AS cutoff_ok
FROM research_experiments e
JOIN dataset_quality_results q ON q.id=e.quality_result_id
JOIN dataset_manifests m ON m.id=e.manifest_id
JOIN account_capital_policy_bindings b ON b.id=e.capital_binding_id
WHERE e.id = :'experiment_id';
```

For `paper_scored`, both quarantine columns must be false, the environment must
be `paper_scored`, and evidence class must be `promotion_evidence`. For
`paper_stress`, environment and evidence class must be `paper_stress` and
`synthetic_stress`.

```sql
SELECT id, state, activation_authority, version_id, account_id,
       capital_binding_id, trim_scale(budget) AS budget, schedule_cron,
       timezone_name, risk_policy_version, mode, sha256, created_at
FROM strategy_deployments
WHERE id = :'deployment_id';

SELECT id, state, legacy_strategy_id, family_id, legacy_snapshot_sha256,
       strategy_legacy_snapshot_sha(legacy_strategy_id) = legacy_snapshot_sha256
         AS snapshot_still_matches,
       sha256, created_at
FROM legacy_strategy_family_mappings
WHERE id = :'mapping_id';

SELECT entity_kind, entity_id, event_kind, prior_state, next_state,
       evidence_sha256, sha256, created_at
FROM strategy_catalog_lifecycle_events
WHERE entity_id IN (:'family_id', :'version_id', :'experiment_id')
ORDER BY created_at, id;
```

Every OVR-302 lifecycle event is initial-only: family/version `registered`,
experiment `declared`, deployment `proposed`, or legacy mapping `mapped` to
`legacy_unvalidated`. Any active, approved, completed, promoted, or retired
state is outside schema 77 and must be treated as forged or out of scope.

## Admission or integrity failure response

1. Do not retry by weakening mode, quarantine, policy, account, capital, time,
   or dataset requirements.
2. Record the attempted IDs/digests and the exact missing or mismatched parent.
3. Preserve all existing catalog and dataset evidence before investigating.
4. Correct the upstream evidence by appending the proper artifact, manifest,
   quality result, binding, or version. Never edit the rejected parent.
5. Retry from a restarted process. Exact input must reproduce the same identity.
6. If a direct database mutation is observed, stop catalog writers and retain
   database/WAL evidence. Append-only triggers are a guardrail, not an
   independent attestation against database-owner actions.

There is no activation incident procedure in OVR-302 because it contains no
activation path. If a proposed deployment appears to be executing, treat that
as an external runtime/cutover incident and inspect the actual scheduler and
execution boundary; the proposal itself is not authorization.

## Rollback

Migration 77 is empty-only reversible. The down migration locks every catalog
table and refuses if any family, version, experiment, deployment, legacy
mapping, or lifecycle event exists. Never delete evidence to force a downgrade.

An authorized disposable empty database may rehearse `77 -> 76 -> 77`. A
database with catalog evidence should retain schema 77 even if older
application code is restored, because schema 77 adds no scheduler, writer
grant, current pointer, or legacy read/write cutover.

`golang-migrate` records a failed nonempty down as dirty version 76 even though
PostgreSQL rolls the transactional schema changes back. Confirm the refusal,
all schema-77 tables, row counts, and retained aggregate hashes before an
authorized `migrate force 77`. Never force metadata after an unexpected or
partially understood failure.

## Current limits and no-cutover status

- Experiment execution, results, metrics, statistical validation, promotion,
  approval, and deployment activation begin in later OVR-303 through OVR-306
  boundaries.
- Risk-policy references are immutable identity text in this slice; the common
  runtime risk authority arrives later and no proposal may bypass it.
- Legacy rows remain mutable and continue on their old runtime paths. Mapping
  records only the captured snapshot and its explicitly unvalidated status.
- No shared database migration, production data change, provider request,
  scheduler activation, API/UI cutover, deployment, or live trade is included.

Label successful isolated qualification `VERIFIED_LOCAL`. Shared/protected
migration, licensed real datasets, protected runner integration, independent
review, promotion authority, deployment activation, and production cutover
remain `BLOCKED_EXTERNAL` until separately authorized and evidenced.
