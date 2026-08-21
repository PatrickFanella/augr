package automation

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/eventmarkets"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	kalshiexecution "github.com/PatrickFanella/get-rich-quick/internal/execution/kalshi"
	polymarketexecution "github.com/PatrickFanella/get-rich-quick/internal/execution/polymarket"
	predictionexecution "github.com/PatrickFanella/get-rich-quick/internal/execution/prediction"
	kalshidiscovery "github.com/PatrickFanella/get-rich-quick/internal/kalshidiscovery"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	pgrepo "github.com/PatrickFanella/get-rich-quick/internal/repository/postgres"
	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

func TestJobOrchestratorRunJob_TracksFailureFieldsAndReset(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{})
	shouldFail := true
	orch.Register("job", "test job", schedulerSpecEveryMinute(), func(context.Context) error {
		if shouldFail {
			return errors.New("boom")
		}
		return nil
	})

	if err := orch.RunJob(context.Background(), "job"); err != nil {
		t.Fatalf("RunJob(first) error = %v", err)
	}
	waitForJobRuns(t, orch, "job", 1)

	status := singleJobStatus(t, orch, "job")
	if status.LastResult != "failed" {
		t.Fatalf("LastResult = %q, want failed", status.LastResult)
	}
	if status.LastError != "boom" {
		t.Fatalf("LastError = %q, want boom", status.LastError)
	}
	if status.LastErrorAt == nil {
		t.Fatal("LastErrorAt = nil, want timestamp")
	}
	if status.ConsecutiveFailures != 1 {
		t.Fatalf("ConsecutiveFailures = %d, want 1", status.ConsecutiveFailures)
	}

	shouldFail = false
	if err := orch.RunJob(context.Background(), "job"); err != nil {
		t.Fatalf("RunJob(second) error = %v", err)
	}
	waitForJobRuns(t, orch, "job", 2)

	status = singleJobStatus(t, orch, "job")
	if status.LastResult != "success" {
		t.Fatalf("LastResult = %q, want success", status.LastResult)
	}
	if status.LastError != "" {
		t.Fatalf("LastError = %q, want empty", status.LastError)
	}
	if status.ConsecutiveFailures != 0 {
		t.Fatalf("ConsecutiveFailures = %d, want 0", status.ConsecutiveFailures)
	}
}

func TestJobOrchestrator_ContainsJobPanicsAsFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		run  func(*JobOrchestrator, *RegisteredJob)
	}{
		{
			name: "manual",
			run: func(orch *JobOrchestrator, job *RegisteredJob) {
				orch.runDirect(job)
			},
		},
		{
			name: "scheduled",
			run: func(orch *JobOrchestrator, job *RegisteredJob) {
				orch.wrapAndRun(job)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			orch := NewJobOrchestrator(OrchestratorDeps{})
			orch.Register("job", "panicking job", schedulerSpecEveryMinute(), func(context.Context) error {
				panic("credential-shaped panic value must not be logged")
			})

			tc.run(orch, orch.jobs["job"])

			status := singleJobStatus(t, orch, "job")
			if status.Running {
				t.Fatal("Running = true after panic")
			}
			if status.RunCount != 1 || status.ErrorCount != 1 || status.ConsecutiveFailures != 1 {
				t.Fatalf("failure counters = runs:%d errors:%d consecutive:%d, want 1/1/1",
					status.RunCount, status.ErrorCount, status.ConsecutiveFailures)
			}
			if !strings.Contains(status.LastError, "job panicked (string)") {
				t.Fatalf("LastError = %q, want redacted panic type", status.LastError)
			}
			if strings.Contains(status.LastError, "credential-shaped") {
				t.Fatalf("LastError leaked panic value: %q", status.LastError)
			}
		})
	}
}

func TestJobOrchestratorStatus_IncludesStuckForWhenRunning(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{})
	started := make(chan struct{})
	release := make(chan struct{})
	orch.Register("job", "blocking job", schedulerSpecEveryMinute(), func(context.Context) error {
		close(started)
		<-release
		return nil
	})

	if err := orch.RunJob(context.Background(), "job"); err != nil {
		t.Fatalf("RunJob() error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("job did not start")
	}

	status := singleJobStatus(t, orch, "job")
	if !status.Running {
		t.Fatal("Running = false, want true")
	}
	if status.StuckFor == nil || *status.StuckFor <= 0 {
		t.Fatalf("StuckFor = %v, want > 0", status.StuckFor)
	}

	close(release)
	waitForJobRuns(t, orch, "job", 1)
}

func TestJobOrchestratorPersistsRunningRowBeforeExecuting(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*JobOrchestrator, *RegisteredJob)
	}{
		{name: "manual", run: func(o *JobOrchestrator, job *RegisteredJob) { o.runDirect(job) }},
		{name: "scheduled", run: func(o *JobOrchestrator, job *RegisteredJob) { o.wrapAndRun(job) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := newRecordingAutomationJobRunRepo()
			started := make(chan struct{})
			release := make(chan struct{})
			done := make(chan struct{})
			orch := NewJobOrchestrator(OrchestratorDeps{JobRunRepo: repo})
			orch.Register("job", "durable running job", schedulerSpecEveryMinute(), func(context.Context) error {
				close(started)
				<-release
				return nil
			})

			go func() {
				defer close(done)
				tc.run(orch, orch.jobs["job"])
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("job did not start")
			}

			running := repo.singleRun(t)
			if running.Status != "running" || running.CompletedAt != nil || running.ID == uuid.Nil {
				t.Fatalf("running row = %+v", running)
			}

			close(release)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("job did not finish")
			}
			completed := repo.singleRun(t)
			if completed.ID != running.ID || completed.Status != "ok" || completed.CompletedAt == nil {
				t.Fatalf("completed row = %+v", completed)
			}
		})
	}
}

