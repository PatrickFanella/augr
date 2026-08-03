# Writing Plans: Twelve-Item Remediation Roadmap

> **For agentic workers:** Execute this roadmap step-by-step. Keep all steps gated, read-only where specified, and do not present destructive commands as immediately executable. This roadmap integrates the four source plans into a single dependency-ordered execution path.

**Goal:** Integrate and sequence all 12 remaining remediation items into one master roadmap with explicit dependencies, stop/go gates, owners, artifacts, acceptance criteria, validation commands, rollback boundaries, and approval checkpoints.

**Source subplans:**
- `docs/superpowers/plans/2026-07-20-recovery-source-control-secrets.md`
- `docs/superpowers/plans/2026-07-20-operational-gates.md`
- `docs/superpowers/plans/2026-07-20-database-maintenance.md`
- `docs/superpowers/plans/2026-07-20-engineering-hardening.md`

---

## Numbered mapping (exact)

1. backup/restore fidelity — `docs/superpowers/plans/2026-07-20-recovery-source-control-secrets.md`
2. Kalshi 20-run gate — `docs/superpowers/plans/2026-07-20-operational-gates.md`
3. Git history/push — `docs/superpowers/plans/2026-07-20-recovery-source-control-secrets.md`
4. 24h/7d soak — `docs/superpowers/plans/2026-07-20-operational-gates.md`
5. P/L residual — `docs/superpowers/plans/2026-07-20-operational-gates.md`
6. duplicate/dormant strategy classification — `docs/superpowers/plans/2026-07-20-operational-gates.md`
7. snapshot retention — `docs/superpowers/plans/2026-07-20-database-maintenance.md`
8. collation maintenance — `docs/superpowers/plans/2026-07-20-database-maintenance.md`
9. Polygon/Ollama governance — `docs/superpowers/plans/2026-07-20-engineering-hardening.md`
10. allocator CAS — `docs/superpowers/plans/2026-07-20-engineering-hardening.md`
11. live atomic fills — `docs/superpowers/plans/2026-07-20-engineering-hardening.md`
12. JWT rotation — `docs/superpowers/plans/2026-07-20-recovery-source-control-secrets.md`

---

## Master dependency DAG

### Hard dependencies
- **1 → 7, 8**: source-index repair, affected-table reindex, and fresh logical restore parity must be completed before any destructive DB work.
- **3 → 9, 10, 11, 12**: Git reconciliation, artifact commit, and approved push must complete before feature rollout and JWT cutover.
- **7 decision → 10, 11**: the migration 000060 item7 decision must be applied first (approved migration or explicit approved no-op reservation) before migration61 and migration62.
- **4 || 2**: soak and settlement runs can proceed read-only in parallel while gates remain open.
- **5 → 6 → 2/4/7/8/9/10/11** (mutations): P/L and strategy classification must be read-only before operator mutations.
- **9 → 10 → 11**: provider governance before allocator/live-fill rollout.
- **10 → 11**: allocator CAS before multi-instance live-fill rollout.
- **11 (authoritative broker fill events) → live atomic path enablement**.
- **12**: JWT rotation is last, only after item 3 is reconciled/pushed and during a controlled session-expiry window.

### Dependency edges by item
- 1 unlocks 7 and 8.
- 3 unlocks the feature-branch/worktree strategy used for 9–11 and must be done before any broad branch fan-out.
- 2 and 4 may be validated in parallel once read-only evidence and operator review artifacts exist.
- 5 and 6 remain read-only gates that must complete before any strategy pause/disable/mutation.
- 9 is the prerequisite for 10.
- 10 is the prerequisite for 11.
- 12 is last and only after source reconciliation/push and an approved session-expiry window.

---

## Execution waves

### Wave 0 — Read-only reconnaissance and approval prep
Scope: 1, 2, 4, 5, 6, 3 (inventory/review only)
- Build evidence packs, classify blockers, and collect operator-facing artifacts.
- No destructive action, no mutation, no push, no retention/compression, no pause/disable.
- If a gate is denied, later mutation waves remain blocked; only read-only evidence gathering, review, and documentation may continue.

### Wave 1 — Recovery and source-control stabilization
Scope: 1, 3, 12 (prep only)
- Complete backup/restore fidelity rehearsal.
- Reconcile/push source control safely.
- Prepare JWT rotation plan and session-expiry window, but do not cut over until source reconciliation is complete and approval is granted.
- If item 1 or item 3 is denied, waves 3, 4, and 5 remain blocked; read-only work in waves 0 and 2 may continue.

