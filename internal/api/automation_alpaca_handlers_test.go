package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/automation"
)

type stubAlpacaAdminReconciler struct {
	summary automation.AlpacaReconcileSummary
	report  automation.AlpacaVerificationReport
	err     error
	calls   int
}

func (s *stubAlpacaAdminReconciler) Reconcile(ctx context.Context) (automation.AlpacaReconcileSummary, error) {
	s.calls++
	return s.summary, s.err
}

func (s *stubAlpacaAdminReconciler) Verify(ctx context.Context) (automation.AlpacaVerificationReport, error) {
	return s.report, s.err
}

func TestRunAlpacaReconcileNowReturnsSummaryAndVerification(t *testing.T) {
	t.Parallel()

	reconciler := &stubAlpacaAdminReconciler{
		summary: automation.AlpacaReconcileSummary{
			OrdersCreated:    2,
			OrdersUpdated:    1,
			PositionsCreated: 1,
			TradesCreated:    3,
		},
		report: automation.AlpacaVerificationReport{
			OrdersChecked:    3,
			PositionsChecked: 1,
			FillsChecked:     3,
		},
	}
	s := &Server{alpacaReconciler: reconciler}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/automation/alpaca/reconcile", nil)
	rr := httptest.NewRecorder()
	s.handleRunAlpacaReconcile(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var resp AlpacaReconcileResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Summary.OrdersCreated != 2 {
		t.Fatalf("OrdersCreated = %d, want 2", resp.Summary.OrdersCreated)
	}
	if resp.Verification.OrdersChecked != 3 {
		t.Fatalf("OrdersChecked = %d, want 3", resp.Verification.OrdersChecked)
	}
	if reconciler.calls != 1 {
		t.Fatalf("Reconcile calls = %d, want 1", reconciler.calls)
	}
}

func TestRunAlpacaReconcileNowRequiresReconciler(t *testing.T) {
	t.Parallel()

	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/automation/alpaca/reconcile", nil)
	rr := httptest.NewRecorder()
	s.handleRunAlpacaReconcile(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
}
