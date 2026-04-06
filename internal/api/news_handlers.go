package api

import (
	"net/http"
)

// handleListNews returns recent news articles with LLM-derived triage data.
// GET /api/v1/news?limit=50&ticker=AAPL
func (s *Server) handleListNews(w http.ResponseWriter, r *http.Request) {
	if s.newsFeedRepo == nil {
		respondError(w, http.StatusServiceUnavailable, "news feed not configured", ErrCodeInternal)
		return
	}

	limit, _ := parsePagination(r)
	ticker := r.URL.Query().Get("ticker")

	if ticker != "" {
		items, err := s.newsFeedRepo.ListByTicker(r.Context(), ticker, limit)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to list news", ErrCodeInternal)
			return
		}
		respondJSON(w, http.StatusOK, items)
		return
	}

	items, err := s.newsFeedRepo.ListRecent(r.Context(), limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list news", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, items)
}
