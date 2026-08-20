# Milestone 5 Point-in-Time 13F Replay Plan

**Goal:** Complete OVR-504 with a deterministic historical 13F replay in which
manager selection, original filings, amendments, and every replay decision use
only evidence publication-available by the applicable cutoff.

**Architecture:** Add a `copyreplay` boundary over one immutable OVR-301
`dataset.Manifest` and migration 91. The manifest contains timestamped manager-
selection observations and SEC filing observations. A reviewed policy selects
managers at one explicit selection cutoff, then each canonical decision time
chooses the latest filing/amendment available at that instant. The replay
persists every selected manager and every decision, including no-filing and
unchanged-filing holds. A narrow OVR-303 adapter emits canonical no-op research
steps only for newly selected filing evidence; it cannot execute or promote.

**Migration:** `000091_point_in_time_13f_replay`

## Locked contracts

1. Every manager score and filing must match one exact OVR-301 manifest
   observation by source key, content SHA-256, and available time. Evidence not
   in the manifest is rejected.
2. The manager-selection cutoff and every decision time are UTC microsecond
   values at or before the manifest decision cutoff. Decision times are unique,
   strictly increasing, and inside the declared evaluation range.
3. Manager eligibility and ranking use only selection observations with
   `available_at <= selection_cutoff`. Score is an exact decimal; descending
   score then canonical manager ID breaks ties. Ineligible, late, duplicated,
   or caller-preselected managers cannot enter the selected set.
4. A filing is eligible only when its manager was selected and both
   `published_at <= available_at <= decision_at`. Report period/effective time
   never substitutes for publication availability.
5. For each manager and decision, choose the greatest report period available;
   within that period choose the highest amendment number available, then exact
   source identity. A later amendment cannot rewrite earlier decisions.
6. Original/amendment chains must remain within one manager/report period;
   amendment numbers increase by one and identify an earlier manifest filing.
   Orphan, cross-manager, cross-period, cyclic, or same-time revision evidence
   is rejected.
7. No-filing and unchanged-filing decisions are retained explicitly. Only a
   newly selected filing emits one unique OVR-303 no-op step, avoiding duplicate
   observation execution while preserving the complete replay audit.
8. Canonical input permutation produces the same replay identity. Exact retries
   converge; changed manifest, policy, selection, decision calendar, filing,
   or normalized result conflicts.
9. Parent, selected-manager, decision, and emitted-step evidence is append-only,
   independently reconstructable, and atomic. Direct mutation, partial graphs,
   ordering drift, or lookahead fails closed.
10. This is research replay only. It grants no copy subscription, allocation,
    quote, execution, promotion, scheduler, deployment, broker, or live-trading
    authority.

## Task 1: Manifest-bound manager and filing model

- [x] Validate exact manifest membership for selection and filing evidence.
- [x] Select top-N eligible managers using only selection-cutoff evidence and
  deterministic tie-breaking.
- [x] Validate original/amendment publication chronology and ownership chains.
- [x] Prove late selection, unmanifested evidence, score ties, orphan/cross-
  manager amendments, and input permutation fail or converge correctly.
- [x] Commit and push the evidence slice.

## Task 2: Point-in-time replay and OVR-303 adapter

- [x] Select filings independently at each decision time without future
  amendments rewriting history.
- [x] Retain selected, unchanged, and no-filing decisions with exact reasons.
- [x] Emit one canonical OVR-303 no-op step per newly selected filing and bind
  exact manifest observation identity and decision JSON.
- [x] Prove report-period ordering, publication boundary equality, late original
  and amendment behavior, manager isolation, restart determinism, and no-lookahead.
- [x] Commit and push the replay slice.

## Task 3: Migration 91 and retained qualification

- [x] Persist immutable replay/manager/decision/step identity and normalized
  rows with PostgreSQL reconstruction guards.
- [x] Prove eight-writer convergence, changed retry conflict, every-stage atomic
  rollback, forgery rejection, append-only evidence, nonempty rollback refusal,
  and empty `91 -> 90 -> 91`.
- [x] Retain multiple managers, originals, a later amendment, pre-publication
  decisions, and post-publication decisions proving history is not rewritten.
- [x] Add an inspection/recovery/rollback runbook with exact IDs and digests.
- [x] Run focused/database races, all backend/static and pinned frontend gates,
  diff review, and isolated kill-switched schema-91 health/API/rollback/reapply.
- [x] Commit/push verified slices, fetch, and prove `0 0` before OVR-505.

## Acceptance evidence to record

- [x] Manager selection uses only evidence available at its declared cutoff.
- [x] Every replay decision uses only a filing version available at that time;
  later amendments never rewrite earlier decisions.
- [x] The OVR-303 adapter binds exact OVR-301 manifest evidence and remains
  no-op research output.
- [x] Local qualification is `VERIFIED_LOCAL`; licensed historical source
  acquisition, independent review, shared migration, runtime adoption,
  promotion, scheduling, deployment, broker routing, and live trading remain
  `BLOCKED_EXTERNAL`.

## Qualification record

OVR-504 is complete locally in commits `051e04a` and `fc5579b`, with the
isolated-verifier correction in `c7e47ce`. Retained database
`augr_ovr504_qual_20260820_v2` is clean schema 91 and contains manifest
`b4fb186f-9a75-3379-411e-2142084487f6`, replay
`6316e44e-9434-3a7f-82c5-24162ab83f45`, and canonical SHA-256
`0a9749dbcbe580b2a64fa36cbaa17c205923437ebf4c71a32ae75adb3e54c28a`.
Its normalized graph has three candidates, four filings, two selected managers,
ten decisions, and four no-op research steps. The decision history retains
pre-filing no-op rows, original Q1 selections, unchanged rows, a later Q1
amendment, and a still-later Q2 original without rewriting earlier decisions.

Eight concurrent writers converged, a changed calendar conflicted, every
injected stage rolled back atomically, restart reconstruction passed, and
forgery plus mutation failed closed. A retained graph refused downgrade, while
`augr_ovr504_empty_20260820` passed `91 -> 90 -> 91`. Repository-wide gates
passed 4,591 race-tested cases across 120 packages, build, vet, lint, formatter,
and symbol-level vulnerability scanning with zero reachable vulnerabilities.
Pinned Node 22.23.2 passed all 162 frontend tests, lint, and production build;
the audit retains one known low-severity Windows-only esbuild development-server
advisory. The exact synchronized `c7e47ce` production-image verifier passed
fresh `1 -> 91`, health, authenticated read-only API, `91 -> 60`, backup/restore,
`60 -> 91`, and post-reapply health, using only disposable project networks.

This evidence is **VERIFIED_LOCAL**. Licensed historical acquisition,
independent review, shared migration, runtime adoption, promotion, scheduling,
deployment, broker routing, and live trading remain **BLOCKED_EXTERNAL**.
