# Augr automation audit — 2026-08-05

Status: in progress  
Production timezone: America/Chicago  
Production stack: Compose project `augr`, `/srv/repos/patrickfanella/augr/docker-compose.nuc.yml`  
Audit start: 2026-08-05 16:36 CDT  

## Release constraint

Deploy exactly once, only after the inventory, execution audit, remediation, affected-path reruns, and complete release gate are finished. Live trading must remain disabled. Existing paper/dry-run modes, allowlists, kill switches, and settlement gates must remain fail-closed.

## Baseline

- Git: clean `main`, seven commits ahead of `origin/main` at `0f7fdf3`; upstream divergence was `7 0` at audit start.
- Current images: app `augr-app:0f7fdf3a0801`; web `augr-web:review-735b344bf7ab`; OpenCode `1.17.20` pinned by digest.
- Health: app, PostgreSQL, Redis, and OpenCode healthy. Schema version 60.
- Runtime controls: `ENABLE_LIVE_TRADING=false`, `ALPACA_PAPER_MODE=true`, `BINANCE_PAPER_MODE=true`, scheduler enabled, Polymarket automation disabled.
- Effective LLM routing: deep `openai/gpt-5.6-sol`, quick `openai/gpt-5.6-luna`, fallback/default OpenCode model `openai/gpt-5.6-terra`.
- A separate legacy Compose project named `augr-prod` is owned by `/srv/server/projects/augr`; it is out of scope and will not be mutated.

## Registry reconciliation

The live orchestration API exposed 27 jobs. The code contains five additional conditionally registered jobs that are absent from the live registry: four Polymarket jobs because `ENABLE_POLYMARKET_AUTOMATION=false`, and `kalshi_reconcile` because no live Kalshi reconciler is wired. These are inventoried as configured-off/unavailable rather than silently omitted. Scheduled strategy pipelines are inventoried separately below because they use the strategy scheduler, not the automation-job registry.

### Strategy scheduler registry

- Database inventory: 163 active paper strategies: 141 stock and 22 Kalshi. There are no active scheduled backtests.
- Stored schedules: 134 stock strategies at `0 */2 * * *`, seven stock strategies at `0 */2 * * 1-5`, and 22 Kalshi strategies at `0 */6 * * *`.
- Runtime inventory: 130 unique strategy keys plus ticker discovery, for 131 registered scheduler jobs. Registration deduplicates `(ticker, market_type, schedule)` and warns on the 33 duplicate stock rows it skips.
- Event-driven gap/hot scans previously bypassed this deduplication by triggering every active row. Commit `ce1dca4` applies deterministic key-level canonicalization to event triggers.
- Ticker discovery is separately enabled because `ENABLE_TICKER_DISCOVERY=true`; it refreshes the stock universe using Polygon and feeds discovery.
- The stock scheduler's session is Alpaca extended hours (Sunday 20:00 ET through Friday 20:00 ET, excluding its overnight break), not only regular NYSE hours. Pre-/after-hours automation jobs use the NYSE calendar.
- Ticker discovery runs at 10:30 UTC on weekdays (`30 10 * * 1-5`). It performs a stock premarket screen and the same paper discovery stages as `discovery_run`; its screen and discovery implementation are covered by scheduler/discovery tests and the latest persisted discovery result. Unlike orchestrator jobs, it has no `automation_job_runs` row or API status, so historical run-level proof is unavailable. **Degraded but usable; durable observability recommended.**

### Live automation jobs

