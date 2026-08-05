package api

import (
	"net/http"

	"github.com/PatrickFanella/get-rich-quick/internal/polymarketdiscovery"
)

// handleGetPolymarketDiscoveryLast returns the most recent discovery run result.
func (s *Server) handleGetPolymarketDiscoveryLast(w http.ResponseWriter, _ *http.Request) {
	res := polymarketdiscovery.LastResult()
	if res == nil {
		respondJSON(w, http.StatusOK, map[string]any{"last": nil})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"last": res})
}

// handleRunPolymarketDiscovery triggers an immediate discovery run via the
// automation orchestrator. Returns 202 with a status message.
func (s *Server) handleRunPolymarketDiscovery(w http.ResponseWriter, r *http.Request) {
	if s.automation == nil {
		respondError(w, http.StatusServiceUnavailable, "automation not configured", ErrCodeInternal)
		return
	}
	if err := s.automation.RunJob(r.Context(), "polymarket_strategy_discovery"); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error(), ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusAccepted, map[string]string{
		"status":  "started",
		"message": "polymarket strategy discovery run started",
	})
}
