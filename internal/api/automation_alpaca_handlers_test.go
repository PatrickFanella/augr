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
	summary  automation.AlpacaReconcileSummary
	report   automation.AlpacaVerificationReport
	plReport automation.AlpacaPLReconciliationReport
	err      error
	calls    int
}

func (s *stubAlpacaAdminReconciler) Reconcile(ctx context.Context) (automation.AlpacaReconcileSummary, error) {
	s.calls++
	return s.summary, s.err
}

func (s *stubAlpacaAdminReconciler) Verify(ctx context.Context) (automation.AlpacaVerificationReport, error) {
	return s.report, s.err
}

func (s *stubAlpacaAdminReconciler) ReconciliationReport(ctx context.Context) (automation.AlpacaPLReconciliationReport, error) {
	return s.plReport, s.err
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

func TestGetAlpacaReconciliationReportReturnsReadOnlyPayload(t *testing.T) {
	t.Parallel()

	reconciler := &stubAlpacaAdminReconciler{
		plReport: automation.AlpacaPLReconciliationReport{
			BrokerCash:          1250,
			BrokerEquity:        1500,
			LocalClosedPnL:      180,
			LocalOpenPnL:        -20,
			TradeCount:          4,
			FeeTotal:            2.5,
			KnownAdjustments:    0,
			UnexplainedResidual: 87.5,
			AdjustmentDetails:   []string{"no persisted adjustment source discovered"},
		},
	}
	s := &Server{alpacaReconciler: reconciler}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/automation/alpaca/reconciliation", nil)
	rr := httptest.NewRecorder()
	s.handleGetAlpacaReconciliationReport(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp AlpacaReconciliationResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Report.BrokerCash != 1250 || resp.Report.UnexplainedResidual != 87.5 {
		t.Fatalf("unexpected report: %+v", resp.Report)
	}
	if reconciler.calls != 0 {
		t.Fatalf("expected reconciliation report path to be read-only, calls=%d", reconciler.calls)
	}
}
