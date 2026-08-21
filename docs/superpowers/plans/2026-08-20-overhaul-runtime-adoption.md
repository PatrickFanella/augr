# Total-overhaul runtime adoption plan

## Scope and safety boundary

Adopt the schema-60 through schema-103 mechanisms into the existing
`tradingagent` process in dependency order. Every mutation path begins disabled,
uses the disposable local database first, and fails closed when its configured
dependencies, schema version, or retained evidence are absent. This plan does
not authorize production deployment, shared-database migration, provider
acquisition, broker routing, capital movement, or a real OVR-702 campaign.

## Cutover sequence

### R0 — read-only inspection

- [x] Persist and recursively reconstruct schema-103 milestone assessments.
- [x] Wire authenticated `GET /api/v1/evidence/assessments/{id}` into the main
  runtime.
- [x] Prove missing, malformed, and forged evidence fails closed through API and
  PostgreSQL restart tests.

### R1 — accounts and balanced ledger

- [x] Add `OVERHAUL_ACCOUNTS_READ_ENABLED=false` and expose read-only scored
  account, capital-tier, and ledger projections.
- [ ] Add an idempotent local bootstrap command for supported tiers, including
  $500 and $5 million; keep it unavailable from HTTP and disabled in runtime.
- [ ] Add `OVERHAUL_LEDGER_WRITE_ENABLED=false`; permit deposits and withdrawals
  only through balanced schema-61 postings with immutable account identity.
- [ ] Prove restart, duplicate-request, concurrent-write, withdrawal, and history
  preservation behavior against disposable PostgreSQL.

### R2 — execution and reconciliation

- [ ] Add read-only lifecycle, reservation, fill, position, and reconciliation
  projections before enabling any writer.
- [ ] Gate simulated execution with `OVERHAUL_PAPER_EXECUTION_ENABLED=false` and
  require executable timestamped inputs, venue rules, typed origin, and complete
  costs.
- [ ] Keep live routing separately fenced by the existing live-trading brake and
  a new `OVERHAUL_LIVE_EXECUTION_ENABLED=false`; never infer authorization from
  paper enablement.
- [ ] Prove crash recovery at each lifecycle boundary, duplicate callbacks,
  partial fills, settlement, and drift incidents.

### R3 — research and strategy lifecycle

- [ ] Require immutable manifests and point-in-time datasets for new experiment
  entrypoints behind `OVERHAUL_RESEARCH_ENABLED=false`.
- [ ] Route promotion and retirement through schema-81 decisions; generated
  strategies remain unable to activate themselves.
- [ ] Prove deterministic replay, robustness/multiple-testing rejection, passive
  benchmark comparison, and exact version lineage.

### R4 — copy and prediction strategies

- [ ] Adopt origins, fresh quote/session gates, drift, and point-in-time filing
  evidence for copy trading behind `OVERHAUL_COPY_ENABLED=false`.
- [ ] Adopt retained book, fee, reservation, queue, and settlement semantics for
  prediction strategies behind `OVERHAUL_PREDICTION_ENABLED=false`.
- [ ] Require licensed/provider evidence and real venue calibration before any
  non-synthetic qualification claim.

### R5 — unattended control plane

- [ ] Instantiate the fenced financial scheduler behind
  `OVERHAUL_SCHEDULER_ENABLED=false` and retain every trigger/run result.
- [ ] Instantiate the supervisor behind `OVERHAUL_SUPERVISOR_ENABLED=false`;
  require persistent brake state, reconciliation health, bounded retries, and
  no direct strategy-activation authority.
- [ ] Emit schema-100 cost attribution and exactly one schema-101 daily brief;
  delivery remains disabled until separately configured and authorized.
- [ ] Prove multi-restart unattended soak, brake/reconciliation incidents, daily
  idempotency, and explanation completeness on disposable infrastructure.

### R6 — elapsed evidence program

- [ ] Select candidates, sources, retention, and scheduler policy with explicit
  user authorization.
- [ ] Run a real 30-day shadow campaign, then a real 60–90-day scored-paper
  campaign; elapsed observations cannot be synthesized or backdated.
- [ ] Run comparable portfolio paper and evidence-linked architecture review.
- [ ] Only after all gates pass, prepare a separate production rollout decision.

## Verification and commit discipline

Each checkbox is a coherent commit only after focused race tests, static checks,
PostgreSQL restart/recovery tests, and relevant API/UI tests pass. Each R-stage
ends with the full release verifier on a clean tree and a synchronization check
against `origin/codex/augr-overhaul`.