func TestJobOrchestratorDoesNotExecuteWithoutDurableRunningRow(t *testing.T) {
	t.Parallel()
	repo := newRecordingAutomationJobRunRepo()
	repo.createErr = errors.New("database unavailable")
	executed := false
	orch := NewJobOrchestrator(OrchestratorDeps{JobRunRepo: repo})
	orch.Register("job", "must be durable", schedulerSpecEveryMinute(), func(context.Context) error {
		executed = true
		return nil
	})

	orch.wrapAndRun(orch.jobs["job"])
	if executed {
		t.Fatal("job executed without a durable running row")
	}
	status := singleJobStatus(t, orch, "job")
	if status.Running || status.ErrorCount != 1 || status.ConsecutiveFailures != 1 {
		t.Fatalf("status after start persistence failure = %+v", status)
	}
}

func TestJobOrchestratorJobRunHydrationFailuresDisableAll(t *testing.T) {
	for _, tc := range []struct {
		name string
		prep func(*recordingAutomationJobRunRepo)
	}{
		{name: "orphan recovery", prep: func(r *recordingAutomationJobRunRepo) { r.failIncompleteErr = errors.New("recovery unavailable") }},
		{name: "summary read", prep: func(r *recordingAutomationJobRunRepo) { r.summariesErr = errors.New("summary unavailable") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := newRecordingAutomationJobRunRepo()
			tc.prep(repo)
			orch := NewJobOrchestrator(OrchestratorDeps{JobRunRepo: repo})
			orch.Register("first", "job", schedulerSpecEveryMinute(), func(context.Context) error { return nil })
			orch.Register("second", "job", schedulerSpecEveryMinute(), func(context.Context) error { return nil })
			orch.hydrateFromDB()
			for _, status := range orch.Status() {
				if status.Enabled {
					t.Fatalf("job %s remained enabled after %s failure", status.Name, tc.name)
				}
			}
		})
	}
}

type recordingAutomationJobRunRepo struct {
	mu                sync.Mutex
	runs              []pgrepo.JobRun
	createErr         error
	failIncompleteErr error
	summariesErr      error
}

func newRecordingAutomationJobRunRepo() *recordingAutomationJobRunRepo {
	return &recordingAutomationJobRunRepo{runs: make([]pgrepo.JobRun, 0)}
}

func (r *recordingAutomationJobRunRepo) Create(_ context.Context, run *pgrepo.JobRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return r.createErr
	}
	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	r.runs = append(r.runs, *run)
	return nil
}

func (r *recordingAutomationJobRunRepo) Complete(_ context.Context, run *pgrepo.JobRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.runs {
		if r.runs[i].ID == run.ID {
			r.runs[i] = *run
			return nil
		}
	}
	return repository.ErrNotFound
}

func (r *recordingAutomationJobRunRepo) FailIncomplete(_ context.Context, _ time.Time, _ string) (int, error) {
	return 0, r.failIncompleteErr
}

func (r *recordingAutomationJobRunRepo) Summaries(context.Context) ([]pgrepo.JobRunSummary, error) {
	return nil, r.summariesErr
}

func (r *recordingAutomationJobRunRepo) singleRun(t *testing.T) pgrepo.JobRun {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.runs) != 1 {
		t.Fatalf("job runs = %+v, want one", r.runs)
	}
	return r.runs[0]
}

var _ AutomationJobRunRepository = (*recordingAutomationJobRunRepo)(nil)

func TestJobOrchestratorRunJob_AutoDisablesAfterThreshold(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{})
	orch.Register("job", "always fails", schedulerSpecEveryMinute(), func(context.Context) error {
		return errors.New("boom")
	})
	orch.SetConsecutiveFailures("job", autoDisableThreshold-1)

	if err := orch.RunJob(context.Background(), "job"); err != nil {
		t.Fatalf("RunJob() error = %v", err)
	}
	waitForJobRuns(t, orch, "job", 1)

	status := singleJobStatus(t, orch, "job")
	if status.ConsecutiveFailures != autoDisableThreshold {
		t.Fatalf("ConsecutiveFailures = %d, want %d", status.ConsecutiveFailures, autoDisableThreshold)
	}
	if status.Enabled {
		t.Fatal("Enabled = true, want false after reaching auto-disable threshold")
	}
}

func TestAutoDisableThresholdSurvivesHydration(t *testing.T) {
	t.Parallel()

	if shouldDisableAfterHydration(autoDisableThreshold - 1) {
		t.Fatal("below-threshold failure count should remain enabled")
	}
	if !shouldDisableAfterHydration(autoDisableThreshold) {
		t.Fatal("threshold failure count should hydrate disabled")
	}
}

func TestJobOrchestratorRunJob_RejectsDisabledJob(t *testing.T) {
	t.Parallel()

	var calls int
	orch := NewJobOrchestrator(OrchestratorDeps{})
	orch.Register("job", "disabled job", schedulerSpecEveryMinute(), func(context.Context) error {
		calls++
		return nil
	})
	if err := orch.SetEnabled("job", false); err != nil {
		t.Fatalf("SetEnabled(false) error = %v", err)
	}

	err := orch.RunJob(context.Background(), "job")
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("RunJob() error = %v, want disabled rejection", err)
	}
	time.Sleep(20 * time.Millisecond)
	if calls != 0 {
		t.Fatalf("disabled job calls = %d, want 0", calls)
	}
	if status := singleJobStatus(t, orch, "job"); status.RunCount != 0 {
		t.Fatalf("disabled job RunCount = %d, want 0", status.RunCount)
	}
}

