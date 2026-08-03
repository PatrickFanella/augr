# Production Health Remediation Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Make Augr's production health signals trustworthy, classify recoverable provider failures correctly, and reduce strategy-pipeline latency without changing trading behavior blindly.

**Architecture:** Deliver four independently releasable tracks in strict order: health/metrics correctness, provider-failure classification, measured pipeline optimization, and production verification. Prometheus remains private to the monitoring network; the public edge exposes only explicit liveness/readiness routes. Performance changes are gated by persisted phase timings and a paper-only canary.

**Tech Stack:** Go 1.24, Prometheus client_golang, chi, PostgreSQL/TimescaleDB, Docker Compose, Nginx, React/Vite, Grafana, shell verification scripts.

Execute this plan in a dedicated clean Git worktree because the primary checkout may contain unrelated in-progress changes. The immutable release checks intentionally reject a dirty worktree.

---

## Scope and release gates

This work addresses the observed production warnings:

- public `/health` and `/metrics` falling through to the SPA;
- missing Go/process and Polymarket metrics;
- misleading `tradingagent_portfolio_value 0` with open positions;
- mismatched pipeline count/duration coverage, including native market paths;
- Kalshi discovery failures hidden as successful automation runs;
- constant rather than exponential Kalshi retry backoff;
- a signal-failure metric that conflates transport, empty-output, and JSON failures;
- 283–746 second strategy pipelines with no production-ready phase breakdown.

Release gates are mandatory:

1. **Gate A — Trustworthy control plane:** health contracts, scrape output, dashboards, and runbooks agree. Do not alter retries or pipeline behavior before this gate passes.
2. **Gate B — Provider classification:** Kalshi rate limits produce a typed deferred outcome and a bounded deferred metric. Ship this classification before changing retry timing. Signal-evaluation reason metrics ship independently in Task 6.
3. **Gate C — Retry canary:** exponential retry behavior runs through one paper-data workload for 24 hours without request amplification or increased job failures.
4. **Gate D — Latency canary:** one-round debate runs only on selected paper strategies until latency and output-quality criteria pass.

## File map

### Health and edge contract

- Modify `internal/api/server.go` — split liveness from dependency readiness.
- Modify `internal/api/server_test.go` — assert both contracts and degraded readiness.
- Modify `deploy/nginx.augr.conf` — explicit health proxies and private metrics behavior.
- Modify `scripts/verify-prod-build.sh` — verify liveness, readiness, and scrape content separately.
- Modify `docker-compose.nuc.yml` — assign immutable, gate-specific app/web image tags.
- Modify `Dockerfile` and `docker-compose.nuc.yml` only if their probes do not target liveness after the split.

### Metrics correctness

- Modify `internal/metrics/metrics.go` — register runtime collectors, add bounded failure/phase metrics, and remove the unbacked aggregate portfolio gauge.
- Modify `internal/metrics/metrics_test.go` — endpoint-level collector and label-contract tests.
- Modify `internal/observability/surfers_metrics.go` and tests — register against the served registry and preserve complete labels.
- Modify `cmd/tradingagent/runtime.go` and tests — construct one registry per runtime.
- Modify `cmd/tradingagent/prod_strategy_runner.go` and tests — align terminal-run metrics across generic, Kalshi, and Polymarket paths.
- Modify `monitoring/grafana/dashboards/trading.json` and `pipeline-health.json` — remove the false portfolio panel and query terminal status explicitly.

### Provider and signal failures

- Modify `internal/automation/jobs_kalshi_discovery.go` and its existing tests in `internal/automation/jobs_premarket_test.go` — replace string matching with typed classification.
- Modify `internal/kalshidiscovery/orchestrator.go`, `internal/domain/kalshi_market.go`, and tests — persist `deferred` rate-limit runs rather than `failed` or falsely successful runs.
- Modify `internal/data/kalshi/client.go` and `client_test.go` — bounded exponential retry backoff.
- Modify `internal/signal/evaluator.go` and `evaluator_test.go` — reason-specific failure metrics and safe logging.
- Modify `monitoring/prometheus/alerts.yml` — actionable rate-limit and signal-failure alerts.

### Pipeline latency

- Modify `internal/agent/runner.go` and tests — return persisted phase timings on the in-memory run result.
- Modify `cmd/tradingagent/prod_strategy_runner.go` and tests — observe preparation and per-phase duration.
- Create `docs/reports/2026-07-27-production-health-baseline.md` — reproducible baseline and canary evidence.
- Create `docs/runbooks/provider-throttling.md` — Kalshi/Reddit operational response.
- Modify `docs/runbooks/rolling-restart.md`, `README.md`, and `docs/design/api-design.md` — canonical endpoint and metric contracts.

---

### Task 1: Capture the immutable production baseline

**Files:**
- Create: `docs/reports/2026-07-27-production-health-baseline.md`

- [ ] **Step 1: Record the deployment identity and endpoint matrix**

Run from a host that can reach both NUC ports:

```bash
date --utc '+observed_at=%Y-%m-%dT%H:%M:%SZ'
git rev-parse HEAD
docker compose -f docker-compose.nuc.yml ps
docker compose -f docker-compose.nuc.yml images
for url in \
  http://10.0.0.56:3030/healthz \
  http://10.0.0.56:3030/health \
  http://10.0.0.56:3029/healthz \
  http://10.0.0.56:3029/health \
  http://10.0.0.56:3029/metrics \
  https://augr.subcult.tv/healthz \
  https://augr.subcult.tv/health \
  https://augr.subcult.tv/metrics; do
  curl --silent --show-error --max-time 15 \
    --output /dev/null \
    --write-out "$url status=%{http_code} type=%{content_type} bytes=%{size_download} total=%{time_total}s\n" \
    "$url"
done
```

Expected baseline: direct app health and metrics return API content; web `/health` and `/metrics` currently return HTML; the public behavior is recorded exactly rather than inferred from committed Nginx configuration.

- [ ] **Step 2: Record current process-lifetime counters and phase timings**

