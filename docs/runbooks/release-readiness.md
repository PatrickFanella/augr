---
title: "Release readiness and recovery drills"
status: "canonical"
updated: "2026-08-07"
---

# Release readiness and recovery drills

Live trading stays disabled throughout this gate. Query
`GET /api/v1/release/readiness` with an authenticated operator token. Every
required paper capability must be ready. Polymarket remains visible as an
optional, blocked historical capability and is not a release requirement for
the US deployment. `live_execution` remains a separate, non-required, blocked capability until a broker/market/strategy/capital-tier
activation is explicitly approved.

Run the automated gate from the repository root:

```sh
./scripts/release-gate.sh
```

## Commit identity and synchronization order

The gate is valid only for the exact commit it prints. Reconcile upstream
before running it:

1. fetch the configured remote and inspect divergence without rewriting or
   discarding local work;
2. reconcile any upstream commits and commit the result;
3. confirm the intended release tree is clean, then run the complete gate;
4. push the exact verified commit without force and confirm the remote ref
   resolves to the same object ID; and
5. build and deploy immutable images from that same object ID.

The release gate records `HEAD` before verification, rechecks the clean tree
after all gate commands, and fails if `HEAD` changed. Any edit, conflict
resolution, merge, rebase, amend, or generated tracked change after a passing
gate invalidates the result; rerun the complete gate on the new candidate.
Pushing an unchanged verified commit does not invalidate it.

## Compromised-secret gate

The automated gate cannot prove that an externally managed credential was
revoked. If a credential, token, passphrase, or private key was exposed in
application logs, audit output, CI output, or another retained channel, the
release is blocked until its owner:

1. revokes or rotates the exposed value at the provider;
2. updates the production secret source without printing the replacement;
3. records owner confirmation in the release evidence; and
4. identifies a bounded postdeployment canary that proves errors are redacted
   without copying, hashing, or otherwise retaining the credential in the
   evidence artifact.

A redaction code change prevents another disclosure but does not remediate the
already exposed value. Passing tests, `RELEASE_DRILLS_VERIFIED=true`, a process
restart, or an unavailable retained log window cannot substitute for rotation
confirmation. Optional integrations with unmet credentials or entitlements may
remain unavailable only when they are explicitly reported as blocked and stay
disabled or fail closed; do not enable them merely to make release readiness
appear green.

For a timestamped operational snapshot around a paper-market boundary, run:

```sh
OBSERVATION_REPORT=/absolute/path/to/evidence.txt \
  ./scripts/observe-paper-boundary.sh <safe-label>
```

The observer records service/database state from the preceding 30 minutes and
only whitelisted warning/error metadata. It deliberately omits raw error
fields, message text, query strings, and provider bodies. Set
`AUGR_COMPOSE_FILE` or `AUGR_BASE_URL` only when observing a different approved
deployment. A snapshot taken after the fact is operational context, not proof
that a scheduled automation was prospectively observed through its inputs,
execution, persistence, and downstream effects. Do not copy unsanitized
production logs into a tracked evidence file.

For a bounded prospective observation that starts before a known job boundary,
run:

```sh
OBSERVATION_REPORT=/absolute/path/to/evidence.txt \
  ./scripts/observe-automation-run.sh <job-name> <not-before-ISO-8601> <safe-label>
```

The prospective observer records its arm time, wakes for a lead-time health and
database precheck, polls across the actual boundary, pins the first admitted
durable run ID, follows that same row to terminal state, and records post-state
plus only allowlisted warning/error metadata. The lead defaults to ten seconds
and can be adjusted with `OBSERVATION_LEAD_SECONDS`. The terminal timeout
defaults to two hours; set `OBSERVATION_TIMEOUT_SECONDS` explicitly for a known
longer job. It refuses a not-before timestamp that is not still in the future,
preventing a retrospective snapshot from being mislabeled prospective. Raw
error text and result values are never retained. This generic evidence still
does not prove job-specific provider contact, prompt/model routing, or domain
writes; pair it with narrowly scoped, sanitized job-specific inspection.

Validate Prometheus rules with `promtool check rules
monitoring/prometheus/alerts.yml` (or the matching Prometheus container image).
Do not set `RELEASE_DRILLS_VERIFIED=true` until the evidence table below is
complete for the deployment being promoted. The flag only records operator
attestation; it does not bypass any capability check or enable live trading.

| Drill | Required evidence | Recovery criterion |
|---|---|---|
| Restart | rolling-restart steps, schema gate output, paper account bootstrap logs | runtime returns healthy with kill-switch and durable positions restored |
| Dependency outage | broker and LLM outage tests/runbooks, alert delivery | deterministic/paper paths fail closed or fall back as documented |
| Stale data | snapshot freshness tests and provider last-success metric | entry is rejected and stale source is identified |
| Order rejection | order-manager rejection test and journal/replay row | no position appears; rejection remains explainable |
| Partial fill | fill-engine/broker partial-fill tests and reconciliation result | filled quantity, cash, trade, and position agree |
| Reconciliation | Alpaca, Polymarket, Kalshi, and options reconciliation tests | zero unexplained drift; any deliberate fixture drift alerts |
| Kill switch | API/file/env and mid-run cancellation tests | new orders stop and active execution is cancelled safely |
| WebSocket reconnect | API smoke/reconnect tests | authenticated reconnect resumes without corrupting persisted state |
| Prediction settlement | shared settler and provider-job tests | 0/1 payout, P&L, closed decision, and replay outcome agree |
| Options expiration | expiry workflow tests | worthless and intrinsic cash settlement persist correctly |
| Options assignment | explicit paper assignment-boundary test | no underlying shares are fabricated; paper options cash-settle and live assignment remains blocked |

For a real soak, run at least one complete scheduler cycle for each enabled
paper market, inspect `/api/v1/automation/status`, `/api/v1/risk/cockpit`, the
decision journal/replay, and Prometheus alerts, then attach timestamps and query
outputs to the release record. Any reconciliation drift, incomplete decision
journal, missing settlement, or unexplained alert fails the release.