| Job | Purpose | Schedule / market calendar | Dependencies / downstream | Start-of-audit state | Provisional classification |
|---|---|---|---|---|---|
| `universe_refresh` | Refresh stock universe from Polygon | Sun 12:00 ET; provider calendar, not market-hours gated | source for watchlist/history/scans | enabled; last run failed 2026-08-02 | Buggy |
| `history_refresh` | Refresh stock OHLCV history | Tue–Sat 04:00 ET | source for scans/backtests | enabled; last run OK | Pending audit |
| `current_data_refresh` | Refresh intraday stock OHLCV | every 15m in NYSE hours | blocks `hot_scan`; feeds strategies | enabled; last run reported 105/115 updated | Buggy |
| `gap_scanner` | Detect premarket gaps/volume | weekdays 08:00 ET, NYSE calendar | blocks `discovery_run`; updates watch scores | enabled; last run OK | Pending audit |
| `hot_scan` | Score top watchlist | every 15m in NYSE hours | waits while refresh runs; may trigger strategies | enabled; last run OK | Pending audit |
| `deep_scan` | Score full stock universe | hourly in NYSE hours | waits while hot scan runs; updates watch scores | enabled; last run OK | Pending audit |
| `discovery_run` | Discover stock strategies | weekdays 08:30 ET, NYSE calendar | waits while gap scan runs; creates candidates/configs | enabled; last run OK | Pending audit |
| `position_review` | Review positions before open | weekdays 09:00 ET, NYSE calendar | strategy/pipeline inputs | enabled; old implementation was a no-op | Outright failure; fixed |
| `earnings_scanner` | Ingest upcoming earnings | weekdays 10:00 ET, NYSE calendar | event context/triggers | enabled; recovered from one error | Pending audit |
| `filing_monitor` | Monitor 8-K filings | every 4h weekdays, NYSE holiday skip | LLM filing analysis/event triggers | enabled; last run OK | Pending audit |
| `news_scan` | Ingest RSS and LLM-triage news | twice hourly in NYSE hours | news context | enabled; last run OK | Pending audit |
| `social_scan` | StockTwits trend/sentiment | every 15m in NYSE hours | social context | enabled; last run queried non-stock positions | Buggy |
| `overnight_backtest` | Chunked five-year backtests | every 30m 01:00–05:59 ET Tue–Sat | waits while history refresh runs; input to sweep/generate | enabled; last run OK | Pending audit |
| `overnight_sweep` | Optimize deployed strategies | 02:00 ET Tue–Sat | waits while backtest runs | enabled; last run OK | Pending audit |
| `overnight_generate` | LLM-generate strategy ideas | 03:00 ET Tue–Sat | waits while sweep/backtest run; feeds options discovery | enabled; last run 2026-08-01 | Pending audit |
| `options_discovery` | Discover option strategies | 03:30 ET Tue–Sat | waits while overnight generation runs | enabled; last run OK | Pending audit |
| `options_scan` | Scan next-day option setups | weekdays 22:00 ET, after-hours/NYSE calendar | option candidates | enabled; last run OK | Pending audit |
| `options_expiry_settlement` | Cash-settle expired paper options | weekdays 23:00 ET, after hours | precedes lifecycle reconcile | enabled; last run OK | Pending audit |
| `options_lifecycle_reconcile` | Reconcile option lifecycle records | weekdays 23:30 ET, after hours | waits while expiry settlement runs | enabled; last run OK | Pending audit |
| `daily_review` | Review daily pipeline/decision quality | weekdays 20:30 ET, after hours | reporting/maintenance | enabled; last run OK | Pending audit |
| `strategy_resweep` | Resweep deployed strategies | weekdays 21:00 ET, after hours | maintenance/candidate state | enabled; last run OK | Pending audit |
| `paper_validation_report` | Generate paper validation reports | weekdays 17:00 ET, after hours | consumes strategies/backtests/runs; report artifacts | enabled; old runs averaged 2h34m and hid item errors | Outright failure; fixed |
| `portfolio_allocator` | Shadow portfolio allocation | :15/:45 after hours, NYSE calendar | consumes opportunity queue; may paper execute | enabled; current queue empty | Pending audit |
| `alpaca_reconcile` | Reconcile paper broker lifecycle | every 5m, 24/7 | orders/trades/positions | enabled; current result consistent | Pending audit |
| `kalshi_discovery` | Generate Kalshi paper strategies | hourly at :15, Kalshi/provider calendar | strategies, snapshots, discovery progress | enabled; active since 21:15 UTC at baseline | Degraded but usable |
| `kalshi_settlement` | Settle resolved Kalshi paper contracts | every 5m, Kalshi/provider calendar | decisions/positions/trades/replay | auto-disabled after gate-ineligible failures | Outright failure, fail-closed; fixed |
| `strategy_tournament` | Compare/prune strategies | Sun 14:00 ET | strategy state | old path capped at 100 and mixed markets | Buggy; fixed |

### Conditional jobs absent from live registry