```bash
curl -fsS http://10.0.0.56:3030/metrics \
  | rg '^tradingagent_(pipeline_runs_total|pipeline_duration_seconds_(sum|count)|automation_job_errors_total|kalshi_rate_limit_total|kalshi_retry_attempts_total|signal_parse_failures_total|positions_open|portfolio_value|circuit_breaker_state|kill_switch_active)'

docker compose -f docker-compose.nuc.yml exec -T postgres \
  psql -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-tradingagent}" -c \
  "SELECT status, phase_timings, completed_at - started_at AS elapsed FROM pipeline_runs WHERE completed_at IS NOT NULL ORDER BY completed_at DESC LIMIT 30;"
```

Expected: the report contains the raw snapshot and states that Prometheus counters reset at process restart.

- [ ] **Step 3: Write the baseline report**

Use these fixed sections: `Deployment identity`, `Endpoint matrix`, `Metric snapshot`, `Recent phase timings`, `Known limitations`, and `Canary thresholds`. Set thresholds to:

```text
health/readiness availability: 100% during the verification window
strategy pipeline p95: <= 300 seconds after optimization
pipeline completion-rate regression: <= 2 percentage points
signal parse-failure regression: <= 1 percentage point
Kalshi physical attempts per logical request: never exceed configured max attempts
kill switch and circuit breaker: unchanged by this work
```

- [ ] **Step 4: Commit the baseline**

```bash
git add docs/reports/2026-07-27-production-health-baseline.md
git commit -m "docs: capture production health baseline"
```

---

### Task 2: Split liveness, readiness, and private metrics at the edge

**Files:**
- Modify: `internal/api/server.go:409-413,741-810`
- Modify: `internal/api/server_test.go:347-445,516-530`
- Modify: `deploy/nginx.augr.conf:8-45`
- Modify: `scripts/verify-prod-build.sh:25-39`
- Modify: `docker-compose.nuc.yml:61-109`
- Modify: `docker-compose.nuc.yml`

- [ ] **Step 1: Write failing router tests for the new contract**

Add tests asserting:

```go
func TestHealthzIsLivenessAndDoesNotCallDependencies(t *testing.T) {
    deps := testDeps()
    db := &stubHealthCheck{err: errors.New("db unavailable")}
    redis := &stubHealthCheck{err: errors.New("redis unavailable")}
    deps.DBHealth = db
    deps.RedisHealth = redis
    server := newTestServerWithDeps(t, deps)

    rec := httptest.NewRecorder()
    server.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

    if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` {
        t.Fatalf("liveness response = %d %s", rec.Code, rec.Body.String())
    }
    if db.calls.Load() != 0 || redis.calls.Load() != 0 {
        t.Fatalf("liveness called dependencies: db=%d redis=%d", db.calls.Load(), redis.calls.Load())
    }
}

func TestHealthIsReadinessAndReturns503WhenDependencyFails(t *testing.T) {
    deps := testDeps()
    deps.DBHealth = &stubHealthCheck{err: errors.New("db unavailable")}
    deps.RedisHealth = &stubHealthCheck{}
    server := newTestServerWithDeps(t, deps)

    for _, path := range []string{"/health", "/api/v1/health"} {
        rec := httptest.NewRecorder()
        server.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
        if rec.Code != http.StatusServiceUnavailable {
            t.Fatalf("%s status = %d, want 503", path, rec.Code)
        }
        var body healthStatusResponse
        if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
            t.Fatalf("decode %s response: %v", path, err)
        }
        if body.Status != "degraded" || body.DB != "error" || body.Redis != "ok" {
            t.Fatalf("%s body = %+v", path, body)
        }
    }
}
```

- [ ] **Step 2: Run the focused test and verify failure**

```bash
go test ./internal/api -run 'TestHealthzIsLiveness|TestHealthIsReadiness' -count=1
```

Expected: FAIL because `/healthz` still invokes dependency checks.

- [ ] **Step 3: Implement the split handlers**

Register and implement:

```go
r.Get("/healthz", s.handleLiveness)
r.Get("/health", s.handleReadiness)
r.Get("/api/v1/health", s.handleReadiness)
r.Get("/metrics", s.handleMetrics)

type livenessResponse struct {
    Status string `json:"status"`
}

func (s *Server) handleLiveness(w http.ResponseWriter, _ *http.Request) {
    respondJSON(w, http.StatusOK, livenessResponse{Status: "ok"})
}

```

Rename the existing `handleHealth` method to `handleReadiness` without changing its dependency checks, two-second shared timeout, response fields, logging level, or HTTP 503 behavior.

Update `TestHealthEndpointUsesSharedTimeout` and `TestHealthEndpointLogsFailuresAtInfo` to probe `/health`; `/healthz` must never invoke those dependency paths. Exempt `/healthz`, `/health`, and `/api/v1/health` from `RateLimiter.Middleware` so operational probes cannot receive HTTP 429.

Update `TestHealthEndpoint`, `TestHealthEndpointDBDown`, and `TestHealthEndpointRedisDown` to test readiness through `/health`; retain a separate liveness test for `/healthz`. Replace the standalone `cmd/tradingagent/main.go` `all-ok` handler and its smoke-test expectations with the same `{"status":"ok"}` liveness contract.

- [ ] **Step 4: Make Nginx routing explicit and keep metrics private**

Replace the synthetic/fallthrough behavior with:

```nginx
location = /healthz {
    access_log off;
    proxy_pass http://app:8080/healthz;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
}

location = /health {
    access_log off;
    proxy_pass http://app:8080/health;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
}

location = /metrics {
    return 404;
}
```

Prometheus continues scraping `app:8080` directly through `monitoring/prometheus/prometheus.yml`; do not proxy `/metrics` through the public web container.

- [ ] **Step 5: Update production verification**

Make `scripts/verify-prod-build.sh` assert:

```bash
docker compose -f docker-compose.prod.yml exec -T app \
  wget -qO- http://127.0.0.1:8080/healthz | jq -e '.status == "ok" and length == 1'
docker compose -f docker-compose.prod.yml exec -T app \
  wget -qO- http://127.0.0.1:8080/health | jq -e '.status == "ok" and .db == "ok" and .redis == "ok"'
docker compose -f docker-compose.prod.yml exec -T app \
  wget -qO- http://127.0.0.1:8080/metrics | rg -q '^tradingagent_scheduler_tick_total'
