package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/automation"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

// newTestOrchestrator creates a minimal orchestrator with no DB deps.
func newTestOrchestrator() *automation.JobOrchestrator {
	return automation.NewJobOrchestrator(automation.OrchestratorDeps{})
}

// registerJob registers a no-op job on the orchestrator.
func registerJob(o *automation.JobOrchestrator, name string) {
	o.Register(name, "test job", scheduler.ScheduleSpec{Cron: "0 * * * *"},
		func(_ context.Context) error { return nil },
	)
}

// TestAutomationHealth verifies the handler returns 200 with valid JSON schema.
func TestAutomationHealth(t *testing.T) {
	t.Parallel()

	o := newTestOrchestrator()
	s := &Server{automation: o}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/automation/health", nil)
	rr := httptest.NewRecorder()
	s.handleGetAutomationHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp AutomationHealthResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// TestAutomationHealthNilAutomation verifies 503 when automation is nil.
func TestAutomationHealthNilAutomation(t *testing.T) {
	t.Parallel()

	s := &Server{automation: nil}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/automation/health", nil)
	rr := httptest.NewRecorder()
	s.handleGetAutomationHealth(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
}

// TestAutomationHealthAllHealthy verifies healthy=true when all jobs have 0 consecutive failures.
func TestAutomationHealthAllHealthy(t *testing.T) {
	t.Parallel()

	o := newTestOrchestrator()
	registerJob(o, "job-a")
	registerJob(o, "job-b")

	s := &Server{automation: o}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/automation/health", nil)
	rr := httptest.NewRecorder()
	s.handleGetAutomationHealth(rr, req)

	var resp AutomationHealthResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !resp.Healthy {
		t.Errorf("expected healthy=true, got false")
	}
	if resp.TotalJobs != 2 {
		t.Errorf("expected total_jobs=2, got %d", resp.TotalJobs)
	}
	if resp.FailingJobs != 0 {
		t.Errorf("expected failing_jobs=0, got %d", resp.FailingJobs)
	}
}

// TestAutomationHealthUnhealthy verifies healthy=false when any job has >=3 consecutive failures.
func TestAutomationHealthUnhealthy(t *testing.T) {
	t.Parallel()

	o := newTestOrchestrator()
	registerJob(o, "bad-job")
	registerJob(o, "good-job")

	o.SetConsecutiveFailures("bad-job", 3)

	s := &Server{automation: o}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/automation/health", nil)
	rr := httptest.NewRecorder()
	s.handleGetAutomationHealth(rr, req)

	var resp AutomationHealthResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Healthy {
		t.Errorf("expected healthy=false when job has >=3 consecutive failures")
	}
}

// TestAutomationHealthFailingJobsCount verifies failing_jobs and degraded_jobs counts are correct.
func TestAutomationHealthFailingJobsCount(t *testing.T) {
	t.Parallel()

	o := newTestOrchestrator()
	registerJob(o, "job-1")
	registerJob(o, "job-2")
	registerJob(o, "job-3")

	// job-1 and job-3 each have >= 1 but < 3 consecutive failures (degraded, not failing).
	o.SetConsecutiveFailures("job-1", 1)
	o.SetConsecutiveFailures("job-3", 2)

	s := &Server{automation: o}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/automation/health", nil)
	rr := httptest.NewRecorder()
	s.handleGetAutomationHealth(rr, req)

	var resp AutomationHealthResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// No job has >= 3 consecutive failures, so failing_jobs=0 and healthy=true.
	if resp.FailingJobs != 0 {
		t.Errorf("expected failing_jobs=0 (none have >=3 consecutive failures), got %d", resp.FailingJobs)
	}
	if resp.DegradedJobs != 2 {
		t.Errorf("expected degraded_jobs=2 (job-1 and job-3 have 1-2 consecutive failures), got %d", resp.DegradedJobs)
	}
	// Neither has >=3 consecutive failures, so healthy=true.
	if !resp.Healthy {
		t.Errorf("expected healthy=true (no job has >=3 consecutive failures)")
	}
}
