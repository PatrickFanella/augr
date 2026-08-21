# Defined-Risk Options V1 Qualification Runbook

## Purpose and boundary

This runbook verifies OVR-405's immutable Defined-Risk Options V1 research
evidence at schema 86. It covers vertical structure, point-in-time quotes,
maximum loss/reward, reservation, atomic or sequential fills, orphan unwind,
expiry settlement, normalized persistence, and deterministic OVR-303 replay.

It does not authorize provider calls, broker complex orders, paper/live order
routing, promotion, allocation, scheduling, deployment, shared-database
migration, or production activation. The common-runner adapter is a research
simulation adapter; `atomic_package` is a pinned assumption, not a claim that a
broker accepted an atomic package.

## Safety preflight

Use a dedicated loopback database. Never point these commands at production or
a shared developer database.

```bash
export DEFINED_RISK_V1_QUALIFICATION_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/augr_ovr405_qual_20260820?sslmode=disable'
psql "$DEFINED_RISK_V1_QUALIFICATION_DB_URL" -Atc \
  "select current_database(),current_schema(),inet_server_addr(),inet_server_port()"
psql "$DEFINED_RISK_V1_QUALIFICATION_DB_URL" -Atc \
  "select version,dirty from schema_migrations"
```

Require a dedicated database, schema `public`, a loopback server address, and
clean version 86. Before retention, require zero rows across the three parent
tables:

```bash
psql "$DEFINED_RISK_V1_QUALIFICATION_DB_URL" -Atc \
  "select (select count(*) from defined_risk_v1_policies)+
          (select count(*) from defined_risk_v1_scenarios)+
          (select count(*) from defined_risk_v1_reports)"
```

## Structure and risk replay

Inspect the two ordered legs and independently recompute width and executable
premium. Long opens use ask; short opens use bid. Quantity is an integer and
both multipliers must match.

```bash
psql "$DEFINED_RISK_V1_QUALIFICATION_DB_URL" -x -c \
  "select s.id,s.strategy,p.execution_mode,s.initial_capital,
          s.requested_contracts,l.sequence,l.option_type,l.strike,l.position,
          l.canonical_leg->'entry'->>'bid' as bid,
          l.canonical_leg->'entry'->>'ask' as ask,
          l.canonical_leg->'entry'->>'bid_size' as bid_size,
          l.canonical_leg->'entry'->>'ask_size' as ask_size
     from defined_risk_v1_scenarios s
     join defined_risk_v1_policies p on p.id=s.policy_id
     join defined_risk_v1_legs l on l.scenario_id=s.id
    order by s.id,l.sequence"
```

For debit verticals, per-contract maximum loss is executable debit times the
multiplier plus two opening-leg fees. For credit verticals it is strike width
times the multiplier minus executable credit plus those fees. Reservation is
maximum loss plus the policy's evidence-derived orphan reserve in sequential
mode, multiplied by whole contracts. Compare the stored derivation:

```bash
psql "$DEFINED_RISK_V1_QUALIFICATION_DB_URL" -x -c \
  "select id,strategy,execution_mode,outcome,contracts,width,
          net_premium_per_contract,maximum_loss_per_contract,
          maximum_reward_per_contract,orphan_reserve_per_contract,
          reserved_capital from defined_risk_v1_reports order by id"
```

## Fill and orphan reconciliation

Atomic rejection must contain zero fills. A settled atomic or sequential
spread contains the protective long open followed by the short open. A
sequential orphan contains the protective long open followed by an `unwind`
sell tied to the separately pinned unwind observation.

```bash
psql "$DEFINED_RISK_V1_QUALIFICATION_DB_URL" -x -c \
  "select r.id,r.outcome,r.reason,r.contracts,r.entry_fees,r.unwind_fees,
          r.orphan_loss,f.sequence,f.instrument_id,f.action,f.quantity,
          f.price,f.fee,f.evidence_id,f.evidence_sha256
     from defined_risk_v1_reports r
     left join defined_risk_v1_fills f on f.report_id=r.id
    order by r.id,f.sequence"
```

For an orphan, recompute cash as initial capital minus protective ask cash and
its fee, plus unwind bid cash minus its fee. Do not substitute midpoint,
theoretical value, the short-leg quote, or completed-spread maximum loss.

## Settlement reconciliation

Only European cash settlement at the pinned expiry is supported. Each call's
intrinsic value is `max(terminal - strike, 0)` and each put's is
`max(strike - terminal, 0)`; short legs negate intrinsic value. Sum both legs,
multiply by the common multiplier and whole contracts, then reconcile opening
cash, fees, payoff, ending cash, and return.

```bash
psql "$DEFINED_RISK_V1_QUALIFICATION_DB_URL" -x -c \
  "select r.id,s.terminal_underlying,r.expiration_payoff,r.entry_fees,
          r.ending_cash,r.after_cost_total_return
     from defined_risk_v1_reports r
     join defined_risk_v1_scenarios s on s.id=r.scenario_id
    order by r.id"
```

Exact-strike intrinsic value is zero. American exercise, early assignment,
dividends, physical delivery, and probabilistic pin treatment are unsupported
and must fail before a scenario is declared.

## Deterministic and database qualification

```bash
go test -race ./internal/strategy/definedrisk/...
DB_URL="$DEFINED_RISK_V1_QUALIFICATION_DB_URL" \
  go test -race ./internal/repository/postgres -run 'TestDefinedRisk' -count=1
DEFINED_RISK_V1_QUALIFICATION_DB_URL="$DEFINED_RISK_V1_QUALIFICATION_DB_URL" \
  go test -race ./internal/repository/postgres \
  -run TestDefinedRiskRetainedQualification -count=1 -v
```

The retained write is one-shot and refuses a nonempty target. Expected
inventory is 2 policies, 7 scenarios, 14 legs, 16 quote observations, 7
reports, and 12 fills. The matrix contains all four verticals, winning, losing,
exact-strike, atomic refusal, sequential success, and orphan unwind paths.

## Failure and recovery

- A policy/scenario/report identity conflict means canonical input drift; do
  not overwrite the existing row.
- A graph reconstruction failure means a parent or normalized child is missing,
  reordered, or forged; preserve the database and investigate.
- Any injected failure at scenario, leg, observation, report, or fill stage must
  roll back the whole transaction.
- Mutation and deletion must fail with `defined-risk v1 evidence is
  append-only`.
- Restart recovery consists only of constructing a fresh repository and loading
  the same report ID. The bytes and SHA-256 must match exactly.

## Rollback

Migration 86 is additive and empty-only reversible. A nonempty database must
refuse rollback without changing retained evidence.

For a separate empty disposable database only:

```bash
migrate -path migrations -database "$EMPTY_DATABASE_URL" down 1
migrate -path migrations -database "$EMPTY_DATABASE_URL" up 1
```

Require `86 -> 85 -> 86`. Never delete retained evidence to make rollback
succeed, and never run this against production or a shared database.
