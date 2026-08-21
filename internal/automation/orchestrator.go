package automation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
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

const (
	// autoDisableThreshold is the number of consecutive failures after which a
	// job is automatically disabled to prevent cascading damage.
	autoDisableThreshold         = 5
	defaultAutomationJobTimeout  = 2 * time.Hour
	jobRunPersistenceTimeout     = 10 * time.Second
	jobControlPersistenceTimeout = 5 * time.Second
)

// ErrJobControlPersistence identifies a failed durable enable/disable write.
var ErrJobControlPersistence = errors.New("automation: job control persistence failed")

type AutomationJobRunRepository interface {
	Create(context.Context, *pgrepo.JobRun) error
	Complete(context.Context, *pgrepo.JobRun) error
	FailIncomplete(context.Context, time.Time, string) (int, error)
	Summaries(context.Context) ([]pgrepo.JobRunSummary, error)
}

// StrategyTrigger triggers an immediate pipeline run for a strategy.
// The scheduler satisfies this interface.
type StrategyTrigger interface {
	TriggerStrategy(strategy domain.Strategy)
}

// TickerDiscoveryJobConfig configures the database-ledger ticker-discovery job.
type TickerDiscoveryJobConfig struct {
	Enabled    bool
	Cron       string
	MinADV     float64
	MaxTickers int
}

// OrchestratorDeps bundles external dependencies required by the orchestrator.
type OrchestratorDeps struct {
	Universe               *universe.Universe
	Polygon                *polygon.Client
	DataService            *data.DataService
	AlpacaReconciler       *AlpacaReconciler
	OptionsProvider        data.OptionsDataProvider
	LLMProvider            llm.Provider
	LLMQuickModel          string
	GeneratorMetrics       discovery.GeneratorMetrics
	TickerDiscovery        TickerDiscoveryJobConfig
	EmbeddingProvider      embedding.Provider // optional; nil = skip embedding during triage
	EventsProvider         data.EventsProvider
	StrategyRepo           repository.StrategyRepository
	PositionRepo           repository.PositionRepository
	OrderRepo              repository.OrderRepository
	TradeRepo              repository.TradeRepository
	OptionSettlementRepo   repository.OptionSettlementRepository
	OpportunityRepo        repository.OpportunityRepository
	AllocationDecisionRepo repository.AllocationDecisionRepository
	RunRepo                repository.PipelineRunRepository
	JobRunRepo             AutomationJobRunRepository
	JobControlRepo         repository.AutomationJobControlRepository
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
	PortfolioAccountBalance   PortfolioAccountBalanceSource
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
	DiscoveryRunRepo          discovery.RunRepository             // required by stock discovery jobs
	OvernightBacktestRuns     repository.OvernightBacktestRunRepository
	JobTimeout                time.Duration
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
	now                 func() time.Time
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
		now:    time.Now,
	}
}

func (o *JobOrchestrator) jobContext() (context.Context, context.CancelFunc) {
	timeout := o.deps.JobTimeout
	if timeout <= 0 {
		timeout = defaultAutomationJobTimeout
	}
	return context.WithTimeout(context.Background(), timeout)
}

// invokeAutomationJob contains a job-local panic so one defective automation
// cannot terminate the scheduler process. Panic values may contain provider or
// model data, so only their type is returned to the durable failure path.
func invokeAutomationJob(ctx context.Context, fn func(context.Context) error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("automation: job panicked (%T)", recovered)
		}
	}()
	if fn == nil {
		return fmt.Errorf("automation: job function is nil")
	}
	return fn(ctx)
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