| Job | Reason absent | Safety disposition |
|---|---|---|
| `polymarket_profiles` | Polymarket automation disabled | do not enable during audit |
| `polymarket_reconcile` | Polymarket automation disabled | do not enable during audit |
| `polymarket_resolutions` | Polymarket automation disabled | do not enable during audit |
| `polymarket_strategy_discovery` | Polymarket automation disabled | do not enable during audit |
| `kalshi_reconcile` | live reconciler dependency not wired | do not create live credentials or enable live execution |

## Dependency graph

1. `universe_refresh` → `history_refresh` → `current_data_refresh` → `hot_scan` → `deep_scan`.
2. `gap_scanner` → `discovery_run` → `overnight_backtest` → `overnight_sweep` → `overnight_generate` → `options_discovery`.
3. Market/event context (`earnings_scanner`, `filing_monitor`, `news_scan`, `social_scan`, `position_review`) → scheduled strategy pipelines.
4. Candidate and decision outputs → `portfolio_allocator` → paper execution → `alpaca_reconcile`.
5. `options_expiry_settlement` → `options_lifecycle_reconcile`.
6. Kalshi discovery/strategy outputs → paper execution → gated `kalshi_settlement`; live `kalshi_reconcile` is intentionally absent.
7. `daily_review`, `strategy_resweep`, `paper_validation_report`, and `strategy_tournament` consume earlier lifecycle state and produce reports/maintenance changes.

The implementation's `DependsOn` relation only prevents overlap; it does not require a successful predecessor. This distinction will be considered in each classification.

## Confirmed findings and remediation log

| Priority | Finding | Evidence / impact | Remediation status |
|---|---|---|---|
| P0 | Provider query credential persisted in a failed automation error and was returned by the authenticated status API. | `universe_refresh` run on 2026-08-02 stored a transport URL containing the credential; another provider echoed its configured credential in a non-success body. | Fixed in `c119d6d`: transport errors remove query strings and configured credentials are redacted from bodies. One historical row was sanitized; database scans now find zero occurrences in automation runs, pipeline runs, or agent events. Provider rotation remains external. |
| P0 | Stock refresh/social jobs accepted known prediction-market positions. | Live logs showed Kalshi contract identifiers sent to stock OHLCV providers and StockTwits; refresh still returned overall success with one failed batch. | Regression tests added; known non-stock positions are filtered. Affected safe reruns pending deploy. |
| P1 | Manual API trigger could execute a disabled job. | `RunJob` did not inspect `Enabled`; disabled state was checked only by cron wrapper. | Regression test added; manual trigger now rejects disabled jobs and direct runner rechecks state. |
| P1 | Kalshi settlement could not qualify its own safety gate: disabled mode never ran previews, while enabled mode rejected ineligibility and auto-disabled. | Live status: 8 consecutive failures, threshold 20, eligibility false. | Fixed in `ce1dca4`: scheduled non-mutating previews build stable evidence; mutation requires configured execution intent, persisted eligibility, and an unchanged fingerprint. Missing/unhealthy gate remains fail-closed. |
| P1 | Operator API credential variables are populated but rejected and no API-key rows exist. | Both configured operator values returned HTTP 401; database `api_keys` table empty. | Authentication state unchanged; audit uses a short-lived JWT signed with the existing secret. Permanent operator-credential repair pending. |
| P1 | `position_review` counted every active strategy as having a position without querying positions. | Eight historical runs reported success in about 0.1s but performed no review. | Fixed in `ce1dca4`: paginated open positions are joined to active strategies; ownership, inactive links, missing prices, and missing stops are summarized. |
| P1 | Paper validation slept up to 119 seconds before each local report, included event markets, persisted item errors, then returned success. | Seven runs averaged 9,237s; the current run remained active beyond an hour. At sampling time 35 reports completed and 12 errored against 163 active paper strategies. | Fixed in `ce1dca4`: artificial delay removed, event markets skipped, complete summary persisted, and eligible item failures fail the job. |
| P1 | Orchestrator jobs ignored `SCHEDULER_JOB_TIMEOUT` and ran with `context.Background()`. | Long/stuck work had no hard ceiling; reports routinely exceeded two hours. | Fixed in `ce1dca4`: manual and scheduled orchestration jobs now receive the configured two-hour timeout. |
| P1 | Risk debate lost authoritative market context and treated ARE's $46.69 lower Bollinger support as current price, despite a $50.10 close. | Run `07b35b8f-...`: judge cited $50.10, later risk roles calculated from $46.69. Final action remained HOLD, so no order was created. | Fixed in `f190265`: the market analyst report is now supplied to every risk debater and the judge. |
| P2 | Deep-think decisions stored zero latency because debate/judge/trader/risk nodes rebuilt completion objects and discarded provider metadata. | ARE, APTV, and ROK had quick-role latency but zero deep-role latency. | Fixed in `beeac89`: the original model, usage, latency, and cost now flow to persistence. |
| P2 | Tournament, earnings, filings, and event triggers had incomplete or wrong-market coverage. | Tournament capped at 100 of 163 and attempted event OHLCV; filings always chose the first 20; earnings included Kalshi; event triggers duplicated scheduler keys. | Fixed in `ce1dca4`: pagination/capability filters, rotating filing batches, honest partial failures, summaries, and canonical triggers. |
| P2 | Successful refresh status concealed failed batches. | Latest pre-fix refresh reported `errors=1`, `updated=105/115`, but persisted `ok`. | Fixed in `ce1dca4`: partial source/batch failures return an aggregate error with the retained summary. |
| P2 | Scheduler-only ticker discovery has logs/metrics but no durable run/status record. | It is the 131st scheduler registration, runs weekdays at 10:30 UTC, and cannot be sampled historically like the 27 orchestrator jobs. | Deferred to a scheduler observability change: persist start/outcome/summary using the same job-run contract without changing discovery behavior. Owner: application maintainers. |
| P2 | PostgreSQL reports a collation-version metadata warning on every operator query. | Repeated database warning; no query failure observed. | Existing collation runbook to be evaluated; defer unless release integrity is affected. |

