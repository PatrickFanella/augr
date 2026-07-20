# P2 Kalshi snapshot sizing

## Measured production state
- Table size: 20,809,555,968 bytes total, 17,797,865,472 bytes heap, 1,742,888,960 bytes indexes.
- Rows: 10,749,315.
- Bytes per row: 1,935.90 total, 1,655.72 heap, 162.14 index.
- Snapshot window: 2026-06-18 21:15:01+00 to 2026-07-20 06:15:09+00.

## Complete-day ingestion observed
- Only ~31 days of history exist, so 60/90-day observed bands are unavailable.
- Complete days queried: 2026-06-19 through 2026-07-19 (current partial day excluded).
- 7 complete days (2026-07-13 through 2026-07-19): 1,191,524 rows total, 170,217.71/day average.
- 14 complete days (2026-07-06 through 2026-07-19): 1,217,874 rows total, 86,991.00/day average.
- 30 complete days (2026-06-20 through 2026-07-19): 8,630,417 rows total, 287,680.57/day average.
- Peak complete-day volume in the sampled window: 2,509,682 snapshots on 2026-06-21.
- Recent complete day: 177,884 on 2026-07-19.

## Benchmark plan shapes
- Latest snapshot by ticker (`KX2028DRUN-28-JTAL`): index scan, 2.522 ms execution, 5 shared reads.
- 7-day count: parallel index-only scan, 15.386 s execution, 1,191,124 heap fetches.
- 30-day group-by ticker: parallel index scan + hash aggregate, 122.491 s execution, temp spill to disk.

## 90-day forecast
Using complete-day ingestion bands and a 1.25x safety factor:
- History-wide scenario (30-day average, including the initial ingestion surge): 287,680.57/day × 90 × 1.25 = 32,364,064 rows over 90 days.
- Recent-rate scenario (7-day average): 170,217.71/day × 90 × 1.25 = 19,149,490 rows over 90 days.
- Heap growth from measured 1,655.72 bytes/row:
  - history-wide: 32,364,064 × 1,655.72 = 53,585,857,278 bytes (49.91 GiB)
  - recent-rate: 19,149,490 × 1,655.72 = 31,706,214,689 bytes (29.53 GiB)
- Index growth from measured 162.14 bytes/row:
  - history-wide: 32,364,064 × 162.14 = 5,247,494,381 bytes (4.89 GiB)
  - recent-rate: 19,149,490 × 162.14 = 3,104,889,833 bytes (2.89 GiB)
- Total relation growth from measured 1,935.90 bytes/row:
  - history-wide: 32,364,064 × 1,935.90 = 62,653,462,454 bytes (58.35 GiB)
  - recent-rate: 19,149,490 × 1,935.90 = 37,071,425,792 bytes (34.53 GiB)

## Proposal only
- Keep current retention/compression disabled.
- Revisit snapshot partitioning / summary-table design if 30-day grouping remains a hot path.
- Investigate vacuum visibility and index-only-scan effectiveness before any policy change.

## Notes
- All measurements were read-only against production PostgreSQL.
- One aggregate growth query ran for longer than a nominal 120s client timeout; the server completed it and the result was captured read-only.
