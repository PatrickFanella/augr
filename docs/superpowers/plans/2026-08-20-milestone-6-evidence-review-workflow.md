# Milestone 6 Evidence Review Workflow Plan

**Goal:** Complete OVR-603 with immutable, independently attributable evidence
reviews that bind exact OVR-602 hypothesis/critic artifacts to the exact OVR-306
policy decision governing a candidate. AI recommendations remain citations;
they cannot construct, alter, apply, or supersede promotion state.

**Architecture:** Add an `evidencereview` boundary and migration 97. A review
case binds one exact hypothesis and one exact critic, one exact promotion policy
and deterministic decision, and the common candidate version. Reviewers append
signed-off evidence assessments using a closed check/disposition vocabulary,
explicit references, independent identity/provenance, and an optional
supersession chain. A deterministic review summary reports agreement,
disagreement, missing evidence, and whether escalation is required. It only
observes the OVR-306 decision's immutable outcome/next state; no constructor,
repository method, database trigger, API, or runtime path can create a
promotion decision or lifecycle event.

**Migration:** `000097_evidence_review_workflow`

## Locked contracts

1. Every case binds exact hypothesis ID/SHA, critic ID/SHA, generated candidate
   version ID/SHA, promotion policy ID/SHA, promotion decision ID/SHA,
   deployment ID/SHA, robustness assessment ID/SHA, and observed OVR-306
   outcome/next state.
2. The hypothesis version, critic hypothesis, promotion decision version,
   assessment, and deployment must form one exact immutable graph. Cross-case,
   cross-version, stale, or partial bindings fail closed.
3. The critic recommendation is retained as an AI recommendation only. The
   authoritative lifecycle observation is reconstructed from the OVR-306
   decision; callers cannot supply or override current/next promotion state.
4. A review has a stable reviewer key, reviewer kind (`human` or
   `independent_service`), organization, identity SHA-256, reviewed-at UTC
   microsecond timestamp, and independent system/developer/user prompt hashes
   when the reviewer kind is an independent service. Secrets and raw prompts
   are never stored.
5. Each review contains exactly one closed state for source integrity,
   reproducibility, statistical controls, cost/capacity, safety boundaries,
   and policy-decision consistency: `pass`, `fail`, or `unknown`. Unknown is
   not pass.
6. Each check cites one or more exact hypothesis source/test/search, critic
   finding/check, promotion gate, or parent digest references. Unknown,
   duplicated, omitted, or reordered references fail closed.
7. Review disposition is deterministic: any critical failed check is
   `reject_evidence`; any fail/unknown is `changes_requested`; all passes are
   `evidence_supported`. This disposition never approves or changes lifecycle.
8. Optional reviewer notes and conflicts are bounded canonical text. No free-
   form field may encode authority, credentials, executable code, schedules,
   risk changes, intents, orders, or secrets.
9. Review supersession is append-only and reviewer-local. A successor binds the
   exact prior review ID/SHA; stale forks and same-key semantic changes fail.
   Earlier reviews remain inspectable.
10. A summary is a deterministic projection of the exact case and accepted
    review heads. It reports counts, consensus/disagreement, unresolved unknown
    checks, and escalation requirement, while copying the authoritative OVR-306
    outcome/next state unchanged.
11. Canonical permutation of checks/references/review heads converges. Any
    semantic, identity, provenance, or parent edit creates a new artifact.
12. Case, reviews, checks, references, supersession links, and summaries are
    append-only, atomic, independently reconstructable, and empty-only
    reversible.
13. OVR-603 performs no provider/model/search call and creates no hypothesis,
    critic, experiment, deployment, promotion decision/event, schedule,
    allocation, intent, order, settlement, or trade.

## Task 1: Exact evidence case and review domain

- [x] Implement exact OVR-602/306 graph binding and authoritative lifecycle
  observation without promotion construction authority.
- [x] Implement closed reviewer identity/provenance, six required checks,
  exact references, deterministic disposition, and reviewer-local supersession.
- [x] Prove supported/changes/rejected outcomes, permutation convergence,
  semantic identity changes, cross-parent rejection, missing/unknown references,
  stale supersession, tamper rejection, and clone-safe restoration.
- [x] Commit and push the domain slice after focused race tests.

## Task 2: Deterministic multi-review summary

- [x] Project accepted reviewer heads into exact pass/fail/unknown counts,
  consensus/disagreement, unresolved-check, and escalation evidence.
- [x] Copy but never calculate or mutate the authoritative OVR-306 outcome and
  next state; prove AI-ready cannot override policy hold/retirement and AI-reject
  cannot undo policy approval.
- [x] Prove review-head ordering convergence, stale/fork/duplicate rejection,
  semantic identity changes, tamper rejection, and authority-surface closure.
- [x] Commit and push the summary slice after focused race tests.

## Task 3: Migration 97, retained qualification, and closure

- [x] Persist immutable case/review/check/reference/supersession/summary graphs
  with exact OVR-602/306 parent scope and PostgreSQL reconstruction.
- [x] Prove eight-writer convergence, changed retry conflict, every-stage atomic
  rollback, normalized forgery rejection, append-only behavior, nonempty
  rollback refusal, and empty `97 -> 96 -> 97`.
- [x] Retain policy-approved-but-review-changes-requested and policy-held-but-
  review-supported cases, proving review disposition never changes lifecycle.
- [x] Add an inspection/recovery/rollback runbook with exact IDs and digests.
- [x] Run focused/database races, repository-wide backend/static and pinned
  frontend gates, diff review, and isolated kill-switched schema-97 health/API/
  rollback/backup/restore/reapply.
- [x] Commit/push verified slices, fetch, and prove `0 0` before OVR-604.

## Acceptance evidence to record

- [x] Every review reconstructs exact hypothesis, critic, promotion policy/
  decision, candidate, reviewer provenance, checks, and references.
- [x] AI recommendations and reviewer dispositions cannot construct, change,
  apply, or supersede promotion state; OVR-306 remains authoritative.
- [x] Local qualification is `VERIFIED_LOCAL`; provider calls, licensed source
  acquisition, independent human review, lifecycle cutover, shared migration,
  scheduling, deployment, allocation, broker routing, and live trading remain
  `BLOCKED_EXTERNAL`.
