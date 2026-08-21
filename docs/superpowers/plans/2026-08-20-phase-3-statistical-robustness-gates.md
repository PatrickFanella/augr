# Phase 3 Statistical Robustness Gates Plan

**Goal:** Complete OVR-305 with immutable, deterministic statistical evidence
that binds exact OVR-304 reports into purged walk-forward folds, explicit cost/
execution perturbations, bootstrap uncertainty, return-concentration checks,
and family-wide multiple-testing correction. No generated candidate may pass
from an unadjusted point estimate.

**Architecture:** Add an `internal/robustness` bounded context and additive
migration 80. One reviewed policy pins fold geometry, purge/embargo durations,
bootstrap algorithm/seed/iterations/confidence, concentration threshold,
perturbation-degradation threshold, family-wise alpha, and Holm-Bonferroni
correction. An assessment accepts a bounded candidate family; every candidate
contains the same number of ordered out-of-sample fold reports, and each report
is an exact immutable OVR-304 parent. The calculator derives fold returns from
retained observations, bootstraps the combined out-of-sample period returns,
calculates raw and adjusted probabilities, checks concentration and declared
perturbations, and emits explicit gate results. PostgreSQL reconstructs the
canonical assessment and normalized candidate/fold/gate graph. Assessment
results are evidence only and cannot promote, retire, schedule, or deploy.

**Migration:** `000080_statistical_robustness_assessments`

## Scope and non-goals

In scope:

- expanding purged walk-forward folds with exact train/test windows;
- explicit purge and embargo durations with ordered, nonoverlapping test data;
- one baseline and bounded named perturbations per candidate;
- deterministic nonparametric bootstrap of combined out-of-sample simple
  returns with percentile confidence intervals and one-sided empirical
  probability of nonpositive mean return;
- positive-return concentration by largest period and top-decile periods;
- family-wide Holm-Bonferroni correction across every candidate tested under
  one search-family identity;
- explicit fold, bootstrap, concentration, perturbation, adjusted-significance,
  and overall gate states with reasons and thresholds;
- scored/stress isolation, retry/restart convergence, exact normalized reload,
  retained qualification, and empty-only rollback.

Out of scope:

- inventing folds, observations, trades, perturbations, or missing reports;
- Bayesian optimization, model fitting, parameter generation, or candidate
  search (OVR-602 and later research workflows);
- promotion, retirement, approval, deployment, allocation, activation, or
  scheduling (OVR-306);
- implementing passive or active candidate strategies (OVR-401 through
  OVR-405), capacity tiers (OVR-406), or live shadow comparison (milestone 5);
- changing legacy backtest metrics, OVR-304 reports, execution, APIs, or UI.

## Locked contracts

1. Every fold test input is an exact completed OVR-304 report. Report ID,
   SHA-256, result, experiment, program, plan, account, policy, mode, window,
   ordered observations, trades, and metrics remain immutable parents.
2. A candidate has exactly the policy fold count. Fold sequences are contiguous;
   train windows end before test windows; the declared purge separates train
   end from test start; test windows never overlap; and the declared embargo
   separates a fold test end from the next fold train start.
3. Baseline and perturbation reports for one fold share the same test window,
   experiment mode, candidate version, and observation frequency. Perturbation
   kind and severity are canonical fields, never inferred from performance.
4. The assessment contains one explicit baseline scenario per candidate and a
   policy-required set of perturbation kinds. Missing, duplicated, renamed, or
   extra scenarios fail closed.
5. Bootstrap samples only combined out-of-sample period returns. Training data,
   in-sample metrics, open lots, legacy bar win rate, and stress reports cannot
   populate scored baseline significance.
6. Bootstrap algorithm, seed, iteration count, confidence level, percentile
   index rule, decimal scale, and sample ordering are policy identity. Identical
   inputs reproduce every draw, interval, probability, ID, and hash.
