package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func (s *Server) handleRiskStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.risk.GetStatus(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get risk status", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, status)
}

func (s *Server) handleKillSwitchToggle(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Active bool   `json:"active"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}

	if body.Active {
		if body.Reason == "" {
			respondError(w, http.StatusBadRequest, "reason is required when activating kill switch", ErrCodeValidation)
			return
		}
		if err := s.risk.ActivateKillSwitch(r.Context(), body.Reason); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to activate kill switch", ErrCodeInternal)
			return
		}
		s.writeAuditLog(r.Context(), actorOf(r), "kill_switch.activated", "system", nil,
			map[string]string{"reason": body.Reason})
	} else {
		if err := s.risk.DeactivateKillSwitch(r.Context()); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to deactivate kill switch", ErrCodeInternal)
			return
		}
		s.writeAuditLog(r.Context(), actorOf(r), "kill_switch.deactivated", "system", nil, nil)
	}
	respondJSON(w, http.StatusOK, map[string]bool{"active": body.Active})
}

func (s *Server) handleMarketKillSwitch(w http.ResponseWriter, r *http.Request) {
	marketType := domain.MarketType(chi.URLParam(r, "type"))
	if marketType == "" {
		respondError(w, http.StatusBadRequest, "market type is required", ErrCodeBadRequest)
		return
	}

	switch r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:] {
	case "stop":
		var body struct {
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
			return
		}
		if body.Reason == "" {
			respondError(w, http.StatusBadRequest, "reason is required", ErrCodeValidation)
			return
		}
		if err := s.risk.ActivateMarketKillSwitch(r.Context(), marketType, body.Reason); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to activate market kill switch", ErrCodeInternal)
			return
		}
		s.writeAuditLog(r.Context(), actorOf(r), "market_kill_switch.activated", "market", nil,
			map[string]string{"market_type": string(marketType), "reason": body.Reason})
		respondJSON(w, http.StatusOK, map[string]any{"market_type": marketType, "active": true})
	case "resume":
		if err := s.risk.DeactivateMarketKillSwitch(r.Context(), marketType); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to deactivate market kill switch", ErrCodeInternal)
			return
		}
		s.writeAuditLog(r.Context(), actorOf(r), "market_kill_switch.deactivated", "market", nil,
			map[string]string{"market_type": string(marketType)})
		respondJSON(w, http.StatusOK, map[string]any{"market_type": marketType, "active": false})
	default:
		respondError(w, http.StatusNotFound, "unknown action", ErrCodeNotFound)
	}
}
