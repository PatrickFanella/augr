package automation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/data/polygon"
	"github.com/PatrickFanella/get-rich-quick/internal/data/rss"
	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	kalshiexecution "github.com/PatrickFanella/get-rich-quick/internal/execution/kalshi"
	polymarketexecution "github.com/PatrickFanella/get-rich-quick/internal/execution/polymarket"
	prediction "github.com/PatrickFanella/get-rich-quick/internal/execution/prediction"
	kalshidiscovery "github.com/PatrickFanella/get-rich-quick/internal/kalshidiscovery"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/llm/embedding"
	"github.com/PatrickFanella/get-rich-quick/internal/portfolio"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	pgrepo "github.com/PatrickFanella/get-rich-quick/internal/repository/postgres"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
	"github.com/PatrickFanella/get-rich-quick/internal/universe"
)

// All cron expressions use Eastern time (America/New_York) so schedules
// align with US equity market hours regardless of server timezone.
var easternTime = mustLoadEastern()

func mustLoadEastern() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		panic("automation: load America/New_York: " + err.Error())
	}
	return loc
}

// autoDisableThreshold is the number of consecutive failures after which a
// job is automatically disabled to prevent cascading damage.
const autoDisableThreshold = 5

// StrategyTrigger triggers an immediate pipeline run for a strategy.
// The scheduler satisfies this interface.
type StrategyTrigger interface {
	TriggerStrategy(strategy domain.Strategy)
}

// OrchestratorDeps bundles external dependencies required by the orchestrator.
type OrchestratorDeps struct {
	Universe               *universe.Universe
	Polygon                *polygon.Client
	DataService            *data.DataService
	AlpacaReconciler       *AlpacaReconciler
	OptionsProvider        data.OptionsDataProvider
	LLMProvider            llm.Provider
	GeneratorMetrics       discovery.GeneratorMetrics
	EmbeddingProvider      embedding.Provider // optional; nil = skip embedding during triage
	EventsProvider         data.EventsProvider
	StrategyRepo           repository.StrategyRepository
	PositionRepo           repository.PositionRepository
	OrderRepo              repository.OrderRepository
	TradeRepo              repository.TradeRepository
	OpportunityRepo        repository.OpportunityRepository
	AllocationDecisionRepo repository.AllocationDecisionRepository
	RunRepo                repository.PipelineRunRepository
	JobRunRepo             *pgrepo.JobRunRepo
	OptionsScanRepo        *pgrepo.OptionsScanRepo
	NewsFeedRepo           *pgrepo.NewsFeedRepo
	StrategyTrigger        StrategyTrigger                        // optional; nil = no event-driven triggers
	PolymarketAccountRepo  repository.PolymarketAccountRepository // optional; nil = skip profiling job
	PolymarketReconciler   *polymarketexecution.Reconciler        // optional; nil = skip reconciliation job
	PredictionSettler      interface {
		PendingMarkets(context.Context, domain.MarketType) ([]string, error)
		SettlePreview(context.Context, domain.MarketType, string) (*prediction.SettlementPreview, error)
		PreviewMarket(context.Context, domain.MarketType, string) (int, error)
		SettleDecisions(context.Context, domain.MarketType, string, string, time.Time, []uuid.UUID) (int, error)
		SettleMarket(context.Context, domain.MarketType, string, string, time.Time) (int, error)
	} // optional; settles paper event positions from provider outcomes
	KalshiReconciler            *kalshiexecution.Reconciler // optional; nil = skip live reconciliation job
	PolymarketResolvedRepo      repository.PolymarketResolvedMarketsRepository
	PolymarketWatchedRepo       repository.PolymarketWatchedMarketsRepository // optional; nil = skip discovery auto-watch
	PolymarketDiscoveryRuns     repository.PolymarketDiscoveryRunRepository   // optional; nil = skip chunked discovery job registration/execution
	PolymarketCLOBURL           string                                        // optional; defaults to Polymarket CLOB base URL
	DisablePolymarketAutomation bool                                          // disables Polymarket profile/reconcile/resolution/discovery cron jobs
	KalshiCatalog               interface {
		ListMarkets(context.Context, kalshidiscovery.ListOptions) ([]kalshidiscovery.MarketCandidate, string, error)
		GetMarket(context.Context, string) (*kalshidiscovery.MarketCandidate, error)
	}
	PortfolioAllocatorMode    portfolio.AllocatorMode
	PortfolioPaperProcessor   portfolio.PaperOrderProcessor
	KalshiWatchedRepo         repository.KalshiWatchedMarketsRepository
	KalshiMarketSnapshotsRepo repository.KalshiMarketSnapshotsRepository
	KalshiDiscoveryRuns       repository.KalshiDiscoveryRunRepository // optional; nil = skip progress recording
	KalshiSettlementGateRepo  repository.KalshiSettlementGateRepository
	KalshiSettlementThreshold int
	KalshiSettlementDryRun    bool
	KalshiSettlementEnabled   bool
	ReportArtifactRepo        *pgrepo.ReportArtifactRepo          // optional; nil = skip report jobs
	BacktestConfigRepo        repository.BacktestConfigRepository // optional; needed by report jobs
	BacktestRunRepo           repository.BacktestRunRepository    // optional; needed by report jobs
	OvernightBacktestRuns     repository.OvernightBacktestRunRepository
	Logger                    *slog.Logger
}

