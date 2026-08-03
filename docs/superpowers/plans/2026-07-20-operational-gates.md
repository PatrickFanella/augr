# Operational Gates Implementation Plan

> **For agentic workers:** Execute this plan in order. Keep all investigation steps read-only, do not mutate production state during diagnosis, and do not enable any gate until the explicit approval checkpoint is signed off.

**Goal:** Close the remaining operational gaps for Kalshi settlement, production soak, P/L reconciliation, and strategy hygiene without widening scope beyond the documented gates.

**Current production state:** schema version `59`; Kalshi settlement is disabled; `kalshi_settlement_gate` is `1/20`; fingerprint is stable; fetched `2` / resolved `1` / would settle `1`; there are `3` real filled Kalshi decisions; `1231` legacy decisions were rejected.

**Scope:** Write-only implementation plan. No code changes, no SQL mutations during investigation steps, no commit execution in this document.

---

## File map

- Create: `docs/superpowers/plans/2026-07-20-operational-gates.md`
- Read-only report artifacts to generate later:
  - `docs/reports/2026-07-20-kalshi-settlement-gate.md`
  - `docs/reports/2026-07-20-kalshi-soak-24h.md`
  - `docs/reports/2026-07-27-kalshi-soak-7d.md`
  - `docs/reports/2026-07-20-alpaca-pl-residual.md`
  - `docs/reports/2026-07-20-strategy-hygiene.md`
  - `docs/reports/2026-07-20-strategy-hygiene.sql`
  - `docs/reports/2026-07-20-strategy-hygiene.test.sh`

---

## Task 2: Complete Kalshi 20-run settlement gate

**Objective:** Finish the remaining 19 disabled/manual dry runs without gaming the gate, preserve drift-resets, require review before enable, and require a canary live settlement before full enablement.

### Steps

- [ ] **Step 1: Freeze the gate contract.**

  Confirm the gate is evaluated only from durable dry-run evidence and current production state, not from backfilled counters or manual increments. Dry-run execution may update only durable gate/job-run telemetry; it must not mutate financial settlement state.

  **Acceptance rule:** the gate remains at `1/20` until one additional qualifying dry run is recorded; no synthetic runs may be counted.

- [ ] **Step 2: Define the remaining 19 dry-run cadence.**

  Run exactly one manual dry run per eligible settlement schedule window until 20 total qualifying runs exist. Use the existing disabled job path or a manual `RunJob` invocation that bypasses enabled-state checks, but only for read-only dry-run summary collection.

  **Anti-gaming constraints:**
  - do not replay the same window more than once to inflate the gate
  - do not backfill missing windows as completed runs
  - do not count runs that observed drift, schema mismatch, or provider errors as passing gate evidence
  - if drift is observed, reset the gate state to `0/20` and restart the cadence from the next clean dry run

- [ ] **Step 3: Capture the exact read-only SQL used for each dry run.**

  ```sql
  SELECT current_setting('server_version_num') AS server_version_num,
         59 AS expected_schema_version,
         COUNT(*) FILTER (WHERE status = 'paper_ordered') AS paper_ordered_count,
         COUNT(*) FILTER (WHERE status = 'live_ordered') AS live_ordered_count,
         COUNT(*) FILTER (WHERE status = 'closed') AS closed_count,
         COUNT(*) FILTER (WHERE market_type = 'kalshi' AND status IN ('paper_ordered', 'live_ordered', 'closed')) AS kalshi_trade_decision_count
  FROM trade_decisions;

  SELECT job_name,
         consecutive_successes,
         threshold,
         eligible,
         projection_fingerprint,
         last_outcome,
         last_error,
         fetched,
         resolved,
         would_settle_markets,
         would_settle_decisions,
         last_run_at,
         updated_at
  FROM kalshi_settlement_gate
  WHERE job_name = 'kalshi_settlement';
  ```

  **Expected results:**
  - the current gate history shows `1` successful qualifying run and `19` remaining required runs
  - no row is inserted or updated by the diagnostic query itself
  - the query output is archived for each run in a dated report artifact

