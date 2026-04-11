package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestParseEnumParam(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		queryVal   string
		wantOK     bool
		wantStatus int
		wantResult domain.MarketType
	}{
		{"absent param returns true, zero value", "", true, 0, ""},
		{"valid enum sets dst", "stock", true, 0, domain.MarketTypeStock},
		{"invalid enum writes 400", "garbage", false, http.StatusBadRequest, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			q := url.Values{}
			if tc.queryVal != "" {
				q.Set("market_type", tc.queryVal)
			}
			var dst domain.MarketType
			ok := ParseEnumParam(w, q, "market_type", &dst)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK && w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if tc.wantOK && dst != tc.wantResult {
				t.Fatalf("dst = %q, want %q", dst, tc.wantResult)
			}
		})
	}
}

func TestParseUUIDParam(t *testing.T) {
	t.Parallel()

	validID := uuid.New()

	tests := []struct {
		name       string
		queryVal   string
		wantOK     bool
		wantStatus int
	}{
		{"absent param is no-op", "", true, 0},
		{"valid uuid sets dst", validID.String(), true, 0},
		{"invalid uuid writes 400", "not-a-uuid", false, http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			q := url.Values{}
			if tc.queryVal != "" {
				q.Set("id", tc.queryVal)
			}
			var dst *uuid.UUID
			ok := ParseUUIDParam(w, q, "id", &dst)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK && w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if tc.wantOK && tc.queryVal != "" && (dst == nil || *dst != validID) {
				t.Fatalf("dst = %v, want %v", dst, validID)
			}
		})
	}
}

func TestParseTimeParam(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)

	tests := []struct {
		name       string
		queryVal   string
		wantOK     bool
		wantStatus int
	}{
		{"absent param is no-op", "", true, 0},
		{"valid RFC3339 sets dst", now.Format(time.RFC3339), true, 0},
		{"invalid time writes 400", "not-a-time", false, http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			q := url.Values{}
			if tc.queryVal != "" {
				q.Set("ts", tc.queryVal)
			}
			var dst *time.Time
			ok := ParseTimeParam(w, q, "ts", time.RFC3339, &dst)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK && w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if tc.wantOK && tc.queryVal != "" && (dst == nil || !dst.Equal(now)) {
				t.Fatalf("dst = %v, want %v", dst, now)
			}
		})
	}
}
