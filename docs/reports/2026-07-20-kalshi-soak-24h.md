# Kalshi soak (24h) evidence

- SOAK_START_UTC: 2026-07-20T17:24:01Z
- SOAK_END_UTC: pending elapsed time
- Scope: read-only initial soak snapshot only

## Snapshot

- Paper submitted backlog / duplicate fills / allocator repeat checks: query bundle defined, not mutated
- Observed log window: app logs include ongoing provider activity and no matching 413/duplicate-fill alarm in the sampled output
- Health endpoint verification: `http://10.0.0.56:3030/healthz` and `https://augr.subcult.tv/healthz` both returned `{"status":"ok","db":"ok","redis":"ok"}`; app/Postgres/Redis were healthy and web was running.

## Notes

- SOAK_END remains pending until elapsed time is available.
- No jobs were enabled.
- No strategy or DB mutations were performed.