- [ ] **Step 4: Define the durable gate evidence to record.**

  For each qualifying dry run, record durable gate/job-run telemetry only:
  - run timestamp
  - schema version observed (`59`)
  - fetched / resolved / would-settle counts
  - whether any drift or provider error occurred
  - operator identity for the manual invocation
  - a hash or fingerprint of the eligible market set to prove the run was not replayed from prior output

- [ ] **Step 5: Require review before enable.**

  Before changing settlement from disabled to enabled, produce a human-reviewed report showing all 20 qualifying dry runs, the last clean fingerprint, and the absence of drift resets after the most recent qualifying run.

  **Operator checkpoint A:** reviewer signs off that the gate is legitimate, current, and not gamed.

- [ ] **Step 6: Canary live settlement.**

  Re-enable settlement only for one canary strategy or one canary run after review approval.

  **Canary acceptance gates:**
  - exactly one live settlement is attempted
  - no duplicate fill is produced
  - the live settlement result matches the dry-run classification
  - no provider error causes the job to self-disable during the canary
  - if the canary fails, roll back to disabled mode and keep the gate evidence intact for diagnosis

- [ ] **Step 7: Explicit commit list for the implementation phase.**

  When code is later updated, the expected commit set is:
  - `internal/automation/jobs_kalshi_settlement.go`
  - `internal/automation/orchestrator.go`
  - `internal/automation/jobs_kalshi_settlement_test.go`
  - `internal/metrics/metrics.go`
  - `internal/config/config.go`
  - `docs/reports/2026-07-20-kalshi-settlement-gate.md`

  Commit message examples:
  - `feat(automation): complete kalshi settlement gate`
  - `docs(ops): add kalshi settlement gate report`

---

## Task 4: Run 24h/7d P0/P1 soak

**Objective:** Define exact queries and operator checks for a 24-hour P0 soak and a 7-day P1 soak, focusing on Ollama 413s, duplicate fills, paper submitted backlog, allocator repeats, Alpaca reconciliation, rate limits, and health.

### Steps

- [ ] **Step 1: Publish the read-only soak report artifacts.**

  Create one report per soak window:
  - `docs/reports/2026-07-20-kalshi-soak-24h.md`
  - `docs/reports/2026-07-27-kalshi-soak-7d.md`

- [ ] **Step 2: Use exact UTC-bounded SQL for the soak dashboard.**

  Capture immutable RFC3339 bounds when each soak starts and ends, then pass them into every event query. The backlog query is an explicit end-of-window state snapshot; all event checks use `[SOAK_START_UTC, SOAK_END_UTC)`.

  ```bash
  test -n "$SOAK_START_UTC"
  test -n "$SOAK_END_UTC"
  docker compose -f /srv/server/projects/augr/docker-compose.nuc.yml exec -T postgres \
    psql -U postgres -d tradingagent -v ON_ERROR_STOP=1 \
    --set=soak_start_utc="$SOAK_START_UTC" \
    --set=soak_end_utc="$SOAK_END_UTC" <<'SQL'
  -- End-of-window paper backlog snapshot
  SELECT COUNT(*) AS paper_submitted_backlog
  FROM orders
  WHERE broker = 'paper'
    AND market_type = 'kalshi'
    AND status = 'submitted'
    AND created_at < :'soak_end_utc'::timestamptz;

  SELECT external_id, COUNT(*) AS duplicate_fill_count
  FROM trades
  WHERE external_id IS NOT NULL
    AND btrim(external_id) <> ''
    AND created_at >= :'soak_start_utc'::timestamptz
    AND created_at < :'soak_end_utc'::timestamptz
  GROUP BY external_id
  HAVING COUNT(*) > 1
  ORDER BY duplicate_fill_count DESC, external_id;

  -- Allocator repeat checks
  SELECT opportunity_id, COUNT(*) AS repeat_count
  FROM allocation_decisions
  WHERE created_at >= :'soak_start_utc'::timestamptz
    AND created_at < :'soak_end_utc'::timestamptz
  GROUP BY opportunity_id
  HAVING COUNT(*) > 1
  ORDER BY repeat_count DESC, opportunity_id;

  -- Alpaca reconciliation inputs
  SELECT COUNT(*) AS trade_count,
         COALESCE(SUM(fee), 0) AS fee_total
  FROM trades
  WHERE order_id IN (
      SELECT id FROM orders WHERE broker = 'alpaca'
  )
    AND created_at >= :'soak_start_utc'::timestamptz
    AND created_at < :'soak_end_utc'::timestamptz;

  SELECT ffi.idempotency_key, COUNT(*) AS duplicate_fill_id_count
  FROM financial_fill_idempotency ffi
  WHERE ffi.created_at >= :'soak_start_utc'::timestamptz
    AND ffi.created_at < :'soak_end_utc'::timestamptz
  GROUP BY ffi.idempotency_key
  HAVING COUNT(*) > 1
  ORDER BY duplicate_fill_id_count DESC, ffi.idempotency_key;
  SQL
  ```

