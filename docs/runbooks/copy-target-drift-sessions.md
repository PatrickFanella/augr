# Copy Target-Drift Sessions Runbook

## Boundary

This runbook inspects OVR-503 schema-90 prepared paper evidence. The retained
starting values are synthetic, explicit origin-attributed inputs. They do not
prove a runtime broker/account position read. This workflow does not source a
position or quote, create an execution proposal, route an order, mutate a
shared database, schedule work, or authorize live trading.

Use the dedicated loopback database:

```bash
export COPY_DRIFT_QUALIFICATION_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/augr_ovr503_qual_20260820?sslmode=disable'
psql "$COPY_DRIFT_QUALIFICATION_DB_URL" -Atc \
  "select current_database(),current_schema(),version,dirty from schema_migrations"
```

Require `public`, clean version 90. Do not run qualification writes against a
shared or production database.

## Inspect session convergence

```bash
psql "$COPY_DRIFT_QUALIFICATION_DB_URL" -x -c \
  "select subscription_id,origin_type,origin_id,source_observation_id,
          session_key,id,sha256,maximum_session_turnover,session_budget,
          starting_drift,prepared_turnover,residual_drift,converged,leg_count
     from copy_target_drift_runs
    order by session_key"
```

Require the same subscription/origin and source observation in every row. The
retained graph uses subscription `2f7efcc7-e85f-4081-8603-d5331735cd13` and
observation `2e84cf5e-b389-47d9-bc68-d50889d5e60c`. Residual drift must be
strictly decreasing until the final zero, and no prepared turnover may exceed
its session budget or maximum policy turnover.

Confirm that no new filing was required:

```bash
psql "$COPY_DRIFT_QUALIFICATION_DB_URL" -Atc \
  "select count(*) from copy_source_observations
    where source_id=(select source_id from copy_subscriptions
                      where id='2f7efcc7-e85f-4081-8603-d5331735cd13')"
```

The retained result is exactly one observation.

## Reconstruct normalized legs

```bash
psql "$COPY_DRIFT_QUALIFICATION_DB_URL" -x -c \
  "select run.session_key,leg.sequence,leg.instrument_key,leg.side,
          leg.current_value,leg.target_value,leg.starting_drift,
          leg.requested_notional,leg.projected_value,leg.residual_drift
     from copy_target_drift_legs leg
     join copy_target_drift_runs run on run.id=leg.run_id
    order by run.session_key,leg.sequence"
```

For buys, require `projected=current+requested <= target`; for sells, require
`projected=current-requested >= target`. Each next session's explicit current
vector must equal the prior projection. Canonical instrument order, the sum of
leg notionals, and every normalized value must match the immutable run JSON.

## Retry, recovery, and rollback

An exact same-session retry is a no-op. A changed target, current value, budget,
source observation, session key, or attribution under the same unique session
must be treated as an idempotency conflict. Never update a retained run or leg;
append-only triggers intentionally reject it. Correct upstream evidence and
prepare a newly keyed session.

```bash
go test -race ./internal/copydrift
export NEW_EMPTY_COPY_DRIFT_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/new_empty_schema_90_database?sslmode=disable'
COPY_DRIFT_QUALIFICATION_DB_URL="$NEW_EMPTY_COPY_DRIFT_DB_URL" \
  go test -race ./internal/repository/postgres \
  -run '^TestCopyDriftRetainedQualification$' -count=1 -v
```

The retained writer requires an empty schema-90 drift boundary and is not a
replay command for an already-populated retained database. Use a new dedicated
database when repeating it.

Migration 90 is empty-only reversible. The disposable database
`augr_ovr503_empty_20260820` passed `90 -> 89 -> 90`. A database containing any
drift run must refuse rollback. Do not delete evidence to force rollback;
restore a pre-migration backup if operational rollback is required.
