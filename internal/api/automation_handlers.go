package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// AutomationJobHealth is the health snapshot for a single job.
type AutomationJobHealth struct {
	Name                string     `json:"name"`
	Enabled             bool       `json:"enabled"`
	Running             bool       `json:"running"`
	LastRun             *time.Time `json:"last_run,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
	ErrorCount          int        `json:"error_count"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	RunCount            int        `json:"run_count"`
}

// AutomationHealthResponse is the response body for GET /api/v1/automation/health.
type AutomationHealthResponse struct {
	Jobs         []AutomationJobHealth `json:"jobs"`
	Healthy      bool                  `json:"healthy"`
	TotalJobs    int                   `json:"total_jobs"`
	FailingJobs  int                   `json:"failing_jobs"`
	DegradedJobs int                   `json:"degraded_jobs"`
}

// handleGetAutomationStatus returns status for all registered jobs.
// GET /api/v1/automation/status
func (s *Server) handleGetAutomationStatus(w http.ResponseWriter, _ *http.Request) {
	if s.automation == nil {
		respondError(w, http.StatusServiceUnavailable, "automation not configured", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, s.automation.Status())
}

// handleGetAutomationHealth returns health status for all registered jobs.
// GET /api/v1/automation/health
func (s *Server) handleGetAutomationHealth(w http.ResponseWriter, _ *http.Request) {
	if s.automation == nil {
		respondError(w, http.StatusServiceUnavailable, "automation not configured", ErrCodeInternal)
		return
	}

	statuses := s.automation.Status()
	jobs := make([]AutomationJobHealth, 0, len(statuses))
	healthy := true
	failingJobs := 0
	degradedJobs := 0

	for _, st := range statuses {
		if st.ConsecutiveFailures >= 3 {
			healthy = false
			failingJobs++
		} else if st.ConsecutiveFailures >= 1 {
			degradedJobs++
		}
		jobs = append(jobs, AutomationJobHealth{
			Name:                st.Name,
			Enabled:             st.Enabled,
			Running:             st.Running,
			LastRun:             st.LastRun,
			LastError:           st.LastError,
			ErrorCount:          st.ErrorCount,
			ConsecutiveFailures: st.ConsecutiveFailures,
			RunCount:            st.RunCount,
		})
	}

	respondJSON(w, http.StatusOK, AutomationHealthResponse{
		Jobs:         jobs,
		Healthy:      healthy,
		TotalJobs:    len(jobs),
		FailingJobs:  failingJobs,
		DegradedJobs: degradedJobs,
	})
}

// handleListAutomationRuns returns persisted automation job execution history.
// GET /api/v1/automation/runs
func (s *Server) handleListAutomationRuns(w http.ResponseWriter, r *http.Request) {
	if s.jobRunRepo == nil {
		respondError(w, http.StatusServiceUnavailable, "automation run history not configured", ErrCodeInternal)
		return
	}

	limit, offset := parsePagination(r)
	runs, err := s.jobRunRepo.List(r.Context(), limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list automation runs", ErrCodeInternal)
		return
	}
	total, err := s.jobRunRepo.Count(r.Context())
	if err != nil {
		s.logger.Warn("count automation runs", "error", err.Error())
	}
	respondListWithTotal(w, runs, total, limit, offset)
}

// handleRunAutomationJob triggers a specific job by name.
// POST /api/v1/automation/jobs/{name}/run
func (s *Server) handleRunAutomationJob(w http.ResponseWriter, r *http.Request) {
	if s.automation == nil {
		respondError(w, http.StatusServiceUnavailable, "automation not configured", ErrCodeInternal)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		respondError(w, http.StatusBadRequest, "job name is required", ErrCodeBadRequest)
		return
	}

	if err := s.automation.RunJob(r.Context(), name); err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "triggered"})
}

// handleSetAutomationJobEnabled enables or disables a job.
// POST /api/v1/automation/jobs/{name}/enable
// Body: {"enabled": true}
func (s *Server) handleSetAutomationJobEnabled(w http.ResponseWriter, r *http.Request) {
	if s.automation == nil {
		respondError(w, http.StatusServiceUnavailable, "automation not configured", ErrCodeInternal)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		respondError(w, http.StatusBadRequest, "job name is required", ErrCodeBadRequest)
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}

	if err := s.automation.SetEnabled(name, req.Enabled); err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}

	respondJSON(w, http.StatusOK, map[string]bool{"enabled": req.Enabled})
}
