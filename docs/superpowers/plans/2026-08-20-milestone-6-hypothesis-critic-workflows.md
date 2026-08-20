# Milestone 6 Hypothesis and Critic Workflow Plan

**Goal:** Complete OVR-602 with immutable hypothesis-generation and critic
artifacts that bind exact sources, prompt/model identity, declared tests, and
search lineage to OVR-301 data, OVR-305 robustness controls, and one OVR-601
compiled strategy candidate.

**Architecture:** Add a `researchworkflow` boundary and migration 96. A
hypothesis artifact binds one exact OVR-301 manifest, one exact OVR-305 policy
and completed assessment defining the search/family-wide statistical controls,
and one exact OVR-601 spec/version/receipt graph. It records a falsifiable claim,
mechanism, expected observations, explicit null/refutation conditions, source
snapshots, complete search-query/result lineage, prompt/model/token/cost
provenance, and declared property/example/experiment tests. A separate critic
artifact binds the hypothesis and independently records source coverage,
lookahead/leakage, multiple-testing, cost/capacity, test, and reproducibility
findings plus a recommendation of `revise`, `reject`, or
`ready_for_experiment_review`. No recommendation changes lifecycle state or
creates an experiment/deployment.

**Migration:** `000096_hypothesis_critic_workflows`

## Locked contracts

1. One hypothesis binds exact manifest ID/SHA, robustness policy/family/
   assessment IDs and SHAs, generated spec/version/receipt IDs and SHAs, and
   one stable workflow key. Cross-family, partial-search, failed robustness,
   or mismatched version bindings fail closed.
2. The claim is falsifiable: nonempty mechanism, predicted observation,
   explicit null, refutation threshold, evaluation horizon, and abstention
   condition are mandatory canonical fields.
3. Every source has a stable key, canonical URI, publisher, title, publication
   and point-in-time availability timestamps, content SHA-256, license, and one
   or more exact manifest observation source keys. Unmanifested, unavailable,
   future, duplicated, or hash-mismatched sources fail.
4. Search lineage is complete and ordered by stable search key. Each search
   records provider, exact query SHA-256, executed-at timestamp, bounded result
   set, and selected/unselected status with source keys and rank. Every source
   must appear in search lineage; no selected result may be omitted.
5. Hypothesis authoring and critic provenance independently require provider,
   model, system/developer/user prompt SHA-256 values, input/output token counts,
   currency, and exact nonnegative monetary cost. Raw prompts and secrets are
   not stored.
6. Declared tests have stable keys, one reviewed type, exact expected outcome,
   and pass/fail acceptance rule. OVR-601 property and example tests must be
   covered; at least one leakage, cost, baseline, and refutation test is
   mandatory.
7. The critic uses a closed finding vocabulary and severity. Each finding binds
   exact source/test/evidence references and a canonical explanation; missing
   references or free-form authority claims fail closed.
8. `ready_for_experiment_review` requires no critical/high open finding and
   explicit passes for source coverage, leakage, multiple-testing, cost/
   capacity, test completeness, and reproducibility. Equality or unknown is not
   a pass.
9. Critic recommendation is advisory evidence only. No artifact or repository
   method can approve/promote/retire a strategy, create an experiment, propose
   a deployment, reserve capital, schedule work, or emit an intent/order.
10. Canonical permutation of sources, searches, results, tests, and findings
    yields stable identity. Any semantic/provenance edit creates a new artifact;
    changed payload under one workflow key conflicts.
11. Hypothesis, source/search/test children, critic, findings/checks, and all
    bindings are append-only, atomic, and independently reconstructable.
12. OVR-602 performs no model/provider/search call. It validates caller-supplied
    point-in-time evidence only; acquisition and independent review remain
    external.

## Task 1: Evidence-bound hypothesis artifact

- [ ] Implement exact OVR-301/305/601 bindings, falsifiable claim contract,
  point-in-time source snapshots, full search lineage, authoring provenance,
  and declared tests.
- [ ] Validate every source against manifest observations and every compiled
  candidate/robustness relationship against exact immutable parents.
- [ ] Prove permutation convergence, semantic-edit identity changes, missing/
  future/unmanifested source rejection, incomplete search/test rejection,
  tamper rejection, and clone-safe restoration.
- [ ] Commit and push the hypothesis slice.

## Task 2: Independent critic artifact

- [ ] Implement closed findings and explicit source/leakage/search-adjustment/
  cost/test/reproducibility checks with advisory recommendation only.
- [ ] Require exact hypothesis binding and deterministic recommendation rules;
  prove critical/high/unknown findings cannot be review-ready.
- [ ] Prove critic permutation convergence, semantic edits change identity,
  missing references fail, tampering fails, and no lifecycle authority exists.
- [ ] Commit and push the critic slice.

## Task 3: Migration 96 and retained qualification

- [ ] Persist immutable hypothesis/source/search/result/test and critic/finding/
  check graphs with exact parent scope and PostgreSQL reconstruction.
- [ ] Prove eight-writer convergence, changed retry conflict, every-stage atomic
  rollback, forgery rejection, append-only evidence, nonempty rollback refusal,
  and empty `96 -> 95 -> 96`.
- [ ] Retain one review-ready synthetic workflow and one critical-finding
  rejection without creating experiment/deployment/lifecycle records.
- [ ] Add an inspection/recovery/rollback runbook with exact IDs and digests.
- [ ] Run focused/database races, repository-wide backend/static and pinned
  frontend gates, diff review, and isolated kill-switched schema-96 health/API/
  rollback/backup/restore/reapply.
- [ ] Commit/push verified slices, fetch, and prove `0 0` before OVR-603.

## Acceptance evidence to record

- [ ] Every hypothesis and critic artifact reconstructs exact sources,
  prompt/model hashes, declared tests, and complete search lineage.
- [ ] Critic recommendations never change promotion/deployment state; policy
  evaluators and explicit future experiment review remain authoritative.
- [ ] Local qualification is `VERIFIED_LOCAL`; model/search invocation,
  licensed source acquisition, independent review, experiment execution, shared
  migration, scheduling, deployment, broker routing, and live trading remain
  `BLOCKED_EXTERNAL`.
