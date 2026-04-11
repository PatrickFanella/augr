package automation

import (
	"context"
	"errors"
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