7. Undefined or numerically unsupported statistics carry explicit unavailable
   reasons. NaN, float infinity, silent zero substitution, and implicit sample
   dropping are forbidden.
8. Concentration uses positive out-of-sample period returns only. It reports
   largest-positive-period share and top-decile-positive-period share; no
   positive periods is an explicit failure, not a zero-concentration pass.
9. Raw empirical probabilities are corrected across every baseline candidate
   in the declared search family using deterministic Holm-Bonferroni ordering
   by `(raw_probability, candidate_id)`. The persisted adjusted value and pass
   state must reconstruct from the complete family; candidates cannot be
   evaluated alone to evade correction.
10. Perturbation stability compares each named scenario's combined fold mean
    with its candidate baseline using the locked maximum degradation threshold.
    Missing or nonpositive perturbed evidence fails the gate.
11. Scored and stress populations remain distinct by parent result/report,
    experiment/account namespace, mode, assessment identity, relational
    constraints, lists, and gate output. Stress evidence cannot satisfy scored
    gates.
12. Identical writers and restarts converge; changed payload under an accepted
    identity conflicts. Policy, assessment, candidate, fold, scenario, sample,
    statistic, and gate rows are append-only; incomplete graphs fail at commit.
13. Repositories list only by explicit search family, candidate, or parent
    report. There is no latest/best/current query and no rank-to-selection API.
14. Migration 80 is additive and empty-only reversible with no backfill,
    runtime trigger, provider call, writer grant, scheduler, API/UI cutover,
    promotion column, or deployment mutation.

## Task 1: Policy, folds, scenarios, and canonical assessment domain

- [x] Add immutable robustness-policy and search-family identities.
- [x] Add strict candidate, ordered fold, baseline, and named perturbation
  inputs bound to exact OVR-304 report identities.
- [x] Add canonical assessment, statistic, and gate states with exact threshold,
  value, availability, reason, sample, and lineage fields.
- [x] Prove restoration, clone safety, stable identities, semantic-order rules,
  tamper rejection, and scored/stress separation.
- [x] Commit and push the domain slice after focused races.

## Task 2: Deterministic statistical calculations

- [x] Validate fold count/order, train/test/purge/embargo geometry, same-window
  scenario pairing, required perturbations, mode, frequency, and evidence pins.
- [x] Implement locked deterministic bootstrap draws, percentile confidence
  bounds, one-sided empirical nonpositive probability, and explicit short/
  constant/extreme-sample handling.
- [x] Compute largest-period/top-decile positive-return concentration and
  perturbation mean degradation without using training or legacy metrics.
- [x] Apply deterministic family-wide Holm-Bonferroni correction and persist
  raw/adjusted probabilities plus fold/bootstrap/concentration/perturbation/
  adjusted-significance/overall gates.
- [x] Cover positive/negative/constant returns, insufficient samples, clustered
  winners, boundary thresholds, candidate-order convergence, and search-family
  expansion that invalidates an otherwise unadjusted winner.
- [x] Commit and push the calculator slice after focused races.

## Task 3: Migration 80 append-only robustness evidence

- [x] Add policy, search-family, assessment, candidate, fold/scenario report,
  statistic, correction, and gate tables.
- [x] Reconstruct canonical bytes, IDs/hashes, ordered counts, exact OVR-304
  parents, fold geometry, scenario completeness, calculations, and corrected
  family population in deferred constraints and normalized reload.
- [x] Reject mutation/deletion, omission/reordering, forged statistics/gates,
  cross-mode/report/window families, changed retries, and incomplete correction
  populations.
- [x] Add no best/current pointer, promotion/retirement status, scheduler,
  provider writer, runtime trigger, legacy backfill, or execution grant.
- [x] Add lock-first empty-only rollback and bump `RequiredSchemaVersion` to 80
  only after isolated real-PostgreSQL migration tests pass.
- [x] Commit and push the migration slice after focused database races.

## Task 4: Repository, service, recovery, and qualification

