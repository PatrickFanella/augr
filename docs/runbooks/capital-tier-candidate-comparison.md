# Capital-Tier Candidate Comparison Runbook

## Boundary

This runbook verifies OVR-406 schema-87 research evidence. It compares five
closed family kinds at the six reviewed finite OVR-206 tiers. It does not rank,
recommend, promote, allocate, schedule, deploy, activate, or place orders.

Use only a dedicated loopback database:

```bash
export CAPACITY_V1_QUALIFICATION_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/augr_ovr406_qual_20260820_v2?sslmode=disable'
psql "$CAPACITY_V1_QUALIFICATION_DB_URL" -Atc \
  "select current_database(),current_schema(),version,dirty from schema_migrations"
```

Require schema `public`, clean version 87, and zero contract/comparison rows
before the one-shot retained write.

## Source-contract inspection

```bash
psql "$CAPACITY_V1_QUALIFICATION_DB_URL" -x -c \
  "select family,evaluation_id,evaluation_sha256,source_report_id,
          source_report_sha256,after_cost_return,capacity_available,
          unavailable_reason,capital_per_unit,maximum_units
     from capacity_v1_contracts order by family"
```

`source_capacity_not_observed` is a valid and important result. It means prior
evidence did not retain executable depth sufficient for a defensible scale
ceiling. Never replace it with midpoint liquidity, hidden size, average volume,
or a generic multiplier. In the retained local fixture, passive control,
wheel, momentum, and trend remain unavailable for that reason. Defined-risk
options retain a `$122` whole-spread reservation and ten-spread two-leg depth.

## Tier arithmetic

```bash
psql "$CAPACITY_V1_QUALIFICATION_DB_URL" -x -c \
  "select f.family,f.minimum_viable_available,f.minimum_viable_tier,
          t.ordinal,t.tier,t.viable,t.reason,t.units,
          t.executable_capital,t.unused_capital,t.saturated
     from capacity_v1_families f
     join capacity_v1_tiers t on t.comparison_id=f.comparison_id
                              and t.family_sequence=f.sequence
    order by f.family,t.ordinal"
```

Require exactly the ordered tiers `500`, `5000`, `25000`, `100000`, `1000000`,
and `5000000` for every family. Available-family units are
`floor(tier / capital_per_unit)` capped by `maximum_units`. Executable capital
is units times capital per unit; unused capital is tier minus executable
capital. `saturated=true` means source depth, not policy capital, binds. The
first viable reviewed tier is minimum viable capital. Unavailable families
must have no manufactured minimum.

After-cost return is source evidence and must be unchanged across tiers. It is
not multiplied by capital or interpreted as scale-invariant profit.

## Verification and recovery

```bash
go test -race ./internal/capacity/...
DB_URL="$CAPACITY_V1_QUALIFICATION_DB_URL" \
  go test -race ./internal/repository/postgres -run 'TestCapacity' -count=1
CAPACITY_V1_QUALIFICATION_DB_URL="$CAPACITY_V1_QUALIFICATION_DB_URL" \
  go test -race ./internal/repository/postgres \
  -run TestCapacityRetainedQualification -count=1 -v
```

Expected retained inventory: 5 contracts, 1 comparison, 5 family rows, and 30
tier rows. Eight identical writers must converge. Any injected family/tier
write failure must leave no comparison graph. Mutation/deletion must fail with
`capacity v1 evidence is append-only`; normalized-row drift must fail reload.

Migration 87 is empty-only reversible. On a separate empty disposable database:

```bash
migrate -path migrations -database "$EMPTY_DATABASE_URL" down 1
migrate -path migrations -database "$EMPTY_DATABASE_URL" up 1
```

Require `87 -> 86 -> 87`. Never delete retained evidence to force rollback,
and never run this workflow against production or a shared database.
