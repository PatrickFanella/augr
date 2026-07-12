package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestCorrelationIDPropagatesValidUUIDAndReplacesUnsafeValue(t *testing.T) {
	want := uuid.NewString()
	handler := CorrelationID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := CorrelationIDFromContext(r.Context()); got != w.Header().Get(correlationIDHeader) {
			t.Fatalf("context id %q != response id", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, tc := range []struct{ supplied, expected string }{{want, want}, {"bad\nlog=value", ""}} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(correlationIDHeader, tc.supplied)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		got := res.Header().Get(correlationIDHeader)
		if tc.expected != "" && got != tc.expected {
			t.Fatalf("id = %q, want %q", got, tc.expected)
		}
		if _, err := uuid.Parse(got); err != nil {
			t.Fatalf("id = %q, want UUID: %v", got, err)
		}
	}
}
