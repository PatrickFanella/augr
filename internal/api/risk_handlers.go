package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

type RiskBreakerLister interface {
	ListTripped(ctx context.Context) ([]domain.RiskBreakerState, error)
}

func (s *Server) handleRiskStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.risk.GetStatus(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get risk status", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, status)
}

func (s *Server) handleKillSwitchToggle(w http.ResponseWriter, r *http.Request) {
	var body KillSwitchToggleRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	body.Reason = strings.TrimSpace(body.Reason)

	if body.Reason == "" {
		respondError(w, http.StatusBadRequest, "reason is required for kill switch changes", ErrCodeValidation)
		return
	}

	if !body.Active && !s.requireAdminKey(w, r) {
		return
	}

	if body.Active {
		if err := s.risk.ActivateKillSwitch(r.Context(), body.Reason); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to activate kill switch", ErrCodeInternal)
			return
		}
		s.writeAuditLog(r.Context(), actorOf(r), "kill_switch.activated", "system", nil,
			map[string]any{"reason": body.Reason, "requested_active": body.Active})
	} else {
		if err := s.risk.DeactivateKillSwitch(r.Context()); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to deactivate kill switch", ErrCodeInternal)
			return
		}
		s.writeAuditLog(r.Context(), actorOf(r), "kill_switch.deactivated", "system", nil,
			map[string]any{"reason": body.Reason, "requested_active": body.Active})
	}

	status, err := s.risk.GetStatus(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to verify kill switch status", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, KillSwitchToggleResponse{
		Active:      status.KillSwitch.Active,
		Reason:      status.KillSwitch.Reason,
		Mechanisms:  status.KillSwitch.Mechanisms,
		ActivatedAt: status.KillSwitch.ActivatedAt,
		UpdatedAt:   status.UpdatedAt,
	})
}

type KillSwitchToggleRequest struct {
	Active bool   `json:"active"`
	Reason string `json:"reason"`
}

type KillSwitchToggleResponse struct {
	Active      bool                       `json:"active"`
	Reason      string                     `json:"reason,omitempty"`
	Mechanisms  []risk.KillSwitchMechanism `json:"mechanisms,omitempty"`
	ActivatedAt *time.Time                 `json:"activated_at,omitempty"`
	UpdatedAt   time.Time                  `json:"updated_at"`
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

type RiskBreakerResetRequest struct {
	Scope string `json:"scope"`
}
type RiskBreakerResetResponse struct {
	Scope   string `json:"scope"`
	Reset   bool   `json:"reset"`
	Message string `json:"message,omitempty"`
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.requireAdminKey(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAdminKey(w http.ResponseWriter, r *http.Request) bool {
	adminKey := os.Getenv("ADMIN_API_KEY")
	if adminKey == "" {
		respondError(w, http.StatusServiceUnavailable, "ADMIN_API_KEY not configured", ErrCodeNotImplemented)
		return false
	}
	if r.Header.Get("X-Admin-Key") != adminKey {
		respondError(w, http.StatusUnauthorized, "admin key required", ErrCodeUnauthorized)
		return false
	}
	return true
}

func (s *Server) handleRiskBreakerReset(w http.ResponseWriter, r *http.Request) {
	var req RiskBreakerResetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid_body", ErrCodeBadRequest)
		return
	}
	req.Scope = strings.TrimSpace(req.Scope)
	if req.Scope == "" {
		respondError(w, http.StatusBadRequest, "missing_scope", ErrCodeValidation)
		return
	}
	if s.riskBreaker == nil {
		respondError(w, http.StatusServiceUnavailable, "risk breaker not configured", ErrCodeNotImplemented)
		return
	}
	if err := s.riskBreaker.Reset(r.Context(), req.Scope); err != nil && !errors.Is(err, repository.ErrNotFound) {
		respondError(w, http.StatusInternalServerError, "reset_failed", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, RiskBreakerResetResponse{Scope: req.Scope, Reset: true})
}

func (s *Server) handleRiskBreakerList(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.riskBreakerLister == nil {
		respondError(w, http.StatusServiceUnavailable, "risk breaker lister not configured", ErrCodeNotImplemented)
		return
	}
	items, err := s.riskBreakerLister.ListTripped(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list risk breakers", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"tripped": items})
}