```

- [ ] **Step 6: Verify and commit**

```bash
go test ./internal/api ./cmd/tradingagent -run 'Health|Metrics' -count=1
docker compose -f docker-compose.nuc.yml build web
docker compose -f docker-compose.nuc.yml run --rm --no-deps web nginx -t
git add internal/api/server.go internal/api/server_test.go cmd/tradingagent/main.go cmd/tradingagent/main_test.go cmd/tradingagent/smoke_test.go deploy/nginx.augr.conf scripts/verify-prod-build.sh docker-compose.nuc.yml
git commit -m "fix: separate liveness from readiness"
```

Add immutable image names to Compose before committing:

```yaml
app:
  image: ${AUGR_APP_IMAGE:-augr-app:local}
  build:
    context: .
    dockerfile: Dockerfile
    target: production

web:
  image: ${AUGR_WEB_IMAGE:-augr-web:local}
  build:
    context: .
    dockerfile: Dockerfile.web
    target: production
```

At every later release gate, require a clean worktree, derive `SHORT_SHA=$(git rev-parse --short=12 HEAD)`, run the complete pre-deploy checks, build gate-specific `AUGR_APP_IMAGE`/`AUGR_WEB_IMAGE` tags, deploy with `--no-build`, and record `docker image inspect --format '{{.Id}}'` for both tags in the baseline report. Never build a gate from a checkout containing later uncommitted tasks.

Use this exact template, setting `GATE` to `gate-a`, `gate-b`, `gate-c`, or `gate-d`:

```bash
test -z "$(git status --porcelain)"
export GATE=gate-a
export SHORT_SHA="$(git rev-parse --short=12 HEAD)"
export AUGR_APP_IMAGE="augr-app:${GATE}-${SHORT_SHA}"
export AUGR_WEB_IMAGE="augr-web:${GATE}-${SHORT_SHA}"
go test ./... -count=1
go vet ./...
npm --prefix web test -- --run
npm --prefix web run build
docker compose -f docker-compose.nuc.yml config >/dev/null
docker compose -f docker-compose.nuc.yml build app web
docker compose -f docker-compose.nuc.yml up -d --no-build app web
docker image inspect --format '{{.Id}}' "${AUGR_APP_IMAGE}" "${AUGR_WEB_IMAGE}"
```

Immediately after each gate window, append its SHA, image IDs, timestamps, queries, results, and decision to the baseline report and commit that evidence:

```bash
git add docs/reports/2026-07-27-production-health-baseline.md
git commit -m "docs: record ${GATE} release evidence"
```

The next gate's clean-worktree check runs only after this evidence commit.

---

### Task 3: Serve one complete Prometheus registry

**Files:**
- Modify: `internal/metrics/metrics.go:54-63,261-303,544-548`
- Modify: `internal/metrics/metrics_test.go:159-253`
- Modify: `cmd/tradingagent/runtime.go:149-155,263-265`
- Modify: `cmd/tradingagent/runtime_test.go:1439-1471`
- Modify: `internal/observability/surfers_metrics.go`
- Modify: `internal/observability/surfers_metrics_test.go`

- [ ] **Step 1: Write failing scrape-contract tests**

Extend `TestHandler` so the served body must include:

```go
expected := []string{
    "go_goroutines",
    "process_start_time_seconds",
    "tradingagent_scheduler_tick_total",
    "polymarket_recorder_lag_seconds",
}
```

Construct `observability.NewSurfersMetrics(m.Registerer())` before invoking `m.Handler()` in the scrape-contract test so the private registry contains both metric families.

Add a runtime test that constructs the API runtime twice and confirms each private registry is isolated without duplicate-registration panic.

- [ ] **Step 2: Verify the tests fail**

```bash
go test ./internal/metrics ./internal/observability ./cmd/tradingagent -run 'TestHandler|Metrics' -count=1
```

Expected: FAIL because Go/process and Surfers collectors are not gathered by `Metrics.Handler()`.

- [ ] **Step 3: Register standard collectors and expose the private registerer**

In `metrics.New`:

```go
reg := prometheus.NewRegistry()
reg.MustRegister(
    prometheus.NewGoCollector(),
    prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
)
```

Add:

```go
func (m *Metrics) Registerer() prometheus.Registerer {
    return m.registry
}
```

- [ ] **Step 4: Register Surfers metrics against the served registry**

In `newAPIServer`, replace the process-global registration with:

```go
appMetrics := metrics.New()
surfersMetrics := observability.NewSurfersMetrics(appMetrics.Registerer())
```

Remove `surfersMetricsOnce`, `surfersMetricsInst`, and the now-unused `prometheus` and `sync` imports from `cmd/tradingagent/runtime.go`.

- [ ] **Step 5: Preserve complete dropped-tick labels**

Change the cleaner helper to accept the slug:

```go
func (c *Cleaner) incDrop(slug, reason string) {
    if c.metrics != nil {
        c.metrics.IncCounter("polymarket_ws_ticks_dropped_total", map[string]string{
            "slug": slug,
            "reason": reason,
        })
    }
}
```

Update each caller to pass the current tick's slug. Add a test rejecting an empty `slug` label. Do not add strategy UUIDs, request IDs, error messages, or market titles as metric labels.

Do not reuse websocket labels for recorder backpressure. Add a dedicated collector:

```go
recorderDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
    Name: "polymarket_recorder_dropped_total",
    Help: "Recorder records dropped by kind and bounded reason.",
}, []string{"kind", "reason"}),

