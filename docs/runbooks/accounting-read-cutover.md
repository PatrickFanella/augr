# Accounting read cutover

## Purpose

This runbook governs the future transition from Augr's legacy compatibility
accounting reads to projections rebuilt from the immutable ledger. It is a
fail-closed evidence and review procedure, not an instruction to enable the
current OVR-105 library in a shared environment.

Current status:

- `VERIFIED_LOCAL`: canonical snapshots, exact comparison, immutable structural
  persistence, rollback guards, and the 30-day evaluator are implemented and
  tested against the loopback-only development database.
- `BLOCKED_EXTERNAL`: no prospective real 30-day parity window exists; the
  runtime has no common capture fence covering every relevant paper and ledger
  writer; no approved reconciliation attestation, reviewer authentication,
  non-owner workload identity, provisioning, rotation, revocation, or incident
  recovery ceremony exists.
- No accounting read path has been switched. No legacy write path has been
  stopped. Migration 70 grants no runtime privileges and starts no scheduler.

Do not treat source code, a local fixture, a manually inserted database row, a
structurally valid checksum, or a passing fake-verifier test as deployed parity
evidence.

## Evidence model

One reconciliation run binds all of the following in exact canonical bytes:

- one explicit account UUID and UTC `as_of`;
- one projection version and comparison policy;
- one mark source, namespace, and maximum age;
- one verified account-scoped capture fence ID and monotonic epoch;
- the actual observation time and source-evidence checksum for each snapshot;
- exact decimal cash, buying power, fees, realized/unrealized P&L, market value,
  equity, and signed quantity per canonical instrument;
- explicit coverage gaps; and
- every result, allowed explanation, generator, independent reviewer, and
  review time.

There is no tolerance. Missing is not zero. A one-unit difference at the 18th
supported decimal place is still a difference. Results are one of:

- `equal`: both exact values are present and identical;
- `explained`: both values are present and differ under one narrowly allowed,
  independently reviewed evidence classification;
- `unexplained`: both values are present and differ without accepted evidence;
- `not_comparable`: one or both values or scopes are absent.

The database proves byte/hash/identity relationships, atomic child completeness,
and append-only storage. It does not prove that a writer observed either source,
owned the named fence, or represented a real generator/reviewer. Database owners
and migrators remain outside the evidence trust boundary.

## Hard prerequisites for a shared dual run

All prerequisites require a separate reviewed implementation and deployment
change. Stop if any item is absent.

1. Map each source to an explicit canonical account. Never assign global legacy
   `orders`, `trades`, or `positions` to the default account by inference.
2. Implement one account-scoped capture fence whose lease is held across both
   source reads. Every relevant paper-account mutation and every ledger
   normalization for that account must participate in that same fence. Matching
   timestamps or independent locks are insufficient.
3. Run ledger replay through the OVR-104 controlled checkpoint function using a
   dedicated non-owner/non-superuser role and a signing secret obtained from an
   approved external secret provider. The role must not direct-write
   checkpoints or read verifier keys.
4. Approve a separate reconciliation attestation design. The capability that
   authenticates exact run bytes and capture identity must not be available to
   the ordinary SQL evidence writer alone. Define key provisioning, rotation,
   revocation, audit, backup, compromise response, and historical verification.
5. Authenticate generator and reviewer identities outside caller-authored JSON.
   The reviewer must be independent of the generator, and review evidence must
   be immutable and bound to the exact fact key and run bytes.
6. Grant only the minimum reviewed privileges to dedicated workload identities.
   Do not grant table writes to the general app role and never use the schema
   owner/migrator as the scheduled worker.
7. Add alerts for failed capture acquisition, lost leases, source failures,
   missing daily evidence, signature/key failures, unexplained differences,
   incomplete coverage, and policy drift.
8. Prove restart, overlapping-worker, delayed-source, key-revocation, and
   writer-contention behavior in a protected non-production environment before
   starting the prospective window.

## Prospective parity window

After every prerequisite is approved and deployed, collect at least one
authenticated run per UTC date for 30 consecutive dates. Evaluation must use
an independently trusted wall clock rather than a timestamp supplied by an
evidence writer. The latest eligible endpoint is the last fully completed UTC
date; the current UTC date never counts. A qualifying window must satisfy all
of these conditions:

