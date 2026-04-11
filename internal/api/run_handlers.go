package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/service"
)

// findRunByID looks up a pipeline run by ID. The PipelineRunRepository.Get
// method requires a tradeDate which is not available from the URL, so we
// list runs in pages and scan for a match.
func (s *Server) findRunByID(ctx context.Context, id uuid.UUID) (*domain.PipelineRun, error) {
	offset := 0
	for {
		runs, err := s.runs.List(ctx, repository.PipelineRunFilter{}, maxLimit, offset)
		if err != nil {
			return nil, err
		}
		if len(runs) == 0 {
			return nil, nil
		}
		for i := range runs {
			if runs[i].ID == id {
				return &runs[i], nil
			}
		}
		if len(runs) < maxLimit {
			return nil, nil
		}
		offset += maxLimit
	}
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	q := r.URL.Query()

	filter := repository.PipelineRunFilter{
		Ticker: q.Get("ticker"),
	}

	if !ParseEnumParam(w, q, "status", &filter.Status) {
		return
	}
	if !ParseUUIDParam(w, q, "strategy_id", &filter.StrategyID) {
		return
	}
	if !ParseTimeParam(w, q, "start_date", time.RFC3339Nano, &filter.StartedAfter) {
		return
	}
	if !ParseTimeParam(w, q, "end_date", time.RFC3339Nano, &filter.StartedBefore) {
		return
	}
	if !ParseTimeParam(w, q, "trade_date", time.RFC3339, &filter.TradeDate) {
		return
	}

	runs, err := s.runs.List(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list runs", ErrCodeInternal)
		return
	}
	total, err := s.runs.Count(r.Context(), filter)
	if err != nil {
		s.logger.Warn("count pipeline runs", "error", err.Error())
	}
	respondListWithTotal(w, runs, total, limit, offset)
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	run, err := s.findRunByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get run", ErrCodeInternal)
		return
	}
	if run == nil {
		respondError(w, http.StatusNotFound, "run not found", ErrCodeNotFound)
		return
	}
	respondJSON(w, http.StatusOK, run)
}

func (s *Server) handleGetRunDecisions(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	limit, offset := parsePagination(r)
	q := r.URL.Query()
	includePrompt := q.Get("include_prompt") == "true"

	filter := repository.AgentDecisionFilter{}
	if !ParseEnumParam(w, q, "agent_role", &filter.AgentRole) {
		return
	}
	if !ParseEnumParam(w, q, "phase", &filter.Phase) {
		return
	}

	decisions, err := s.decisions.GetByRun(r.Context(), id, filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get decisions", ErrCodeInternal)
		return
	}

	type decisionResponse struct {
		domain.AgentDecision
		PromptText string `json:"prompt_text,omitempty"`
	}

	responses := make([]decisionResponse, len(decisions))
	for i, d := range decisions {
		resp := decisionResponse{AgentDecision: d}
		if includePrompt {
			resp.PromptText = d.PromptText
		}
		responses[i] = resp
	}

	total, err := s.decisions.CountByRun(r.Context(), id, filter)
	if err != nil {
		s.logger.Warn("count run decisions", "error", err.Error())
	}
	respondListWithTotal(w, responses, total, limit, offset)
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	if err := s.runSvc.Cancel(r.Context(), id); err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "run not found", ErrCodeNotFound)
			return
		}
		if svcErr, ok := err.(*service.ServiceError); ok {
			respondError(w, svcErr.Status, svcErr.Message, ErrCodeBadRequest)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to cancel run", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) handleGetRunSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.snapshots == nil {
		respondError(w, http.StatusNotImplemented, "snapshots not configured", ErrCodeNotImplemented)
		return
	}
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}

	snapshots, err := s.snapshots.GetByRun(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "run not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get snapshot", ErrCodeInternal)
		return
	}

	grouped := make(map[string]json.RawMessage)
	for _, snap := range snapshots {
		grouped[snap.DataType] = snap.Payload
	}

	respondJSON(w, http.StatusOK, grouped)
}
