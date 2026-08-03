# Strategy hygiene evidence

- Read-only snapshot time UTC: 2026-07-20T17:24:01Z
- Production inventory method: `docker compose exec -T postgres psql -U postgres -d tradingagent -X -v ON_ERROR_STOP=1` using the SELECT-only SQL bundle in `docs/reports/2026-07-20-strategy-hygiene.sql`.

## Summary

- Stale active strategies (no run in 7 days): 10
- Duplicate active ticker groups: 16
- Baseline reconciliation: stale count is 2 lower than the earlier baseline of 12.

## Raw rows — stale active strategies

| id | ticker | market | name | schedule | created_at | updated_at | last_run_at | classification |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 3743993e-7ab5-4e70-afce-ee9f8930e09b | KXPRESNOMD-28-RGAL | kalshi | auto: kalshi KXPRESNOMD-28-RGAL | 0 */6 * * * | 2026-07-20 16:40:19.587352+00 | 2026-07-20 16:40:19.587352+00 | null | needs operator decision (uncertain; brand-new, no run evidence) |
| fb6464ee-3312-4dbd-9079-e487c1aec5ba | KXPRESNOMD-28-AKLO | kalshi | auto: kalshi KXPRESNOMD-28-AKLO | 0 */6 * * * | 2026-07-20 14:40:34.408666+00 | 2026-07-20 14:40:34.408666+00 | null | needs operator decision (uncertain; brand-new, no run evidence) |
| 0c9b1126-8999-40d1-bfb7-e6a84b07dcd6 | TXN | stock | discovery: TXN TXN Mean Reversion Bounce | 0 */2 * * 1-5 | 2026-07-20 12:44:47.151894+00 | 2026-07-20 12:44:47.151894+00 | null | needs operator decision (uncertain; brand-new, no run evidence; duplicate group present) |
| 101db12e-a679-4c8d-8755-663c26660acf | WDC | stock | discovery: WDC WDC Mean Reversion Rebound | 0 */2 * * 1-5 | 2026-07-20 12:44:47.141809+00 | 2026-07-20 12:44:47.141809+00 | null | intentional variant (same schedule/name family as active WDC set; duplicate group present) |
| 06751b22-b678-437f-b00b-80557dc15f47 | KXPRESNOMD-28-JP | kalshi | auto: kalshi KXPRESNOMD-28-JP | 0 */6 * * * | 2026-07-20 11:40:47.551443+00 | 2026-07-20 11:40:47.551443+00 | null | needs operator decision (uncertain; brand-new, no run evidence) |
| f47205a0-cfcb-42cd-8dbf-bc7e558b98b5 | CTAS | stock | discovery: CTAS CTAS Trend Pullback Long | 0 */2 * * 1-5 | 2026-07-17 12:42:17.342633+00 | 2026-07-17 12:42:17.342633+00 | null | legacy holdover (older than newest active cohort, no run evidence) |
| ca2fd13b-1a91-4c26-bf3c-e6662a0c2135 | MLAC | stock | discovery: MLAC MLAC Swing Reclaim | 0 */2 * * 1-5 | 2026-07-04 07:08:33.563343+00 | 2026-07-04 07:08:33.563343+00 | null | legacy holdover (older active row, no recent run evidence) |
| 31da17b6-70bf-418a-8f53-b9516ca6a3ce | ZCMD | stock | discovery: ZCMD ZCMD Oversold Rebound | 0 */2 * * 1-5 | 2026-06-30 07:03:12.618829+00 | 2026-06-30 07:03:12.618829+00 | 2026-06-30 13:55:12.715502+00 | legacy holdover (last run is stale; duplicate group present) |
| 417e121c-44b3-495f-bb06-de5f5e6add28 | NOTV | stock | discovery: NOTV NOTV Oversold Mean Reversion | 0 */2 * * 1-5 | 2026-06-06 07:02:22.415374+00 | 2026-06-06 07:02:22.415374+00 | 2026-07-06 15:02:31.306586+00 | schedule issue (weekly cadence appears not to be running; no duplicate group) |
| 4a76f6b5-438a-4ef8-98c1-761ee9260015 | HSPT | stock | discovery: HSPT HSPT RSI Mean Reversion | 0 */2 * * 1-5 | 2026-06-23 07:02:35.746272+00 | 2026-06-23 07:02:35.746272+00 | 2026-07-10 16:08:33.420046+00 | schedule issue (weekly cadence appears not to be running; no duplicate group) |

## Raw rows — duplicate active ticker groups

| market | ticker | active_count | classification | notes |
| --- | --- | --- | --- | --- |
| stock | WDC | 7 | intentional variant | all are active, paper=true, same schedule, varied names; this looks like a strategy family rather than an accidental clone |
| stock | GLW | 4 | intentional variant | same schedule, paper=true, varied strategy names; likely family variants |
| stock | GNRC | 3 | intentional variant | same schedule, paper=true, varied names; likely family variants |
| stock | HPE | 3 | intentional variant | same schedule, paper=true, varied names; likely family variants |
| stock | MRVL | 3 | intentional variant | same schedule, paper=true, varied names; likely family variants |
| stock | MU | 3 | intentional variant | same schedule, paper=true, varied names; likely family variants |
| stock | STX | 3 | intentional variant | same schedule, paper=true, varied names; likely family variants |
| stock | AMD | 2 | intentional variant | same schedule, paper=true, varied names; likely family variants |
| stock | GS | 2 | intentional variant | same schedule, paper=true, varied names; likely family variants |
| stock | JLHL | 2 | intentional variant | same schedule, paper=true, varied names; likely family variants |
| stock | MCHP | 2 | intentional variant | same schedule, paper=true, varied names; likely family variants |
| stock | NVDA | 2 | intentional variant | same schedule, paper=true, varied names; likely family variants |
| stock | ON | 2 | intentional variant | same schedule, paper=true, varied names; likely family variants |
| stock | ORCL | 2 | intentional variant | same schedule, paper=true, varied names; likely family variants |
| stock | TXN | 2 | needs operator decision (uncertain) | one row has no run evidence yet; both are active and newly created |
| stock | ZCMD | 2 | accidental duplicate or legacy holdover (uncertain) | one row is stale with last run on 2026-06-30; another is newer; same schedule/name family suggests either deliberate replacement or overlap |

## Pause candidates (proposals only)

No mutations were performed. Proposed pause candidates with rollback state recorded as `not applied`:

- `f47205a0-cfcb-42cd-8dbf-bc7e558b98b5` (CTAS) — proposal only; rollback state: not applied.
- `ca2fd13b-1a91-4c26-bf3c-e6662a0c2135` (MLAC) — proposal only; rollback state: not applied.
- `31da17b6-70bf-418a-8f53-b9516ca6a3ce` (ZCMD) — proposal only; rollback state: not applied.
- `417e121c-44b3-495f-bb06-de5f5e6add28` (NOTV) — proposal only; rollback state: not applied.
- `4a76f6b5-438a-4ef8-98c1-761ee9260015` (HSPT) — proposal only; rollback state: not applied.

## Notes

- Duplicate groups must be reviewed as intentional variant / accidental duplicate / legacy holdover / needs operator decision.
- Dormant strategies must be reviewed as intentional variant / schedule issue / legacy holdover / needs operator decision.
- SELECT-only inventory only.
- No pause/disable/mutation actions were executed.