func (a recorderMetricsAdapter) IncDropped(kind string, n int) {
    a.m.recorderDropped.WithLabelValues(kind, "backpressure").Add(float64(n))
}
```

- [ ] **Step 6: Verify and commit**

```bash
go test ./internal/metrics ./internal/observability ./internal/marketdata/polymarket ./cmd/tradingagent -count=1
git add internal/metrics internal/observability internal/marketdata/polymarket cmd/tradingagent/runtime.go cmd/tradingagent/runtime_test.go
git commit -m "fix: serve a complete isolated metrics registry"
```

---

### Task 4: Correct portfolio and pipeline metric semantics

**Files:**
- Modify: `internal/metrics/metrics.go:14-52,65-74,214-222,261-300,305-311,480-485`
- Modify: `internal/metrics/metrics_test.go`
- Modify: `cmd/tradingagent/prod_strategy_runner.go:208-247,607-849,1736-1761`
- Modify: `cmd/tradingagent/runtime_test.go`
- Modify: `monitoring/grafana/dashboards/trading.json`
- Modify: `monitoring/grafana/dashboards/pipeline-health.json`
- Modify: `README.md`
- Modify: `docs/runbooks/rolling-restart.md`
- Modify: `docs/runbooks/database-backup-restore.md`
- Modify: `docs/getting-started.md`
- Modify: `docs/design/api-design.md`
- Modify: `docs/design/infrastructure/deployment-and-operations.md`
- Modify: `docs/frontend/api-implementation-review.md`

- [ ] **Step 1: Write failing metric-semantic tests**

Cover these contracts:

```go
func TestPipelineDurationHasTerminalStatus(t *testing.T) {
    m := metrics.New()
    m.ObservePipelineDuration("completed", 12)
    m.ObservePipelineDuration("failed", 7)
    // Gather and assert distinct status="completed" and status="failed" series.
}

func TestNativeRunsRecordTerminalMetrics(t *testing.T) {
    // Execute one Kalshi and one Polymarket native result through RunStrategy.
    // Assert each increments tradingagent_pipeline_runs_total and observes one
    // duration with the matching terminal status.
}
```

- [ ] **Step 2: Remove the misleading aggregate portfolio gauge**

Delete `PortfolioValue`, its registration, `SetPortfolioValue`, and its unit-test assertions. Remove the Grafana panel querying `tradingagent_portfolio_value`.

Do not replace it with a sum. The app has no canonical cross-broker aggregate and summing paper, Alpaca, Kalshi, and Polymarket could double-count capital. A future broker-specific metric must use an explicit contract such as `tradingagent_account_equity{broker="alpaca",mode="paper"}` and update only after a successful account fetch.

- [ ] **Step 3: Give pipeline duration an explicit terminal-status label**

Change the collector and helper to:

```go
PipelineDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
    Name:    "tradingagent_pipeline_duration_seconds",
    Help:    "Terminal pipeline duration in seconds by status.",
    Buckets: []float64{30, 60, 120, 180, 300, 480, 600, 900, 1200},
}, []string{"status"}),

func (m *Metrics) ObservePipelineDuration(status string, seconds float64) {
    m.PipelineDuration.WithLabelValues(status).Observe(seconds)
}
```

Update `recordPipelineMetrics` to call `ObservePipelineDuration(run.Status.String(), ...)`. Keep the run counter's existing labels in this release to avoid combining cardinality migration with the semantic repair.

- [ ] **Step 4: Record native terminal runs exactly once**

At the end of both `runKalshiNative` and `runPolymarketNative`, after the persisted run has a terminal status and `CompletedAt`, call:

```go
r.recordPipelineMetrics(result.Run)
r.refreshExecutionMetrics(context.Background())
```

Use one helper path for success and failure so early returns cannot double-count. Tests must cover completed and failed native runs.

Before recording the generic runner result, copy its canonical terminal signal onto the run:

```go
result.Run.Signal = result.Signal
r.recordPipelineMetrics(result.Run)
```

Add buy and sell test cases; do not accept normalization to `hold` when `RunResult.Signal` is populated.

- [ ] **Step 5: Align dashboards**

Use:

```promql
histogram_quantile(
  0.95,
  sum by (le) (rate(tradingagent_pipeline_duration_seconds_bucket{status="completed"}[1h]))
)
```

Keep failed-run latency in a separate panel using `{status="failed"}`. Ensure dashboard panels do not compare completed counters against all terminal durations.

- [ ] **Step 6: Verify and commit**

```bash
go test ./internal/metrics ./cmd/tradingagent -run 'Pipeline|Metrics|Native' -count=1
jq empty monitoring/grafana/dashboards/trading.json monitoring/grafana/dashboards/pipeline-health.json
git add internal/metrics cmd/tradingagent monitoring/grafana/dashboards
git commit -m "fix: align portfolio and pipeline metrics"
```

- [ ] **Step 7: Commit the canonical health and metric documentation before Gate A**

Document `/healthz` as dependency-free liveness, `/health` and `/api/v1/health` as readiness, and `/metrics` as private. Remove every `all-ok` claim, the aggregate portfolio-value contract, and the old unlabeled pipeline-duration query from the listed README, runbooks, and design/review documents.

```bash
git add README.md docs/runbooks/rolling-restart.md docs/runbooks/database-backup-restore.md docs/getting-started.md docs/design/api-design.md docs/design/infrastructure/deployment-and-operations.md docs/frontend/api-implementation-review.md
git commit -m "docs: align production health and metric contracts"
```

**Stop gate:** run the immutable release template with `GATE=gate-a`, provision/reload the external Prometheus/Grafana files as described in Task 8, and pass Gate A before starting Task 5.

---

### Task 5: Classify Kalshi throttling and add bounded exponential backoff

**Files:**
- Modify: `internal/domain/kalshi_market.go:8-15`
- Modify: `internal/kalshidiscovery/orchestrator.go:102-141`
- Modify: `internal/kalshidiscovery/orchestrator_test.go`
- Modify: `internal/automation/jobs_kalshi_discovery.go:45-76`
- Modify: `internal/automation/jobs_premarket_test.go`
- Modify: `internal/automation/orchestrator.go`
- Modify: `internal/automation/orchestrator_test.go`
- Modify: `internal/data/kalshi/client.go:283-380`
- Modify: `internal/data/kalshi/client_test.go`
- Modify: `internal/metrics/metrics.go`
- Modify: `internal/metrics/metrics_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/validate.go`
- Modify: `.env.example`
- Modify: `docker-compose.nuc.yml`

- [ ] **Step 1: Write failing typed-classification tests**

Test a wrapped `*providergovernor.RateLimitError` rather than matching text:

```go
func TestIsKalshiRateLimitUsesTypedError(t *testing.T) {
    typed := providergovernor.Wrap("kalshi", "data", http.MethodGet, 429, time.Minute, "limited")
    if !isKalshiRateLimit(fmt.Errorf("fetch markets: %w", typed)) {
        t.Fatal("wrapped typed rate limit was not recognized")
    }
    if isKalshiRateLimit(errors.New("status=429 in unrelated text")) {
        t.Fatal("plain text must not be classified as a rate limit")
    }
}
```

Add an orchestrator test asserting a typed throttle persists status `deferred`, not `failed`, while preserving the partial counters.

Add generic automation tests asserting deferred jobs are persisted as `deferred`, do not increment `ErrorCount` or `automation_job_errors_total`, do not reset or increment `ConsecutiveFailures`, and are not logged/persisted as successful.

Add and register this collector in `internal/metrics/metrics.go`, add its helper to the automation metrics contract, and increment it exactly once for the bounded reason `provider_rate_limit`; this supplies the Gate B control and Gate C comparison windows:

```go
AutomationJobDeferredTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
    Name: "tradingagent_automation_job_deferred_total",
    Help: "Automation jobs deferred by bounded reason.",
}, []string{"job_name", "reason"}),