type automationJobControlRepoStub struct {
	controls []domain.AutomationJobControl
	listErr  error
	setErr   error
	writes   []domain.AutomationJobControl
}

func (r *automationJobControlRepoStub) List(context.Context) ([]domain.AutomationJobControl, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return append([]domain.AutomationJobControl(nil), r.controls...), nil
}

func (r *automationJobControlRepoStub) SetEnabled(_ context.Context, name string, enabled bool, actor string) error {
	if r.setErr != nil {
		return r.setErr
	}
	r.writes = append(r.writes, domain.AutomationJobControl{JobName: name, Enabled: enabled, UpdatedBy: actor})
	return nil
}

func TestJobOrchestratorSetEnabledPersistsBeforeMemory(t *testing.T) {
	t.Parallel()

	controls := &automationJobControlRepoStub{setErr: errors.New("database unavailable")}
	orch := NewJobOrchestrator(OrchestratorDeps{JobControlRepo: controls})
	orch.Register("job", "controlled job", schedulerSpecEveryMinute(), func(context.Context) error { return nil })

	err := orch.SetEnabled("job", false)
	if err == nil || !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("SetEnabled() error = %v, want persistence failure", err)
	}
	if status := singleJobStatus(t, orch, "job"); !status.Enabled {
		t.Fatal("job changed in memory after control persistence failed")
	}
}

func TestJobOrchestratorSetEnabledPersistsActor(t *testing.T) {
	t.Parallel()

	controls := &automationJobControlRepoStub{}
	orch := NewJobOrchestrator(OrchestratorDeps{JobControlRepo: controls})
	orch.Register("job", "controlled job", schedulerSpecEveryMinute(), func(context.Context) error { return nil })

	if err := orch.SetEnabledBy(context.Background(), "job", false, "operator@example.com"); err != nil {
		t.Fatalf("SetEnabledBy() error = %v", err)
	}
	if len(controls.writes) != 1 || controls.writes[0].JobName != "job" || controls.writes[0].Enabled || controls.writes[0].UpdatedBy != "operator@example.com" {
		t.Fatalf("control writes = %+v", controls.writes)
	}
	if status := singleJobStatus(t, orch, "job"); status.Enabled {
		t.Fatal("durably disabled job remained enabled in memory")
	}
}

func TestJobOrchestratorHydratesDurableDisabledControl(t *testing.T) {
	t.Parallel()

	controls := &automationJobControlRepoStub{controls: []domain.AutomationJobControl{{JobName: "job", Enabled: false, UpdatedBy: "operator"}}}
	orch := NewJobOrchestrator(OrchestratorDeps{JobControlRepo: controls})
	orch.Register("job", "controlled job", schedulerSpecEveryMinute(), func(context.Context) error { return nil })
	orch.hydrateFromDB()

	if status := singleJobStatus(t, orch, "job"); status.Enabled {
		t.Fatal("durably disabled job hydrated enabled")
	}
}

func TestJobOrchestratorControlHydrationFailureDisablesAll(t *testing.T) {
	t.Parallel()

	controls := &automationJobControlRepoStub{listErr: errors.New("database unavailable")}
	orch := NewJobOrchestrator(OrchestratorDeps{JobControlRepo: controls})
	orch.Register("first", "controlled job", schedulerSpecEveryMinute(), func(context.Context) error { return nil })
	orch.Register("second", "controlled job", schedulerSpecEveryMinute(), func(context.Context) error { return nil })
	orch.hydrateFromDB()

	for _, status := range orch.Status() {
		if status.Enabled {
			t.Fatalf("job %s remained enabled after control hydration failure", status.Name)
		}
	}
}

func TestJobOrchestratorRunJobRejectsOutsideConfiguredSession(t *testing.T) {
	t.Parallel()

	var calls int
	orch := NewJobOrchestrator(OrchestratorDeps{})
	orch.now = func() time.Time {
		return time.Date(2026, time.August, 6, 21, 0, 0, 0, time.UTC) // 5:00 PM ET
	}
	orch.Register("market_job", "market-window job", scheduler.ScheduleSpec{
		Type:         scheduler.ScheduleTypeMarketHours,
		MarketType:   string(domain.MarketTypeStock),
		SkipWeekends: true,
		SkipHolidays: true,
	}, func(context.Context) error {
		calls++
		return nil
	})

	err := orch.RunJob(context.Background(), "market_job")
	if err == nil || !strings.Contains(err.Error(), "outside configured session") {
		t.Fatalf("RunJob() error = %v, want configured-session rejection", err)
	}
	time.Sleep(20 * time.Millisecond)
	if calls != 0 {
		t.Fatalf("job calls = %d, want 0", calls)
	}
}

