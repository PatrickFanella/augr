---
title: "Point-in-time dataset evidence"
description: "Construction, inspection, quarantine, preservation, and rollback boundaries for OVR-301 dataset manifests and quality results."
status: "canonical"
updated: "2026-08-20"
tags: [datasets, research, point-in-time, quality, provenance]
---

# Point-in-time dataset evidence

Use this runbook to construct and inspect OVR-301 research-input evidence. The
implementation is an additive library and schema boundary. It does not fetch
provider data, choose a current dataset, start an experiment, alter legacy
backtests, activate a scheduler, or authorize promotion or deployment.

## Safety boundary

- Preserve the source query or file, raw observations, request and content
  SHA-256 values, provider/source namespace, revision and correction lineage,
  symbology, adjustment policy, timezone/calendar, license, and retention facts.
- Never infer `available_at` from market/effective time. Unknown availability is
  not experiment-grade evidence and must not be hidden by a synthetic value.
- Never repair, delete, reorder, or overwrite a quarantined manifest. Corrected
  provider evidence produces a new immutable partition, manifest, and quality
  result.
- Never turn `not_assessed` into `passed`. Supply independently retained session,
  instrument-window, corporate-action, or cross-provider evidence and evaluate a
  new result.
- Do not promote mutable `historical_ohlcv`, provider caches, or float-derived
  rows into a manifest unless exact source bytes and all required point-in-time
  facts are independently available.
- Treat database-owner access as privileged maintenance. Append-only triggers
  protect normal operation but do not make an owner an independent attestor.

## Constructing a manifest

Construct through `internal/dataset.NewManifest`; persist through
`DatasetRepo.RecordDatasetManifest` in one transaction. A manifest requires:

1. One UTC microsecond `decision_cutoff`.
2. One or more bounded partitions. Each partition identifies its kind,
   provider, source, namespace, exact request/file hash, media type, symbology,
   adjustment policy, timezone, calendar, revision, license, and retention
   policy.
3. One or more observations per partition. Each observation retains effective,
   optional publication, observed, and available time separately, plus source
   identity, revision/correction lineage, exact content hash, optional canonical
   instrument, and supported exact decimal facts.
4. `published_at <= observed_at <= available_at <= decision_cutoff` for every
   applicable observation.

Partition row counts, content digests, and effective/observed/available bounds
are derived from ordered observations. Callers do not author those values.
Partition and observation sequence is canonical and contiguous; changing input
order cannot change the manifest ID or digest.

Register the exact reviewed `dataset-quality-policy-v1` artifact before storing
quality results. Evaluate with `internal/dataset.Evaluate`, supplying retained
expected-session evidence, instrument validity windows, and declared external
assessments. Persist through `DatasetRepo.RecordDatasetQualityResult`.

## Quality result semantics

Every applicable policy check is one of:

- `passed`: the exact check and any required external evidence succeeded.
- `failed`: retained evidence proves a material defect.
- `not_assessed`: required evidence was absent or the external check was not
  performed. This is not equivalent to success.

Required `failed` or `not_assessed` checks quarantine the manifest. Optional
provider spot comparison can remain `not_assessed` without setting quarantine,
but remains visibly unevaluated. Nonpassing checks have one deterministic
finding with a stable issue code and evidence list.

Policy check codes are `bid_ask`, `content_integrity`,
`corporate_action_reconciliation`, `correction_lineage`,
`instrument_validity`, `monotonic_time`, `nonnegative_depth`,
`nonnegative_volume`, `no_lookahead`, `provider_spot_comparison`,
`session_coverage`, and `unique_source_identity`.

## Read-only inspection

Use a database identity with `SELECT` only. Scope every query to a known
manifest or result; schema 76 deliberately has no current/latest pointer.

### Manifest identity and provenance

```sql
SELECT id, decision_cutoff, partition_count, observation_count, sha256,
       encode(digest(canonical_bytes, 'sha256'), 'hex') = sha256 AS hash_ok,
       canonical_json = convert_from(canonical_bytes, 'UTF8')::jsonb AS json_ok,
       created_at
FROM dataset_manifests
WHERE id = :'manifest_id';

SELECT sequence, kind, provider, source_name, namespace, request_sha256,
       content_sha256, media_type, effective_start, effective_end,
       observed_start, observed_end, available_start, available_end,
       symbology_version, adjustment_policy, timezone_name, calendar_name,
       revision, supersedes_content_sha256, row_count, license_name,
       retention_policy
FROM dataset_manifest_partitions
WHERE manifest_id = :'manifest_id'
ORDER BY sequence;
```