func (m *Metrics) RecordAutomationJobDeferred(jobName, reason string) {
    m.AutomationJobDeferredTotal.WithLabelValues(jobName, reason).Inc()
}
```

- [ ] **Step 2: Add the deferred discovery state**

```go
const (
    KalshiDiscoveryStatusRunning   = "running"
    KalshiDiscoveryStatusCompleted = "completed"
    KalshiDiscoveryStatusDeferred  = "deferred"
    KalshiDiscoveryStatusFailed    = "failed"
)
```

In the deferred finalizer, use `errors.As` to set `run.Status = domain.KalshiDiscoveryStatusDeferred` and add `deferred_reason: "provider_rate_limit"` to the run summary. Increment `res.FetchedAll` as each page's candidates are appended so a later-page throttle preserves partial progress.

Introduce a typed generic automation outcome:

```go
type DeferredError struct {
    Reason string
    Err    error
}

func (e *DeferredError) Error() string { return e.Reason + ": " + e.Err.Error() }
func (e *DeferredError) Unwrap() error { return e.Err }
```

Return `&DeferredError{Reason: "provider_rate_limit", Err: err}` from the Kalshi job boundary. Update both scheduled and manual execution paths plus `persistRun` so this error produces status `deferred`, does not count as success or hard failure, and remains inspectable through `errors.As`.

- [ ] **Step 3: Replace string matching**

```go
func isKalshiRateLimit(err error) bool {
    var rateLimitErr *providergovernor.RateLimitError
    return errors.As(err, &rateLimitErr)
}
```

Remove the `strings` import. Preserve the orchestrator's auto-disable behavior for genuine hard failures.

- [ ] **Step 4: Verify and commit classification without retry changes**

```bash
go test ./internal/automation ./internal/kalshidiscovery ./internal/providergovernor -count=1
git add internal/domain/kalshi_market.go internal/kalshidiscovery internal/automation internal/metrics
git commit -m "fix: persist provider throttles as deferred"
```

**Stop gate:** deploy this exact commit with `GATE=gate-b` and verify deferred jobs are neither successes nor hard failures before implementing the retry switch.

Keep `KALSHI_EXPONENTIAL_BACKOFF=false` for a 24-hour post-Gate-B control window and record these Prometheus query results with explicit start/end timestamps:

```promql
increase(tradingagent_automation_job_deferred_total{job_name="kalshi_discovery",reason="provider_rate_limit"}[24h])
increase(tradingagent_automation_job_errors_total{job_name="kalshi_discovery"}[24h])
increase(tradingagent_kalshi_rate_limit_total{client_type="data",method="GET"}[24h])
```

- [ ] **Step 5: Write failing exponential-backoff tests and a disabled-by-default switch**

Use jitter disabled and assert:

```go
tests := []struct {
    attempt int
    want    time.Duration
}{
    {attempt: 0, want: 100 * time.Millisecond},
    {attempt: 1, want: 200 * time.Millisecond},
    {attempt: 2, want: 400 * time.Millisecond},
    {attempt: 8, want: 2 * time.Second},
}
```

Also assert `Retry-After` wins when it is larger but within `maxBackoff`, and that context cancellation interrupts sleep.

Add `KALSHI_EXPONENTIAL_BACKOFF=false` to configuration. Wire the flag only to the public/data Kalshi client initially; the authenticated execution client retains existing behavior during the canary. Validate the value as a boolean, constrain `KALSHI_MAX_ATTEMPTS` to `1..5`, and expose the non-secret settings without exposing credentials. Add `KALSHI_EXPONENTIAL_BACKOFF: ${KALSHI_EXPONENTIAL_BACKOFF:-false}` to the app's Compose `environment` so the canary can override the env-file value without editing tracked files.

Add a per-logical-request attempts histogram so the retry gate is measurable:

```go
KalshiRequestAttempts: prometheus.NewHistogramVec(prometheus.HistogramOpts{
    Name:    "tradingagent_kalshi_request_attempts",
    Help:    "Physical HTTP attempts per logical Kalshi request.",
    Buckets: []float64{1, 2, 3, 4, 5},
}, []string{"client_type", "method", "outcome"}),
```

Observe it exactly once when `doWithRetry` returns, with bounded outcomes `success`, `rate_limited`, `server_error`, `client_error`, `transport_error`, or `cancelled`. Tests must assert one observation per logical request and an observed count no greater than configured `maxAttempts`.

- [ ] **Step 6: Implement overflow-safe bounded exponential backoff behind the switch**

```go
func (c *Client) nextBackoff(attempt int, retryAfter time.Duration) time.Duration {
    backoff := c.baseBackoff
    if backoff <= 0 {
        backoff = 100 * time.Millisecond
    }
    for i := 0; i < attempt; i++ {
        if c.maxBackoff > 0 && backoff >= c.maxBackoff/2 {
            backoff = c.maxBackoff
            break
        }
        backoff *= 2
    }
    if c.maxBackoff > 0 && backoff > c.maxBackoff {
        backoff = c.maxBackoff
    }
    if c.jitterRatio > 0 {
        backoff = c.jitter(backoff)
        if c.maxBackoff > 0 && backoff > c.maxBackoff {
            backoff = c.maxBackoff
        }
    }
    if retryAfter > backoff {
        backoff = retryAfter
    }
    return backoff
}
```

When `KALSHI_EXPONENTIAL_BACKOFF=false`, preserve the current constant-backoff result. Retain the existing immediate return when `Retry-After > maxBackoff`; never sleep past the configured retry budget.

- [ ] **Step 7: Verify and commit retry capability while it remains disabled**

```bash
go test ./internal/automation ./internal/kalshidiscovery ./internal/providergovernor -count=1
go test ./internal/data/kalshi -run 'Retry|RateLimit|Backoff|Cooldown' -count=1
```

Expected: unrelated strings remain hard failures; flag-off waits remain constant; flag-on waits are `100ms, 200ms, 400ms` capped at `2s`.

```bash
git add internal/data/kalshi internal/config internal/metrics .env.example cmd/tradingagent/runtime.go docker-compose.nuc.yml
git commit -m "feat: add gated exponential Kalshi backoff"
```

- [ ] **Step 8: Enable only the data-client canary after Gate B**

Set `KALSHI_EXPONENTIAL_BACKOFF=true` for one 24-hour paper-data deployment. Do not alter execution-client retries. Compare the `tradingagent_kalshi_request_attempts` histogram's `+Inf` count with the bucket at the configured maximum; they must be equal. Re-run the three explicit 24-hour Prometheus queries from the Gate B control window. Roll back the flag if any logical request exceeds `KALSHI_MAX_ATTEMPTS`, deferred count or hard job-error count exceeds the control window, or p95 wait exceeds `KALSHI_MAX_BACKOFF`.

Run the immutable release template with `GATE=gate-c` and `export KALSHI_EXPONENTIAL_BACKOFF=true` before the Compose commands. Roll back with the recorded Gate B image tags and `KALSHI_EXPONENTIAL_BACKOFF=false`.

---

### Task 6: Split signal-evaluation failure reasons

**Files:**
- Modify: `internal/metrics/metrics.go:23,112-115,271,348-350`
- Modify: `internal/metrics/metrics_test.go`
- Modify: `internal/signal/evaluator.go:125-180`
- Modify: `internal/signal/evaluator_test.go`
- Modify: `monitoring/prometheus/alerts.yml`

- [ ] **Step 1: Write failing reason-specific tests**

Create table-driven cases for `provider_error`, `empty_response`, and `invalid_json`; each case must increment exactly one labeled counter while preserving the existing low-urgency fallback behavior. Every non-empty evaluation must also increment `tradingagent_signal_evaluations_total{outcome="success|fallback"}` exactly once.

```go
func assertSignalFailure(t *testing.T, m *metrics.Metrics, reason string, want float64) {
    t.Helper()
    got := testutil.ToFloat64(m.SignalEvaluationFailuresTotal.WithLabelValues(reason))
    if got != want {
        t.Fatalf("reason %s = %v, want %v", reason, got, want)
    }
}
```

- [ ] **Step 2: Replace the conflated counter**

```go
SignalEvaluationFailuresTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
    Name: "tradingagent_signal_evaluation_failures_total",
    Help: "Signal evaluation failures by bounded reason.",
}, []string{"reason"}),

SignalEvaluationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
    Name: "tradingagent_signal_evaluations_total",
    Help: "Signal evaluations by terminal outcome.",
}, []string{"outcome"}),

func (m *Metrics) RecordSignalEvaluationFailure(reason string) {
    switch reason {
    case "provider_error", "empty_response", "invalid_json":
        m.SignalEvaluationFailuresTotal.WithLabelValues(reason).Inc()
    default:
        m.SignalEvaluationFailuresTotal.WithLabelValues("provider_error").Inc()
    }
}
```

Do not add raw source names, titles, prompt text, response text, or errors as labels.

- [ ] **Step 3: Record the correct reason and remove raw model output from logs**

Use `provider_error` at the completion error, `empty_response` for blank content, and `invalid_json` at `json.Unmarshal`. Increment outcome `fallback` on those paths and `success` only after a valid parsed response. Replace:

```go
slog.String("content", content)
```

with:

```go
slog.Int("response_length", len(content))
```

- [ ] **Step 4: Add a ratio alert**

```yaml
- alert: AugrSignalEvaluationFailureRateHigh
  expr: sum(rate(tradingagent_signal_evaluation_failures_total[15m])) / clamp_min(sum(rate(tradingagent_signal_evaluations_total[15m])), 0.001) > 0.05
  for: 15m
  labels: { severity: warning }
  annotations:
    summary: "Signal evaluation failure rate exceeds 5%"
    runbook: "docs/runbooks/provider-throttling.md"
```

- [ ] **Step 5: Verify and commit**

```bash
go test ./internal/metrics ./internal/signal -run 'Signal|Evaluator|Metric' -count=1
git add internal/metrics internal/signal monitoring/prometheus/alerts.yml
git commit -m "fix: classify signal evaluation failures"
```

Deploy this committed metric-only behavior with `GATE=gate-b-signal` through the immutable release template before using its alert as a later gate.

---

### Task 7: Instrument phase latency, then run a paper-only one-round canary

**Files:**
- Modify: `internal/metrics/metrics.go`
- Modify: `internal/metrics/metrics_test.go`
- Modify: `internal/agent/runner.go:365-464`
- Modify: `internal/agent/runner_test.go`
- Modify: `cmd/tradingagent/prod_strategy_runner.go:208-236,1736-1747`
- Modify: `cmd/tradingagent/runtime_test.go`

- [ ] **Step 1: Write failing phase/preparation metric tests**

Define bounded labels only:

```go
PipelinePreparationDuration *prometheus.HistogramVec // labels: market_type, outcome
PipelinePhaseDuration       *prometheus.HistogramVec // labels: phase, status
```

Tests must cover `analysis`, `research_debate`, `trading`, and `risk_debate`, plus `completed` and `failed`. Reject arbitrary phase values by normalizing them to `unknown` in the helper.

- [ ] **Step 2: Return phase timings on the in-memory run**

Immediately after marshaling timings in both success and failure paths:

```go
phaseTimingsJSON, _ := json.Marshal(phaseTimings)
run.PhaseTimings = phaseTimingsJSON
```

This makes the same values available to persistence, API results, and metric recording.

- [ ] **Step 3: Add multi-minute buckets and bounded helpers**

```go
var pipelineLatencyBuckets = []float64{5, 15, 30, 60, 120, 180, 300, 480, 600, 900, 1200}

PipelinePreparationDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
    Name: "tradingagent_pipeline_preparation_duration_seconds",
    Help: "Strategy data preparation duration by market type and outcome.",
    Buckets: pipelineLatencyBuckets,
}, []string{"market_type", "outcome"}),

PipelinePhaseDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
    Name: "tradingagent_pipeline_phase_duration_seconds",
    Help: "Pipeline phase duration by phase and terminal status.",
    Buckets: pipelineLatencyBuckets,
}, []string{"phase", "status"}),
```

- [ ] **Step 4: Observe preparation and persisted phases**

Start a timer before `prepareStrategyRun`. Record `outcome="success"` or `outcome="error"` when it returns. In `recordPipelineMetrics`, unmarshal `run.PhaseTimings` into `map[string]int64`, strip the `_ms` suffix, and observe seconds using the run's terminal status.

Do not parallelize research debate: later participants currently consume earlier contributions, so parallel execution would change behavior.

- [ ] **Step 5: Verify instrumentation**

```bash
go test ./internal/agent ./internal/metrics ./cmd/tradingagent -run 'PhaseTiming|Preparation|PipelineMetrics' -count=1
curl -fsS http://10.0.0.56:3030/metrics \
  | rg '^tradingagent_pipeline_(preparation|phase)_duration_seconds'
```

Expected after staging runs: non-empty series for preparation, analysis, research debate, and trading; values reconcile within one second of persisted `phase_timings`.

- [ ] **Step 6: Commit and deploy instrumentation before collecting canary data**

```bash
git add internal/metrics internal/agent/runner.go internal/agent/runner_test.go cmd/tradingagent/prod_strategy_runner.go cmd/tradingagent/runtime_test.go
git commit -m "feat: expose strategy pipeline phase latency"
```

Run the immutable release template with `GATE=gate-d`. Do not begin the cohort until the deployed image IDs and canary start timestamp are recorded.

- [ ] **Step 7: Run the paper-only canary without changing the global default**

Select at least ten paper strategies with comparable recent successful three-round runs. Clone each into a paper-only canary whose only config difference is `config.pipeline_config.debate_rounds=1`; keep the original three-round strategy as its control. Run both cohorts on the same ticker, schedule, provider, and model tier until each cohort has at least 30 terminal runs.

Compute the cohort evidence from explicit control/canary strategy-ID sets rather than relying on the marshaled config's Go field names:

```sql
WITH cohort_runs AS (
  SELECT
    CASE
      WHEN strategy_id = ANY((:'canary_strategy_ids')::uuid[]) THEN 'one_round_canary'
      WHEN strategy_id = ANY((:'control_strategy_ids')::uuid[]) THEN 'three_round_control'
    END AS cohort,
    status,
    signal,
    EXTRACT(EPOCH FROM (completed_at - started_at)) AS elapsed_seconds
  FROM pipeline_runs
  WHERE completed_at IS NOT NULL
    AND started_at >= :'canary_started_at'
    AND strategy_id = ANY(
      (:'canary_strategy_ids')::uuid[] || (:'control_strategy_ids')::uuid[]
    )
)
SELECT
  cohort,
  COUNT(*) AS terminal_runs,
  COUNT(*) FILTER (WHERE status = 'failed') AS failed_runs,
  COUNT(*) FILTER (WHERE signal = 'buy') AS buy_runs,
  COUNT(*) FILTER (WHERE signal = 'sell') AS sell_runs,
  COUNT(*) FILTER (WHERE signal = 'hold') AS hold_runs,
  percentile_cont(0.95) WITHIN GROUP (ORDER BY elapsed_seconds) AS p95_seconds
FROM cohort_runs
GROUP BY cohort
ORDER BY cohort;
```

Record concrete `psql` variables `canary_started_at`, `canary_strategy_ids`, and `control_strategy_ids` in the baseline report before running the query. Review every buy/sell run plus a random sample of 20 hold runs from each cohort using their persisted agent decisions and reports.

Gate the global change on all of:

```text
canary p95 end-to-end duration <= 300 seconds
canary p95 improves by >= 25% versus control
completion rate declines by <= 2 percentage points
pipeline failure rate increases by <= 2 percentage points
all sampled decisions have evidence consistent with their terminal signal
no increase in live orders because the canary set is paper-only
```

- [ ] **Step 8: Promote one-round execution only through explicit strategy overrides**

Inventory every active strategy before promotion. Do not edit live strategies or paper strategies outside the reviewed cohort; the unchanged global default keeps them at three rounds. Apply `debate_rounds=1` only to paper strategies whose individual canary evidence passed.

Do not change `defaultPipelineDebateRounds` in this plan. A future global-default change requires a separate staged live-trading plan with broker-specific acceptance and rollback gates.

- [ ] **Step 9: Record and verify the explicit promotion set**

```bash
docker compose -f docker-compose.nuc.yml exec -T postgres \
  psql -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-tradingagent}" -c \
  "SELECT id, name, is_paper, is_active, config #> '{pipeline_config,debate_rounds}' AS debate_rounds FROM strategies WHERE is_active ORDER BY is_paper, name;"