// RegisteredJob tracks a single automated job and its runtime state.
type RegisteredJob struct {
	Name                string
	Description         string
	Schedule            scheduler.ScheduleSpec
	Fn                  func(ctx context.Context) error
	DependsOn           []string // job names that must not be running
	mu                  sync.Mutex
	StartedAt           *time.Time
	LastRun             *time.Time
	LastResult          string
	LastSummary         map[string]int
	LastError           string
	LastErrorAt         *time.Time
	RunCount            int
	ErrorCount          int
	ConsecutiveFailures int
	SettlementGate      *SettlementGateStatus
	Running             bool
	Enabled             bool
}

// JobStatus is the read-only snapshot returned by Status.
type JobStatus struct {
	Name                string                `json:"name"`
	Description         string                `json:"description"`
	Schedule            string                `json:"schedule"`
	LastRun             *time.Time            `json:"last_run,omitempty"`
	LastResult          string                `json:"last_result"`
	LastSummary         map[string]int        `json:"last_summary,omitempty"`
	LastError           string                `json:"last_error,omitempty"`
	LastErrorAt         *time.Time            `json:"last_error_at,omitempty"`
	RunCount            int                   `json:"run_count"`
	ErrorCount          int                   `json:"error_count"`
	ConsecutiveFailures int                   `json:"consecutive_failures"`
	StuckFor            *time.Duration        `json:"stuck_for,omitempty"`
	Running             bool                  `json:"running"`
	Enabled             bool                  `json:"enabled"`
	SettlementGate      *SettlementGateStatus `json:"settlement_gate,omitempty"`
}

type SettlementGateStatus struct {
	ConsecutiveSuccesses  int        `json:"consecutive_dry_run_successes"`
	Threshold             int        `json:"threshold"`
	Eligible              bool       `json:"eligible"`
	ProjectionFingerprint string     `json:"projection_fingerprint,omitempty"`
	LastOutcome           string     `json:"last_outcome,omitempty"`
	LastError             string     `json:"last_error,omitempty"`
	LastRunAt             *time.Time `json:"last_run_at,omitempty"`
	Fetched               int        `json:"fetched"`
	Resolved              int        `json:"resolved"`
	WouldSettleMarkets    int        `json:"would_settle_markets"`
	WouldSettleDecisions  int        `json:"would_settle_decisions"`
}

// AutomationJobMetrics is implemented by *metrics.Metrics.
// It is defined here as an interface to avoid an import cycle.
type AutomationJobMetrics interface {
	RecordAutomationJobError(jobName string)
	RecordAlpacaReconcileRun(result string)
	RecordKalshiReconcileRun(result string)
	RecordKalshiSettlementDryRun(result string)
	RecordKalshiSettlementOutcome(result string)
	RecordKalshiSettlementTransition(from, to string)
}

// ReportWorkerMetrics captures report worker success/error emission.
type ReportWorkerMetrics interface {
	RecordReportWorkerSuccess(strategyID string)
	RecordReportWorkerError(strategyID string)
}

// JobOrchestrator is the central registry and cron runner for all automated jobs.
type JobOrchestrator struct {
	jobs                map[string]*RegisteredJob
	cron                *cron.Cron
	deps                OrchestratorDeps
	logger              *slog.Logger
	rssAggregator       *rss.Aggregator
	metrics             AutomationJobMetrics
	reportMetrics       ReportWorkerMetrics
	reportWorker        *ReportWorker
	kalshiGateUnhealthy bool
}