func TestJobOrchestratorRunJobAllowsRerunInsideConfiguredSession(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	orch := NewJobOrchestrator(OrchestratorDeps{})
	orch.now = func() time.Time {
		return time.Date(2026, time.August, 6, 15, 0, 0, 0, time.UTC) // 11:00 AM ET
	}
	orch.Register("market_job", "market-window job", scheduler.ScheduleSpec{
		Type:         scheduler.ScheduleTypeMarketHours,
		MarketType:   string(domain.MarketTypeStock),
		SkipWeekends: true,
		SkipHolidays: true,
	}, func(context.Context) error {
		close(done)
		return nil
	})

	if err := orch.RunJob(context.Background(), "market_job"); err != nil {
		t.Fatalf("RunJob() error = %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("market-session rerun did not start")
	}
}

func TestJobOrchestratorRunJobRejectsOverlapSynchronously(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	orch := NewJobOrchestrator(OrchestratorDeps{})
	orch.Register("job", "blocking job", schedulerSpecEveryMinute(), func(context.Context) error {
		close(started)
		<-release
		return nil
	})

	if err := orch.RunJob(context.Background(), "job"); err != nil {
		t.Fatalf("first RunJob() error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first run did not start")
	}
	if err := orch.RunJob(context.Background(), "job"); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second RunJob() error = %v, want synchronous overlap rejection", err)
	}
	close(release)
	waitForJobRuns(t, orch, "job", 1)
}

func TestJobOrchestratorRunDirectEnforcesTimeout(t *testing.T) {
	orch := NewJobOrchestrator(OrchestratorDeps{JobTimeout: 10 * time.Millisecond})
	orch.Register("job", "test", schedulerSpecEveryMinute(), func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	orch.runDirect(orch.jobs["job"])
	status := singleJobStatus(t, orch, "job")
	if status.LastResult != "failed" {
		t.Fatalf("LastResult = %q, want failed", status.LastResult)
	}
	if !strings.Contains(status.LastError, context.DeadlineExceeded.Error()) {
		t.Fatalf("LastError = %q, want deadline exceeded", status.LastError)
	}
}

func TestJobOrchestratorWrapAndRun_AutoDisabledJobsAreSkipped(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{})
	orch.Register("job", "always fails", schedulerSpecEveryMinute(), func(context.Context) error {
		return errors.New("boom")
	})
	orch.SetConsecutiveFailures("job", autoDisableThreshold-1)

	job := orch.jobs["job"]
	orch.wrapAndRun(job)

	status := singleJobStatus(t, orch, "job")
	if status.ConsecutiveFailures != autoDisableThreshold {
		t.Fatalf("ConsecutiveFailures = %d, want %d", status.ConsecutiveFailures, autoDisableThreshold)
	}
	if status.Enabled {
		t.Fatal("Enabled = true, want false after reaching auto-disable threshold")
	}
	if status.RunCount != 1 {
		t.Fatalf("RunCount after first run = %d, want 1", status.RunCount)
	}

	orch.wrapAndRun(job)
	status = singleJobStatus(t, orch, "job")
	if status.RunCount != 1 {
		t.Fatalf("RunCount after disabled scheduled invocation = %d, want 1", status.RunCount)
	}
}

func TestDependencyBlockerRequiresSuccessfulSameDayRun(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{})
	orch.Register("upstream", "upstream", schedulerSpecEveryMinute(), func(context.Context) error { return nil })
	orch.Register("consumer", "consumer", schedulerSpecEveryMinute(), func(context.Context) error { return nil }, "upstream")
	now := time.Date(2026, time.August, 6, 8, 30, 0, 0, easternTime)
	upstream := orch.jobs["upstream"]
	consumer := orch.jobs["consumer"]

	if dep, reason := orch.dependencyBlocker(consumer, now); dep != "upstream" || !strings.Contains(reason, "has not completed") {
		t.Fatalf("never-run blocker = (%q, %q)", dep, reason)
	}
	priorDay := now.AddDate(0, 0, -1)
	upstream.LastRun = &priorDay
	upstream.LastResult = "ok"
	if dep, reason := orch.dependencyBlocker(consumer, now); dep != "upstream" || !strings.Contains(reason, "prior") {
		t.Fatalf("prior-day blocker = (%q, %q)", dep, reason)
	}
	upstream.LastRun = &now
	upstream.LastResult = "error"
	if dep, reason := orch.dependencyBlocker(consumer, now); dep != "upstream" || !strings.Contains(reason, "not successful") {
		t.Fatalf("failed blocker = (%q, %q)", dep, reason)
	}
	upstream.LastResult = "ok in 2s"
	if dep, reason := orch.dependencyBlocker(consumer, now); dep != "" || reason != "" {
		t.Fatalf("successful blocker = (%q, %q), want none", dep, reason)
	}
}

type stubAutomationMetrics struct {
	alpacaRuns     map[string]int
	kalshiDryRuns  map[string]int
	kalshiOutcomes map[string]int
	transitions    []string
}

func (m *stubAutomationMetrics) RecordAutomationJobError(string) {}

func (m *stubAutomationMetrics) RecordAlpacaReconcileRun(result string) {
	if m.alpacaRuns == nil {
		m.alpacaRuns = make(map[string]int)
	}
	m.alpacaRuns[result]++
}

func (m *stubAutomationMetrics) RecordKalshiReconcileRun(result string) {
	m.RecordAlpacaReconcileRun("kalshi_" + result)
}

func (m *stubAutomationMetrics) RecordKalshiSettlementDryRun(result string) {
	if m.kalshiDryRuns == nil {
		m.kalshiDryRuns = make(map[string]int)
	}
	m.kalshiDryRuns[result]++
}