- [ ] **Step 3: Use exact log queries for provider failures.**

  ```bash
  test -n "$SOAK_START_UTC"
  test -n "$SOAK_END_UTC"
  docker compose -f /srv/server/projects/augr/docker-compose.nuc.yml logs \
    --since "$SOAK_START_UTC" --until "$SOAK_END_UTC" app \
    | rg -n "413|payload too large|ollama|rate limit|Retry-After|duplicate fill|submitted backlog|allocator repeat|reconciliation|health"
  ```

  **Expected results:**
  - Ollama 413s remain at `0` for the soak window
  - duplicate fills remain at `0`
  - paper submitted backlog trends to `0` and stays there
  - allocator repeat count remains `0`
  - Alpaca reconciliation emits a stable residual and no unexpected mutation
  - health checks remain green and no provider limit breach causes a cascade disable

- [ ] **Step 4: Use exact metric queries for the soak dashboard.**

  Record a UTC start timestamp for each soak, then query counter increases over `[start,end]` (PromQL `increase(metric[24h])` for the 24-hour gate and `increase(metric[7d])` for the seven-day gate; use `max_over_time`/histogram rates for gauges and histograms) for:
  - `tradingagent_automation_job_errors_total`
  - `tradingagent_alpaca_reconcile_runs_total`
  - `tradingagent_kalshi_reconcile_runs_total`
  - `tradingagent_kalshi_rate_limit_total`
  - `tradingagent_kalshi_retry_attempts_total`
  - `tradingagent_kalshi_retry_wait_seconds`
  - `tradingagent_kalshi_settlement_dry_run_total`
  - `tradingagent_kalshi_settlement_outcome_total`
  - `tradingagent_kalshi_settlement_transition_total`
  - `tradingagent_orders_total`
  - `tradingagent_pipeline_runs_total`
  - `tradingagent_scheduler_tick_total`

  Do not compare raw lifetime counter totals as soak-window failures. Use SQL/log checks with the same explicit UTC window where a metric does not exist. **Expected results:** windowed error/duplicate/backlog increases meet the stated zero or bounded thresholds, counters remain monotonic, and the residual is reported explicitly instead of being suppressed.

- [ ] **Step 5: Define the 24h P0 soak acceptance gate.**

  Accept the 24-hour soak only if:
  - no Ollama 413s
  - no duplicate fills
  - paper submitted backlog is not increasing and ends at zero
  - allocator repeats are zero
  - Alpaca reconciliation runs without mutation
  - all health endpoints return success