// NewJobOrchestrator constructs a new orchestrator.
func NewJobOrchestrator(deps OrchestratorDeps) *JobOrchestrator {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &JobOrchestrator{
		jobs:   make(map[string]*RegisteredJob),
		cron:   cron.New(cron.WithLocation(easternTime)),
		deps:   deps,
		logger: logger,
	}
}

// WithJobMetrics attaches a metrics sink to the orchestrator.
// Call before Start(). Safe to call with nil (disables metrics).
func (o *JobOrchestrator) WithJobMetrics(m AutomationJobMetrics) {
	o.metrics = m
}

// WithReportMetrics attaches report-worker-specific metrics.
// Call before Start(). Safe to call with nil.
func (o *JobOrchestrator) WithReportMetrics(m ReportWorkerMetrics) {
	o.reportMetrics = m
}

// SetConsecutiveFailures sets the ConsecutiveFailures counter on a job.
// Primarily for testing and operational resets.
func (o *JobOrchestrator) SetConsecutiveFailures(name string, n int) {
	if job, ok := o.jobs[name]; ok {
		job.mu.Lock()
		job.ConsecutiveFailures = n
		job.mu.Unlock()
	}
}

func (o *JobOrchestrator) SetLastSummary(name string, summary map[string]int) {
	if job, ok := o.jobs[name]; ok {
		job.mu.Lock()
		job.LastSummary = cloneSummary(summary)
		job.mu.Unlock()
	}
}

// Register adds a job to the registry.
func (o *JobOrchestrator) Register(name, description string, spec scheduler.ScheduleSpec, fn func(ctx context.Context) error, dependsOn ...string) {
	o.jobs[name] = &RegisteredJob{
		Name:        name,
		Description: description,
		Schedule:    spec,
		Fn:          fn,
		DependsOn:   dependsOn,
		Enabled:     true,
	}
}

// RegisterAll registers all automated jobs from every job group.
func (o *JobOrchestrator) RegisterAll() {
	o.registerBrokerReconciliationJobs()
	o.registerMarketJobs()
	o.registerPreMarketJobs()
	o.registerPostMarketJobs()
	o.registerOptionsLifecycleJobs()
	o.registerEventJobs()
	o.registerOvernightJobs()
	o.registerWeeklyJobs()
	o.registerNewsJobs()
	if !o.deps.DisablePolymarketAutomation {
		o.registerPolymarketProfileJob()
		o.registerPolymarketReconciliationJobs()
		o.registerPolymarketResolutionsJob()
		o.registerPolymarketDiscoveryJob()
	}
	o.registerKalshiDiscoveryJob()
	o.registerKalshiSettlementJob()
	o.registerKalshiReconciliationJob()
	o.registerReportJobs()
	o.registerPortfolioAllocatorJobs()
}

// Start starts the cron engine with all registered jobs.
// It hydrates in-memory counters from the database first.
func (o *JobOrchestrator) Start() error {
	o.hydrateFromDB()

	for _, job := range o.jobs {
		j := job // capture for closure
		_, err := o.cron.AddFunc(j.Schedule.Cron, func() {
			o.wrapAndRun(j)
		})
		if err != nil {
			return fmt.Errorf("automation: failed to schedule job %q: %w", j.Name, err)
		}
		o.logger.Info("automation: scheduled job",
			slog.String("name", j.Name),
			slog.String("cron", j.Schedule.Cron),
			slog.String("type", string(j.Schedule.Type)),
		)
	}
	o.cron.Start()
	o.logger.Info("automation: orchestrator started", slog.Int("jobs", len(o.jobs)))
	return nil
}

// Stop stops all jobs and the cron engine.
func (o *JobOrchestrator) Stop() {
	ctx := o.cron.Stop()
	<-ctx.Done()
	o.logger.Info("automation: orchestrator stopped")
}