### Wave 2 — Read-only operational verification
Scope: 2, 4, 5, 6
- Finish Kalshi gate evidence, soak validation, residual explanation, and strategy classification.
- These can run with no production mutation and in parallel where read-only.

### Wave 3 — Database maintenance after recovery gate
Scope: 7, 8
- Only after item 1 passes and approvals are recorded.
- If item 1 is denied, this wave is blocked; read-only item 7/8 analysis may continue, but no production maintenance.

### Wave 4 — Engineering hardening
Scope: 9, 10, 11
- Provider governance first, then allocator CAS, then live atomic fills.
- If item 3 is denied, this wave is blocked; only read-only design/test preparation may continue until reconciliation/push is approved.
- If item 7 decision is denied or left unresolved, item 10 and item 11 remain blocked.

### Wave 5 — JWT rotation cutover
Scope: 12
- Last step, during approved session-expiry window.
- If item 3 is denied, this wave is blocked; only read-only auth rehearsal and documentation may continue.

---

## Item-by-item roadmap

### 1) backup/restore fidelity
- **Source:** `docs/superpowers/plans/2026-07-20-recovery-source-control-secrets.md`
- **Owner:** DB/ops lead
- **Artifacts:** TOC capture, rehearsal restore log, parity report, fidelity runbook, decision record
- **Acceptance criteria:** source duplicate repair and affected-table reindex complete first; then a fresh same-version clean-target rehearsal passes row/chunk/schema parity and every unique index rebuilds; no production restore without passing rehearsal
- **Validation commands:**
  - `pg_restore -l "$SOURCE_BACKUP_PATH"`
  - `createdb "$REHEARSAL_DB_NAME"`
  - `pg_restore --verbose --clean --if-exists --no-owner --no-privileges -d "$REHEARSAL_DB_NAME" "$SOURCE_BACKUP_PATH"`
  - parity SQL from the subplan
- **Rollback boundary:** stop at rehearsal if any parity or restore step fails; no production restore
- **Requires user approval:** yes, before any production restore or fallback selection
- **Notes:** physical fallback is evaluated only, not default; do not present restore commands as immediately executable

### 2) Kalshi 20-run gate
- **Source:** `docs/superpowers/plans/2026-07-20-operational-gates.md`
- **Owner:** automation/settlement owner
- **Artifacts:** gate history report, run fingerprints, canary summary
- **Acceptance criteria:** 20 qualifying dry runs, no gaming/backfill, stable fingerprint, review signoff, one canary live settlement only after approval
- **Validation commands:** read-only SQL from the subplan; archive each run’s output
- **Rollback boundary:** if canary fails, return to disabled mode while preserving evidence
- **Requires user approval:** yes, before enabling settlement and before canary live settlement

### 3) Git history/push
- **Source:** `docs/superpowers/plans/2026-07-20-recovery-source-control-secrets.md`
- **Owner:** release/source-control owner
- **Artifacts:** ref snapshot, commit inventory, classification table, push proposal, backup branch ref
- **Acceptance criteria:** full 61-commit inventory classified; safe topology chosen; full tests pass; push proposal approved
- **Validation commands:**
  - `git fetch origin`
  - `git show-ref --heads --tags --dereference > docs/reports/2026-07-20-git-show-ref.txt`
  - `git log --oneline --decorate --reverse origin/main..HEAD > docs/reports/2026-07-20-git-history-inventory.txt`
  - `go test ./... -count=1`
- **Rollback boundary:** preserve backup branch; no force-push ever
- **Requires user approval:** yes, before any remote update

### 4) 24h/7d soak
- **Source:** `docs/superpowers/plans/2026-07-20-operational-gates.md`
- **Owner:** operations/on-call lead
- **Artifacts:** 24h report, 7d report, SQL snapshots, log extracts, metrics snapshots
- **Acceptance criteria:** no Ollama 413s, no duplicate fills, no growing paper backlog, zero allocator repeats, stable reconciliation residual, green health
- **Validation commands:** read-only SQL, log search, metrics queries from the subplan
- **Rollback boundary:** stop widening scope; keep reports intact if any day fails
- **Requires user approval:** yes, before expanding beyond canary or moving from daily review to broader change

