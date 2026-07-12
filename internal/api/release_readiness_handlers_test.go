package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/operations"
)

func TestReleaseReadinessHandlerReturnsCapabilityReport(t *testing.T) {
	s := &Server{releaseReadiness: operations.SourceFunc(func(context.Context) (operations.ReadinessReport, error) {
		return operations.BuildReadiness(operations.BuildInput{Database: true, Schema: true, DecisionJournal: true, Scheduler: true, OptionsData: true, PolymarketData: true, PolymarketSettlement: true, KalshiData: true, KalshiSettlement: true, RecoveryDrillsPassed: true}), nil
	})}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/release/readiness", nil)
	res := httptest.NewRecorder()
	s.handleReleaseReadiness(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	if body := res.Body.String(); !strings.Contains(body, `"release_ready":true`) || !strings.Contains(body, `"name":"options"`) {
		t.Fatalf("body = %s", body)
	}
}
