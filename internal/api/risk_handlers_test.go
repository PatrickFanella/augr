package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

type fakeRiskBreaker struct {
	resetErr   error
	resetCalls []string
}

type fakeKillSwitchRiskEngine struct {
	active           bool
	reason           string
	activateReasons  []string
	deactivateCalls  int
	verificationTime time.Time
}

type fakeRiskBreakerLister struct{ items []domain.RiskBreakerState }

func (f *fakeRiskBreakerLister) ListTripped(_ context.Context) ([]domain.RiskBreakerState, error) {
	return f.items, nil
}

func (f *fakeRiskBreaker) Allow(_ context.Context, _ string) error            { return nil }
func (f *fakeRiskBreaker) Trip(_ context.Context, scope, reason string) error { return nil }
func (f *fakeRiskBreaker) Reset(_ context.Context, scope string) error {
	f.resetCalls = append(f.resetCalls, scope)
	return f.resetErr
}

func (f *fakeKillSwitchRiskEngine) CheckPreTrade(context.Context, *domain.Order, risk.Portfolio) (bool, string, error) {
	return true, "", nil
}
func (f *fakeKillSwitchRiskEngine) CheckPositionLimits(context.Context, string, float64, risk.Portfolio) (bool, string, error) {
	return true, "", nil
}
func (f *fakeKillSwitchRiskEngine) GetStatus(context.Context) (risk.EngineStatus, error) {
	if f.verificationTime.IsZero() {
		f.verificationTime = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	}
	status := risk.EngineStatus{RiskStatus: domain.RiskStatusNormal, UpdatedAt: f.verificationTime}
	status.KillSwitch.Active = f.active
	status.KillSwitch.Reason = f.reason
	if f.active {
		status.KillSwitch.Mechanisms = []risk.KillSwitchMechanism{risk.KillSwitchMechanismAPI}
	}
	return status, nil
}
func (*fakeKillSwitchRiskEngine) TripCircuitBreaker(context.Context, string) error { return nil }
func (*fakeKillSwitchRiskEngine) ResetCircuitBreaker(context.Context) error        { return nil }
func (f *fakeKillSwitchRiskEngine) IsKillSwitchActive(context.Context) (bool, error) {
	return f.active, nil
}
func (f *fakeKillSwitchRiskEngine) ActivateKillSwitch(_ context.Context, reason string) error {
	f.active = true
	f.reason = reason
	f.activateReasons = append(f.activateReasons, reason)
	return nil
}
func (f *fakeKillSwitchRiskEngine) DeactivateKillSwitch(context.Context) error {
	f.active = false
	f.deactivateCalls++
	return nil
}
func (*fakeKillSwitchRiskEngine) IsMarketKillSwitchActive(context.Context, domain.MarketType) (bool, error) {
	return false, nil
}
func (*fakeKillSwitchRiskEngine) ActivateMarketKillSwitch(context.Context, domain.MarketType, string) error {
	return nil
}
func (*fakeKillSwitchRiskEngine) DeactivateMarketKillSwitch(context.Context, domain.MarketType) error {
	return nil
}
func (*fakeKillSwitchRiskEngine) UpdateMetrics(context.Context, float64, float64, int) error {
	return nil
}

