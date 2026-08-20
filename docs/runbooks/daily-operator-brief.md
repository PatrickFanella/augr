# Daily Operator Brief and Incident Inbox Runbook

## Boundary

This runbook inspects synthetic OVR-607 schema-101 evidence. A brief reports
retained facts and projects open incidents. It does not send email/chat/pager
messages, acknowledge or close incidents, change a brake/risk limit, flatten or
cancel, revise a review/promotion decision, schedule, allocate, settle, route,
deploy, or trade.

Use only the composed loopback qualification database:

```bash
export OPERATOR_BRIEF_QUALIFICATION_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/augr_ovr607_qual_20260820?sslmode=disable'
psql "$OPERATOR_BRIEF_QUALIFICATION_DB_URL" -Atc \
  'select current_database(),current_schema()'
```

Require `augr_ovr607_qual_20260820` and `public`. This database combines only
immutable synthetic OVR-207/604/605 and OVR-603/606 parent evidence. Never run
qualification writes against a shared or production database.

## Inspect retained briefs

```bash
psql "$OPERATOR_BRIEF_QUALIFICATION_DB_URL" -x -c \
  "select id,sha256,operating_day,timezone,generated_at,supervisor_id,
          reconciliation_id,cost_report_id,review_summary_id,
          performance_evaluation_id,incident_count
     from daily_operator_briefs order by operating_day"
```

Retained evidence includes:

- baseline brief `dd6641f9-890c-51a8-1382-cd6e94708687`, SHA-256
  `f05fd157064264f4e31c0f127ab01ac24a983ce641fc8fa16844fd1308379dfc`;
- attention brief `793964db-52a3-ea8e-30cc-cf03a6044af9`, SHA-256
  `bbe3ad96b2e8f9155fa4e61dfe7644bc23dd532eab162605c13892918547ba45`;
- exact all-pass and provider-failure supervisor assessments
  `49af1a6a-3b27-cac1-e18a-f8a58277cbc3` and
  `4cb14b93-bcc8-26df-8c65-0430032256eb`;
- exact complete and incomplete cost reports
  `b118a586-995e-f07e-bbf3-e0a88bc90441` and
  `ee15a682-2fa0-d1fd-5fee-26ef1568718e`.

## Inspect the explanation and inbox

```bash
psql "$OPERATOR_BRIEF_QUALIFICATION_DB_URL" -x -c \
  "select brief_id,sequence,section_name,section_status,headline,explanation,
          evidence_kind,evidence_id,evidence_sha256
     from daily_operator_brief_sections order by brief_id,sequence;
   select brief_id,sequence,incident_key,severity,incident_state,source_kind,
          source_id,source_sha256,summary,required_action
     from daily_operator_brief_incidents order by brief_id,sequence"
```

Every brief has performance, decisions, drift, risk, and costs sections. Both
retained days explicitly report performance `unavailable`: the composed graph
does not contain a completed OVR-304 evaluation, so each brief carries an open
incident instead of inventing performance. The attention day also retains:

- `supervisor_check:market_data`;
- `work_halted:new_exposure`;
- `cost_unknown:shared_infrastructure`.

Its risk facts still show protective exits, settlement, and reconciliation as
eligible. Eligibility is reported evidence only and invokes no work.

## Recovery and incident handling

1. Treat every row as immutable source-linked evidence. Do not update an
   incident to acknowledge/close it; schema 101 intentionally has no such API.
2. Follow `source_kind`, `source_id`, and `source_sha256` to the exact producer.
   Repair or complete that upstream evidence under its own reviewed workflow.
3. Generate a later operating-day brief from new daily evidence. Same-day
   changed reuse is a conflict; never overwrite the original brief.
4. Delivering a notification or creating a mutable ticket requires a separate
   reviewed integration with authentication, dedupe, recipients, escalation,
   acknowledgement, and audit policy. This schema provides none of those.
5. Never delete evidence to force rollback. Restore a verified pre-migration
   backup if schema removal is required.

## Qualification and rollback

```bash
go test -race ./internal/operatorbrief/...
OPERATOR_BRIEF_QUALIFICATION_DB_URL="$OPERATOR_BRIEF_QUALIFICATION_DB_URL" \
  go test -race ./internal/repository/postgres \
  -run '^TestOperatorBriefRetainedQualification$' -count=1 -v
```

Qualification proves every repository-stage rollback, eight-writer
convergence, changed daily conflict, restart, append-only update/delete
rejection, direct-SQL incident forgery rejection, exact parent reconstruction,
and independent work admissions. Nonempty rollback refused. A separate empty
database passed `101 -> 100 -> 101`.

## Qualification status

- `VERIFIED_LOCAL`: deterministic brief/section/fact/incident derivation,
  exact retained parent binding, persistence, reconstruction, atomicity,
  concurrency, restart, forgery rejection, and rollback gates.
- `BLOCKED_EXTERNAL`: completed retained performance for these composed days,
  notification/ticket delivery, acknowledgement/closure, shared migration,
  cutover, deployment, scheduling, risk or financial mutations, and trading.
