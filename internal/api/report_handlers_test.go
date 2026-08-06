package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	pgrepo "github.com/PatrickFanella/get-rich-quick/internal/repository/postgres"
)

// These tests exercise the "not configured" handler path by using the
// default test server setup, where Server.reportArtifacts is left nil.

type stubReportArtifactStore struct {
	latest *pgrepo.ReportArtifact
	err    error
}

func (s *stubReportArtifactStore) List(context.Context, pgrepo.ReportArtifactFilter, int, int) ([]pgrepo.ReportArtifact, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.latest == nil {
		return nil, nil
	}
	return []pgrepo.ReportArtifact{*s.latest}, nil
}

type stubReportMetrics struct {
	calls      int
	strategyID string
	seconds    float64
}

func (s *stubReportMetrics) ObserveReportStaleness(strategyID string, seconds float64) {
	s.calls++
	s.strategyID = strategyID
	s.seconds = seconds
}

func TestHandleGetLatestReport_NotConfigured(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	// reportArtifacts is nil by default → 501
	rr := doRequest(t, srv, http.MethodGet, "/api/v1/strategies/"+stratA.ID.String()+"/reports/latest", nil)
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotImplemented)
	}
}

func TestHandleListReports_NotConfigured(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	rr := doRequest(t, srv, http.MethodGet, "/api/v1/strategies/"+stratA.ID.String()+"/reports", nil)
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotImplemented)
	}
}

func TestHandleGetLatestReport_InvalidUUID(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	rr := doRequest(t, srv, http.MethodGet, "/api/v1/strategies/not-a-uuid/reports/latest", nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestReportLatestResponse_StaleSeconds(t *testing.T) {
	t.Parallel()

	completed := time.Now().Add(-5 * time.Minute)
	resp := reportLatestResponse{
		ReportArtifact: pgrepo.ReportArtifact{
			ID:          uuid.New(),
			StrategyID:  stratA.ID,
			ReportType:  "paper_validation",
			TimeBucket:  time.Now().Truncate(24 * time.Hour),
			Status:      "completed",
			ReportJSON:  json.RawMessage(`{"decision":"GO"}`),
			CompletedAt: &completed,
		},
		StaleSeconds: 300,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	stale, ok := got["stale_seconds"].(float64)
	if !ok {
		t.Fatal("stale_seconds not present in response")
	}
	if stale != 300 {
		t.Fatalf("stale_seconds = %f, want 300", stale)
	}
}

func TestHandleGetLatestReport_RecordsStalenessMetricWithResponseValue(t *testing.T) {
	t.Parallel()

	completed := time.Now().Add(-5 * time.Minute)
	metricsSink := &stubReportMetrics{}
	deps := testDeps()
	deps.ReportArtifacts = &stubReportArtifactStore{
		latest: &pgrepo.ReportArtifact{
			ID:          uuid.New(),
			StrategyID:  stratA.ID,
			ReportType:  "paper_validation",
			TimeBucket:  time.Now().Truncate(24 * time.Hour),
			Status:      "completed",
			ReportJSON:  json.RawMessage(`{"decision":"GO"}`),
			CompletedAt: &completed,
		},
	}
	deps.ReportMetrics = metricsSink
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/strategies/"+stratA.ID.String()+"/reports/latest", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := decodeJSON[reportLatestResponse](t, rr)

	if metricsSink.calls != 1 {
		t.Fatalf("metrics calls = %d, want 1", metricsSink.calls)
	}
	if metricsSink.strategyID != stratA.ID.String() {
		t.Fatalf("metrics strategyID = %q, want %q", metricsSink.strategyID, stratA.ID.String())
	}
	if metricsSink.seconds != resp.StaleSeconds {
		t.Fatalf("metrics stale seconds = %f, want response stale_seconds %f", metricsSink.seconds, resp.StaleSeconds)
	}
}

func TestHandleGetLatestReportReturnsCurrentPendingArtifact(t *testing.T) {
	t.Parallel()

	created := time.Now().Add(-2 * time.Minute)
	deps := testDeps()
	deps.ReportArtifacts = &stubReportArtifactStore{
		latest: &pgrepo.ReportArtifact{
			ID:         uuid.New(),
			StrategyID: stratA.ID,
			ReportType: "paper_validation",
			TimeBucket: time.Now().Truncate(24 * time.Hour),
			Status:     "pending",
			ReportJSON: json.RawMessage(`{"state":"pending","reason":"no_backtest_runs"}`),
			CreatedAt:  created,
		},
	}
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/strategies/"+stratA.ID.String()+"/reports/latest", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := decodeJSON[reportLatestResponse](t, rr)
	if resp.Status != "pending" || resp.CompletedAt != nil {
		t.Fatalf("latest report = %+v, want current pending artifact", resp.ReportArtifact)
	}
	if resp.StaleSeconds < 119 || resp.StaleSeconds > 121 {
		t.Fatalf("stale_seconds = %f, want pending artifact age near 120", resp.StaleSeconds)
	}
}