func (m *stubAutomationMetrics) RecordKalshiSettlementOutcome(result string) {
	if m.kalshiOutcomes == nil {
		m.kalshiOutcomes = make(map[string]int)
	}
	m.kalshiOutcomes[result]++
}

func (m *stubAutomationMetrics) RecordKalshiSettlementTransition(from, to string) {
	m.transitions = append(m.transitions, from+"->"+to)
}

func TestJobOrchestratorStatus_IncludesLastSummary(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{})
	orch.Register("alpaca_reconcile", "test job", schedulerSpecEveryMinute(), func(context.Context) error { return nil })
	orch.SetLastSummary("alpaca_reconcile", map[string]int{"orders_created": 2, "trades_created": 3})

	status := singleJobStatus(t, orch, "alpaca_reconcile")
	if status.LastSummary == nil {
		t.Fatal("LastSummary = nil, want populated")
	}
	if status.LastSummary["orders_created"] != 2 {
		t.Fatalf("orders_created = %d, want 2", status.LastSummary["orders_created"])
	}
	status.LastSummary["orders_created"] = 99
	statusAgain := singleJobStatus(t, orch, "alpaca_reconcile")
	if statusAgain.LastSummary["orders_created"] != 2 {
		t.Fatalf("mutated summary leaked into orchestrator: %d", statusAgain.LastSummary["orders_created"])
	}
}

func TestJobOrchestratorRegisterAllAddsCurrentDataRefreshBeforeHotScan(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{})
	orch.RegisterAll()

	if _, ok := orch.jobs["current_data_refresh"]; !ok {
		t.Fatal("current_data_refresh job not registered")
	}
	hotScan, ok := orch.jobs["hot_scan"]
	if !ok {
		t.Fatal("hot_scan job not registered")
	}
	if len(hotScan.DependsOn) != 1 || hotScan.DependsOn[0] != "current_data_refresh" {
		t.Fatalf("hot_scan depends_on = %#v, want [current_data_refresh]", hotScan.DependsOn)
	}
}

func TestJobOrchestratorRegisterAllAddsPolymarketReconcile(t *testing.T) {
	t.Parallel()

	reconciler := polymarketexecution.NewReconciler(polymarketexecution.ReconcilerDeps{
		Broker: &polymarketBrokerStub{positions: []domain.Position{{Ticker: "market-one:YES", Side: domain.PositionSideLong, Quantity: 10}}},
		PositionRepo: &polymarketPositionRepoStub{positions: []domain.Position{{
			MarketType: domain.MarketTypePolymarket,
			Ticker:     "market-one",
			Side:       domain.PositionSideLong,
			Quantity:   10,
		}}},
		AuditLogRepo: &polymarketAuditRepoStub{},
		Metrics:      &polymarketReconcilerMetricsStub{},
		Logger:       slog.Default(),
	})
	orch := NewJobOrchestrator(OrchestratorDeps{PolymarketReconciler: reconciler})
	orch.RegisterAll()

	status := singleJobStatus(t, orch, "polymarket_reconcile")
	if status.Schedule == "" {
		t.Fatal("polymarket_reconcile schedule is empty")
	}

	if err := orch.RunJob(context.Background(), "polymarket_reconcile"); err != nil {
		t.Fatalf("RunJob() error = %v", err)
	}
	waitForJobRuns(t, orch, "polymarket_reconcile", 1)

	status = singleJobStatus(t, orch, "polymarket_reconcile")
	if status.LastResult != "success" {
		t.Fatalf("LastResult = %q, want success", status.LastResult)
	}
	if status.LastSummary == nil || status.LastSummary["drifts"] != 0 {
		t.Fatalf("LastSummary = %#v, want drifts=0", status.LastSummary)
	}
}

func TestJobOrchestratorRegisterAllCanDisablePolymarketAutomation(t *testing.T) {
	t.Parallel()

	reconciler := polymarketexecution.NewReconciler(polymarketexecution.ReconcilerDeps{
		Broker:       &polymarketBrokerStub{},
		PositionRepo: &polymarketPositionRepoStub{},
		AuditLogRepo: &polymarketAuditRepoStub{},
		Metrics:      &polymarketReconcilerMetricsStub{},
		Logger:       slog.Default(),
	})
	orch := NewJobOrchestrator(OrchestratorDeps{
		PolymarketReconciler:        reconciler,
		DisablePolymarketAutomation: true,
	})
	orch.RegisterAll()

	for _, name := range []string{"polymarket_profiles", "polymarket_reconcile", "polymarket_resolutions", "polymarket_strategy_discovery"} {
		if _, ok := orch.jobs[name]; ok {
			t.Fatalf("job %q registered with DisablePolymarketAutomation=true", name)
		}
	}
}