func TestRiskBreakerList(t *testing.T) {
	tests := []struct {
		name       string
		srv        *Server
		wantStatus int
		wantCount  int
	}{
		{name: "nil lister", srv: &Server{}, wantStatus: http.StatusServiceUnavailable},
		{name: "empty", srv: &Server{riskBreakerLister: &fakeRiskBreakerLister{}}, wantStatus: http.StatusOK, wantCount: 0},
		{name: "two tripped", srv: &Server{riskBreakerLister: &fakeRiskBreakerLister{items: []domain.RiskBreakerState{{Scope: "a"}, {Scope: "b"}}}}, wantStatus: http.StatusOK, wantCount: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/risk/breakers", nil)
			tt.srv.handleRiskBreakerList(rr, req)
			if rr.Code != tt.wantStatus {
				t.Fatalf("status=%d want=%d", rr.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusOK {
				var body struct {
					Tripped []domain.RiskBreakerState `json:"tripped"`
				}
				_ = json.Unmarshal(rr.Body.Bytes(), &body)
				if len(body.Tripped) != tt.wantCount {
					t.Fatalf("count=%d want=%d", len(body.Tripped), tt.wantCount)
				}
			}
		})
	}
}

func TestHandleKillSwitchToggleRequiresReasonAndVerifiesStatus(t *testing.T) {
	riskEngine := &fakeKillSwitchRiskEngine{}
	srv := &Server{risk: riskEngine}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/risk/killswitch", bytes.NewBufferString(`{"active":true,"reason":"  operator halt  "}`))
	srv.handleKillSwitchToggle(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if got := riskEngine.activateReasons[0]; got != "operator halt" {
		t.Fatalf("reason = %q, want trimmed reason", got)
	}
	var body KillSwitchToggleResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !body.Active || len(body.Mechanisms) != 1 {
		t.Fatalf("response = %+v, want active status with mechanism", body)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/risk/killswitch", bytes.NewBufferString(`{"active":true,"reason":"   "}`))
	srv.handleKillSwitchToggle(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("blank reason status=%d want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleKillSwitchDeactivateRequiresAdminAndReason(t *testing.T) {
	t.Setenv("ADMIN_API_KEY", "test-key")
	riskEngine := &fakeKillSwitchRiskEngine{active: true, reason: "halted"}
	srv := &Server{risk: riskEngine}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/risk/killswitch", bytes.NewBufferString(`{"active":false,"reason":"cleared after review"}`))
	srv.handleKillSwitchToggle(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("missing admin status=%d want %d", rr.Code, http.StatusUnauthorized)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/risk/killswitch", bytes.NewBufferString(`{"active":false,"reason":"   "}`))
	req.Header.Set("X-Admin-Key", "test-key")
	srv.handleKillSwitchToggle(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("blank reason status=%d want %d", rr.Code, http.StatusBadRequest)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/risk/killswitch", bytes.NewBufferString(`{"active":false,"reason":"cleared after review"}`))
	req.Header.Set("X-Admin-Key", "test-key")
	srv.handleKillSwitchToggle(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if riskEngine.deactivateCalls != 1 {
		t.Fatalf("deactivate calls = %d, want 1", riskEngine.deactivateCalls)
	}
	var body KillSwitchToggleResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if body.Active {
		t.Fatalf("active = true, want verified inactive response")
	}
}

func TestHandleRiskBreakerReset(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*testing.T) (*Server, *http.Request, *httptest.ResponseRecorder)
		wantStatus int
		wantBody   string
	}{
		{name: "missing scope", setup: func(t *testing.T) (*Server, *http.Request, *httptest.ResponseRecorder) {
			t.Setenv("ADMIN_API_KEY", "test-key")
			srv := &Server{}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/risk/breaker/reset", bytes.NewBufferString(`{}`))
			req.Header.Set("X-Admin-Key", "test-key")
			return srv, req, httptest.NewRecorder()
		}, wantStatus: http.StatusBadRequest, wantBody: `{"error":"missing_scope"}`},
		{name: "nil breaker", setup: func(t *testing.T) (*Server, *http.Request, *httptest.ResponseRecorder) {
			t.Setenv("ADMIN_API_KEY", "test-key")
			srv := &Server{}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/risk/breaker/reset", bytes.NewBufferString(`{"scope":"global"}`))
			req.Header.Set("X-Admin-Key", "test-key")
			return srv, req, httptest.NewRecorder()
		}, wantStatus: http.StatusServiceUnavailable, wantBody: `{"error":"risk breaker not configured"}`},
		{name: "success", setup: func(t *testing.T) (*Server, *http.Request, *httptest.ResponseRecorder) {
			t.Setenv("ADMIN_API_KEY", "test-key")
			br := &fakeRiskBreaker{}
			srv := &Server{riskBreaker: br}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/risk/breaker/reset", bytes.NewBufferString(`{"scope":"strategy:abc"}`))
			req.Header.Set("X-Admin-Key", "test-key")
			return srv, req, httptest.NewRecorder()
		}, wantStatus: http.StatusOK, wantBody: `{"scope":"strategy:abc","reset":true}`},
		{name: "admin disabled", setup: func(t *testing.T) (*Server, *http.Request, *httptest.ResponseRecorder) {
			t.Setenv("ADMIN_API_KEY", "")
			srv := &Server{}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/risk/breaker/reset", bytes.NewBufferString(`{"scope":"global"}`))
			req.Header.Set("X-Admin-Key", "test-key")
			return srv, req, httptest.NewRecorder()
		}, wantStatus: http.StatusServiceUnavailable, wantBody: `{"error":"ADMIN_API_KEY not configured"}`},
		{name: "wrong key", setup: func(t *testing.T) (*Server, *http.Request, *httptest.ResponseRecorder) {
			t.Setenv("ADMIN_API_KEY", "test-key")
			srv := &Server{}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/risk/breaker/reset", bytes.NewBufferString(`{"scope":"global"}`))
			req.Header.Set("X-Admin-Key", "wrong")
			return srv, req, httptest.NewRecorder()
		}, wantStatus: http.StatusUnauthorized, wantBody: `{"error":"admin key required"}`},
		{name: "repo not found treated as success", setup: func(t *testing.T) (*Server, *http.Request, *httptest.ResponseRecorder) {
			t.Setenv("ADMIN_API_KEY", "test-key")
			br := &fakeRiskBreaker{resetErr: repository.ErrNotFound}
			srv := &Server{riskBreaker: br}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/risk/breaker/reset", bytes.NewBufferString(`{"scope":"global"}`))
			req.Header.Set("X-Admin-Key", "test-key")
			return srv, req, httptest.NewRecorder()
		}, wantStatus: http.StatusOK, wantBody: `{"scope":"global","reset":true}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, req, rr := tt.setup(t)
			srv.requireAdmin(http.HandlerFunc(srv.handleRiskBreakerReset)).ServeHTTP(rr, req)
			if rr.Code != tt.wantStatus {
				t.Fatalf("status=%d want %d body=%s", rr.Code, tt.wantStatus, rr.Body.String())
			}
			var got, want map[string]any
			_ = json.Unmarshal([]byte(rr.Body.String()), &got)
			_ = json.Unmarshal([]byte(tt.wantBody), &want)
			for k, v := range want {
				if got[k] != v {
					t.Fatalf("body[%s]=%v want %v", k, got[k], v)
				}
			}
		})
	}
}
