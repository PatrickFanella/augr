# Evidence Review Workflow Runbook

## Boundary

This runbook inspects synthetic OVR-603 schema-97 evidence-review artifacts.
The workflow binds exact OVR-602 hypothesis/critic evidence to exact OVR-306
policy decisions and records independent review checks. A review disposition is
advisory evidence only. The OVR-306 decision remains the sole authoritative
lifecycle outcome; OVR-603 cannot approve, promote, retire, deploy, schedule,
allocate, submit an intent/order, settle, or trade.

Use only the dedicated loopback database:

```bash
export EVIDENCE_REVIEW_QUALIFICATION_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/augr_ovr603_qual_20260820_v3?sslmode=disable'
psql "$EVIDENCE_REVIEW_QUALIFICATION_DB_URL" -Atc \
  "select current_database(),current_schema()"
```

Require `augr_ovr603_qual_20260820_v3` and `public`. Never run qualification
writes against a shared or production database.

## Inspect exact case authority

```bash
psql "$EVIDENCE_REVIEW_QUALIFICATION_DB_URL" -x -c \
  "select id,hypothesis_id,hypothesis_sha256,critic_id,critic_sha256,
          critic_recommendation,version_id,version_sha256,promotion_policy_id,
          promotion_policy_sha256,promotion_decision_id,
          promotion_decision_sha256,deployment_id,deployment_sha256,
          assessment_id,assessment_sha256,authoritative_outcome,
          authoritative_next_state,reference_count,sha256,canonical_json
     from evidence_review_cases order by id"
```

Require the approved-but-review-escalated case
`ace93dd9-6828-87ca-49d7-b2f5203779e4`, SHA-256
`59191cd1610e223245e14e30bfe4552f9d03d0ea5e159cfa8eac8953de7dc5a7`.
Its review summary is `58e4d1a0-8b72-b8e1-b9ad-6c90b68510e9`, SHA-256
`2f4466b5a4e4bff4e4ff804780027e663577e69625ae7eb3b3b2c1bb58c85539`.
Review disagreement requires escalation, while authoritative OVR-306 state
remains `approved -> shadow`.

Require the held-but-review-supported case
`2f7dc76e-6d16-1c37-97d9-6ed50e21b9f9`, SHA-256
`fc6c946afc54e90aa4825b99adcbf83d24c86f27852d917996480228dd08c00b`.
Its supported summary is `3ee039af-18d3-f233-631f-c0d048fbcd86`, SHA-256
`68e67168022670c57e00d5f7eb5a5bddb0cb92928972bb08f288d5c8ebfc4c32`.
Full review support does not override the authoritative OVR-306 held `shadow`
state.

## Inspect reviews and summaries

```bash
psql "$EVIDENCE_REVIEW_QUALIFICATION_DB_URL" -c \
  "select id,case_id,reviewer_key,reviewer_kind,reviewed_at,prior_review_id,
          disposition,authoritative_outcome,authoritative_next_state,sha256
     from evidence_reviews order by case_id,reviewer_key;
   select review_id,sequence,check_name,severity,check_state,canonical_row
     from evidence_review_checks order by review_id,sequence;
   select id,case_id,consensus,escalation_required,authoritative_outcome,
          authoritative_next_state,sha256 from evidence_review_summaries
     order by case_id"
```

Each case has one human and one independently attributed service review, six
required checks per review, exact retained references, and two unique summary
heads. Unknown is never pass. Critical failure rejects evidence; any other
fail/unknown requests changes; all passes support evidence.

## Retry, recovery, and rollback

```bash
go test -race ./internal/evidencereview/...
EVIDENCE_REVIEW_QUALIFICATION_DB_URL="$EVIDENCE_REVIEW_QUALIFICATION_DB_URL" \
  go test -race ./internal/repository/postgres \
  -run '^TestEvidenceReviewRetainedQualification$' -count=1 -v
```

The retained qualification proves eight-writer convergence, same-reviewer
semantic conflict, nine injected rollback boundaries, restart reconstruction,
nested normalized-reference verification, database-side disposition/authority
recomputation, append-only rejection, normalized forgery rejection, and
nonempty rollback refusal. Exact OVR-602 parent rows came from the retained
schema-96 fixture. Exact synthetic deployment/promotion parents were inserted
without replaying their already-qualified upstream child graphs; schema-97
foreign keys, digests, and authority triggers remained enabled for OVR-603.

Migration 97 is empty-only reversible. A dedicated database passed
`97 -> 96 -> 97`; retained cases refuse rollback. Never alter or delete evidence
to force rollback. Restore a verified pre-migration backup.

## Qualification status

- `VERIFIED_LOCAL`: deterministic case/review/summary domains, exact parent
  graph, reviewer identity/provenance, closed checks/references, immutable
  persistence, database authority reconstruction, concurrency, atomicity,
  restart, forgery rejection, empty rollback, and retained rollback refusal.
- `BLOCKED_EXTERNAL`: provider calls, licensed source acquisition, independent
  human review, lifecycle cutover, shared migration, scheduling, deployment,
  allocation, broker routing, and live trading.