## Sampling plan

For jobs with at least five runs, sample at minimum: earliest available, middle, latest, slowest, and an error/retry/anomalous run when present. Expand whenever inputs, outputs, duration, or outcome differ materially. For jobs with fewer than five runs, inspect all available runs plus a safe reproduction. LLM-bearing paths additionally sample decisions and agent events across early/middle/late roles and verify prompt data, schema, and effective model tier.

## Execution evidence

All persisted automation runs were queried. Samples were stratified by earliest, 25th percentile, median, 75th percentile, latest, slowest, and anomalous/error outcome. This gives at least five records where five exist; every record was inspected where fewer exist.

| Automation | Population and representative evidence | Assessment and classification before final deploy |
|---|---|---|
| `alpaca_reconcile` | 2,202 runs, 2,187 OK/15 recovered DNS errors; `0d5690bd`, `0052886c`, `dc2e4279`, `07ad9df0` (slow), `8453e98b` (error), `34e43e0f` (latest). | Paper broker only; latest updated one order, created no trades/positions, and found no drift. **Healthy with recommendations.** |
| `current_data_refresh` | 463 labeled OK; `c5da4ea7`, `777c952c`, `46651f35`, `e29ca55d`, `6245dd7b` (397s), `d21e6608`. | Correct stock/5m/48h inputs after filter fix. Latest pre-fix output was 105/115 with one failed batch. **Degraded; fixed.** |
| `daily_review` | Seven OK, all inspected (`e945623f` through `d47e13d8`), 0.1–0.2s. | Deterministic database review, no LLM or financial mutation. **Healthy.** |
| `deep_scan` | 82 OK; `99a382fb`, `e6a51c35`, `54cd3cf7`, `e51dc2a3`, `17d2cfb7` (2,292s), `b38389a0`. | Cached OHLCV updates stock scores; long outlier recovered, latest 37s. **Healthy with recommendations.** |
| `discovery_run` | Eight OK; `811ea571`, `8f457e6d`, `efb5c61a`, `5f4386a0` (9,300s), `22930be0`. | Latest: 30 candidates/generated/swept, six validated, three paper deployments, zero errors. **Healthy with recommendations.** |
| `earnings_scanner` | Seven OK/one DNS error; `b68eafa9`, `99b1a754`, `bc150990`, `7c1bb4e5`, `fa2cb592`. | Current Finnhub dates; pre-fix ticker set mixed markets. No execution. **Degraded; fixed.** |
| `filing_monitor` | 47 labeled OK; `3fce9b04`, `bb9095af`, `890a5bb9`, `23e949a1`, `5b255488` (322s), `663155f0`. | Stock-only, but permanently favored first 20 and hid rate-limit exits. Quick-tier analysis is advisory. **Degraded; fixed.** |
| `gap_scanner` | Eight OK; `f01663cd`, `87dfa7b6`, `2509e967`, `029cbc7e` (40s), `a2396213`. | Polygon stock snapshots and score writes correct; duplicate paper triggers fixed. **Healthy with recommendation.** |
| `history_refresh` | Seven OK, 984–1,351s; all sampled, latest `0cc34d2c`. | Paginates universe and persists historical stock bars; provider-bound but stable. **Healthy with recommendations.** |
| `hot_scan` | 152 labeled OK; `6c208352`, `f2ac84fa`, `8ad65a6f`, `1bcb01ed` (4,688s), `1c24a7c6`, `2462501c`. | Recent scans ~2s; duplicate strategy-row triggers caused redundant long pipelines and are now canonicalized. **Degraded; fixed.** |
| `kalshi_discovery` | 203 OK/20 provider errors; `89dde857`, `fcba2b22`, `fd9839d5`, `31dd9e9a`, `44886619`, `c045f4a3`. | Demo catalog, conservative screen, at most one paper deployment. Errors were DNS/timeout/500. **Degraded but usable; external provider.** |
| `kalshi_settlement` | 294 OK previews/eight errors; `23d083df`, `64656faa`, `9956cbc0`, `4d535067`, `97703831`, `adabec4b`. | Paper settlement stayed fail-closed but gate progression was impossible. Preview fingerprint/explicit IDs preserve idempotency. **Outright failure; fixed.** |
| `news_scan` | 231 OK; `cd37e984`, `723c0877`, `1db282d6`, `5e7d9e43`, `660b6b7b` (356s), `ffa4d8ff`. | RSS, stock relevance, quick-tier triage, and persisted news coherent. **Healthy with recommendations.** |
| `options_discovery` | Seven OK, all sampled, 13–95s; latest `9e4b1b23` had nine candidates, three generated/scored/swept, none deployed. | Correct dependency and paper-only output. **Healthy.** |
| `options_expiry_settlement` | Seven OK, all sampled, <0.1s; latest `d5d2506f`. | No expired open options; zero mutations is correct/idempotent. **Healthy.** |
| `options_lifecycle_reconcile` | Seven OK, all sampled, <0.1s; latest `91088044`. | Zero option lifecycle records/findings; correct empty-state result. **Healthy.** |
| `options_scan` | Seven OK, all sampled, 645–765s; latest `bb1a2bf8`. | Slow sequential provider scan, but bounded/after-hours and paper-only. **Healthy with recommendations.** |
| `overnight_backtest` | 62 OK; `e0aefd30`, `99064932`, `4bf6a211`, `f497b8c6` (76s), `dc7be297`, `6ddb254e`. | Chunk checkpointing and idempotent zero-work runs coherent. **Healthy.** |
| `overnight_generate` | Four runs, all inspected: `bd1aad9d`, `f0638384`, `000c440a`, `17b76f2c` (539–879s). | Deep-tier structured paper candidates; only four records available. **Healthy with recommendations.** |
| `overnight_sweep` | Six OK, all sampled, 475–646s; latest `e7cde925`. | OHLCV capability filter and candidate updates coherent. **Healthy with recommendations.** |
| `paper_validation_report` | Seven labeled OK, 8,681–10,128s; `5124e1f1`, `49a4fce7`, `595daba1`, `edbf8d37`, `a7208b29`, `c8bca9ae`. | Artificial jitter and hidden item errors made OK meaningless and exceeded intended timeout. **Outright failure; fixed.** |
| `portfolio_allocator` | 116 OK; `4862d731`, `582b3894`, `bff14856`, `aa3f1231`, `51df96ec`, `630ed10f`. | Queue empty; zero selected/executed correct. Paper balance fallback visible; live trading off. **Healthy with recommendations.** |
| `position_review` | Eight labeled OK; five stratified samples through `7408cf5b`, all ~0.1s. | Pre-fix function never queried positions. **Outright failure; fixed.** |
| `social_scan` | 463 labeled OK; `d921dce9`, `4acf574a`, `9ffaac57` (455s), `53817e62`, `f3f8f15a`, `5c806052`. | StockTwits intermittently 503; pre-fix Kalshi identifiers were submitted. Empty data correctly skips social LLM. **Degraded but usable; wrong-market fixed.** |
| `strategy_resweep` | Seven OK, all sampled, 78–146s; latest `6695d4f0`. | Paginates active strategies and skips unsupported OHLCV markets. **Healthy.** |
| `strategy_tournament` | Two runs, both inspected: `091a3a92` 14s; `335949b0` 3,571s. | Pre-fix cap omitted 63 strategies and mixed event markets into OHLCV/rules. **Buggy; fixed.** |
| `universe_refresh` | Two runs, both inspected: `dc6995a1` OK 42s; `22e7e216` DNS error 8s. | Correct source, external DNS/provider dependent; failed path exposed credential before containment. **External failure recovered; leak fixed.** |

