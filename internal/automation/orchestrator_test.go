package automation

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

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

type stubAutomationMetrics struct {
	alpacaRuns map[string]int
}

func (m *stubAutomationMetrics) RecordAutomationJobError(string) {}

func (m *stubAutomationMetrics) RecordAlpacaReconcileRun(result string) {
	if m.alpacaRuns == nil {
		m.alpacaRuns = make(map[string]int)
	}
	m.alpacaRuns[result]++
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
