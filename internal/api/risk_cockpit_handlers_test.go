package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

func TestRiskCockpitRoute(t *testing.T) {
	repo := &stubTradeDecisionJournalRepo{
		listResult: []domain.TradeDecision{
			{MarketType: domain.MarketTypeStock, RiskStatus: domain.RiskDecisionApproved, Status: domain.TradeDecisionStatusPaper, ApprovedSize: 4, NetEV: 1.1},
			{MarketType: domain.MarketTypeCrypto, RiskStatus: domain.RiskDecisionRejected, Status: domain.TradeDecisionStatusRejected},
		},
	}
	deps := testDeps()
	deps.TradeDecisions = repo
	currentPrice := 4.5
	unrealized := 0.5
	deps.Positions = &stubPositionRepo{positions: []domain.Position{{
		MarketType: domain.MarketTypeStock, Ticker: "AAPL", Side: domain.PositionSideLong,
		Quantity: 1, AvgEntry: 4, CurrentPrice: &currentPrice, UnrealizedPnL: &unrealized,
	}}}
	deps.Risk = &stubRiskEngine{getStatusFn: func(context.Context) (risk.EngineStatus, error) {
		return risk.EngineStatus{
			KillSwitch:     risk.KillSwitchStatus{Active: true},
			CircuitBreaker: risk.CircuitBreakerStatus{State: risk.CircuitBreakerPhaseCooldown},
			UpdatedAt:      time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
		}, nil
	}}
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/risk/cockpit", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	body := decodeJSON[risk.CockpitSummary](t, rr)
	if !body.KillSwitchActive || !body.CircuitBreaker {
		t.Fatalf("unexpected active flags: %+v", body)
	}
	if len(body.Exposures) != 5 {
		t.Fatalf("exposures len = %d want 5", len(body.Exposures))
	}
	if body.Exposures[0].MarketType != domain.MarketTypeStock || body.Exposures[0].OpenPositions != 1 || body.Exposures[0].GrossExposure != 4 {
		t.Fatalf("unexpected stock exposure: %+v", body.Exposures[0])
	}
	if len(body.Warnings) == 0 {
		t.Fatalf("expected warnings, got %+v", body)
	}
}

func TestRiskCockpitHandlerMissingDeps(t *testing.T) {
	for _, tc := range []struct {
		name string
		srv  *Server
	}{
		{name: "missing risk", srv: &Server{}},
		{name: "missing decisions", srv: &Server{risk: &stubRiskEngine{}}},
		{name: "missing positions", srv: &Server{risk: &stubRiskEngine{}, tradeDecisions: &stubTradeDecisionJournalRepo{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/risk/cockpit", nil)
			tc.srv.handleRiskCockpit(rr, req)
			if rr.Code != http.StatusNotImplemented {
				t.Fatalf("status = %d want %d body=%s", rr.Code, http.StatusNotImplemented, rr.Body.String())
			}
			body := decodeJSON[ErrorResponse](t, rr)
			if body.Code != ErrCodeNotImplemented {
				t.Fatalf("code = %q want %q", body.Code, ErrCodeNotImplemented)
			}
		})
	}
}

func TestRiskCockpitHandlerErrors(t *testing.T) {
	t.Run("status error", func(t *testing.T) {
		deps := testDeps()
		deps.TradeDecisions = &stubTradeDecisionJournalRepo{listResult: []domain.TradeDecision{{MarketType: domain.MarketTypeStock, RiskStatus: domain.RiskDecisionApproved, Status: domain.TradeDecisionStatusPaper, ApprovedSize: 1}}}
		deps.Risk = &stubRiskEngine{getStatusFn: func(context.Context) (risk.EngineStatus, error) { return risk.EngineStatus{}, errors.New("boom") }}
		srv := newTestServerWithDeps(t, deps)
		rr := doRequest(t, srv, http.MethodGet, "/api/v1/risk/cockpit", nil)
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d want %d body=%s", rr.Code, http.StatusInternalServerError, rr.Body.String())
		}
	})

	t.Run("repo error", func(t *testing.T) {
		deps := testDeps()
		repo := &stubTradeDecisionJournalRepo{
			listResult: []domain.TradeDecision{{MarketType: domain.MarketTypeStock, RiskStatus: domain.RiskDecisionApproved, Status: domain.TradeDecisionStatusPaper, ApprovedSize: 1}},
			listErr:    errors.New("boom"),
		}
		deps.TradeDecisions = repo
		deps.Risk = &stubRiskEngine{getStatusFn: func(context.Context) (risk.EngineStatus, error) { return risk.EngineStatus{}, nil }}
		srv := newTestServerWithDeps(t, deps)
		rr := doRequest(t, srv, http.MethodGet, "/api/v1/risk/cockpit", nil)
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d want %d body=%s", rr.Code, http.StatusInternalServerError, rr.Body.String())
		}
	})
}
