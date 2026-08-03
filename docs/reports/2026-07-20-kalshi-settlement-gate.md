# Kalshi settlement gate evidence

- Read-only source: production schema 59 / `kalshi_settlement_gate`
- Snapshot time UTC: 2026-07-20T17:24:01Z

## Evidence

```text
server_version_num=170002
expected_schema_version=59
paper_ordered_count=3
live_ordered_count=0
closed_count=0
kalshi_trade_decision_count=3

job_name=kalshi_settlement
consecutive_successes=1
threshold=20
eligible=false
projection_fingerprint=726b25866a747daa5622987a5489098a497c0b7cfa64623f5ca6d9057fcde197
last_outcome=success
last_error=
fetched=2
resolved=1
would_settle_markets=1
would_settle_decisions=1
last_run_at=2026-07-20 09:52:09.715966+00
updated_at=2026-07-20 09:52:09.735662+00
```

## Notes

- No settlement job was invoked.
- No database writes were performed.
- Current gate state remains `1/20` with 19 qualifying runs remaining.

## Qualifying run history

| run time UTC | prior | new | outcome | fingerprint stable | fetched | resolved | would settle markets | would settle decisions | financial mutation |
| --- | ---: | ---: | --- | --- | ---: | ---: | ---: | ---: | --- |
| 2026-07-20T09:52:09Z | 0 | 1 | success | baseline | 2 | 1 | 1 | 1 | none (dry run) |
| 2026-07-20T20:12:57Z | 1 | 2 | success | yes | 2 | 1 | 1 | 1 | none (dry run) |
| 2026-07-20T20:25:11Z | 2 | 3 | success (job run `ok`) | yes — `726b25866a747daa5622987a5489098a497c0b7cfa64623f5ca6d9057fcde197` | 2 | 1 | 1 | 1 | none — untouched (`dry_run=1`; job disabled) |

Current gate state is `2/20`, `eligible=false`; 18 qualifying hourly windows remain. The guarded recurring task `augr-kalshi-settlement-dry-run` runs at minute 20, invokes at most once per eligible window, and disables itself on drift, error, reset, or 20/20. It cannot enable settlement or execute a live canary.

As of 2026-07-20T20:25:11Z, the durable gate state is `3/20`, `eligible=false`; 17 qualifying hourly windows remain. The production job remained disabled throughout run `2bfd59c9-99ce-4168-975b-6e421e98642f`.

| 2026-07-20T21:54:20Z | 3 | 4 | success (job run `ok`) | yes — `726b25866a747daa5622987a5489098a497c0b7cfa64623f5ca6d9057fcde197` | 2 | 1 | 1 | 1 | none — untouched (`dry_run=1`; job disabled) |

As of 2026-07-20T21:54:20Z, the durable gate state is `4/20`, `eligible=false`; 16 qualifying hourly windows remain. The production job remained disabled throughout run `1fb23343-758d-4a1c-b7dc-4b86805fb084`.

| 2026-07-20T22:29:15Z | 4 | 5 | success (job run `ok`) | yes — `726b25866a747daa5622987a5489098a497c0b7cfa64623f5ca6d9057fcde197` | 2 | 1 | 1 | 1 | none — untouched (`dry_run=1`; job disabled) |

As of 2026-07-20T22:29:15Z, the durable gate state is `5/20`, `eligible=false`; 15 qualifying hourly windows remain. The production job remained disabled throughout run `a8dc38c5-b876-468b-a30f-8716c6c8bd2b`.

| 2026-07-20T23:29:39Z | 5 | 6 | success (job run `ok`) | yes — `726b25866a747daa5622987a5489098a497c0b7cfa64623f5ca6d9057fcde197` | 2 | 1 | 1 | 1 | none — untouched (`dry_run=1`; job disabled) |

As of 2026-07-20T23:29:39Z, the durable gate state is `6/20`, `eligible=false`; 14 qualifying hourly windows remain. The production job remained disabled throughout run `c0a35430-f4ee-47f5-93fe-aef2a17845c1`.

| 2026-07-21T00:33:37Z | 6 | 7 | success (job run `ok`) | yes — `726b25866a747daa5622987a5489098a497c0b7cfa64623f5ca6d9057fcde197` | 2 | 1 | 1 | 1 | none — untouched (`dry_run=1`; job disabled before and after) |

