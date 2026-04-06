package automation

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/data/polygon"
	"github.com/PatrickFanella/get-rich-quick/internal/data/rss"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
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

// OrchestratorDeps bundles external dependencies required by the orchestrator.
type OrchestratorDeps struct {
	Universe        *universe.Universe
	Polygon         *polygon.Client
	DataService     *data.DataService
	OptionsProvider data.OptionsDataProvider
	LLMProvider     llm.Provider
	EventsProvider  data.EventsProvider
	StrategyRepo    repository.StrategyRepository
	RunRepo         repository.PipelineRunRepository
	JobRunRepo      *pgrepo.JobRunRepo
	OptionsScanRepo *pgrepo.OptionsScanRepo
	NewsFeedRepo    *pgrepo.NewsFeedRepo
	Logger          *slog.Logger
}

// RegisteredJob tracks a single automated job and its runtime state.
type RegisteredJob struct {
	Name        string
	Description string
	Schedule    scheduler.ScheduleSpec
	Fn          func(ctx context.Context) error
	DependsOn   []string // job names that must not be running
	mu          sync.Mutex
	LastRun     *time.Time
	LastResult  string
	LastError   string
	RunCount    int
	ErrorCount  int
	Running     bool
	Enabled     bool
}

// JobStatus is the read-only snapshot returned by Status.
type JobStatus struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Schedule    string     `json:"schedule"`
	LastRun     *time.Time `json:"last_run,omitempty"`
	LastResult  string     `json:"last_result"`
	LastError   string     `json:"last_error,omitempty"`
	RunCount    int        `json:"run_count"`
	ErrorCount  int        `json:"error_count"`
	Running     bool       `json:"running"`
	Enabled     bool       `json:"enabled"`
}

// JobOrchestrator is the central registry and cron runner for all automated jobs.
type JobOrchestrator struct {
	jobs          map[string]*RegisteredJob
	cron          *cron.Cron
	deps          OrchestratorDeps
	logger        *slog.Logger
	rssAggregator *rss.Aggregator
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
	o.registerMarketJobs()
	o.registerPreMarketJobs()
	o.registerPostMarketJobs()
	o.registerEventJobs()
	o.registerOvernightJobs()
	o.registerWeeklyJobs()
	o.registerNewsJobs()
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
		s := JobStatus{
			Name:        job.Name,
			Description: job.Description,
			Schedule:    job.Schedule.Describe(),
			LastRun:     job.LastRun,
			LastResult:  job.LastResult,
			LastError:   job.LastError,
			RunCount:    job.RunCount,
			ErrorCount:  job.ErrorCount,
			Running:     job.Running,
			Enabled:     job.Enabled,
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
	job, ok := o.jobs[name]
	if !ok {
		return fmt.Errorf("automation: unknown job %q", name)
	}
	o.logger.Info("automation: manual trigger", slog.String("job", name))
	go o.runDirect(job)
	return nil
}

// runDirect runs a job immediately without checking ShouldFire (for manual triggers).
func (o *JobOrchestrator) runDirect(job *RegisteredJob) {
	job.mu.Lock()
	if job.Running {
		job.mu.Unlock()
		o.logger.Warn("automation: skipping overlapping run", slog.String("job", job.Name))
		return
	}
	job.Running = true
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
		o.logger.Error("automation: job failed", slog.String("job", job.Name), slog.Duration("elapsed", elapsed), slog.Any("error", err))
	} else {
		job.LastResult = "success"
		job.LastError = ""
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
	job.mu.Lock()
	job.Enabled = enabled
	job.mu.Unlock()
	o.logger.Info("automation: job enabled state changed",
		slog.String("job", name),
		slog.Bool("enabled", enabled),
	)
	return nil
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
	job.Running = true
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
		job.LastResult = fmt.Sprintf("error after %s", elapsed.Truncate(time.Millisecond))
	} else {
		job.LastError = ""
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

	run := &pgrepo.JobRun{
		JobName:     jobName,
		Status:      status,
		StartedAt:   start.UTC(),
		CompletedAt: &completed,
		DurationNs:  elapsed.Nanoseconds(),
		Error:       errMsg,
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
		job.RunCount = s.RunCount
		job.ErrorCount = s.ErrorCount
		job.mu.Unlock()
	}

	o.logger.Info("automation: hydrated job stats from DB", slog.Int("jobs", len(summaries)))
}
