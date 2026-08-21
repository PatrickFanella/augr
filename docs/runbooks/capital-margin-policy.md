# Capital-tier and margin-policy evidence

This runbook describes the local OVR-206 capital admission boundary introduced
by schema 74. It is an evidence and rehearsal system. It does not select a
broker account, change an existing account, enable a scheduler, or enable live
trading.

## Reviewed policy

One immutable `capital-margin-policy-v1` artifact contains all reviewed values:

| Profile | Initial long | Initial short | Maintenance long | Maintenance short | Maximum gross | Shorts | Evidence use |
|---|---:|---:|---:|---:|---:|---|---|
| `cash` | 1.00 | 0 | 1.00 | 0 | 1x | No | scored paper only |
| `reg_t` | 0.50 | 1.50 | 0.25 | 0.30 | 2x | Yes | scored paper only |
| `portfolio` | 0.15 | 0.30 | 0.15 | 0.30 | 6x | Yes | scored paper approximation only |
| `stress_unlimited` | 0 | 0 | 0 | 0 | unbounded | Yes | isolated synthetic stress only |

The exact starting-capital tiers are `$500`, `$5,000`, `$25,000`, `$100,000`,
`$1,000,000`, and `$5,000,000`. The policy artifact is content addressed. Its
version, SHA-256, canonical bytes, canonical JSON, and deterministic UUID must
all agree in Go and PostgreSQL.

These profiles are deterministic research approximations, not claims of broker
or regulatory parity. They do not model house requirements, concentration,
liquidity, volatility, portfolio offsets, settlement timing, borrow
availability, fees outside the simulation policy, or intraday rule changes.
Version the policy instead of editing facts when any reviewed assumption
changes.

## Account binding and isolation

An account is created explicitly through the account repository and receives
its own immutable opening-capital flow. A separate binding then copies and pins
the account ID, tier, profile, environment, buying-power multiplier, evidence
class, storage namespace, currency, and exact policy artifact.

The database independently rejects copied-fact drift. It also enforces:

- scored accounts use `promotion_evidence`, a `paper_scored/` namespace, a
  finite profile, and that profile's exact positive maximum-gross multiplier;
- stress accounts use `synthetic_stress`, a `paper_stress/` namespace,
  `stress_unlimited`, and a zero multiplier;
- every tier is one reviewed policy tier;
- one account has at most one immutable capital-policy binding.

Zero buying power is meaningful only for the isolated unbounded stress profile.
It never means zero capacity or unlimited capacity for a scored account.

## Admission semantics

`StateFromProjection` is the sole v1 capital-state builder. It consumes an exact
OVR-104 portfolio projection and requires complete active OVR-201 identity for
every open position. Only USD equity and ETF exposure is supported. Missing,
extra, mixed, derivative, prediction-market, crypto, or foreign-currency
identity fails closed.

`Assess` creates deterministic admitted or rejected evidence before routing.
The finite profiles check settled cash where applicable, initial margin,
maximum gross exposure, and post-trade maintenance. Exposure reductions remain
possible during a maintenance deficiency. Stress admission is visibly
`stress_unbounded` and never promotion eligible.

Canonical rejection reasons are:

- `short_not_supported`
- `maintenance_breach`
- `insufficient_settled_cash`
- `reserve_breach`
- `insufficient_buying_power`
- `gross_exposure_breach`

Malformed identity or unsupported assets return an error and produce no
assessment. A valid rejection produces assessment evidence but no routed order,
fill, economic normalization, ledger transaction, or simulation outcome.

## Inspection

Use a read-only PostgreSQL session. Never infer a current policy: there is no
current-policy pointer in schema 74.

```sql
SELECT policy_version, sha256, created_at
FROM capital_margin_policy_artifacts
ORDER BY created_at, policy_version;

SELECT account_id, tier, margin_profile, environment, evidence_class,
       storage_namespace, buying_power_multiplier, policy_version, created_at
FROM account_capital_policy_bindings
ORDER BY environment, tier, account_id;

SELECT a.id, a.starting_capital, a.margin_profile, a.environment,
       COUNT(f.id) FILTER (WHERE f.source = 'account_opening') AS opening_flows
FROM accounts a
JOIN account_capital_policy_bindings b ON b.account_id = a.id
LEFT JOIN capital_flows f ON f.account_id = a.id
GROUP BY a.id, a.starting_capital, a.margin_profile, a.environment
ORDER BY a.environment, a.starting_capital;
```

For the reviewed replay matrix, call `capital.NewReplayMatrix` with the one
reviewed policy and seven explicit account contexts. The six scored results
have a scored-matrix hash. The separate stress result is excluded from that
hash, and the complete evidence set has a distinct canonical hash.

## Incident response

Treat any of the following as an incident and stop new exposure for the
affected account:

- a binding cannot be reconstructed from its policy artifact;
- account facts differ from the immutable binding;
- a projection checksum, payload, instrument set, or currency fails capital
  state construction;
- the runtime cannot find the exact pinned policy version;
- scored and stress namespaces or evidence classes are mixed;
- an idempotency retry presents changed artifact or binding bytes;
- schema version is below or above the runtime-required version.

Preserve the source projection, artifact, binding, and assessment bytes. Do not
rewrite the account or binding. Repair upstream identity or introduce a new
versioned policy/account according to the incident's cause.

## Migration and rollback

Migration 74 creates no accounts, policies, bindings, grants, runtime route, or
activation flag. Apply it explicitly and restart a process that previously
failed schema-version startup. The down migration takes exclusive locks and
refuses while either schema-74 table contains evidence. To rehearse rollback,
use an empty isolated database and prove `74 -> 73 -> 74`; do not delete
evidence merely to make downgrade succeed.

## Qualification boundary

Local qualification requires all migrations through 74 on a dedicated
loopback-only PostgreSQL instance, isolated Redis, the global kill switch on,
live trading and the scheduler off, and no provider credentials. Retain one
policy artifact, six distinct scored Reg-T accounts and bindings, and one
distinct stress/unlimited account and binding. Reload them after restart and
verify the retained row counts are unchanged.

Passing this rehearsal is `VERIFIED_LOCAL`. It is not deployment, protected
staging, real-provider, broker-margin, or production evidence. Shared database
migration and any cutover remain separate authorized operations.