func TestJobOrchestratorRegisterAllAddsKalshiDiscovery(t *testing.T) {
	t.Parallel()

	origRun := kalshiDiscoveryRun
	defer func() { kalshiDiscoveryRun = origRun }()

	var gotCfg kalshidiscovery.Config
	var gotDeps kalshidiscovery.Deps
	kalshiDiscoveryRun = func(_ context.Context, cfg kalshidiscovery.Config, deps kalshidiscovery.Deps) (*kalshidiscovery.Result, error) {
		gotCfg = cfg
		gotDeps = deps
		return &kalshidiscovery.Result{FetchedAll: 50, Screened: 15, Proposed: 2, Skipped: 1, Deployed: []kalshidiscovery.DeployedStrategy{{Reused: true}}, Errors: []string{"snapshot failed"}}, nil
	}

	orch := NewJobOrchestrator(OrchestratorDeps{
		StrategyRepo:              &kalshiStrategyRepoStub{},
		KalshiCatalog:             kalshiCatalogStub{},
		KalshiWatchedRepo:         &kalshiWatchedRepoStub{},
		KalshiMarketSnapshotsRepo: &kalshiSnapshotsRepoStub{},
		KalshiDiscoveryRuns:       &kalshiDiscoveryRunsRepoStub{},
	})
	orch.RegisterAll()

	status := singleJobStatus(t, orch, "kalshi_discovery")
	if status.Schedule == "" || !strings.Contains(status.Schedule, "15 * * * *") {
		t.Fatalf("kalshi_discovery schedule = %q, want cron 15 * * * *", status.Schedule)
	}

	if err := orch.RunJob(context.Background(), "kalshi_discovery"); err != nil {
		t.Fatalf("RunJob() error = %v", err)
	}
	waitForJobRuns(t, orch, "kalshi_discovery", 1)
	status = singleJobStatus(t, orch, "kalshi_discovery")
	if status.LastSummary["fetched"] != 50 || status.LastSummary["screened"] != 15 || status.LastSummary["deployed"] != 1 || status.LastSummary["created"] != 0 || status.LastSummary["reused"] != 1 || status.LastSummary["errors"] != 1 {
		t.Fatalf("Kalshi discovery summary = %#v, want persisted result counts", status.LastSummary)
	}

	if gotCfg.DryRun {
		t.Fatal("Kalshi discovery cfg.DryRun = true, want false")
	}
	if gotCfg.FetchLimit != 50 || gotCfg.MaxDeployments != 1 || gotCfg.MinConviction != 0.70 {
		t.Fatalf("Kalshi discovery cfg = %#v, want conservative paper settings", gotCfg)
	}
	if gotCfg.Screener.MaxCandidates != 15 || gotCfg.Screener.MinVolume != 1000 || gotCfg.Screener.MinOpenInterest != 500 || gotCfg.Screener.MaxSpreadPct != 12 || gotCfg.Screener.MinDaysToClose != 3 {
		t.Fatalf("Kalshi discovery screener = %#v, want default conservative config", gotCfg.Screener)
	}
	if gotDeps.Catalog == nil || gotDeps.Strategies == nil || gotDeps.Watched == nil || gotDeps.Snapshots == nil || gotDeps.DiscoveryRuns == nil {
		t.Fatalf("Kalshi discovery deps = %#v, want all persistence dependencies wired", gotDeps)
	}
	if gotDeps.Logger == nil {
		t.Fatal("Kalshi discovery logger = nil")
	}
}

func TestJobOrchestratorRegisterAllAddsKalshiSettlement(t *testing.T) {
	t.Parallel()
	orch := NewJobOrchestrator(OrchestratorDeps{
		KalshiCatalog:     kalshiCatalogStub{},
		PredictionSettler: predictionexecution.NewSettler(nil, nil, nil, nil, nil),
	})
	orch.RegisterAll()
	status := singleJobStatus(t, orch, "kalshi_settlement")
	if status.Schedule == "" || status.Schedule == "Manual only" {
		t.Fatalf("kalshi settlement schedule = %q", status.Schedule)
	}
	if !status.Enabled {
		t.Fatal("kalshi_settlement enabled = false, want preview scheduler active")
	}
}

func TestJobOrchestratorRegisterAllAddsKalshiReconciliation(t *testing.T) {
	t.Parallel()
	orch := NewJobOrchestrator(OrchestratorDeps{KalshiReconciler: kalshiexecution.NewReconciler(kalshiexecution.ReconcilerDeps{})})
	orch.RegisterAll()
	status := singleJobStatus(t, orch, "kalshi_reconcile")
	if status.Schedule == "" || status.Schedule == "Manual only" {
		t.Fatalf("kalshi reconciliation schedule = %q", status.Schedule)
	}
}

type kalshiSettlementGateStub struct {
	state       *domain.KalshiSettlementGateState
	err         error
	failPersist bool
}

func (s *kalshiSettlementGateStub) Get(context.Context, string) (*domain.KalshiSettlementGateState, error) {
	return s.state, s.err
}

func (s *kalshiSettlementGateStub) RecordSuccess(context.Context, string, int, int, int, int, int, string, time.Time) (*domain.KalshiSettlementGateState, error) {
	if s.failPersist {
		return nil, errors.New("persist failed")
	}
	return s.state, nil
}

func (s *kalshiSettlementGateStub) RecordFailure(context.Context, string, int, int, int, int, int, time.Time, string) (*domain.KalshiSettlementGateState, error) {
	if s.failPersist {
		return nil, errors.New("persist failed")
	}
	return s.state, nil
}

func TestJobOrchestratorSetEnabledKalshiSettlementDoesNotRequireEligibility(t *testing.T) {
	orch := NewJobOrchestrator(OrchestratorDeps{KalshiSettlementGateRepo: &kalshiSettlementGateStub{state: &domain.KalshiSettlementGateState{Eligible: false}}})
	orch.Register("kalshi_settlement", "test", schedulerSpecEveryMinute(), func(context.Context) error { return nil })
	if err := orch.SetEnabled("kalshi_settlement", true); err != nil {
		t.Fatalf("SetEnabled(true) error = %v, preview scheduling must not require eligibility", err)
	}
	orch.deps.KalshiSettlementGateRepo = &kalshiSettlementGateStub{state: &domain.KalshiSettlementGateState{Eligible: true}}
	if err := orch.SetEnabled("kalshi_settlement", false); err != nil {
		t.Fatalf("SetEnabled(false) error = %v", err)
	}
}

