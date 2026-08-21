# Promotion and retirement decisions

This runbook operates OVR-306 lifecycle evidence. A local decision is not a
runtime activation. Migration 81 adds no scheduler reader, deployment worker,
provider call, allocation mutation, order path, AI approval, or UI status
writer. Until an independently reviewed cutover explicitly adopts the derived
projection, every result remains research-governance evidence only.

## Preconditions

Use an isolated schema at migration 81 with execution credentials absent, live
trading disabled, and the kill switch engaged. The exact immutable chain must
already exist:

1. an OVR-302 proposed deployment and its strategy version, account, capital
   binding, budget, mode, risk policy, schedule text, ID, and SHA-256;
2. a complete OVR-305 search family and assessment containing that exact
   version once;
3. the normalized required gates and their exact states, thresholds,
   observations, reasons, and descriptions;
4. a reviewed content-addressed OVR-306 policy.

Do not substitute a report from another version, mode, family, assessment, or
deployment. Do not reduce the family to the preferred candidate.

## Policy review

Every policy must require `overall_robustness`; additional required gates are
sorted into policy identity. The pass action is fixed to `shadow`. The failure
action is either `hold` or `retire` and must be chosen before evaluating the
candidate.

The default local qualification policy uses `hold`. A failed synthetic test
therefore preserves the proposed deployment for investigation instead of
silently retiring it. A retirement policy is a separate immutable identity and
must be supported by explicit reviewed criteria. Retirement never deletes
strategy, deployment, market-data, experiment, order, position, or ledger
evidence.

## Evaluation and state projection

The service request contains only deployment ID, assessment ID, policy, and an
optional exact prior-decision ID. It contains no pass boolean, outcome, reason,
or next state. The service reloads every parent and derives:

- candidate membership in the complete family;
- the policy-required normalized gates;
- `approved`, `held`, or `retired`;
- exact prior and next states;
- a content-addressed decision and lifecycle event.

The first decision starts from immutable deployment state `proposed`. Passing
all required gates produces `proposed -> shadow`. A hold preserves the prior
state. A qualifying retirement policy produces `prior -> retired`. Later
decisions name the exact prior decision. A unique serialized-head constraint
rejects forks and stale competing writers.

There is no writable current-status column. The repository derives state from
the one decision-chain head. Inspect full history by explicit deployment,
version, assessment, or family; never select a candidate through a "best" or
"latest passing" query.

## Verification

Run focused races against an isolated PostgreSQL target:

```bash
go test -race ./internal/promotion -count=1
DB_URL="$ISOLATED_DB_URL" go test -race ./internal/repository/postgres \
  -run '^TestPromotionRepository' -count=1
```

Verify identical eight-writer convergence, competing-head rejection, restarted
normalized reconstruction, policy/decision/gate/event append-only refusal,
rollback at every child stage, and nonempty migration rollback refusal. Then
run the repository-wide backend, pinned Node 22 frontend, migration, image,
authenticated health/API, backup/restore, and rollback/reapply gates.

Record policy, deployment, assessment, family, robustness-policy, candidate,
decision, prior-decision, and lifecycle-event IDs and SHA-256 values. Record
normalized counts and exact outcome/reason/state values. A source/unit pass is
not retained-schema, runtime, deployment, or user-experience proof.

## Failure response

If a parent, candidate, required gate, chain head, hash, or normalized row does
not reconstruct:

1. stop evaluation for the deployment;
2. preserve every row, canonical payload, log, and source identity;
3. classify missing/partial family, cross-version/mode, gate failure,
   persistence corruption, or stale/forked state;
4. create new upstream evidence or a new reviewed policy instead of altering
   accepted evidence;
5. rerun through the deterministic service.

AI, UI, or operator recommendations may cite a decision but cannot create or
modify it and cannot bypass a failed gate. Never manually insert a passing
outcome or edit a lifecycle event.

## Rollback and activation boundary

Migration 81 is empty-only reversible. A schema with any promotion policy or
decision must refuse `81 -> 80`. Preserve required evidence and use a separate
empty isolated database for rollback rehearsal. Never disable append-only
triggers or delete evidence to make rollback succeed.

Label synthetic local qualification `VERIFIED_LOCAL`. Real candidate data,
independent statistical and lifecycle review, scheduler adoption, allocation,
deployment, shadow runtime, paper/live activation, and production cutover stay
`BLOCKED_EXTERNAL` until separately authorized and verified.