As of 2026-07-21T00:33:37Z, the durable gate state is `7/20`, `eligible=false`; 13 qualifying hourly windows remain. The production job remained disabled throughout run `865a0732-82c6-4351-a22c-f806768de89d`.

| 2026-07-21T01:25:07Z | 7 | 8 | success (job run `ok`) | yes — `726b25866a747daa5622987a5489098a497c0b7cfa64623f5ca6d9057fcde197` | 2 | 1 | 1 | 1 | none — untouched (`dry_run=1`; job disabled before and after) |

As of 2026-07-21T01:25:07Z, the durable gate state is `8/20`, `eligible=false`; 12 qualifying hourly windows remain. The production job remained disabled throughout run `d2e3d5b9-548b-46a2-9a43-ca237054a582`.

| 2026-07-21T02:31:35Z | 8 | 9 | success (job run `ok`) | yes — `726b25866a747daa5622987a5489098a497c0b7cfa64623f5ca6d9057fcde197` | 2 | 1 | 1 | 1 | none — untouched (`dry_run=1`; job disabled before trigger; 0 closed decisions after) |

As of 2026-07-21T02:31:35Z, the durable gate state is `9/20`, `eligible=false`; 11 qualifying hourly windows remain. The production job remained disabled for run `09ac8c54-14b5-4c0d-ab62-266795b08598`.

| 2026-07-21T03:28:46Z | 9 | 10 | success (job run `ok`) | yes — `726b25866a747daa5622987a5489098a497c0b7cfa64623f5ca6d9057fcde197` | 2 | 1 | 1 | 1 | none — untouched (`dry_run=1`; job disabled before and after; paper/live/closed/Kalshi decisions `5/0/0/5` → `5/0/0/5`) |

As of 2026-07-21T03:28:46Z, the durable gate state is `10/20`, `eligible=false`; 10 qualifying hourly windows remain. The production job remained disabled throughout run `4ac3e9a1-a7fe-41cd-95fb-21ece20fd9c2`.

| 2026-07-21T04:23:18Z | 10 | 11 | success (job run `ok`) | yes — `726b25866a747daa5622987a5489098a497c0b7cfa64623f5ca6d9057fcde197` | 2 | 1 | 1 | 1 | none — untouched (`dry_run=1`; job disabled at preflight; paper/live/closed/Kalshi decisions `5/0/0/5` → `5/0/0/5`) |

As of 2026-07-21T04:23:18Z, the durable gate state is `11/20`, `eligible=false`; 9 qualifying hourly windows remain. The production job remained disabled for run `321711de-5b2e-4cf8-8e9e-41d56b23a349`.

| 2026-07-21T05:22:56Z | 11 | 12 | success (job run `ok`) | yes — `726b25866a747daa5622987a5489098a497c0b7cfa64623f5ca6d9057fcde197` | 2 | 1 | 1 | 1 | none — untouched (`dry_run=1`; job disabled at preflight; paper/live/closed/Kalshi decisions `5/0/0/5` → `5/0/0/5`) |

As of 2026-07-21T05:22:56Z, the durable gate state is `12/20`, `eligible=false`; 8 qualifying hourly windows remain. The production job remained disabled for run `40a742a0-145f-4a41-a1d7-1fad6456c638`.

| 2026-07-21T06:30:22Z | 12 | 13 | success (job run `ok`) | yes — `726b25866a747daa5622987a5489098a497c0b7cfa64623f5ca6d9057fcde197` | 2 | 1 | 1 | 1 | none — untouched (`dry_run=1`; job disabled at preflight; paper/live/closed/Kalshi decisions `6/0/0/6` → `6/0/0/6`) |

As of 2026-07-21T06:30:22Z, the durable gate state is `13/20`, `eligible=false`; 7 qualifying hourly windows remain. The production job remained disabled for run `d2cded56-a3e7-4909-87e8-d8217820ec9d`.

| 2026-07-21T07:24:51Z | 13 | 14 | success (job run `ok`) | yes — `726b25866a747daa5622987a5489098a497c0b7cfa64623f5ca6d9057fcde197` | 2 | 1 | 1 | 1 | none — untouched (`dry_run=1`; job disabled at preflight; paper/live/closed/Kalshi decisions `6/0/0/6` → `6/0/0/6`) |