- [x] Add exact parent reload, atomic policy/family/assessment registration,
  normalized reconstruction, and explicit family/candidate/report listing.
- [x] Prove identical/eight-writer convergence, changed retry conflict, restart,
  interruption and rollback at every child stage, and clean-database replay.
- [x] Retain scored/stress families plus an unadjusted winner that fails after
  family expansion, stable perturbations, degraded perturbations, concentrated
  returns, unavailable statistics, and threshold boundaries.
- [x] Add a runbook covering fold design, leakage prevention, perturbations,
  uncertainty, correction interpretation, preservation, no-promotion response,
  and rollback.
- [x] Apply fresh `1 -> 80`, retain a complete family, prove nonempty rollback
  refusal and separate empty `80 -> 79 -> 80`.
- [x] Run focused/full database races, backend build/race/vet/lint/format/vuln,
  pinned Node 22 frozen install/audit/tests/lint/build, diff review, and isolated
  kill-switched schema-80 health/API/rollback/reapply smoke.
- [x] Commit/push verified slices, fetch, and prove `0 0` divergence before
  OVR-306.

## Acceptance evidence to record

- [x] No baseline passes from an unadjusted point estimate or a partial search
  family; every gate states its samples, assumptions, threshold, and reason.
- [x] Walk-forward evidence is out-of-sample, purged, embargoed, ordered, and
  bound to exact OVR-304 reports without training leakage.
- [x] Perturbation, bootstrap, concentration, and multiple-testing results
  replay identically from retained evidence and normalized rows.
- [x] Scored/stress evidence stays isolated, and no robustness pass grants
  promotion, deployment, scheduling, or capital-allocation authority.
- [x] Local qualification is `VERIFIED_LOCAL`; real candidate reports, licensed
  data, independent statistical review, promotion, deployment, and production
  cutover remain `BLOCKED_EXTERNAL`.

## Qualification record — 2026-08-20

- `VERIFIED_LOCAL`: retained isolated schema `augr_ovr305_qual_20260820`
  contains assessment `22501eba-953f-2a33-801d-63eefee6bb14`, SHA-256
  `1a53eda31330d14335ff048e5bee235482027f1359aa12a99ee69ace76c46e3a`,
  family `407dbd81-e6f8-382a-315d-1269067b66dc`, policy
  `2a72d80e-9fe8-fbea-dc55-9d807155b7f7`, and candidate version
  `3451a886-558c-39b6-e384-647c2a3cbc3e`.
- The retained normalized graph has `1/1/1/1/2/4/10/5` policy/family/
  assessment/candidate/fold/scenario/statistic/gate rows. Exact restarted reload,
  eight concurrent writers, append-only refusal, normalized-forgery rejection,
  every-child interruption rollback, and nonempty migration refusal passed.
- Domain races retain scored/stress isolation, positive/negative/constant and
  concentrated samples, stable/degraded perturbations, threshold boundaries,
  and family expansion that rejects candidates which pass unadjusted.
- Empty real-PostgreSQL rehearsal passed `78 -> 80 -> 79 -> 80`. The production
  image smoke passed fresh `1 -> 80`, authenticated read-only API health,
  rollback to schema 60, backup/restore, reapply through 80, and restarted
  health with execution credentials absent.
- Backend unit races passed across all packages; focused real-PostgreSQL races
  passed for OVR-303/304/305 and the legacy integration fixture was aligned to
  migration 62 before its race passed. Build, vet, golangci-lint, gofumpt, and
  govulncheck passed with zero reachable vulnerabilities.
- Pinned Node `22.23.2` frozen install, audit-at-high, 162 tests, lint, and build
  passed. Audit reports one low-severity Windows-only esbuild development-server
  advisory. Node 26 is unsupported and was not used as qualification evidence.
- `BLOCKED_EXTERNAL`: real candidate evidence, licensed data, independent
  statistical review, lifecycle promotion/retirement, deployment, and
  production cutover.
