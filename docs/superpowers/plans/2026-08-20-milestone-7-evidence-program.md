# Milestone 7 Evidence Program Plan

> Execute the campaigns in dependency order. Local fixtures may qualify the
> machinery, but cannot satisfy elapsed-time, external-data, deployment, or
> profitability claims.

**Goal:** Complete the locally executable OVR-701 campaign and make OVR-702
through OVR-705 mechanically inspectable, fail-closed evidence gates. Preserve
honest rejection as a valid research outcome and keep live trading disabled.

**Boundary:** Local code, tests, synthetic fixtures, and read-only evidence
review only. No shared migration, provider traffic, scheduling, deployment,
capital movement, broker route, production state change, or live trading.

## OVR-701 — golden replay and restart

- [x] Add one reproducible campaign command covering deterministic experiment
  replay, duplicate convergence, injected child-stage failure, ledger
  reconstruction, reconciliation restart, settlement idempotency, and brake
  restart.
- [x] Run the campaign repeatedly and prove stable results on a clean commit.
- [ ] Run repository-wide backend/static and pinned frontend gates.
- [ ] Record exact command, commit, results, and limitations; commit and sync.

## OVR-702 — 30-day shadow campaign

- [ ] Define two-candidate admission, exact UTC interval, daily completeness,
  critical-defect, executable-data, simulated-fill, and slippage-divergence
  evidence requirements.
- [ ] Provide a read-only assessor that rejects intervals under 30 elapsed days,
  missing days, fewer than two candidates, unknown slippage, or any critical
  defect.
- [ ] Prove the assessor with synthetic pass/fail fixtures without claiming the
  synthetic pass is a real 30-day run.
- [ ] Start or adopt a real run only with separately authorized scheduler,
  provider, deployment, and retained-data scope.

## OVR-703 — 60–90 day scored-paper campaign

- [ ] Require exact OVR-702 evidence plus 60–90 elapsed days, full after-cost
  attribution, sample-size/statistical evidence, and immutable candidate
  outcomes.
- [ ] Accept either at least one positive after-cost candidate or an honest
  all-candidates-rejected terminal result.
- [ ] Reject synthetic duration, missing costs, hidden candidates, and unlimited
  paper margin as promotion evidence.

## OVR-704 — portfolio paper campaign

- [ ] Require exact OVR-703 outcomes and compare a combined allocation with the
  best eligible single sleeve over the same retained interval and cost basis.
- [ ] Require the combined allocation to improve or preserve risk-adjusted
  evidence; otherwise retain a rejected result.
- [ ] Keep allocation evidence separate from authority to move capital or trade.

## OVR-705 — architecture readiness review

- [ ] Review deposits, resizing, unattended operation, brake behavior, restart,
  reconciliation, daily explanations, and every unresolved blocker against
  exact retained evidence.
- [ ] Return `ready`, `not_ready`, or `blocked`; never infer readiness from unit
  tests, synthetic duration, a container health check, or source inspection.
- [ ] Re-run the full release gate and produce a final local/external boundary.

## Completion rule

The local implementation is complete when OVR-701 passes and OVR-702–705 have
tested fail-closed assessors/runbooks. Milestone 7 acceptance remains open until
the real elapsed campaigns and architecture review bind authorized retained
evidence. If no strategy clears the gates, a complete honest rejection is an
acceptable research result; fabricated elapsed time or profitability is not.