// Status returns status for all registered jobs, sorted by name.
func (o *JobOrchestrator) Status() []JobStatus {
	statuses := make([]JobStatus, 0, len(o.jobs))
	for _, job := range o.jobs {
		job.mu.Lock()
		var stuckFor *time.Duration
		if job.Running && job.StartedAt != nil {
			d := time.Since(*job.StartedAt)
			stuckFor = &d
		}
		s := JobStatus{
			Name:                job.Name,
			Description:         job.Description,
			Schedule:            job.Schedule.Describe(),
			LastRun:             job.LastRun,
			LastResult:          job.LastResult,
			LastSummary:         cloneSummary(job.LastSummary),
			LastError:           job.LastError,
			LastErrorAt:         job.LastErrorAt,
			RunCount:            job.RunCount,
			ErrorCount:          job.ErrorCount,
			ConsecutiveFailures: job.ConsecutiveFailures,
			StuckFor:            stuckFor,
			Running:             job.Running,
			Enabled:             job.Enabled,
			SettlementGate:      cloneSettlementGateStatus(job.SettlementGate),
		}
		job.mu.Unlock()
		statuses = append(statuses, s)
	}
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].Name < statuses[j].Name
	})
	return statuses
}

// RunJob triggers a specific job by name immediately, bypassing the
// schedule/market-hours check (but still respecting dedup and dependencies).
func (o *JobOrchestrator) RunJob(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	job, ok := o.jobs[name]
	if !ok {
		return fmt.Errorf("automation: unknown job %q", name)
	}
	job.mu.Lock()
	enabled := job.Enabled
	job.mu.Unlock()
	if !enabled {
		return fmt.Errorf("automation: job %q is disabled", name)
	}
	o.logger.Info("automation: manual trigger", slog.String("job", name))
	go o.runDirect(job)
	return nil
}

// runDirect runs a job immediately without checking ShouldFire (for manual triggers).
func (o *JobOrchestrator) runDirect(job *RegisteredJob) {
	job.mu.Lock()
	if !job.Enabled {
		job.mu.Unlock()
		o.logger.Info("automation: skipping disabled manual run", slog.String("job", job.Name))
		return
	}
	if job.Running {
		job.mu.Unlock()
		o.logger.Warn("automation: skipping overlapping run", slog.String("job", job.Name))
		return
	}
	startedAt := time.Now()
	job.Running = true
	job.StartedAt = &startedAt
	job.mu.Unlock()

	// Check dependencies.
	for _, dep := range job.DependsOn {
		if depJob, ok := o.jobs[dep]; ok {
			depJob.mu.Lock()
			running := depJob.Running
			depJob.mu.Unlock()
			if running {
				o.logger.Info("automation: skipping job, dependency still running",
					slog.String("job", job.Name),
					slog.String("blocked_by", dep),
				)
				job.mu.Lock()
				job.Running = false
				job.mu.Unlock()
				return
			}
		}
	}

	defer func() {
		job.mu.Lock()
		job.Running = false
		job.StartedAt = nil
		job.mu.Unlock()
	}()

	o.logger.Info("automation: job starting", slog.String("job", job.Name))
	start := time.Now()
	err := job.Fn(context.Background())
	elapsed := time.Since(start)

	job.mu.Lock()
	now := time.Now()
	job.LastRun = &now
	job.RunCount++
	if err != nil {
		job.ErrorCount++
		job.LastResult = "failed"
		job.LastError = err.Error()
		job.LastErrorAt = &now
		job.ConsecutiveFailures++
		o.logger.Error("automation: job failed", slog.String("job", job.Name), slog.Duration("elapsed", elapsed), slog.Any("error", err))
		if o.metrics != nil {
			o.metrics.RecordAutomationJobError(job.Name)
		}
		if job.ConsecutiveFailures >= autoDisableThreshold {
			job.Enabled = false
			o.logger.Error("automation: auto-disabled job after consecutive failures",
				slog.String("job", job.Name),
				slog.Int("consecutive_failures", job.ConsecutiveFailures),
			)
		}
	} else {
		job.LastResult = "success"
		job.LastError = ""
		job.ConsecutiveFailures = 0
		o.logger.Info("automation: job completed", slog.String("job", job.Name), slog.Duration("elapsed", elapsed))
	}
	job.mu.Unlock()

	o.persistRun(job.Name, start, elapsed, err)
}

