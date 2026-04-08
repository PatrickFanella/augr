package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/signal"
)

// handleListSignalEvents returns evaluated signal events from the in-memory store.
//
// GET /api/v1/signals/evaluated?min_urgency=1&limit=50&offset=0
func (s *Server) handleListSignalEvents(w http.ResponseWriter, r *http.Request) {
	if s.signalStore == nil {
		respondJSON(w, http.StatusOK, map[string]any{"data": []any{}, "total": 0})
		return
	}

	q := r.URL.Query()
	minUrgency, _ := strconv.Atoi(q.Get("min_urgency"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	events := s.signalStore.ListSignals(minUrgency, limit, offset)
	if events == nil {
		events = []signal.StoredSignal{}
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": events, "total": len(events)})
}

// handleListTriggerLog returns trigger events from the in-memory store.
//
// GET /api/v1/signals/triggers?limit=50&offset=0
func (s *Server) handleListTriggerLog(w http.ResponseWriter, r *http.Request) {
	if s.signalStore == nil {
		respondJSON(w, http.StatusOK, map[string]any{"data": []any{}, "total": 0})
		return
	}

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	triggers := s.signalStore.ListTriggers(limit, offset)
	if triggers == nil {
		triggers = []signal.StoredTrigger{}
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": triggers, "total": len(triggers)})
}

// handleListWatchTerms returns the current watch index terms.
//
// GET /api/v1/signals/watchlist
func (s *Server) handleListWatchTerms(w http.ResponseWriter, r *http.Request) {
	if s.watchIndex == nil {
		respondJSON(w, http.StatusOK, map[string]any{"data": []any{}})
		return
	}
	terms := s.watchIndex.ListTerms()
	if terms == nil {
		terms = []signal.WatchTerm{}
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": terms})
}

// handleAddWatchTerm adds a manual watch term (optionally scoped to a strategy).
//
// POST /api/v1/signals/watchlist
// Body: {"term": "...", "strategy_id": "..."}  (strategy_id optional)
func (s *Server) handleAddWatchTerm(w http.ResponseWriter, r *http.Request) {
	if s.watchIndex == nil {
		respondError(w, http.StatusServiceUnavailable, "signal hub not running", "signal_hub_unavailable")
		return
	}

	var body struct {
		Term       string `json:"term"`
		StrategyID string `json:"strategy_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Term == "" {
		respondError(w, http.StatusBadRequest, "term is required", "invalid_request")
		return
	}

	strategyID := uuid.Nil
	if body.StrategyID != "" {
		id, err := uuid.Parse(body.StrategyID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid strategy_id", "invalid_request")
			return
		}
		strategyID = id
	}
	s.watchIndex.AddManual(body.Term, strategyID)
	respondJSON(w, http.StatusCreated, map[string]string{"term": body.Term})
}

// handleDeleteWatchTerm removes a manual watch term.
//
// DELETE /api/v1/signals/watchlist/{term}
func (s *Server) handleDeleteWatchTerm(w http.ResponseWriter, r *http.Request) {
	if s.watchIndex == nil {
		respondError(w, http.StatusServiceUnavailable, "signal hub not running", "signal_hub_unavailable")
		return
	}
	term := chi.URLParam(r, "term")
	if term == "" {
		respondError(w, http.StatusBadRequest, "term is required", "invalid_request")
		return
	}
	s.watchIndex.RemoveManualTerm(term)
	w.WriteHeader(http.StatusNoContent)
}
