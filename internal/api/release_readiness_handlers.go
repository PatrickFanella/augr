package api

import (
	"net/http"
)

func (s *Server) handleReleaseReadiness(w http.ResponseWriter, r *http.Request) {
	if s.releaseReadiness == nil {
		respondError(w, http.StatusServiceUnavailable, "release readiness unavailable", ErrCodeInternal)
		return
	}
	report, err := s.releaseReadiness.Readiness(r.Context())
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, "release readiness check failed", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, report)
}
