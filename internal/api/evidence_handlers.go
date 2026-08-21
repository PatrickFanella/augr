package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/evidenceprogram"
)

type milestoneAssessmentResponse struct {
	ID        uuid.UUID                     `json:"id"`
	SHA256    string                        `json:"sha256"`
	Campaign  string                        `json:"campaign"`
	Outcome   evidenceprogram.Outcome       `json:"outcome"`
	Blockers  []string                      `json:"blockers"`
	Parents   []evidenceprogram.EvidenceRef `json:"parents"`
	Canonical json.RawMessage               `json:"canonical"`
}

func (s *Server) handleGetMilestoneAssessment(w http.ResponseWriter, r *http.Request) {
	if s.milestoneEvidence == nil {
		respondError(w, http.StatusNotImplemented, "milestone evidence repository is not configured", ErrCodeNotImplemented)
		return
	}
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	assessment, err := s.milestoneEvidence.GetAssessment(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "milestone assessment not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to reconstruct milestone assessment", ErrCodeInternal)
		return
	}
	record := assessment.Record()
	respondJSON(w, http.StatusOK, milestoneAssessmentResponse{
		ID: record.ID, SHA256: record.SHA256, Campaign: record.Campaign,
		Outcome: record.Outcome, Blockers: record.Blockers, Parents: record.Parents,
		Canonical: record.CanonicalBytes,
	})
}