- [ ] **Step 6: Define the 7d P1 soak acceptance gate.**

  Accept the 7-day soak only if the 24h gates continue to hold every day, with no new provider-rate-limit spike, no replay regressions, and no restart-related settlement drift.

  **Operator checkpoint B:** review the daily soak report before moving from day 1 to day 2, then again before enabling any broader settlement change.

---

## Task 5: Explain P/L residual

**Objective:** Explain the current `-151.514251` residual by tracing broker equity/cash against local closed/open PnL, fees, and audits without manual correction. Only after evidence is complete should a ledger-only proposal be drafted.

### Steps

- [ ] **Step 1: Freeze the reconciliation scope.**

  No manual corrections, backfills, or compensating writes are permitted in the investigation.

- [ ] **Step 2: Run the exact read-only reconciliation SQL.**

  ```sql
  SELECT COALESCE(SUM(COALESCE(p.realized_pnl, 0)), 0)
  FROM positions p
  WHERE p.closed_at IS NOT NULL AND (
      EXISTS (SELECT 1 FROM position_provenance pp WHERE pp.position_id = p.id AND pp.broker = 'alpaca') OR
      EXISTS (
          SELECT 1
          FROM trades t
          JOIN orders o ON o.id = t.order_id
          WHERE t.position_id = p.id AND o.broker = 'alpaca'
      )
  );

  SELECT COALESCE(SUM(COALESCE(p.unrealized_pnl, 0)), 0)
  FROM positions p
  WHERE p.closed_at IS NULL AND (
      EXISTS (SELECT 1 FROM position_provenance pp WHERE pp.position_id = p.id AND pp.broker = 'alpaca') OR
      EXISTS (
          SELECT 1
          FROM trades t
          JOIN orders o ON o.id = t.order_id
          WHERE t.position_id = p.id AND o.broker = 'alpaca'
      )
  );

  SELECT COUNT(*)
  FROM trades t
  JOIN orders o ON o.id = t.order_id
  WHERE o.broker = 'alpaca';

  SELECT COALESCE(SUM(COALESCE(t.fee, 0)), 0)
  FROM trades t
  JOIN orders o ON o.id = t.order_id
  WHERE o.broker = 'alpaca';

  SELECT id, event_type, entity_type, entity_id, details, created_at
  FROM audit_log
  WHERE created_at >= NOW() - INTERVAL '30 days'
    AND event_type IN ('fee', 'cash_adjustment', 'position_close', 'manual_adjustment')
  ORDER BY created_at DESC;
  ```

  **Expected result:** the residual remains visible as `-151.514251` until traced; do not coerce it to zero.

  **Residual formula:** use the exact implementation formula from `internal/automation/alpaca_reconciliation.go`:
  `report.UnexplainedResidual = report.BrokerEquity - (report.BrokerCash + report.LocalClosedPnL + report.LocalOpenPnL - report.FeeTotal)`.

- [ ] **Step 3: Trace the residual components.**

  Compare broker equity/cash to:
  - local closed PnL
  - local open PnL
  - fees
  - audit entries
  - any persisted adjustment categories that already exist

  Produce a line-item table showing each component and the arithmetic bridge to the residual.

- [ ] **Step 4: Produce the report artifact.**

  Create `docs/reports/2026-07-20-alpaca-pl-residual.md` containing:
  - the residual value
  - the authenticated GET `/api/v1/automation/alpaca/reconciliation` response used as the broker snapshot source
  - the saved JSON response payload
  - exact SQL used
  - broker snapshot timestamp
  - local totals
  - any discovered adjustment explanation
  - explicit statement that no manual correction was applied

- [ ] **Step 5: Draft the ledger-only proposal only after evidence exists.**

  If the residual is still unexplained after the trace, propose a ledger-only follow-up that records the reason without mutating historical fills.

  **Acceptance gate:** no ledger proposal may be implemented until the report shows which component(s) account for the discrepancy or proves the discrepancy is truly unexplained.

---

## Task 6: Classify duplicate/dormant strategies

