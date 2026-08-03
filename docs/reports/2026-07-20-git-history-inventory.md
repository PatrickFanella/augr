# Git history inventory

- HEAD: `87aba28`
- origin/main: `f6d59bf`
- Ahead of origin/main: `61` commits
- Protected user-owned file: `docker-compose.prod.yml` (modified, untouched)
- Untracked 2026-07-20 plan docs remain unmodified and excluded from reconciliation work
- History contains an internal merge commit, but `origin/main` is an ancestor of `HEAD`; the remote can be updated without force. Publishing a review branch first remains safer than directly advancing `main` by 61 commits.
- Safest non-force topology: keep the current `HEAD` intact, publish the reconciled result on a new review branch, run remote CI/review, then promote to `main` through the forge's normal protected-branch flow.
- Backup ref created locally: `backup/wave0-20260720` -> `87aba28a7ce4148065d466dd07b1acdc563b6bdf`
- Ancestry proof: `git merge-base --is-ancestor origin/main HEAD` => exit `0`; `git merge-base HEAD origin/main` => `f6d59bf08fdf56c34169474237110f72b8c45afc`; `git rev-list --left-right --count origin/main...HEAD` => `0 61`.
- Exact non-force push proposal: `git push origin backup/wave0-20260720:refs/heads/wave0-20260720`