func TestJobOrchestratorSetEnabledKalshiSettlementAllowsPreviewAfterGateLoadError(t *testing.T) {
	orch := NewJobOrchestrator(OrchestratorDeps{KalshiSettlementGateRepo: &kalshiSettlementGateStub{err: errors.New("boom")}})
	orch.Register("kalshi_settlement", "test", schedulerSpecEveryMinute(), func(context.Context) error { return nil })
	if err := orch.SetEnabled("kalshi_settlement", true); err != nil {
		t.Fatalf("SetEnabled(true) error = %v, job itself enforces the mutation gate", err)
	}
}

func TestJobOrchestratorSetEnabledKalshiSettlementDoesNotUseHealthLatchForScheduling(t *testing.T) {
	orch := NewJobOrchestrator(OrchestratorDeps{KalshiSettlementGateRepo: &kalshiSettlementGateStub{state: &domain.KalshiSettlementGateState{Eligible: true}}})
	orch.Register("kalshi_settlement", "test", schedulerSpecEveryMinute(), func(context.Context) error { return nil })
	orch.kalshiGateUnhealthy = true
	if err := orch.SetEnabled("kalshi_settlement", false); err != nil {
		t.Fatalf("SetEnabled(false) error = %v", err)
	}
	if err := orch.SetEnabled("kalshi_settlement", true); err != nil {
		t.Fatalf("SetEnabled(true) error = %v, preview scheduler should recover health", err)
	}
}

func TestStrategyResweepSkipsKalshiBeforeOHLCVDownload(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{
		StrategyRepo: &kalshiStrategyRepoStub{strategies: []domain.Strategy{
			{
				ID:         uuid.New(),
				Name:       "auto: kalshi KXMENWORLDCUP-26-US",
				Ticker:     "KXMENWORLDCUP-26-US",
				MarketType: domain.MarketTypeKalshi,
				Status:     domain.StrategyStatusActive,
				IsPaper:    true,
			},
		}},
	})

	if err := orch.strategyResweep(context.Background()); err != nil {
		t.Fatalf("strategyResweep() error = %v", err)
	}
}

func TestSupportsOHLCVResweep(t *testing.T) {
	t.Parallel()

	for _, marketType := range []domain.MarketType{domain.MarketTypeStock, domain.MarketTypeCrypto} {
		if !eventmarkets.SupportsOHLCVResweep(marketType) {
			t.Fatalf("SupportsOHLCVResweep(%q) = false, want true", marketType)
		}
	}
	for _, marketType := range []domain.MarketType{domain.MarketTypeKalshi, domain.MarketTypePolymarket, domain.MarketTypeOptions} {
		if eventmarkets.SupportsOHLCVResweep(marketType) {
			t.Fatalf("SupportsOHLCVResweep(%q) = true, want false", marketType)
		}
	}
}

func TestJobOrchestratorAlpacaReconcileRecordsMetricsAndSummary(t *testing.T) {
	t.Parallel()

	metrics := &stubAutomationMetrics{}
	orch := NewJobOrchestrator(OrchestratorDeps{Logger: slog.Default()})
	orch.WithJobMetrics(metrics)
	orch.Register("alpaca_reconcile", "test job", schedulerSpecEveryMinute(), func(context.Context) error {
		orch.SetLastSummary("alpaca_reconcile", map[string]int{"orders_created": 1})
		metrics.RecordAlpacaReconcileRun("success")
		return nil
	})

	if err := orch.RunJob(context.Background(), "alpaca_reconcile"); err != nil {
		t.Fatalf("RunJob() error = %v", err)
	}
	waitForJobRuns(t, orch, "alpaca_reconcile", 1)

	status := singleJobStatus(t, orch, "alpaca_reconcile")
	if status.LastSummary == nil || status.LastSummary["orders_created"] != 1 {
		t.Fatalf("LastSummary = %#v, want orders_created=1", status.LastSummary)
	}
	if metrics.alpacaRuns["success"] != 1 {
		t.Fatalf("alpaca success runs = %d, want 1", metrics.alpacaRuns["success"])
	}
}

func waitForJobRuns(t *testing.T, orch *JobOrchestrator, jobName string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := singleJobStatus(t, orch, jobName)
		if status.RunCount >= want && !status.Running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach run_count=%d", jobName, want)
}

func singleJobStatus(t *testing.T, orch *JobOrchestrator, jobName string) JobStatus {
	t.Helper()
	for _, status := range orch.Status() {
		if status.Name == jobName {
			return status
		}
	}
	t.Fatalf("job status %q not found", jobName)
	return JobStatus{}
}

func schedulerSpecEveryMinute() scheduler.ScheduleSpec {
	return scheduler.ScheduleSpec{Cron: "* * * * *", Type: scheduler.ScheduleTypeCron}
}

type kalshiCatalogStub struct{}

func (kalshiCatalogStub) ListMarkets(context.Context, kalshidiscovery.ListOptions) ([]kalshidiscovery.MarketCandidate, string, error) {
	return nil, "", nil
}

func (kalshiCatalogStub) GetMarket(context.Context, string) (*kalshidiscovery.MarketCandidate, error) {
	return nil, nil
}

type kalshiStrategyRepoStub struct {
	strategies []domain.Strategy
}

