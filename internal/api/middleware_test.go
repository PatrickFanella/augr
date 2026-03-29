package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeadersMiddleware(t *testing.T) {
	t.Parallel()

	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	tests := []struct {
		header string
		want   string
	}{
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
	}
	for _, tt := range tests {
		if got := rr.Header().Get(tt.header); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func TestMaxRequestBodyMiddleware(t *testing.T) {
	t.Parallel()

	const limit = 16

	handler := MaxRequestBody(limit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, limit+1)
		_, err := r.Body.Read(buf)
		if err != nil {
			respondError(w, http.StatusRequestEntityTooLarge, "body too large", ErrCodeBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Small body should pass through.
	small := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("ok"))
	rrSmall := httptest.NewRecorder()
	handler.ServeHTTP(rrSmall, small)
	if rrSmall.Code != http.StatusOK {
		t.Fatalf("small body: status = %d, want %d", rrSmall.Code, http.StatusOK)
	}

	// Oversized body should be rejected.
	large := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", limit+10)))
	rrLarge := httptest.NewRecorder()
	handler.ServeHTTP(rrLarge, large)
	if rrLarge.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large body: status = %d, want %d", rrLarge.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestSecurityHeadersOnAPIEndpoint(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	rr := doRequest(t, srv, http.MethodGet, "/healthz", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
	if got := rr.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want %q", got, "DENY")
	}
}

func TestMaxRequestBodyOnAPIEndpoint(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	// A request body exceeding 1 MiB should fail.
	oversized := strings.Repeat("x", (1<<20)+100)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/strategies", strings.NewReader(oversized))
	req.Header.Set("Content-Type", "application/json")

	tokenPair, err := srv.auth.GenerateTokenPair("test-user")
	if err != nil {
		t.Fatalf("GenerateTokenPair() error = %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+tokenPair.AccessToken)

	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("oversized body: status = %d, want %d\nbody: %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}