- exactly one nonconflicting evidence identity is selected for every required
  date through the declared UTC-midnight end date;
- all selected runs belong to one account and use one unchanged comparison,
  projection, mark-source, mark-namespace, and mark-age policy;
- canonical bytes and relational rows revalidate;
- the approved verifier accepts each run under a known, unrevoked key and trust
  policy;
- neither source is synthetic and position coverage is complete;
- all required metrics and all unioned canonical positions are present; and
- `unexplained_count = 0` and `not_comparable_count = 0` on every day.

An `explained` difference is not exact equality. Review it explicitly and keep
its classification visible in the cutover evidence. A current-day endpoint,
future endpoint, future-dated row, single missing date, conflicting duplicate,
policy change, invalid attestation, unexplained result, or coverage gap
restarts the prospective window after the underlying issue is corrected. Never
edit or delete an earlier run.

Useful read-only inspection queries after a real writer exists:

```sql
SELECT
    account_id,
    as_of::date AS evidence_date,
    id,
    policy_version,
    projection_version,
    mark_source,
    mark_namespace,
    equal_count,
    explained_count,
    unexplained_count,
    not_comparable_count,
    synthetic,
    attestation_type,
    attestation_key_id
FROM accounting_reconciliation_runs
WHERE account_id = '<reviewed-account-uuid>'
ORDER BY as_of, id;

SELECT run_id, fact_key, legacy_value, ledger_value, delta, status, reason_code
FROM accounting_reconciliation_results
WHERE run_id = '<run-uuid>'
ORDER BY fact_key;
```

SQL inspection is diagnostic only. The application must reload canonical bytes,
revalidate every child row, and invoke the approved `EvidenceVerifier` before a
window can qualify.

## Cutover review

A passing `EvaluateCutover` result has no side effect and is not authorization
to change production. Prepare a separate cutover change that includes:

- the exact account and 30-day run-ID manifest;
- verifier/key status and independent review sign-off;
- every explained difference and linked evidence;
- API, UI, risk, allocator, reconciliation, and reporting consumers being
  switched;
- a frozen inventory of legacy readers and writers;
- shadow-read telemetry and discrepancy alerts;
- a rollback feature switch that does not discard ledger or reconciliation
  evidence; and
- a timed operator rehearsal proving rollback restores legacy reads without
  enabling new exposure or mutating accounting history.

Do not combine read cutover with stopping legacy writes, deleting compatibility
tables, changing margin policy, or enabling live execution. Those require their
own dependency-ordered reviews.

## Incident handling

If any daily run fails or becomes nonqualifying:

1. Keep legacy reads authoritative and halt any pending cutover.
2. Preserve both source snapshots, exact bytes, fence identity, logs, and
   verifier result. Do not rewrite the reconciliation row.
3. If the capture fence or attestation boundary may be compromised, stop the
   reconciliation writer, revoke the affected key through the approved
   mechanism, and treat the entire affected window as nonqualifying.
4. Classify an arithmetic difference only after immutable source evidence and
   independent review exist. Unknown semantics remain `unexplained` or
   `not_comparable`.
5. Correct source adapters, projection mechanics, identity mapping, or runtime
   coordination in a reviewed change, then begin a new prospective window.

## Migration rollback

Migration 70 can be downgraded only when both reconciliation tables are empty.
The down migration first takes exclusive locks so a concurrent writer cannot
race the emptiness check. If any evidence exists, rollback intentionally fails.
Preserve or restore the database; never truncate evidence to make a downgrade
pass.

For the disposable loopback database only:

```bash
export AUGR_PHASE1_DB_URL='postgres://postgres:postgres@127.0.0.1:55464/tradingagent?sslmode=disable'
psql "$AUGR_PHASE1_DB_URL" -Atc \
  "SELECT count(*) FROM accounting_reconciliation_runs;
   SELECT count(*) FROM accounting_reconciliation_results;"
migrate -path migrations -database "$AUGR_PHASE1_DB_URL" down 1
migrate -path migrations -database "$AUGR_PHASE1_DB_URL" up 1
migrate -path migrations -database "$AUGR_PHASE1_DB_URL" version
```

Never run these downgrade commands against staging, production, a shared
database, or an environment containing real evidence without an explicit
backup/restore and change-management review.
