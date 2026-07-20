# P2 strategy hygiene report

Snapshot time: 2026-07-20T06:36:49Z UTC

## Methodology

- Read-only inventory only; no strategy rows were changed.
- Queried the live PostgreSQL container via `docker compose exec postgres psql` against `tradingagent`.
- SQL bundle is SELECT-only and grouped into three inventories:
  1. active strategies with no run in the last 7 days
  2. duplicate active ticker groups
  3. active strategies that are paused or unscheduled

## Results

### 1) Active strategies with no weekly run in the last 7 days

- Count: 12
- Top rows:
  1. ALK — `discovery: ALK ALK Pullback Reversion` — last_run_at: null
  2. CTAS — `discovery: CTAS CTAS Trend Pullback Long` — last_run_at: null
  3. JBHT — `discovery: JBHT JBHT Trend Continuation` — last_run_at: null
  4. GS — `discovery: GS GS Trend Continuation` — last_run_at: null
  5. FTNT — `discovery: FTNT FTNT Trend Pullback` — last_run_at: null
  6. TDG — `discovery: TDG TDG Mean Reversion Dip Buy` — last_run_at: null
  7. VLO — `discovery: VLO VLO Trend Momentum` — last_run_at: null
  8. WTO — `discovery: WTO WTO Trend Pullback` — last_run_at: null
  9. MLAC — `discovery: MLAC MLAC Swing Reclaim` — last_run_at: null
  10. ZCMD — `discovery: ZCMD ZCMD Oversold Rebound` — last_run_at: 2026-06-30 13:55:12.715502+00
  11. NOTV — `discovery: NOTV NOTV Oversold Mean Reversion` — last_run_at: 2026-07-06 15:02:31.306586+00
  12. HSPT — `discovery: HSPT HSPT RSI Mean Reversion` — last_run_at: 2026-07-10 16:08:33.420046+00

### 2) Duplicate active ticker groups

- Count: 15 duplicate groups
- Top groups (by active count):
  1. WDC — 6 active strategies
  2. GLW — 4 active strategies
  3. GNRC — 3 active strategies
  4. HPE — 3 active strategies
  5. MRVL — 3 active strategies
  6. MU — 3 active strategies
  7. STX — 3 active strategies
  8. AMD — 2 active strategies
  9. GS — 2 active strategies
  10. JLHL — 2 active strategies
  11. MCHP — 2 active strategies
  12. NVDA — 2 active strategies
  13. ON — 2 active strategies
  14. ORCL — 2 active strategies
  15. ZCMD — 2 active strategies

Intentional-variant review notes: every duplicate group observed is paper=true with identical `schedule_cron` and `skip_next_run=false`, so they look like portfolio expansion / strategy-family variants rather than accidental deschedules. Operator review still required before any merge or disable action.

### 3) Active strategies that are paused or unscheduled

- Count: 0
- Top rows: none

## Ambiguity notes

- Some duplicate groups may be intentional variants (for example, paper vs live or staged rollouts); those are flagged but not treated as defects without operator review.
- A strategy missing a run in the last 7 days may still be intentionally idle depending on market status or operational cadence.
- This report is read-only and informational.

## No changes made

No changes were made to production strategy state, schedules, or database contents.