Review license and retention facts before copying or retaining source bytes.
These fields record the declared provenance boundary; they are not a legal
determination or an automatic deletion instruction.

### Observation timing and correction lineage

```sql
SELECT partition_sequence, sequence, source_key, instrument_id,
       effective_at, published_at, observed_at, available_at,
       revision, correction_of, content_sha256, bid, ask, volume, depth
FROM dataset_manifest_observations
WHERE manifest_id = :'manifest_id'
ORDER BY partition_sequence, sequence;

SELECT count(*) FILTER (WHERE available_at > manifest_decision_cutoff) AS lookahead,
       count(*) FILTER (WHERE published_at IS NOT NULL
                         AND published_at > observed_at) AS publication_inversions,
       count(*) FILTER (WHERE observed_at > available_at) AS availability_inversions
FROM dataset_manifest_observations
WHERE manifest_id = :'manifest_id';
```

All three counts must be zero. A correction points to a prior original in the
same partition and cannot hide the original through a chain or cycle.

### Quality checks and findings

```sql
SELECT id, policy_version, manifest_id, quarantined, check_count,
       finding_count, sha256,
       encode(digest(canonical_bytes, 'sha256'), 'hex') = sha256 AS hash_ok,
       created_at
FROM dataset_quality_results
WHERE id = :'quality_result_id';

SELECT sequence, check_key, partition_content_sha256, kind, check_code,
       required, status, severity, evidence_sha256
FROM dataset_quality_checks
WHERE result_id = :'quality_result_id'
ORDER BY sequence;

SELECT sequence, finding_key, partition_content_sha256, check_code,
       finding_code, severity, evidence
FROM dataset_quality_findings
WHERE result_id = :'quality_result_id'
ORDER BY sequence;
```

Repository reload performs exact canonical reconstruction and compares every
stored child row with the canonical graph. Use a successful reload and
deterministic recomputation as stronger local evidence than these quick queries.

## Quarantine response

1. Stop the affected manifest from entering any future experiment workflow.
   OVR-301 does not itself activate or halt a runner.
2. Record manifest ID/digest, quality-result ID/digest, policy version, partition
   hash, check/finding codes, status, severity, and evidence hash.
3. Preserve source bytes and timing/provenance metadata under their license and
   retention constraints before investigating.
4. Distinguish `failed` from `not_assessed`. A failure needs corrected upstream
   evidence or an explicit source decision; not-assessed needs the missing
   independent assessment.
5. Create a new partition/manifest/result. Never mutate or delete the old graph
   and never silently clamp, fill, net, or reorder defective values.
6. Recompute from a restarted process. Identical evidence must converge to the
   same IDs and hashes; changed evidence must append distinct identities.

## Rollback

Migration 76 is empty-only reversible. Its down migration takes an exclusive
lock and refuses while any policy, manifest, or quality evidence exists. That
refusal is the safe outcome. Never delete evidence to force a downgrade.

An authorized disposable empty-database rehearsal may run `76 -> 75 -> 76`.
For a database containing evidence, preserve it and use the normal backup and
restore boundary. Runtime rollback does not require dropping schema 76 because
no dataset scheduler, current pointer, experiment runner, or read cutover is
activated.

## Current limits and no-cutover status

- Existing legacy historical caches and backtests remain visibly unpinned.
- The current repository accepts already-constructed exact evidence; it does not
  download or license data and does not prove provider completeness.
- Instrument-window, session, corporate-action, and cross-provider assessment
  inputs must come from independently retained evidence.
- OVR-302 will define immutable experiments that pin these IDs. OVR-301 alone
  does not enforce an experiment foreign key or authorize research execution.
- There is no shared migration, provider call, scheduler, API/UI, deployment,
  promotion, or production cutover in this local implementation.

Label successful isolated qualification `VERIFIED_LOCAL`. Shared/protected
migration, source licensing approval, real-provider completeness, protected
runner integration, deployment, and operational ownership remain
`BLOCKED_EXTERNAL` until separately reviewed and authorized.
