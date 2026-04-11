package api

import (
	"net/http"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	if s.events == nil {
		respondError(w, http.StatusNotImplemented, "events not configured", ErrCodeNotImplemented)
		return
	}
	limit, offset := parsePagination(r)
	q := r.URL.Query()

	filter := repository.AgentEventFilter{
		EventKind: q.Get("event_kind"),
	}
	if !ParseUUIDParam(w, q, "pipeline_run_id", &filter.PipelineRunID) {
		return
	}
	if !ParseUUIDParam(w, q, "strategy_id", &filter.StrategyID) {
		return
	}
	if !ParseEnumParam(w, q, "agent_role", &filter.AgentRole) {
		return
	}
	if !ParseTimeParam(w, q, "after", time.RFC3339Nano, &filter.CreatedAfter) {
		return
	}
	if !ParseTimeParam(w, q, "before", time.RFC3339Nano, &filter.CreatedBefore) {
		return
	}

	events, err := s.events.List(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list events", ErrCodeInternal)
		return
	}
	total, err := s.events.Count(r.Context(), filter)
	if err != nil {
		s.logger.Warn("count events", "error", err.Error())
	}
	respondListWithTotal(w, events, total, limit, offset)
}