### 5) P/L residual
- **Source:** `docs/superpowers/plans/2026-07-20-operational-gates.md`
- **Owner:** finance/reconciliation owner
- **Artifacts:** residual report, line-item bridge, exact SQL transcript
- **Acceptance criteria:** residual explained or proven unexplained without manual correction; no coercion to zero
- **Validation commands:** read-only SQL from the subplan
- **Rollback boundary:** none; investigation remains read-only
- **Requires user approval:** no mutation approval at this stage; approval only needed for any later ledger-only follow-up

### 6) duplicate/dormant strategy classification
- **Source:** `docs/superpowers/plans/2026-07-20-operational-gates.md`
- **Owner:** trading ops / strategy governance owner
- **Artifacts:** SELECT-only hygiene report, classification matrix, rollback notes for any proposed pause
- **Acceptance criteria:** all 15 duplicate groups and 12 dormant strategies classified; no pauses executed without approval; rollback plan for every mutation candidate
- **Validation commands:** strategy hygiene SQL from the subplan
- **Rollback boundary:** no mutation until approval; restore exact prior state if a pause is later approved and needs reversal
- **Requires user approval:** yes, before any pause/disable/mutation

### 7) snapshot retention
- **Source:** `docs/superpowers/plans/2026-07-20-database-maintenance.md`
- **Owner:** DB retention owner
- **Artifacts:** retention analysis, consumer lookback inventory, proposal, rehearsal notes, tests draft
- **Acceptance criteria:** retention window derived from actual lookbacks; implementation option compared; no production deletion/compression until item 1 passes and recovery fidelity is approved
- **Validation commands:** read-only SQL from the subplan
- **Rollback boundary:** preserve original table until approved cutover; no deletion before backup validation
- **Requires user approval:** yes, before any production-facing migration or retention policy

### 8) collation maintenance
- **Source:** `docs/superpowers/plans/2026-07-20-database-maintenance.md`
- **Owner:** PostgreSQL DBA
- **Artifacts:** inventory, maintenance sequence, rehearsal report, rollback plan, maintenance window plan
- **Acceptance criteria:** rehearsal precedes `REINDEX`; planner checks pass; `REFRESH COLLATION VERSION` last; downtime/lock impact documented
- **Validation commands:** inventory SQL, rehearsal restore, representative query checks, future-only maintenance commands from the subplan
- **Rollback boundary:** revert to pre-maintenance restored backup if query health regresses; halt before refresh if issues appear
- **Requires user approval:** yes, before scheduling production maintenance window

### 9) Polygon/Ollama governance
- **Source:** `docs/superpowers/plans/2026-07-20-engineering-hardening.md`
- **Owner:** platform/provider owner
- **Artifacts:** failing tests, provider-scoped governor wiring, provider tests, staging verification
- **Acceptance criteria:** Polygon and Ollama use provider-scoped governor; POST safety preserved; no global retry policy; Kalshi behavior unchanged
- **Validation commands:** targeted `go test` for polygon/ollama/providergovernor from the subplan
- **Rollback boundary:** remove only new governor wiring
- **Requires user approval:** yes, before staging rollout and broader enablement

### 10) allocator CAS (migration 61)
- **Source:** `docs/superpowers/plans/2026-07-20-engineering-hardening.md`
- **Owner:** automation/repository owner
- **Artifacts:** claim semantics tests, migration, repo contract update, multi-orchestrator validation
- **Acceptance criteria:** atomic queued→selected/processing claim; no duplicate decisions/orders; expired claims recover; two orchestrators safe concurrently; item 7 decision resolved before migration61 work begins
- **Validation commands:** repository and allocator tests from the subplan
- **Rollback boundary:** disable claim path and revert to single-instance behavior
- **Requires user approval:** yes, before enabling claim-based allocation behind a deploy/config gate

### 11) live atomic fills (migration 62)
- **Source:** `docs/superpowers/plans/2026-07-20-engineering-hardening.md`
- **Owner:** execution/repository owner
- **Artifacts:** authoritative live fill interface, persistence UoW, shadow comparison tests, broker-specific acceptance tests
- **Acceptance criteria:** stable external fill IDs, idempotent duplicate delivery, partial fills aggregate correctly, atomic persistence, live trading stays disabled until all acceptance gates pass; item 7 decision resolved before migration62 work begins
- **Validation commands:** targeted live-fill suite from the subplan
- **Rollback boundary:** keep live brokers disabled or shadow-only if any acceptance gate fails
- **Requires user approval:** yes, before any live-trading enablement

