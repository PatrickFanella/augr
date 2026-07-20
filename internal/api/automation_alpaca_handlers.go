package api

import (
	"context"
	"net/http"

	"github.com/PatrickFanella/get-rich-quick/internal/automation"
)

type AlpacaAutomationReconciler interface {
	Reconcile(ctx context.Context) (automation.AlpacaReconcileSummary, error)
	Verify(ctx context.Context) (automation.AlpacaVerificationReport, error)
	ReconciliationReport(ctx context.Context) (automation.AlpacaPLReconciliationReport, error)
}

type AlpacaReconcileResponse struct {
	Summary      automation.AlpacaReconcileSummary   `json:"summary"`
	Verification automation.AlpacaVerificationReport `json:"verification"`
}

type AlpacaReconciliationResponse struct {
	Report automation.AlpacaPLReconciliationReport `json:"report"`
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

func (s *Server) handleGetAlpacaReconciliationReport(w http.ResponseWriter, r *http.Request) {
	if s.alpacaReconciler == nil {
		respondError(w, http.StatusServiceUnavailable, "alpaca reconciliation not configured", ErrCodeInternal)
		return
	}
	report, err := s.alpacaReconciler.ReconciliationReport(r.Context())
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error(), ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, AlpacaReconciliationResponse{Report: report})
}
