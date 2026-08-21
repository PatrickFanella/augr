# Statistical robustness evidence

This runbook operates OVR-305 statistical evidence locally. A robustness
assessment is immutable research evidence. It does not promote or retire a
strategy, schedule a run, allocate capital, activate a deployment, or authorize
live trading. OVR-306 owns policy-governed lifecycle decisions.

## Preconditions

Use an isolated PostgreSQL database or schema at migration 80. Never point the
qualification variables at production or a shared development database. Keep
the kill switch engaged and provider execution credentials absent.

The exact parent chain must already exist:

1. an immutable strategy version and declared experiment (OVR-302);
2. a completed reproducible result (OVR-303);
3. a completed trade/portfolio evaluation for every baseline and perturbation
   window (OVR-304);
4. one declared search family containing every candidate actually tried.

Record IDs and SHA-256 values for the policy, family, assessment, every
candidate, and every source report. Preserve canonical bytes and normalized
rows together; neither is sufficient evidence alone.

## Fold design and leakage prevention

Choose walk-forward folds before inspecting test results. Every candidate uses
the policy's exact fold count and the same ordered geometry. For each fold:

- `train_end + purge <= test_start`;
- test windows are nonoverlapping and ordered;
- `prior_test_end + embargo <= next_train_start`;
- baseline and perturbation reports cover the identical test window;
- all reports use the candidate's exact compiled strategy version, experiment
  mode, and evaluation policy;
- training observations never enter bootstrap or concentration samples.

Reject the assessment if any parent report is missing, mutable, cross-mode,
outside its admitted experiment window, or not reconstructable. Do not fill a
gap with a nearby report or infer a perturbation from its returns.

## Perturbations

The reviewed policy names every required perturbation. The current contract
requires an explicit severity string and an immutable OVR-304 report for each
kind in every fold. Cost, slippage, latency, fill, and liquidity assumptions
must be changed upstream and rerun; renaming a baseline report does not create
perturbation evidence.

`perturbation_stability` compares combined out-of-sample mean returns with the
locked degradation threshold. A missing or nonpositive perturbed sample fails
closed. Preserve failure evidence; do not silently remove an adverse scenario.

## Uncertainty, concentration, and family correction

The policy pins bootstrap algorithm, seed, iterations, confidence level,
decimal scale, and sample ordering. Replays must reproduce the same interval,
raw probability, identity, and hash. Unsupported, non-finite, constant, or
short samples use explicit states and reasons; they are never converted to a
passing zero.

Positive-return concentration reports the largest positive-period share and
top-decile positive-period share. No positive periods is a failure. A strategy
whose gains depend on too few observations fails even if its mean is positive.

Holm-Bonferroni correction covers the complete declared search family. Read
`raw_nonpositive_mean_probability` only as an intermediate value. Promotion
review must use `holm_adjusted_nonpositive_mean_probability` and the explicit
`multiple_testing_adjustment` and `overall_robustness` gates. Adding candidates
creates a new family and assessment; never evaluate a preferred candidate
alone to evade the family-wide correction.

## Verification

Run focused deterministic and PostgreSQL races first:

```bash
go test -race ./internal/robustness -count=1
DB_URL="$ISOLATED_DB_URL" go test -race ./internal/repository/postgres \
  -run '^TestRobustness' -count=1
```

Then run the repository's full backend, frontend, migration, image, health,
backup/restore, and rollback/reapply gates from the release-readiness runbook.
A passing unit test is not retained-database, image, runtime, deployment, or
user-experience proof.

For retained qualification, preserve:

- schema version and database/schema name;
- policy, family, assessment, candidate, and report IDs plus SHA-256 values;
- normalized row counts for candidates, folds, scenarios, statistics, gates;
- scored and stress population isolation;
- identical retry and restarted-process reconstruction;
- one unadjusted winner rejected after family expansion;
- stable and degraded perturbations, concentrated returns, unavailable values,
  and exact threshold boundaries;
- nonempty rollback refusal.

Label local synthetic evidence `VERIFIED_LOCAL`. Real candidate reports,
licensed market data, independent statistical review, lifecycle changes,
deployment, and production cutover remain `BLOCKED_EXTERNAL` until separately
authorized and verified.

## Failure response

If reconstruction, a gate, or a replay fails:

1. stop evaluation of the affected family;
2. preserve canonical and normalized rows, logs, policy identity, and source
   report lineage;
3. classify the failure as missing evidence, leakage/geometry, perturbation,
   uncertainty, concentration, family correction, or persistence/recovery;
4. create new upstream evidence rather than editing accepted rows;
5. rerun the complete family under a new content-addressed assessment.

Never update or delete an assessment to make it pass. Mutation triggers and
deferred reconstruction constraints are intentional containment controls.

## Rollback

Migration 80 is empty-only reversible. Before attempting `80 -> 79`, verify all
robustness policy, family, and assessment tables are empty. A database holding
any robustness evidence must refuse rollback. Preserve or export required
evidence, use a separately authorized retention process, and rerun rollback on
an empty isolated database. Never bypass append-only triggers or delete
evidence merely to satisfy a rollback drill.
