# Autonomous Daily Supervisor Runbook

## Boundary

This runbook inspects the synthetic OVR-605 schema-99 supervisor boundary. The
retained database is loopback-only. The supervisor records deterministic
dependency assessments and work-class admissions. It cannot schedule work,
call a provider, change a brake or risk limit, promote a strategy, allocate
capital, construct an execution intent or order, settle a position, cancel a
protective exit, or flatten an account.

Use only the dedicated retained qualification database:

```bash
export DAILY_SUPERVISOR_QUALIFICATION_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/augr_ovr605_qual_v5_20260820?sslmode=disable'
psql "$DAILY_SUPERVISOR_QUALIFICATION_DB_URL" -Atc \
  'select current_database(),current_schema()'
```

Require `augr_ovr605_qual_v5_20260820` and `public`. Never run qualification
writes against a shared or production database.

## Inspect retained assessments

```bash
psql "$DAILY_SUPERVISOR_QUALIFICATION_DB_URL" -x -c \
  "select id,sha256,operating_day,timezone,evaluated_at,policy_version,
          reconciliation_id,reconciliation_sha256,scheduler_occurrence_id,
          scheduler_effect_id,prior_assessment_id
     from daily_supervisor_assessments order by evaluated_at,id"
```

The retained evidence includes:

- all-pass assessment `49af1a6a-3b27-cac1-e18a-f8a58277cbc3`, SHA-256
  `841d4db0f348a629619402084d562bd844b9aa85252c760ba2732398c78208c8`;
- its strictly later same-day successor
  `d47d675b-2257-1edc-dfcc-808cfa66c447`;
- provider-failure assessment `4cb14b93-bcc8-26df-8c65-0430032256eb`,
  SHA-256
  `17f92f5d75a531e03aa0518a9848408f6bd716dc02ab9bdb95e14966e8154c70`;
- exact OVR-207 reconciliation run
  `43918e0f-dce4-0539-1166-08f22fb74120`;
- all-pass occurrence/effect
  `293889a0-2980-cc2c-15fc-dd9daa49b3a0` /
  `78307a70-0cd3-186a-46c9-dc1eb160f352`;
- provider-failure occurrence/effect
  `951e844f-3ae0-5b91-458e-5017b1817452` /
  `67d0662c-2d4b-5756-f888-409c58099b2d`.

Every assessment is content-addressed and binds the exact reconciliation run
and digest plus the exact fenced OVR-604 occurrence/effect and digests.

## Inspect checks, admissions, and attention

```bash
psql "$DAILY_SUPERVISOR_QUALIFICATION_DB_URL" -x -c \
  "select a.operating_day,c.sequence,c.check_name,c.check_state,
          c.evidence_id,c.evidence_sha256,c.observed_at,c.fresh_through,c.reason
     from daily_supervisor_assessments a
     join daily_supervisor_checks c on c.assessment_id=a.id
    order by a.evaluated_at,c.sequence;
   select a.operating_day,x.work_class,x.admission,
          coalesce(string_agg(b.check_name,',' order by b.sequence),'') blocked_by
     from daily_supervisor_assessments a
     join daily_supervisor_actions x on x.assessment_id=a.id
     left join daily_supervisor_action_blockers b
       on (b.assessment_id,b.action_sequence)=(x.assessment_id,x.sequence)
    group by a.operating_day,a.evaluated_at,x.sequence,x.work_class,x.admission
    order by a.evaluated_at,x.sequence"
```

On 2026-08-21, `market_data=fail` deterministically produces
`new_exposure=halted` with `blocked_by=market_data`. Protective exits,
settlements, reconciliation, and evidence-only work remain eligible because
their narrower reviewed dependencies pass. Eligibility is only evidence for a
future consumer; it does not execute the work.

## Failure handling and recovery

1. Treat missing, stale, unknown, failed, or drifting required evidence as
   failed. Never convert an unknown check to pass operationally.
2. Inspect `daily_supervisor_attention` and exact evidence IDs/digests. Repair
   the producing dependency rather than editing the assessment; all schema-99
   evidence is append-only.
3. Re-run the supervisor only through a new fenced OVR-604 occurrence/effect.
   A changed retry on an already claimed effect is rejected.
4. For the same operating day, point a strictly later assessment at the exact
   prior ID/digest. A prior assessment has at most one successor; stale or
   forked chains fail closed.
5. Never delete evidence to force recovery or rollback. Restore a verified
   pre-migration backup if schema removal is required.

## Qualification and rollback

```bash
go test -race ./internal/dailysupervisor/... ./internal/financialscheduler/...
DAILY_SUPERVISOR_QUALIFICATION_DB_URL="$DAILY_SUPERVISOR_QUALIFICATION_DB_URL" \
  go test -race ./internal/repository/postgres \
  -run '^TestDailySupervisorRetainedQualification$' -count=1 -v
```

The retained qualification proves every repository-stage rollback,
eight-writer convergence, exact replay, changed retry conflict, restart reads,
strict supersession, fork rejection, append-only update/delete rejection,
direct-SQL action forgery rejection, and independent fail-closed work gates.

Migration 99 is empty-only reversible. A separate dedicated database passed
`99 -> 98 -> 99`. The retained nonempty database refused rollback. Never remove
assessments, checks, actions, attention, policy, reconciliation, or scheduler
evidence to make a rollback pass.

## Qualification status

- `VERIFIED_LOCAL`: deterministic supervisor policy, exact evidence binding,
  fail-closed action derivation, persistence/reconstruction, atomicity,
  concurrency, supersession, forgery rejection, restart, and rollback gates.
- `BLOCKED_EXTERNAL`: supervisor activation or cutover, shared migration,
  provider access, brake/risk mutation, automatic flatten/cancellation,
  allocation, settlement, broker routing, deployment, and live trading.