// SetEnabled enables or disables a job.
func (o *JobOrchestrator) SetEnabled(name string, enabled bool) error {
	job, ok := o.jobs[name]
	if !ok {
		return fmt.Errorf("automation: unknown job %q", name)
	}
	if name == "kalshi_settlement" && enabled {
		if o.kalshiGateUnhealthy {
			return fmt.Errorf("automation: kalshi settlement gate unhealthy")
		}
		if o.deps.KalshiSettlementGateRepo == nil {
			o.kalshiGateUnhealthy = true
			return fmt.Errorf("automation: kalshi settlement gate unavailable")
		}
		state, err := o.deps.KalshiSettlementGateRepo.Get(context.Background(), name)
		if err != nil {
			o.kalshiGateUnhealthy = true
			return fmt.Errorf("automation: load kalshi settlement gate: %w", err)
		}
		if state == nil || !state.Eligible {
			return fmt.Errorf("automation: kalshi settlement gate not eligible")
		}
	}
	job.mu.Lock()
	previous := job.Enabled
	job.Enabled = enabled
	if name == "kalshi_settlement" && o.deps.KalshiSettlementGateRepo != nil {
		if state, err := o.deps.KalshiSettlementGateRepo.Get(context.Background(), name); err == nil {
			job.SettlementGate = settlementGateStatusFromState(state)
		}
	}
	job.mu.Unlock()
	if name == "kalshi_settlement" && o.metrics != nil && previous != enabled {
		o.metrics.RecordKalshiSettlementTransition(fmt.Sprintf("%t", previous), fmt.Sprintf("%t", enabled))
	}
	o.logger.Info("automation: job enabled state changed",
		slog.String("job", name),
		slog.Bool("enabled", enabled),
	)
	return nil
}

func settlementGateStatusFromState(state *domain.KalshiSettlementGateState) *SettlementGateStatus {
	if state == nil {
		return nil
	}
	return &SettlementGateStatus{
		ConsecutiveSuccesses:  state.ConsecutiveSuccesses,
		Threshold:             state.Threshold,
		Eligible:              state.Eligible,
		ProjectionFingerprint: state.ProjectionFingerprint,
		LastOutcome:           state.LastOutcome,
		LastError:             state.LastError,
		LastRunAt:             state.LastRunAt,
		Fetched:               state.Fetched,
		Resolved:              state.Resolved,
		WouldSettleMarkets:    state.WouldSettleMarkets,
		WouldSettleDecisions:  state.WouldSettleDecisions,
	}
}

// wrapAndRun is the common wrapper that checks preconditions and runs the job.
func (o *JobOrchestrator) wrapAndRun(job *RegisteredJob) {
	now := time.Now()

	job.mu.Lock()
	if !job.Enabled {
		job.mu.Unlock()
		return
	}
	if !job.Schedule.ShouldFire(now) {
		job.mu.Unlock()
		return
	}
	if job.Running {
		job.mu.Unlock()
		o.logger.Warn("automation: skipping overlapping run", slog.String("job", job.Name))
		return
	}
	startedAt := time.Now()
	job.Running = true
	job.StartedAt = &startedAt
	job.mu.Unlock()

	// Check dependencies — skip if any dependency is currently running.
	for _, dep := range job.DependsOn {
		if depJob, ok := o.jobs[dep]; ok {
			depJob.mu.Lock()
			running := depJob.Running
			depJob.mu.Unlock()
			if running {
				o.logger.Info("automation: skipping job, dependency still running",
					slog.String("job", job.Name),
					slog.String("blocked_by", dep),
				)
				job.mu.Lock()
				job.Running = false
				job.mu.Unlock()
				return
			}
		}
	}

	defer func() {
		job.mu.Lock()
		job.Running = false
		job.StartedAt = nil
		job.mu.Unlock()
	}()

	o.logger.Info("automation: job starting", slog.String("job", job.Name))
	start := time.Now()

	ctx := context.Background()
	err := job.Fn(ctx)

	elapsed := time.Since(start)

	job.mu.Lock()
	job.LastRun = &now
	job.RunCount++
	if err != nil {
		job.ErrorCount++
		job.LastError = err.Error()
		job.LastErrorAt = &now
		job.ConsecutiveFailures++
		job.LastResult = fmt.Sprintf("error after %s", elapsed.Truncate(time.Millisecond))
		if o.metrics != nil {
			o.metrics.RecordAutomationJobError(job.Name)
		}
		if job.ConsecutiveFailures >= autoDisableThreshold {
			job.Enabled = false
			o.logger.Error("automation: auto-disabled job after consecutive failures",
				slog.String("job", job.Name),
				slog.Int("consecutive_failures", job.ConsecutiveFailures),
			)
		}
	} else {
		job.LastError = ""
		job.ConsecutiveFailures = 0
		job.LastResult = fmt.Sprintf("ok in %s", elapsed.Truncate(time.Millisecond))
	}
	job.mu.Unlock()

	o.persistRun(job.Name, start, elapsed, err)

	if err != nil {
		o.logger.Error("automation: job failed",
			slog.String("job", job.Name),
			slog.Duration("elapsed", elapsed),
			slog.Any("error", err),
		)
	} else {
		o.logger.Info("automation: job completed",
			slog.String("job", job.Name),
			slog.Duration("elapsed", elapsed),
		)
	}
}