### Scheduled pipeline and model evidence

- Current OpenCode deployment completed ARE `07b35b8f-6942-4b38-b0ec-83e5baa04a75` (497.7s), APTV `1254d0d0-2037-4dd1-8c67-8eef5b145365` (431.1s), and ROK `ef9f0214-4e15-4f00-8606-c64f6b354597` (471.4s). Additional current runs are being collected for the five-sample post-deploy set.
- Market/fundamentals/news roles used OpenCode `openai/gpt-5.6-luna`; debate, judge, trader, and risk roles used `openai/gpt-5.6-sol`. Social was intentionally skipped with no model/tokens when its source returned no usable data. No sampled call required terra fallback.
- Stored prompt text and structured envelopes were present. Judge prompts remained below the 96 KiB preflight budget: ARE 67,466 characters/14,853 tokens; APTV 62,597/13,976; ROK 65,068/14,441. Prompt compaction tests cover overflow and report dropped/truncated context.
- All three final signals were HOLD and created no orders. ARE's snapshot closed at $50.099998 on 2026-08-05 13:30 UTC. Early roles used it correctly, but risk roles later misread $46.69 support as current price; `f190265` supplies the missing market report.
- Pre-fix deep roles stored zero latency while quick roles stored 21–41s values. `beeac89` preserves completion metadata; post-deploy evidence is required.
- Seven-day pipeline history was 217 completed/2,170 failed stock and 291 completed/192 failed Kalshi runs. Most stock failures were obsolete Ollama connection errors before `0f7fdf3`; current OpenCode examples complete, but historical success alone is not treated as current proof.