### 12) JWT rotation
- **Source:** `docs/superpowers/plans/2026-07-20-recovery-source-control-secrets.md`
- **Owner:** auth/deployment owner
- **Artifacts:** secret cutover record, login/refresh/me/WebSocket verification, rollback instructions, operator signoff
- **Acceptance criteria:** source control is reconciled and approved before cutover; controlled session-expiry window approved; new sessions work; old sessions fail as expected; rollback path documented
- **Validation commands:** secret-store deployment command path only at runtime, then auth flow checks from the subplan
- **Rollback boundary:** restore previous secret from protected store and redeploy auth service; do not reuse discarded secret in plain text
- **Requires user approval:** yes, before secret deployment and session invalidation

---

## Stop/go gates

- **Go gate A:** item 1 source repair and affected-table reindex complete, and a fresh same-image logical restore passes row/schema parity.
- **Go gate B:** item 3 reconciliation inventory and push proposal approved.
- **Go gate C:** item 2 settlement gate has 20 qualifying runs and review signoff.
- **Go gate D:** item 5 residual investigated and item 6 classification complete before any operator mutation.
- **Go gate E:** item 1 passed before item 7 or 8 production work.
- **Go gate F:** item 9 deployed and verified before item 10.
- **Go gate G:** item 10 proven safe before item 11 live-enable work.
- **Go gate H:** item 7 decision applied before item 10 or item 11 migration work; item 12 only after item 3 is reconciled/pushed and the controlled expiry window is open.

---

## Approval checkpoints

- **Checkpoint 1:** approve backup/restore rehearsal target and artifact set.
- **Checkpoint 2:** approve Git inventory and push topology.
- **Checkpoint 3:** approve Kalshi gate legitimacy and canary plan.
- **Checkpoint 4:** approve strategy pause candidates and rollback plans.
- **Checkpoint 5:** approve retention window and maintenance window.
- **Checkpoint 6:** approve provider-governance staging rollout.
- **Checkpoint 7:** approve allocator claim rollout.
- **Checkpoint 8:** approve live atomic fill enablement.
- **Checkpoint 9:** approve JWT rotation window and expected forced logout behavior.

---

## Recommended operator sequence

1. Finish item 1 rehearsal evidence.
2. Reconcile item 3 source control.
3. Run read-only items 2, 4, 5, and 6 in parallel where practical.
4. Resolve item 7 decision, then unlock items 7 and 8 after item 1 passes.
5. Deploy item 9, then item 10, then item 11, with item 10 and item 11 blocked until item 7 is resolved.
6. Execute item 12 last in the approved expiry window, after item 3 is reconciled/pushed.

---

## Validation command set

- Recovery / DB: `pg_restore -l`, rehearsal restore, parity SQL.
- Git: `git fetch origin`, `git show-ref`, `git log --reverse origin/main..HEAD`, repository test suite.
- Kalshi / soak / hygiene: read-only SQL, log search, metrics queries from the operational-gates plan.
- Maintenance: inventory SQL, rehearsal restore, `REINDEX`/`REFRESH COLLATION VERSION` only as future-approved commands.
- Engineering: targeted `go test` commands for providergovernor, polygon, ollama, allocator, repository, execution.
- JWT: secret-store deployment path only at runtime, then login/refresh/me/WebSocket verification.

---

## Rollback boundaries

- No production restore until recovery rehearsal passes.
- No force-push or destructive ref rewrite.
- No retention deletion or compression without item 1 gate and approval.
- No collation refresh outside approved maintenance window.
- No claim-based allocator rollout without the CAS path and rollback disable switch.
- No live trading unless authoritative fill events and idempotency gates pass.
- No JWT cutover without explicit approval and session-expiry coordination.
- If any gate is denied, later mutation waves are blocked; only read-only evidence gathering, review, and documentation may continue in earlier waves.

---

## Recommended execution option choice

**Recommended option:** Single dependency-ordered execution sequence with only explicitly read-only overlap.

**Why:** it preserves the required order, keeps destructive work behind the recovery gate, and avoids implying independent tracks.

**Expected commit grouping:**
1. Recovery/source-control docs group (items 1, 3, 12 docs)
2. Operational-gates docs group (items 2, 4, 5, 6 docs)
3. Database-maintenance docs group (items 7, 8 docs)
4. Item-7 schema decision group, including the approved `000060` implementation or explicit no-op reservation and schema-version synchronization
5. Provider-governance implementation group (item 9 code/tests)
6. Allocator-CAS implementation group (item 10 code/tests/migration `000061`)
7. Authoritative-live-fill implementation group (item 11 code/tests/migration `000062`)