As of 2026-07-21T07:24:51Z, the durable gate state is `14/20`, `eligible=false`; 6 qualifying hourly windows remain. The production job remained disabled for run `206d3491-7cca-42e0-aa06-e70e59ae61bb`.

| 2026-07-21T08:29:51Z | 14 | 15 | success (job run `ok`) | yes — `726b25866a747daa5622987a5489098a497c0b7cfa64623f5ca6d9057fcde197` | 2 | 1 | 1 | 1 | none — untouched (`dry_run=1`; job disabled before and after; paper/live/closed/Kalshi decisions `6/0/0/6` → `6/0/0/6`) |

As of 2026-07-21T08:29:51Z, the durable gate state is `15/20`, `eligible=false`; 5 qualifying hourly windows remain. The production job remained disabled throughout run `229b7fe8-c746-47ae-9d2e-50503df9ed43`.

| 2026-07-21T09:31:26Z | 15 | 16 | success (job run `ok`) | yes — `726b25866a747daa5622987a5489098a497c0b7cfa64623f5ca6d9057fcde197` | 2 | 1 | 1 | 1 | none — untouched (`dry_run=1`; job disabled before and after; Kalshi-scoped decisions/positions/trades/replay/idempotency snapshot unchanged) |

As of 2026-07-21T09:31:26Z, the durable gate state is `16/20`, `eligible=false`; 4 qualifying hourly windows remain. The production job remained disabled throughout run `dcb5c6b6-eb91-4dee-b8aa-6af3b6a1ddd6`.

| 2026-07-21T10:31:56Z | 16 | 17 | success (job run `ok`) | yes — `726b25866a747daa5622987a5489098a497c0b7cfa64623f5ca6d9057fcde197` | 2 | 1 | 1 | 1 | none — untouched (`dry_run=1`; job disabled before and after; Kalshi-scoped decisions/positions/trades/replay/idempotency snapshot unchanged) |

As of 2026-07-21T10:31:56Z, the durable gate state is `17/20`, `eligible=false`; 3 qualifying hourly windows remain. The production job remained disabled throughout run `c4ca70cd-7e8a-4e0d-a10a-85dfefcfabdb`.

| 2026-07-21T11:30:22Z | 17 | 18 | success (job run `ok`) | yes — `726b25866a747daa5622987a5489098a497c0b7cfa64623f5ca6d9057fcde197` | 2 | 1 | 1 | 1 | none — untouched (`dry_run=1`; job disabled before and after; Kalshi-scoped decision state hash and settlement positions/trades/replay/idempotency snapshot unchanged) |

As of 2026-07-21T11:30:22Z, the durable gate state is `18/20`, `eligible=false`; 2 qualifying hourly windows remain. The production job remained disabled throughout run `dffab172-96f1-4606-a74d-8954b063765b`.

| 2026-07-21T17:17:17Z | 18 | 19 | success (job run `ok`) | yes — `726b25866a747daa5622987a5489098a497c0b7cfa64623f5ca6d9057fcde197` | 2 | 1 | 1 | 1 | none — untouched (`dry_run=1`; job disabled before and after; Kalshi-scoped decision state hash and settlement positions/trades/replay/idempotency snapshot unchanged) |

As of 2026-07-21T17:17:17Z, the durable gate state is `19/20`, `eligible=false`; 1 qualifying hourly window remains. The production job remained disabled throughout run `da69615b-8923-4ebb-bbeb-59bc01b9bad3`.

| 2026-07-21T18:26:23Z | 19 | 20 | success (job run `ok`) | yes — `726b25866a747daa5622987a5489098a497c0b7cfa64623f5ca6d9057fcde197` | 2 | 1 | 1 | 1 | none — untouched (`dry_run=1`; job disabled before and after; Kalshi-scoped decision/positions/trades/replay/idempotency counts and hashes unchanged) |

As of 2026-07-21T18:26:23Z, the durable gate state is `20/20`, `eligible=true`. The recurring task is disabled, and the production job remained disabled throughout run `d1165df3-7c8f-450a-b322-51fcecb28721`. Enabling settlement or running a live canary still requires separate operator approval.
