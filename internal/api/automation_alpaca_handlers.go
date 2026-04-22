package api

import (
	"context"
	"net/http"

	"github.com/PatrickFanella/get-rich-quick/internal/automation"
)

type AlpacaAutomationReconciler interface {
	Reconcile(ctx context.Context) (automation.AlpacaReconcileSummary, error)
	Verify(ctx context.Context) (automation.AlpacaVerificationReport, error)
}

type AlpacaReconcileResponse struct {
	Summary      automation.AlpacaReconcileSummary   `json:"summary"`
	Verification automation.AlpacaVerificationReport `json:"verification"`
}

func (s *Server) handleRunAlpacaReconcile(w http.ResponseWriter, r *http.Request) {
	if s.alpacaReconciler == nil {
		respondError(w, http.StatusServiceUnavailable, "alpaca reconciliation not configured", ErrCodeInternal)
		return
	}
	summary, err := s.alpacaReconciler.Reconcile(r.Context())
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error(), ErrCodeInternal)
		return
	}
	verification, err := s.alpacaReconciler.Verify(r.Context())
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error(), ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, AlpacaReconcileResponse{Summary: summary, Verification: verification})
}

func (s *Server) handleVerifyAlpacaReconcile(w http.ResponseWriter, r *http.Request) {
	if s.alpacaReconciler == nil {
		respondError(w, http.StatusServiceUnavailable, "alpaca reconciliation not configured", ErrCodeInternal)
		return
	}
	report, err := s.alpacaReconciler.Verify(r.Context())
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error(), ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, report)
}