**Objective:** Rerun the SELECT-only report, classify the 15 duplicate groups and 12 dormant strategies, and prepare audited pause actions only after approval and rollback planning.

### Steps

- [ ] **Step 1: Generate the read-only strategy hygiene report.**

  Create:
  - `docs/reports/2026-07-20-strategy-hygiene.sql`
  - `docs/reports/2026-07-20-strategy-hygiene.md`
  - `docs/reports/2026-07-20-strategy-hygiene.test.sh`

- [ ] **Step 2: Use the exact SELECT-only report SQL.**

  ```sql
  -- Dormant active strategies: no weekly run in 7 days.
  SELECT s.id,
         s.name,
         s.ticker,
         s.market_type,
         s.schedule_cron,
         s.status,
         s.skip_next_run,
         s.is_paper,
         s.is_active,
         s.updated_at,
         max(pr.started_at) AS last_run_at
  FROM strategies s
  LEFT JOIN pipeline_runs pr ON pr.strategy_id = s.id
  WHERE s.is_active = TRUE
  GROUP BY s.id, s.name, s.ticker, s.market_type, s.schedule_cron, s.status, s.skip_next_run, s.is_paper, s.is_active, s.updated_at
  HAVING max(pr.started_at) IS NULL OR max(pr.started_at) < NOW() - INTERVAL '7 days'
  ORDER BY last_run_at NULLS FIRST, s.updated_at DESC;

  -- Duplicate active ticker groups.
  SELECT market_type,
         ticker,
         count(*) AS active_strategy_count,
         array_agg(id ORDER BY updated_at DESC) AS strategy_ids,
         array_agg(name ORDER BY updated_at DESC) AS strategy_names,
         array_agg(status ORDER BY updated_at DESC) AS strategy_statuses,
         array_agg(schedule_cron ORDER BY updated_at DESC) AS strategy_schedule_crons,
         array_agg(skip_next_run ORDER BY updated_at DESC) AS strategy_skip_next_runs,
         array_agg(is_paper ORDER BY updated_at DESC) AS strategy_is_papers,
         array_agg(is_active ORDER BY updated_at DESC) AS strategy_is_actives
  FROM strategies
  WHERE is_active = TRUE
  GROUP BY market_type, ticker
  HAVING count(*) > 1
  ORDER BY active_strategy_count DESC, market_type, ticker;
  ```

- [ ] **Step 3: Classify the 15 duplicate groups and 12 dormant strategies.**

  For each duplicate group, label it as one of:
  - intentional variant
  - accidental duplicate
  - legacy holdover

  For each dormant strategy, label it as one of:
  - intentionally paused
  - schedule drift
  - no longer used

- [ ] **Step 4: Define audited pause actions only after approval.**

  No strategy pause, disable, or mutation is allowed until operator approval is recorded. Any strategy action must use the real API route and current status model, and remain approval-gated.

  **Operator checkpoint C:** confirm which duplicate groups should remain and which dormant strategies should be paused.

- [ ] **Step 5: Prepare rollback instructions.**

  Every pause action must include a rollback plan that restores the exact prior strategy state, including `status`, `skip_next_run`, and `is_active`.

  Approved actions should target the live API surface (e.g. `/api/v1/...`) and preserve the existing trade decision statuses (`candidate`, `rejected`, `paper_ordered`, `live_ordered`, `closed`).

- [ ] **Step 6: Acceptance gates.**

  Accept the hygiene work only if:
  - the report is SELECT-only
  - the operator has classified all 15 duplicate groups and 12 dormant strategies
  - no pause actions are executed without approval
  - rollback instructions are present for every proposed pause

---

## Sequencing rule

Do not enable Kalshi settlement until the 20-run gate is fully satisfied and reviewed. Do not start any mutation step for strategy hygiene until the report is approved. Keep the P/L residual investigation read-only until the evidence pack is complete. The soak reports must be generated before any canary rollout is widened.
