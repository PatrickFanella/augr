package api

import (
	"net/http"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func (s *Server) handleListAuditLog(w http.ResponseWriter, r *http.Request) {
	if s.auditLog == nil {
		respondError(w, http.StatusNotImplemented, "audit log not configured", ErrCodeNotImplemented)
		return
	}
	limit, offset := parsePagination(r)
	q := r.URL.Query()

	filter := repository.AuditLogFilter{
		EventType:  q.Get("event_type"),
		EntityType: q.Get("entity_type"),
		Actor:      q.Get("actor"),
	}
	if !ParseUUIDParam(w, q, "entity_id", &filter.EntityID) {
		return
	}
	if !ParseTimeParam(w, q, "after", time.RFC3339Nano, &filter.CreatedAfter) {
		return
	}
	if !ParseTimeParam(w, q, "before", time.RFC3339Nano, &filter.CreatedBefore) {
		return
	}

	entries, err := s.auditLog.Query(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to query audit log", ErrCodeInternal)
		return
	}
	total, err := s.auditLog.Count(r.Context(), filter)
	if err != nil {
		s.logger.Warn("count audit log", "error", err.Error())
	}
	respondListWithTotal(w, entries, total, limit, offset)
}