// RegisteredJobKeys returns a sorted copy of the registered job inventory.
// OVR-604 uses it to fail closed when a financial job lacks an explicit
// distributed occurrence/effect classification.
func (o *JobOrchestrator) RegisteredJobKeys() []string {
	keys := make([]string, 0, len(o.jobs))
	for key := range o.jobs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// RegisterAll registers all automated jobs from every job group.
func (o *JobOrchestrator) RegisterAll() {
	o.registerBrokerReconciliationJobs()
	o.registerMarketJobs()
	o.registerPreMarketJobs()
	o.registerTickerDiscoveryJob()
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

// RunJob triggers a specific job by name immediately while retaining the
// schedule's market-session, weekday, and holiday safety gates. The cron
// minute itself is intentionally not checked by ScheduleSpec.ShouldFire, so
// operators can rerun a job anywhere inside its authorized session.
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
	schedule := job.Schedule
	job.mu.Unlock()
	if !enabled {
		return fmt.Errorf("automation: job %q is disabled", name)
	}
	now := time.Now()
	if o.now != nil {
		now = o.now()
	}
	if !schedule.ShouldFire(now) {
		return fmt.Errorf("automation: job %q is outside configured session (%s)", name, schedule.Describe())
	}
	startedAt := time.Now()
	if err := claimManualJob(job, startedAt); err != nil {
		return err
	}
	o.logger.Info("automation: manual trigger", slog.String("job", name))
	go o.runClaimedDirect(job, startedAt)
	return nil
}

// runDirect runs a job immediately without checking ShouldFire (for manual triggers).
func (o *JobOrchestrator) runDirect(job *RegisteredJob) {
	startedAt := time.Now()
	if err := claimManualJob(job, startedAt); err != nil {
		o.logger.Info("automation: manual run not admitted", slog.String("job", job.Name), slog.Any("error", err))
		return
	}
	o.runClaimedDirect(job, startedAt)
}

func claimManualJob(job *RegisteredJob, startedAt time.Time) error {
	job.mu.Lock()
	defer job.mu.Unlock()
	if !job.Enabled {
		return fmt.Errorf("automation: job %q is disabled", job.Name)
	}
	if job.Running {
		return fmt.Errorf("automation: job %q is already running", job.Name)
	}
	job.Running = true
	job.StartedAt = &startedAt
	return nil
}

func (o *JobOrchestrator) runClaimedDirect(job *RegisteredJob, startedAt time.Time) {
	// Require every dependency to have completed successfully in today's
	// Eastern automation cycle, not merely to be idle at this instant.
	if dep, reason := o.dependencyBlocker(job, startedAt); dep != "" {
		o.recordDependencySkip(job, startedAt, dep, reason)
		return
	}

	defer func() {
		job.mu.Lock()
		job.Running = false
		job.StartedAt = nil
		job.mu.Unlock()
	}()
	run, beginErr := o.beginRun(job, startedAt)
	if beginErr != nil {
		now := time.Now()
		_ = o.applyRunPersistenceFailure(job, now, beginErr)
		o.logger.Error("automation: failed to persist running job", slog.String("job", job.Name), slog.Any("error", beginErr))
		return
	}

	o.logger.Info("automation: job starting", slog.String("job", job.Name))
	start := time.Now()
	ctx, cancel := o.jobContext()
	defer cancel()
	err := invokeAutomationJob(ctx, job.Fn)
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

	if persistErr := o.completeRun(run, job, time.Now(), elapsed, err); persistErr != nil {
		o.logger.Error("automation: failed to persist job run", slog.String("job", job.Name), slog.Any("error", persistErr))
		if err == nil {
			_ = o.applyRunPersistenceFailure(job, now, persistErr)
		}
	}
}

// SetEnabled enables or disables a job.
func (o *JobOrchestrator) SetEnabled(name string, enabled bool) error {
	return o.SetEnabledBy(context.Background(), name, enabled, "system")
}

// SetEnabledBy durably records an operator override before changing the
// in-memory scheduler state. If persistence fails, the current state is kept.
func (o *JobOrchestrator) SetEnabledBy(ctx context.Context, name string, enabled bool, actor string) error {
	job, ok := o.jobs[name]
	if !ok {
		return fmt.Errorf("automation: unknown job %q", name)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "unknown"
	}
	// Serialize persistence and memory mutation with scheduler admission. This
	// prevents a run from starting while a disable is being committed and keeps
	// concurrent control requests in the same order in PostgreSQL and memory.
	job.mu.Lock()
	previous := job.Enabled
	if o.deps.JobControlRepo != nil {
		persistCtx, cancel := context.WithTimeout(ctx, jobControlPersistenceTimeout)
		err := o.deps.JobControlRepo.SetEnabled(persistCtx, name, enabled, actor)
		cancel()
		if err != nil {
			job.mu.Unlock()
			return fmt.Errorf("%w for %q: %v", ErrJobControlPersistence, name, err)
		}
	}
	job.Enabled = enabled
	if name == "kalshi_settlement" && o.deps.KalshiSettlementGateRepo != nil {
		gateCtx, gateCancel := context.WithTimeout(ctx, jobControlPersistenceTimeout)
		state, err := o.deps.KalshiSettlementGateRepo.Get(gateCtx, name)
		gateCancel()
		if err == nil {
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
		slog.String("actor", actor),
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

	if dep, reason := o.dependencyBlocker(job, startedAt); dep != "" {
		o.recordDependencySkip(job, startedAt, dep, reason)
		return
	}

	defer func() {
		job.mu.Lock()
		job.Running = false
		job.StartedAt = nil
		job.mu.Unlock()
	}()
	run, beginErr := o.beginRun(job, startedAt)
	if beginErr != nil {
		_ = o.applyRunPersistenceFailure(job, time.Now(), beginErr)
		o.logger.Error("automation: failed to persist running job", slog.String("job", job.Name), slog.Any("error", beginErr))
		return
	}

	o.logger.Info("automation: job starting", slog.String("job", job.Name))
	start := time.Now()

	ctx, cancel := o.jobContext()
	defer cancel()
	err := invokeAutomationJob(ctx, job.Fn)

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

	if persistErr := o.completeRun(run, job, time.Now(), elapsed, err); persistErr != nil {
		o.logger.Error("automation: failed to persist job run", slog.String("job", job.Name), slog.Any("error", persistErr))
		if err == nil {
			err = o.applyRunPersistenceFailure(job, now, persistErr)
		}
	}

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

func (o *JobOrchestrator) dependencyBlocker(job *RegisteredJob, now time.Time) (string, string) {
	for _, dep := range job.DependsOn {
		depJob, ok := o.jobs[dep]
		if !ok {
			return dep, "not registered"
		}
		depJob.mu.Lock()
		running := depJob.Running
		enabled := depJob.Enabled
		lastRun := depJob.LastRun
		lastResult := depJob.LastResult
		depJob.mu.Unlock()

		switch {
		case !enabled:
			return dep, "disabled"
		case running:
			return dep, "still running"
		case lastRun == nil:
			return dep, "has not completed"
		case !sameMarketDate(lastRun.In(easternTime), now.In(easternTime)):
			return dep, "latest run is from a prior automation day"
		case !successfulJobResult(lastResult):
			return dep, "latest run was not successful"
		}
	}
	return "", ""
}

func successfulJobResult(result string) bool {
	normalized := strings.ToLower(strings.TrimSpace(result))
	return normalized == "ok" || normalized == "success" || strings.HasPrefix(normalized, "ok in ")
}

func (o *JobOrchestrator) recordDependencySkip(job *RegisteredJob, at time.Time, dep, reason string) {
	message := fmt.Sprintf("dependency %s %s", dep, reason)
	o.logger.Warn("automation: skipping job, dependency unavailable",
		slog.String("job", job.Name),
		slog.String("blocked_by", dep),
		slog.String("reason", reason),
	)

	job.mu.Lock()
	job.Running = false
	job.StartedAt = nil
	job.LastRun = &at
	job.LastResult = "skipped: " + message
	job.RunCount++
	lastErrorAt := job.LastErrorAt
	consecutiveFailures := job.ConsecutiveFailures
	job.mu.Unlock()

	if o.deps.JobRunRepo == nil {
		return
	}
	completed := at
	run := &pgrepo.JobRun{
		JobName:             job.Name,
		Status:              "skipped",
		StartedAt:           at.UTC(),
		CompletedAt:         &completed,
		Result:              map[string]int{"dependency_blocked": 1},
		LastErrorAt:         lastErrorAt,
		ConsecutiveFailures: consecutiveFailures,
	}
	persistCtx, cancel := context.WithTimeout(context.Background(), jobRunPersistenceTimeout)
	defer cancel()
	if err := o.deps.JobRunRepo.Create(persistCtx, run); err != nil {
		o.logger.Error("automation: failed to persist dependency skip",
			slog.String("job", job.Name),
			slog.Any("error", err),
		)
		_ = o.applyRunPersistenceFailure(job, at, err)
	}
}

func (o *JobOrchestrator) beginRun(job *RegisteredJob, start time.Time) (*pgrepo.JobRun, error) {
	if o.deps.JobRunRepo == nil {
		return nil, nil
	}
	job.mu.Lock()
	lastErrorAt := job.LastErrorAt
	consecutiveFailures := job.ConsecutiveFailures
	job.mu.Unlock()
	run := &pgrepo.JobRun{
		JobName:             job.Name,
		Status:              "running",
		StartedAt:           start.UTC(),
		LastErrorAt:         lastErrorAt,
		ConsecutiveFailures: consecutiveFailures,
	}
	persistCtx, cancel := context.WithTimeout(context.Background(), jobRunPersistenceTimeout)
	defer cancel()
	if err := o.deps.JobRunRepo.Create(persistCtx, run); err != nil {
		return nil, err
	}
	return run, nil
}

func (o *JobOrchestrator) applyRunPersistenceFailure(job *RegisteredJob, at time.Time, persistErr error) error {
	err := fmt.Errorf("automation: persist run state: %w", persistErr)
	job.mu.Lock()
	job.ErrorCount++
	job.LastError = err.Error()
	job.LastErrorAt = &at
	job.ConsecutiveFailures++
	job.LastResult = "failed: run persistence"
	disable := job.ConsecutiveFailures >= autoDisableThreshold
	if disable {
		job.Enabled = false
	}
	job.mu.Unlock()
	if o.metrics != nil {
		o.metrics.RecordAutomationJobError(job.Name)
	}
	if disable {
		o.logger.Error("automation: auto-disabled job after consecutive persistence failures",
			slog.String("job", job.Name),
			slog.Int("consecutive_failures", autoDisableThreshold),
		)
	}
	return err
}

func (o *JobOrchestrator) completeRun(run *pgrepo.JobRun, job *RegisteredJob, completedAt time.Time, elapsed time.Duration, jobErr error) error {
	if o.deps.JobRunRepo == nil || run == nil {
		return nil
	}

	status := "ok"
	var errMsg string
	if jobErr != nil {
		status = "error"
		errMsg = jobErr.Error()
	}

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

	run.Status = status
	completedAt = completedAt.UTC()
	run.CompletedAt = &completedAt
	run.DurationNs = elapsed.Nanoseconds()
	run.Result = result
	run.Error = errMsg
	run.LastErrorAt = lastErrorAt
	run.ConsecutiveFailures = consecutiveFailures

	persistCtx, cancel := context.WithTimeout(context.Background(), jobRunPersistenceTimeout)
	defer cancel()
	return o.deps.JobRunRepo.Complete(persistCtx, run)
}

// hydrateFromDB loads historical run stats from the database to restore
// counters after a server restart.
func (o *JobOrchestrator) hydrateFromDB() {
	o.hydrateJobControls()
	if o.deps.JobRunRepo == nil {
		return
	}
	recoveryAt := time.Now().UTC()
	const recoveryReason = "automation process restarted before the job persisted a terminal outcome"
	recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), jobRunPersistenceTimeout)
	recovered, recoveryErr := o.deps.JobRunRepo.FailIncomplete(recoveryCtx, recoveryAt, recoveryReason)
	recoveryCancel()
	if recoveryErr != nil {
		o.logger.Error("automation: failed to recover incomplete job runs", slog.Any("error", recoveryErr))
		o.disableAllJobs()
		return
	} else if recovered > 0 {
		o.logger.Warn("automation: recovered incomplete job runs", slog.Int("runs", recovered))
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

	summaryCtx, summaryCancel := context.WithTimeout(context.Background(), jobRunPersistenceTimeout)
	summaries, err := o.deps.JobRunRepo.Summaries(summaryCtx)
	summaryCancel()
	if err != nil {
		o.logger.Warn("automation: failed to hydrate job stats from DB", slog.Any("error", err))
		o.disableAllJobs()
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
		if shouldDisableAfterHydration(s.ConsecutiveFailures) {
			job.Enabled = false
		}
		job.mu.Unlock()
	}

	o.logger.Info("automation: hydrated job stats from DB", slog.Int("jobs", len(summaries)))
}

func (o *JobOrchestrator) hydrateJobControls() {
	if o.deps.JobControlRepo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), jobControlPersistenceTimeout)
	defer cancel()
	controls, err := o.deps.JobControlRepo.List(ctx)
	if err != nil {
		o.disableAllJobs()
		o.logger.Error("automation: failed to hydrate durable job controls; all jobs disabled", slog.Any("error", err))
		return
	}
	for _, control := range controls {
		job, ok := o.jobs[control.JobName]
		if !ok {
			continue
		}
		job.mu.Lock()
		job.Enabled = control.Enabled
		job.mu.Unlock()
	}
}

func (o *JobOrchestrator) disableAllJobs() {
	for _, job := range o.jobs {
		job.mu.Lock()
		job.Enabled = false
		job.mu.Unlock()
	}
}

func shouldDisableAfterHydration(consecutiveFailures int) bool {
	return consecutiveFailures >= autoDisableThreshold
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
