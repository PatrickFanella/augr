# Full Cost Attribution Runbook

## Boundary

This runbook inspects the synthetic OVR-606 schema-100 cost evidence boundary.
The retained database is loopback-only. Attribution reports describe costs and
rebates; they cannot fetch invoices, write ledger economics, change an evidence
review or promotion decision, deploy a strategy, schedule work, alter risk,
allocate capital, settle, route an order, or trade.

Use only the dedicated retained qualification database:

```bash
export COST_ATTRIBUTION_QUALIFICATION_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/augr_ovr606_qual_v5_20260820?sslmode=disable'
psql "$COST_ATTRIBUTION_QUALIFICATION_DB_URL" -Atc \
  'select current_database(),current_schema()'
```

Require `augr_ovr606_qual_v5_20260820` and `public`. Never run qualification
writes against a shared or production database.

## Inspect retained statements and parents

```bash
psql "$COST_ATTRIBUTION_QUALIFICATION_DB_URL" -x -c \
  "select id,sha256,case_id,summary_id,hypothesis_id,manifest_id,account_id,
          window_start,window_end,currency,actual_costs,estimated_costs,
          actual_rebates,estimated_rebates,known_net_cost,unknown_count,coverage
     from full_cost_attribution_reports order by id"
```

Retained evidence includes:

- incomplete report `ee15a682-2fa0-d1fd-5fee-26ef1568718e`, SHA-256
  `38d3911c85690319e8d62cd9a963f44d1605107635d7d79f63d12298dee70d2f`,
  known net cost `3.75`, and one explicit infrastructure unknown;
- complete-with-estimates report `b118a586-995e-f07e-bbf3-e0a88bc90441`,
  SHA-256
  `0e42fe028f068bbcaf70520f4d151d59a7829099c113518a404a777eea982e94`,
  known net cost `4.5`, and zero unknowns;
- exact OVR-602 hypothesis `5bebfd98-db48-be22-c564-b925f3a4289c` and
  OVR-301 manifest `0f80f104-790b-d376-2367-0b5b12796ee2`;
- account `00000000-0000-4000-8000-000000000064`;
- actual fee transaction `0b547577-8f7e-4f1c-a477-0feb45af9224` and actual
  rebate transaction `988d8f67-25f3-4251-9c29-17b7bc1afafb`.

Each report also binds one exact OVR-603 case/summary and their digests. The
summary remains authoritative only for review consensus; the cost report does
not revise its checks or promotion outcome.

## Inspect knowledge state, evidence, and methods

```bash
psql "$COST_ATTRIBUTION_QUALIFICATION_DB_URL" -x -c \
  "select report_id,sequence,line_key,category,knowledge_status,amount,
          evidence_kind,evidence_id,evidence_sha256,method,method_sha256,
          explanation
     from full_cost_attribution_lines order by report_id,sequence"
```

Interpret the states literally:

- `actual` has an amount and retained evidence. Model actuals are reconstructed
  from exact hypothesis provenance. Fee/rebate actuals are reconstructed from
  the bound account, immutable ledger postings, event type, currency, and
  reporting window.
- `estimated` has an amount, content-addressed evidence, a method key/digest,
  and explanation. It is not promoted to actual merely because the arithmetic
  is exact.
- `unknown` has no amount, evidence, or method. It contributes to
  `unknown_count`, never zero. `known_net_cost` therefore is not a full total
  while coverage is `incomplete_unknown`.

Fees add to cost; rebates subtract. Currency conversion is absent by design.
Never compare or combine reports in different currencies without a separately
reviewed, point-in-time FX artifact.

## Failure handling and correction

1. Inspect the exact case/summary, hypothesis/manifest, account/window, line
   evidence, and method digest. Do not edit a report; schema 100 is append-only.
2. A changed retry for the same review summary is a conflict. Correct upstream
   evidence and create a new reviewed case/summary before creating a new report.
3. If an invoice or allocation remains unavailable, retain `unknown`. Do not
   insert a zero estimate to clear completeness.
4. If a ledger amount differs, reconcile the immutable economic source and
   normalization. Never create an offsetting attribution-only fact.
5. Restore a verified pre-migration backup if schema removal is required; do
   not delete cost evidence to force rollback.

## Qualification and rollback

```bash
go test -race ./internal/costattribution/...
COST_ATTRIBUTION_QUALIFICATION_DB_URL="$COST_ATTRIBUTION_QUALIFICATION_DB_URL" \
  go test -race ./internal/repository/postgres \
  -run '^TestCostAttributionRetainedQualification$' -count=1 -v
```

The retained qualification proves every repository-stage rollback,
eight-writer convergence, changed retry conflict, restart reads, exact model
and ledger actuals, forged fee rejection, direct-SQL graph rejection,
append-only update/delete rejection, and independent incomplete/complete
coverage. The nonempty database refused rollback. A separate empty database
passed `100 -> 99 -> 100`.

## Qualification status

- `VERIFIED_LOCAL`: deterministic cost lines/totals, actual/estimated/unknown
  separation, exact parent/model/ledger checks, persistence, reconstruction,
  atomicity, concurrency, forgery rejection, restart, and rollback gates.
- `BLOCKED_EXTERNAL`: invoice/licensed-data/cloud-billing acquisition, external
  artifact authenticity, shared migration, review or promotion cutover,
  deployment, scheduling, allocation, settlement, broker routing, and trading.