// persistRun writes a job run to the database.
func (o *JobOrchestrator) persistRun(jobName string, start time.Time, elapsed time.Duration, jobErr error) {
	if o.deps.JobRunRepo == nil {
		return
	}

	completed := start.Add(elapsed)
	status := "ok"
	var errMsg string
	if jobErr != nil {
		status = "error"
		errMsg = jobErr.Error()
	}

	job := o.jobs[jobName]
	var lastErrorAt *time.Time
	var consecutiveFailures int
	var result map[string]int
	if job != nil {
		job.mu.Lock()
		lastErrorAt = job.LastErrorAt
		consecutiveFailures = job.ConsecutiveFailures
		result = cloneSummary(job.LastSummary)
		job.mu.Unlock()
	}

	run := &pgrepo.JobRun{
		JobName:             jobName,
		Status:              status,
		StartedAt:           start.UTC(),
		CompletedAt:         &completed,
		DurationNs:          elapsed.Nanoseconds(),
		Result:              result,
		Error:               errMsg,
		LastErrorAt:         lastErrorAt,
		ConsecutiveFailures: consecutiveFailures,
	}

	if err := o.deps.JobRunRepo.Create(context.Background(), run); err != nil {
		o.logger.Warn("automation: failed to persist job run",
			slog.String("job", jobName),
			slog.Any("error", err),
		)
	}
}

// hydrateFromDB loads historical run stats from the database to restore
// counters after a server restart.
func (o *JobOrchestrator) hydrateFromDB() {
	if o.deps.JobRunRepo == nil {
		return
	}
	if o.deps.KalshiSettlementGateRepo != nil {
		if state, err := o.deps.KalshiSettlementGateRepo.Get(context.Background(), "kalshi_settlement"); err == nil {
			if job, ok := o.jobs["kalshi_settlement"]; ok {
				job.mu.Lock()
				job.SettlementGate = &SettlementGateStatus{ConsecutiveSuccesses: state.ConsecutiveSuccesses, Threshold: state.Threshold, Eligible: state.Eligible, ProjectionFingerprint: state.ProjectionFingerprint, LastOutcome: state.LastOutcome, LastError: state.LastError, LastRunAt: state.LastRunAt, Fetched: state.Fetched, Resolved: state.Resolved, WouldSettleMarkets: state.WouldSettleMarkets, WouldSettleDecisions: state.WouldSettleDecisions}
				job.mu.Unlock()
				o.kalshiGateUnhealthy = false
			}
		} else if !errors.Is(err, repository.ErrNotFound) {
			o.logger.Warn("automation: failed to hydrate kalshi settlement gate", slog.Any("error", err))
			o.kalshiGateUnhealthy = true
		}
	}

	summaries, err := o.deps.JobRunRepo.Summaries(context.Background())
	if err != nil {
		o.logger.Warn("automation: failed to hydrate job stats from DB", slog.Any("error", err))
		return
	}

	for _, s := range summaries {
		job, ok := o.jobs[s.JobName]
		if !ok {
			continue
		}
		job.mu.Lock()
		job.LastRun = s.LastRun
		job.LastResult = s.LastResult
		job.LastError = s.LastError
		job.LastErrorAt = s.LastErrorAt
		job.RunCount = s.RunCount
		job.ErrorCount = s.ErrorCount
		job.ConsecutiveFailures = s.ConsecutiveFailures
		job.mu.Unlock()
	}

	o.logger.Info("automation: hydrated job stats from DB", slog.Int("jobs", len(summaries)))
}

func cloneSummary(summary map[string]int) map[string]int {
	if len(summary) == 0 {
		return nil
	}
	cloned := make(map[string]int, len(summary))
	for key, value := range summary {
		cloned[key] = value
	}
	return cloned
}

func cloneSettlementGateStatus(s *SettlementGateStatus) *SettlementGateStatus {
	if s == nil {
		return nil
	}
	clone := *s
	return &clone
}
