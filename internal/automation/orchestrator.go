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
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
	"github.com/PatrickFanella/get-rich-quick/internal/universe"
)

// OrchestratorDeps bundles external dependencies required by the orchestrator.
type OrchestratorDeps struct {
	Universe     *universe.Universe
	Polygon      *polygon.Client
	DataService  *data.DataService
	LLMProvider  llm.Provider
	StrategyRepo repository.StrategyRepository
	RunRepo      repository.PipelineRunRepository
	Logger       *slog.Logger
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
	jobs   map[string]*RegisteredJob
	cron   *cron.Cron
	deps   OrchestratorDeps
	logger *slog.Logger
}

// NewJobOrchestrator constructs a new orchestrator.
func NewJobOrchestrator(deps OrchestratorDeps) *JobOrchestrator {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &JobOrchestrator{
		jobs:   make(map[string]*RegisteredJob),
		cron:   cron.New(),
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
	o.registerOvernightJobs()
	o.registerWeeklyJobs()
}

// Start starts the cron engine with all registered jobs.
func (o *JobOrchestrator) Start() error {
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

// RunJob triggers a specific job by name immediately.
func (o *JobOrchestrator) RunJob(ctx context.Context, name string) error {
	job, ok := o.jobs[name]
	if !ok {
		return fmt.Errorf("automation: unknown job %q", name)
	}
	o.logger.Info("automation: manual trigger", slog.String("job", name))
	go o.wrapAndRun(job)
	return nil
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
