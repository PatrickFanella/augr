# Milestone 7 Evidence Program Runbook

## Current result

The locally executable golden replay/restart campaign is `VERIFIED_LOCAL`.
OVR-702 through OVR-705 do not yet have real elapsed campaign evidence and are
`BLOCKED_EXTERNAL`. The code in `internal/evidenceprogram` and the schema-102
repository qualify their fail-closed assessment and durable evidence machinery;
they do not qualify elapsed time, external observations, or deployment.

The complete local gate passed on commit
`36bd23e07173ceb22f202a41f5f7111b239ca418`. It included the focused campaign
twice, repository backend tests/vet/static analysis, 162 pinned frontend tests
plus lint/typecheck/build, Compose and Dockerfile checks, isolated production
health/authenticated read-only API, schema `101 -> 60 -> 101` with schema-60
backup/restore, seven Prometheus rules, secret-history review across 1,292
commits, and final clean-tree/commit identity.

Do not turn a synthetic test interval into a 30-, 60-, or 90-day claim. Do not
infer profitability or readiness from source inspection, a passing unit test,
a healthy container, or an unscored paper account.

## OVR-701 golden replay and restart

Run from a clean commit:

```bash
./scripts/golden-replay-campaign.sh
```

The command runs every selected test twice under the race detector and refuses
success if the commit changes while it runs. It covers:

- exact experiment replay after restart, clean-database reproduction, partial
  fills, explicit no-ops/rejections, and scored/stress separation;
- failed completion retry plus rollback after every normalized result stage;
- capital-policy replay without duplicate economics;
- exact ledger projection rebuild, concurrent convergence, failure atomicity,
  and late backdated correction;
- venue reconciliation across Alpaca/Kalshi, independent perturbations,
  correction/bust/unstable states, and restart after every persisted stage;
- execution-lifecycle child failure, idempotent prediction settlement, and
  emergency-brake restart/reduce-only behavior.

This is deterministic local replay evidence, not a venue or deployment soak.

## OVR-702 shadow assessment

`NewShadowCampaign` binds a stable campaign key and exact UTC start to one
content-addressed OVR-401 benchmark report and two to sixteen distinct OVR-302
strategy versions. `NewShadowDay` accepts exactly one complete, source-linked
candidate observation for each sequence from 0 through 29. `BuildShadowAssessment`
derives the assessor input from the ordered retained graph rather than accepting
an operator-authored summary.

Schema 102 stores campaign, candidate, day, and day-candidate rows append-only.
The repository converges identical concurrent writes, rejects changed artifacts
under a stable key or day sequence, rolls every injected intermediate failure
back atomically, and reconstructs canonical bytes after restart. The down
migration refuses to discard retained campaign evidence.

`AssessShadow` then requires one exact UTC interval of at least 30 elapsed days,
complete daily evidence, at least two unique candidates, at least 30 observed
days per candidate, zero critical defects, executable-data samples, simulated
fills, and a measured decimal slippage divergence. Missing or defective
evidence produces `held` with sorted blockers. The local 30-day database fixture
proves only deterministic machinery; its synthetic dates and observations are
not elapsed campaign evidence.

A real run additionally needs separately authorized candidate selection,
scheduling, provider access, retained daily data, and deployment. None was
started by local qualification.

## OVR-703 scored-paper assessment

`AssessPaper` binds the exact qualified shadow assessment and accepts only
60–90 elapsed days. Every candidate needs observations, complete after-cost
evidence, statistically honest scoring, and bounded paper margin. It returns:

- `qualified` when at least one complete candidate has positive after-cost
  expectancy;
- `rejected` when complete evidence honestly shows no positive candidate;
- `held` when duration, costs, statistics, margin, observations, or the shadow
  parent are incomplete.

An honest `rejected` outcome completes the research question; it grants no
promotion or execution authority.

## OVR-704 portfolio assessment

`AssessPortfolio` requires a qualified positive scored-paper parent and compares
the combined allocation with the best single sleeve over the same interval and
cost basis. Equal or better risk-adjusted evidence is `qualified`; worse
evidence is `rejected`; a noncomparable or incomplete graph is `held`. The
assessment does not move capital, resize an account, or place orders.

## OVR-705 readiness assessment

`AssessReadiness` requires exact evidence for all seven capabilities:

1. `accept_deposits`
2. `resize_safely`
3. `run_unattended`
4. `brake`
5. `restart`
6. `reconcile`
7. `daily_explanation`

Every capability has its own evidence ID and SHA-256. The result is `ready` only
when the portfolio parent is qualified and every capability passes. A complete
negative review is `not_ready`; an incomplete portfolio graph is `blocked`.
The result cannot enable live trading. ADR 019 still requires a separate future
live-activation decision after real shadow/scored-paper evidence exists.

## Local verification

```bash
go test -race -count=2 ./internal/evidenceprogram
go test -race -count=1 ./internal/repository/postgres -run '^TestShadowCampaignRepository'
go vet ./internal/evidenceprogram
./scripts/golden-replay-campaign.sh
./scripts/release-gate.sh
```

## Authority boundary

- `VERIFIED_LOCAL`: deterministic assessment construction, content addressing,
  exact benchmark/version parent binding, append-only schema-102 persistence,
  atomic and concurrent repository behavior, restart reconstruction, stable
  blockers, honest rejection, race/static tests, and the OVR-701 replay/restart
  campaign.
- `BLOCKED_EXTERNAL`: real 30-day shadow run, real 60–90 day scored-paper run,
  portfolio paper run, retained capability review, provider/deployment soak,
  shared migration, scheduler adoption, capital mutation, broker routing,
  production cutover, and live trading.

The next authorized operational step is to select the two shadow candidates and
approve the scheduler/provider/deployment/data-retention scope. Until then,
there is intentionally no retained OVR-702 campaign assessment to inspect.