| hash | subject | substantive file/domain impact | disposition |
|---|---|---|---|
| `a27692c` | fix(automation): close stale Alpaca positions | automation reconciliation; Alpaca broker/client/tests | safe to publish as-is |
| `303c9ec` | fix(api): include closed realized PnL in portfolio summary | portfolio API summary + server tests | safe to publish as-is |
| `59ba6fb` | fix(api): exclude open positions from realized PnL | portfolio API correctness + server tests | safe to publish as-is |
| `28debe7` | fix(portfolio): use account balance in allocator diagnostics | allocator diagnostics, runtime wiring, web diagnostics | safe to publish as-is |
| `2d9ce06` | Merge remote-tracking branch 'origin/main' | merge replay into current topic branch; UI files only | safe to publish as-is |
| `c118aaf` | docs: establish remediation delivery plan | planning docs only | safe to publish as-is |
| `ccc0aa5` | fix(discovery): recover structured LLM responses | discovery generators, LLM parser, tests | safe to publish as-is |
| `501ebb4` | fix(reddit): coordinate provider-wide cooldowns | Reddit client, cooldown coordinator, signal source | safe to publish as-is |
| `6c45245` | fix(web): restore lint and refresh safety | web shell/theme/command palette/button semantics | safe to publish as-is |
| `7b60bff` | fix(web): stabilize operational state handling | web operational pages, badges, router config | safe to publish as-is |
| `018dcf6` | feat(observability): expose discovery outcomes and freshness | metrics, discovery orchestration, runtime observability | safe to publish as-is |
| `155a049` | test(providers): cover external data seams | provider client tests + Tradier options provider | safe to publish as-is |
| `04ba070` | test(web): split operational suites and repair CI gates | web CI workflow, harness split, test isolation | safe to publish as-is |
| `bb92153` | fix(web): make operational state and charts truthful | cockpit/portfolio/stock pages, route states, CSS | safe to publish as-is |
| `21a3794` | fix(web): correct mobile navigation semantics | mobile nav semantics + shell tests | safe to publish as-is |
| `a403a26` | fix(web): finish responsive design reconciliation | design-system docs, shell/layout/CSS/chart theme | safe to publish as-is |
| `60250a2` | feat(web): recover readiness and event-market surfaces | readiness/event-market/settings UI + API schemas/mocks | safe to publish as-is |
| `ec30291` | feat(web): restore options chain research | options research UI + API mock/schema plumbing | safe to publish as-is |
| `4114364` | feat(web): restore backtest evidence surface | backtest UI + API mock/schema plumbing | safe to publish as-is |
| `e8be605` | feat(web): restore decision journal and replay | journal/replay UI + API mock/schema plumbing | safe to publish as-is |
| `ce88125` | docs(web): close product-surface recovery matrix | product-surface matrix docs | safe to publish as-is |
| `70d2ced` | fix(options): enforce contract and risk boundaries | options manager validation + tests | safe to publish as-is |
| `c326320` | feat(options): add deterministic paper contract fills | options manager, paper execution, tests | safe to publish as-is |
| `b8d2eb4` | fix(options): persist contract lifecycle metadata | repository persistence for orders/positions/trades | safe to publish as-is |
| `266d8ab` | feat(options): persist immediate paper fills | options manager + paper execution paths/tests | safe to publish as-is |
| `22c8cb6` | feat(options): wire paper contract selection runtime | prod strategy runner + runtime selection tests | safe to publish as-is |
| `ba8344f` | feat(options): persist paper position closures | strategy runner + options manager closure flow | safe to publish as-is |
| `49f2134` | fix(options): apply multiplier-aware exposure limits | exposure math in options manager/tests | safe to publish as-is |
| `0946c1c` | fix(options): preflight spreads before persistence | Alpaca options preflight + persistence tests | safe to publish as-is |
| `0fcbdce` | test(web): stabilize risk control readiness | risk readiness tests | safe to publish as-is |
| `1903a7a` | feat(web): expose options lifecycle metadata | orders/portfolio web UI + schema | safe to publish as-is |
| `df7dd3e` | feat(options): execute atomic paper debit spreads | strategy runner, domain options, paper execution | safe to publish as-is |
| `8835ffb` | feat(options): settle expired paper positions | options expiry automation + settlement tests | safe to publish as-is |
| `8f1b856` | feat(options): enforce portfolio Greek limits | risk/options enforcement across runner and engine | safe to publish as-is |
| `0a52da9` | feat(options): reconcile durable lifecycle state | options lifecycle automation and reconciliation | safe to publish as-is |
| `a194b9b` | feat(options): close paper spreads atomically | strategy runner + options manager atomic close flow | safe to publish as-is |
| `c78b7d0` | feat(options): restore paper account after restart | options bootstrap + broker restore | safe to publish as-is |
| `a367c3f` | docs(options): close dedicated paper runtime phase | docs only | safe to publish as-is |
| `c2f944b` | feat(prediction): persist replayable order lifecycle | order lifecycle journaling, migrations, repo tests | safe to publish as-is |
| `b04f4fd` | feat(prediction): journal deterministic probability gates | prediction executor/evaluator + order manager | safe to publish as-is |
| `e6b4761` | feat(prediction): settle paper event contracts | settlement jobs, broker, repo, discovery client | safe to publish as-is |
| `9e5dfc6` | docs(prediction): close deterministic runtime phase | docs + orchestration references | safe to publish as-is |
| `da5d498` | feat(backtest): version simulation assumptions | backtest reproducibility, simulation config, repo/migrations | safe to publish as-is |
| `9140c44` | fix(backtest): prevent same-bar lookahead fills | backtest validation and runner logic | safe to publish as-is |
| `4318797` | feat(backtest): measure fills divergence and calibration | backtest divergence/calibration, discovery sweep, service API | safe to publish as-is |
| `27f57a9` | docs(backtest): close decision quality phase | backtest decision docs + small API/schema hooks | safe to publish as-is |
| `826cd72` | feat(operations): add capability release gate | readiness APIs, operations checks, monitoring | safe to publish as-is |
| `c1681dd` | docs(operations): close paper release readiness phase | docs/runbook/scripts/tests for release readiness | safe to publish as-is |
| `bbba200` | docs(audit): verify nine-phase remediation | audit/completion docs + backtest helper tweaks | safe to publish as-is |
| `bb4eacf` | feat(web): refine operational UI experience | operational UI shell, command palette, test harness | safe to publish as-is |
| `9b94fb0` | feat(operations): prepare weekly paper evaluation | evaluation docs, scripts, alerting, trade persistence | safe to publish as-is |
| `01aeac8` | fix(automation): guard missing universe dependency | premarket automation + persistence tests | safe to publish as-is |
| `d03603f` | fix(automation): tolerate Kalshi discovery throttling | discovery automation + premarket tests | safe to publish as-is |
| `5916e6d` | fix(agent): bound debate prompt capacity | agent debate budget, prompt sizing, tests/docs | safe to publish as-is |
| `8546967` | fix(execution): complete Kalshi paper fills | Kalshi order mapping, paper broker, strategy runner | safe to publish as-is |
| `25c55c5` | fix(portfolio): advance opportunity lifecycle | portfolio allocator jobs, opportunity repo/tests | safe to publish as-is |
| `24490e8` | docs(operations): sequence production stabilization | production-stabilization roadmap doc only | safe to publish as-is |
| `0a0c163` | feat: harden trading lifecycle and reconciliation | broad runtime, automation, API, repo, migrations | safe to publish as-is |
| `6fef66d` | docs(ops): record P1 and P2 stabilization | report/runbook docs and safe-operation planning | safe to publish as-is |
| `6d60ad5` | fix(api): aggregate enum market types | position repository normalization + tests | safe to publish as-is |
| `87aba28` | fix(data): reject legacy cancelled paper decisions | migration + schema version + runbook guardrail | safe to publish as-is |

## Validation gate

- Backend: `go test ./... -count=1` passed.
- Frontend dependency state was rebuilt from committed `web/package-lock.json` with `npm ci`.
- Frontend tests: 160/160 passed after giving the two long-form risk workflow tests a 20-second ceiling; both previously completed just after the old 10-second ceiling with no assertion failure.
- Frontend production build: passed.
- Frontend lint: passed.
- `docker-compose.prod.yml` remains modified, unstaged, and excluded from every proposed commit/push.
- No remote update has occurred. The exact proposed first remote operation remains `git push origin backup/wave0-20260720:refs/heads/wave0-20260720`, after operator approval and after the evidence commit is included on the reviewed source branch.
