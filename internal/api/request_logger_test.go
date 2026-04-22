package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestLoggerSkipsProbePaths(t *testing.T) {
	t.Parallel()

	var logOutput bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logOutput, nil))
	handler := RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, path := range []string{"/health", "/healthz", "/metrics", "/api/v1/automation/status", "/api/v1/strategies"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request to %s returned status %d, want 200", path, rr.Code)
		}
	}

	if strings.TrimSpace(logOutput.String()) != "" {
		t.Fatalf("expected probe paths to be suppressed from logs, got: %s", logOutput.String())
	}
}

func TestRequestLoggerLogsNonSuppressedPath(t *testing.T) {
	t.Parallel()

	var logOutput bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logOutput, nil))
	handler := RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/strategies/123", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("request status = %d, want %d", rr.Code, http.StatusAccepted)
	}

	out := logOutput.String()
	if !strings.Contains(out, "http request") {
		t.Fatalf("expected request log entry, got: %s", out)
	}
	if !strings.Contains(out, `"path":"/api/v1/strategies/123"`) {
		t.Fatalf("expected path field in request log, got: %s", out)
	}
}