func (s *kalshiStrategyRepoStub) Create(context.Context, *domain.Strategy) error { return nil }
func (s *kalshiStrategyRepoStub) Get(context.Context, uuid.UUID) (*domain.Strategy, error) {
	return nil, repository.ErrNotFound
}

func (s *kalshiStrategyRepoStub) List(context.Context, repository.StrategyFilter, int, int) ([]domain.Strategy, error) {
	return append([]domain.Strategy(nil), s.strategies...), nil
}

func (s *kalshiStrategyRepoStub) Count(context.Context, repository.StrategyFilter) (int, error) {
	return 0, nil
}
func (s *kalshiStrategyRepoStub) Update(context.Context, *domain.Strategy) error { return nil }
func (s *kalshiStrategyRepoStub) Delete(context.Context, uuid.UUID) error        { return nil }
func (s *kalshiStrategyRepoStub) UpdateThesis(context.Context, uuid.UUID, json.RawMessage) error {
	return nil
}

func (s *kalshiStrategyRepoStub) GetThesisRaw(context.Context, uuid.UUID) (json.RawMessage, error) {
	return nil, nil
}

type kalshiWatchedRepoStub struct{}

func (s *kalshiWatchedRepoStub) Upsert(context.Context, *domain.KalshiWatchedMarket) error {
	return nil
}
func (s *kalshiWatchedRepoStub) SetEnabled(context.Context, string, bool) error { return nil }
func (s *kalshiWatchedRepoStub) ListEnabled(context.Context) ([]domain.KalshiWatchedMarket, error) {
	return nil, nil
}

type kalshiSnapshotsRepoStub struct{}

func (s *kalshiSnapshotsRepoStub) Create(context.Context, *domain.KalshiMarketSnapshot) error {
	return nil
}

func (s *kalshiSnapshotsRepoStub) ListLatestByTicker(context.Context, string, int) ([]domain.KalshiMarketSnapshot, error) {
	return nil, nil
}

func (s *kalshiSnapshotsRepoStub) ListRecent(context.Context, int) ([]domain.KalshiMarketSnapshot, error) {
	return nil, nil
}

type kalshiDiscoveryRunsRepoStub struct{}

func (s *kalshiDiscoveryRunsRepoStub) Create(context.Context, *domain.KalshiDiscoveryRun) error {
	return nil
}

func (s *kalshiDiscoveryRunsRepoStub) GetActive(context.Context) (*domain.KalshiDiscoveryRun, error) {
	return nil, repository.ErrNotFound
}

func (s *kalshiDiscoveryRunsRepoStub) Finish(context.Context, *domain.KalshiDiscoveryRun) error {
	return nil
}

func (s *kalshiDiscoveryRunsRepoStub) ListLatest(context.Context, int) ([]domain.KalshiDiscoveryRun, error) {
	return nil, nil
}

type polymarketBrokerStub struct {
	positions []domain.Position
}

func (s *polymarketBrokerStub) SubmitOrder(context.Context, *domain.Order) (string, error) {
	return "", nil
}

func (s *polymarketBrokerStub) CancelOrder(context.Context, string) error { return nil }

func (s *polymarketBrokerStub) GetOrderStatus(context.Context, string) (domain.OrderStatus, error) {
	return "", nil
}

func (s *polymarketBrokerStub) GetPositions(context.Context) ([]domain.Position, error) {
	return append([]domain.Position(nil), s.positions...), nil
}

func (s *polymarketBrokerStub) GetAccountBalance(context.Context) (execution.Balance, error) {
	return execution.Balance{}, nil
}

type polymarketPositionRepoStub struct {
	positions []domain.Position
}

func (s *polymarketPositionRepoStub) Create(context.Context, *domain.Position) error { return nil }
func (s *polymarketPositionRepoStub) CreateAlpacaOwned(context.Context, *domain.Position) error {
	return nil
}

func (s *polymarketPositionRepoStub) Get(context.Context, uuid.UUID) (*domain.Position, error) {
	return nil, repository.ErrNotFound
}

func (s *polymarketPositionRepoStub) List(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}

func (s *polymarketPositionRepoStub) Count(context.Context, repository.PositionFilter) (int, error) {
	return 0, nil
}
func (s *polymarketPositionRepoStub) Update(context.Context, *domain.Position) error { return nil }
func (s *polymarketPositionRepoStub) Delete(context.Context, uuid.UUID) error        { return nil }
func (s *polymarketPositionRepoStub) GetOpen(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return append([]domain.Position(nil), s.positions...), nil
}

func (s *polymarketPositionRepoStub) ListOpenAlpacaOwned(context.Context, int, int) ([]domain.Position, error) {
	return nil, nil
}

func (s *polymarketPositionRepoStub) CountOpen(context.Context, repository.PositionFilter) (int, error) {
	return len(s.positions), nil
}

func (s *polymarketPositionRepoStub) GetByStrategy(context.Context, uuid.UUID, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}

type polymarketAuditRepoStub struct{}

func (s *polymarketAuditRepoStub) Create(context.Context, *domain.AuditLogEntry) error { return nil }

func (s *polymarketAuditRepoStub) Query(context.Context, repository.AuditLogFilter, int, int) ([]domain.AuditLogEntry, error) {
	return nil, nil
}

func (s *polymarketAuditRepoStub) Count(context.Context, repository.AuditLogFilter) (int, error) {
	return 0, nil
}

type polymarketReconcilerMetricsStub struct{}

func (s *polymarketReconcilerMetricsStub) IncDrift(string) {}