## Commits

- `c119d6d` — redact provider credentials/query strings; focused data client tests passed.
- `757690b` — isolate stock refresh/social context by market; focused automation tests passed.
- `4a53868` — reject disabled manual runs and recheck at dispatch; orchestrator tests passed.
- `ce1dca4` — repair settlement previews/gate, enforce timeouts, implement position review, make report/refresh outcomes honest, paginate/filter scanners, rotate filings, and canonicalize triggers; full automation suite passed.
- `beeac89` — preserve LLM model/usage/latency/cost through deep-agent paths; full agent suite passed.
- `f190265` — ground risk debate/judge prompts in market context; full agent suite passed.
- `59c98d9` — align the API contract test with preview scheduling while retaining the independent mutation gate.

## Release gate, synchronization, deployment, and rollback

Pre-deployment gate passed on 2026-08-05. The first full run correctly stopped on one stale API test that still treated the settlement scheduler switch as mutation authorization; `59c98d9` updated that contract. The complete rerun then passed:

- `go test ./...` and `go vet ./...`.
- `golangci-lint run ./...` with zero issues.
- Frontend: 10 files/162 tests, ESLint, TypeScript project build, and Vite production build.
- Default and production NUC Compose rendering.
- Prometheus `alerts.yml`: seven valid rules.
- Migration package tests and schema-version review; no migration is introduced by this change set and production remains at version 60.
- `git diff --check` and a focused `origin/main...HEAD` credential/private-key pattern scan. No secret-like additions were found.

Previous images recorded for rollback: app `augr-app:0f7fdf3a0801`; web `augr-web:review-735b344bf7ab`. Synchronization, deployment, and post-deploy results remain pending.

## Deferred items / external blockers

- Provider credential rotation requires access to the provider account and a coordinated secret replacement; owner and exact action will be recorded after code/data containment is complete.
- US-equity market-hours reruns after code remediation require the next valid NYSE session. Any timer created will be recorded here.