```

Append the reviewed one-round strategy IDs and confirmation that no live strategy was mutated to the baseline report. If the canary gate fails, restore the canary clones to three rounds or disable them.

---

### Task 8: Add runbooks, alerts, deployment verification, and a 24-hour soak

**Files:**
- Create: `docs/runbooks/provider-throttling.md`
- Modify: `docs/runbooks/rolling-restart.md`
- Modify: `docs/runbooks/database-backup-restore.md`
- Modify: `docs/runbooks/README.md`
- Modify: `README.md:158-166,221-223`
- Modify: `docs/getting-started.md`
- Modify: `docs/design/api-design.md:16-26`
- Modify: `docs/design/infrastructure/deployment-and-operations.md`
- Modify: `docs/frontend/api-implementation-review.md`
- Modify: `cmd/tradingagent/main.go`
- Modify: `cmd/tradingagent/main_test.go`
- Modify: `cmd/tradingagent/smoke_test.go`
- Modify: `monitoring/prometheus/alerts.yml`
- Modify: `docs/reports/2026-07-27-production-health-baseline.md`

- [ ] **Step 1: Document the canonical operational contract**

State exactly:

```text
GET /healthz: process liveness; HTTP 200 with {"status":"ok"}; no dependency checks
GET /health: dependency readiness; HTTP 200 when DB/Redis are healthy, HTTP 503 when degraded
GET /api/v1/health: readiness alias used by the web/API proxy
GET /metrics: private app/monitoring-network endpoint; not exposed by the public web proxy
```

Remove every `{"status":"all-ok"}` claim.

- [ ] **Step 2: Write the provider-throttling runbook**

Include commands for:

```bash
curl -fsS http://10.0.0.56:3030/metrics \
  | rg 'tradingagent_(kalshi_rate_limit|kalshi_retry|data_source_(last_success|cooldown)|signal_evaluation_failures)'

docker compose -f docker-compose.nuc.yml logs --since=30m app \
  | rg 'rate limit|deferred|cooldown|signal evaluator'
```

Define response actions: preserve last-good data during provider cooldown, do not disable the job for typed throttles, investigate hard failures, and never bypass the live-trading kill switch to compensate for stale data.

- [ ] **Step 3: Add operational alerts**

Add warning alerts for sustained Kalshi rate limits and Reddit cooldown past its expected window. Use `increase(...)` and timestamp comparison rather than alerting on historical process-lifetime counters.

- [ ] **Step 4: Verify immutable releases were deployed in gate order**

For each gate, first run `go test ./... -count=1`, `go vet ./...`, the web test/build commands, and Compose config validation against a clean committed SHA. Build gate-specific image tags as defined in Task 2, deploy those exact tags with `docker compose ... up -d --no-build`, and record SHA, image IDs, start time, end time, and rollback tags.

Required order: Tasks 2–4 image and Gate A soak; Task 5 classification-only image and Gate B soak; Task 5 flag-off retry-capability image; data-client-only flag-on Gate C soak; Task 6 metric-classification image; Task 7 instrumentation image; paper-only Gate D cohort. Do not package code from a later gate into an earlier image.

The Prometheus/Grafana stack is external to `docker-compose.nuc.yml`. Set `MONITORING_CONFIG_DIR`, `PROMETHEUS_URL`, `GRAFANA_URL`, and `GRAFANA_TOKEN`; provision this repository's files into the external stack's bind-mounted configuration directory, then reload and query it:

```bash
install -m 0644 monitoring/prometheus/alerts.yml \
  "${MONITORING_CONFIG_DIR}/prometheus/alerts.yml"
install -m 0644 monitoring/grafana/dashboards/*.json \
  "${MONITORING_CONFIG_DIR}/grafana/dashboards/"
curl -fsS -X POST "${PROMETHEUS_URL}/-/reload"
curl -fsS -H "Authorization: Bearer ${GRAFANA_TOKEN}" -X POST \
  "${GRAFANA_URL}/api/admin/provisioning/dashboards/reload"
curl -fsSG "${PROMETHEUS_URL}/api/v1/query" \
  --data-urlencode 'query=up{job=~"tradingagent|augr-api"}' \
  | jq -e '.data.result != [] and ([.data.result[].value[1]] | all(. == "1"))'
```

Gate A is blocked if the external-stack owner cannot provision/reload these files or if the Prometheus query returns no target.

- [ ] **Step 5: Verify the live endpoint matrix**

```bash
curl -fsS https://augr.subcult.tv/healthz | jq -e '.status == "ok" and length == 1'
curl -fsS https://augr.subcult.tv/health | jq -e '.status == "ok" and .db == "ok" and .redis == "ok"'
test "$(curl -sS -o /dev/null -w '%{http_code}' https://augr.subcult.tv/metrics)" = "404"
curl -fsS http://10.0.0.56:3030/metrics \
  > /tmp/augr-metrics.txt
for metric in go_goroutines process_start_time_seconds tradingagent_scheduler_tick_total polymarket_recorder_lag_seconds; do
  rg -q "^${metric}" /tmp/augr-metrics.txt || { echo "missing metric: ${metric}" >&2; exit 1; }
done
rm -f /tmp/augr-metrics.txt
```

- [ ] **Step 6: Run a 24-hour soak and record the decision**

The soak passes only if:

```text
no liveness/readiness false positives
no duplicate-registration panic
no empty required metric labels
no hard-failure auto-disable caused by a typed Kalshi throttle
no request count beyond configured max attempts
no kill-switch or circuit-breaker state change caused by remediation
latency and quality thresholds from Task 7 are met before changing the default
```

Append the before/after metric table and explicit pass/fail decision to `docs/reports/2026-07-27-production-health-baseline.md`.

- [ ] **Step 7: Run final checks and commit documentation**

```bash
go test ./... -count=1
go vet ./...
npm --prefix web test -- --run
npm --prefix web run build
docker compose -f docker-compose.nuc.yml config >/dev/null
git add README.md docs monitoring/prometheus/alerts.yml docker-compose.nuc.yml
git commit -m "docs: add production health remediation runbooks"
```

---

## Self-review checklist

- Every observed warning maps to a task and a production verification command.
- Metrics are fixed before retry or latency behavior changes.
- `/metrics` remains private; `/healthz` cannot restart the app because DB or Redis is temporarily unavailable.
- The unbacked aggregate portfolio gauge is removed rather than populated from one broker under a misleading name.
- Kalshi rate limits are typed and persisted as deferred; hard failures still count toward auto-disable.
- Retry waits are exponential, capped, cancellation-aware, and cannot exceed configured attempts.
- Signal failure reasons are bounded and raw model output is not logged.
- Debate semantics remain serial; the only behavior change is a paper-only, evidence-gated round-count canary.
- Native and generic strategy runs emit the same terminal metric contract exactly once.
- Full tests, build, Compose validation, endpoint checks, and a 24-hour soak precede completion.
