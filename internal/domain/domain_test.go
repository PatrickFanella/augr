package domain_test

import (
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestMarketTypeString(t *testing.T) {
	tests := []struct {
		mt   domain.MarketType
		want string
	}{
		{domain.MarketTypeStock, "stock"},
		{domain.MarketTypeCrypto, "crypto"},
		{domain.MarketTypePolymarket, "polymarket"},
	}

	for _, tc := range tests {
		if got := tc.mt.String(); got != tc.want {
			t.Errorf("MarketType(%q).String() = %q, want %q", tc.mt, got, tc.want)
		}
	}
}

func TestPipelineStatusString(t *testing.T) {
	tests := []struct {
		ps   domain.PipelineStatus
		want string
	}{
		{domain.PipelineStatusRunning, "running"},
		{domain.PipelineStatusCompleted, "completed"},
		{domain.PipelineStatusFailed, "failed"},
		{domain.PipelineStatusCancelled, "cancelled"},
	}

	for _, tc := range tests {
		if got := tc.ps.String(); got != tc.want {
			t.Errorf("PipelineStatus(%q).String() = %q, want %q", tc.ps, got, tc.want)
		}
	}
}

func TestPipelineSignalString(t *testing.T) {
	tests := []struct {
		ps   domain.PipelineSignal
		want string
	}{
		{domain.PipelineSignalBuy, "buy"},
		{domain.PipelineSignalSell, "sell"},
		{domain.PipelineSignalHold, "hold"},
	}

	for _, tc := range tests {
		if got := tc.ps.String(); got != tc.want {
			t.Errorf("PipelineSignal(%q).String() = %q, want %q", tc.ps, got, tc.want)
		}
	}
}

func TestAgentRoleString(t *testing.T) {
	tests := []struct {
		r    domain.AgentRole
		want string
	}{
		{domain.AgentRoleMarketAnalyst, "market_analyst"},
		{domain.AgentRoleBullResearcher, "bull_researcher"},
		{domain.AgentRoleBearResearcher, "bear_researcher"},
		{domain.AgentRoleTrader, "trader"},
		{domain.AgentRoleInvestJudge, "invest_judge"},
		{domain.AgentRoleRiskManager, "risk_manager"},
		{domain.AgentRoleAggressiveAnalyst, "aggressive_analyst"},
		{domain.AgentRoleConservativeAnalyst, "conservative_analyst"},
		{domain.AgentRoleNeutralAnalyst, "neutral_analyst"},
	}

	for _, tc := range tests {
		if got := tc.r.String(); got != tc.want {
			t.Errorf("AgentRole(%q).String() = %q, want %q", tc.r, got, tc.want)
		}
	}
}

func TestPhaseString(t *testing.T) {
	tests := []struct {
		p    domain.Phase
		want string
	}{
		{domain.PhaseAnalysis, "analysis"},
		{domain.PhaseResearchDebate, "research_debate"},
		{domain.PhaseTrading, "trading"},
		{domain.PhaseRiskDebate, "risk_debate"},
	}

	for _, tc := range tests {
		if got := tc.p.String(); got != tc.want {
			t.Errorf("Phase(%q).String() = %q, want %q", tc.p, got, tc.want)
		}
	}
}

func TestOrderSideString(t *testing.T) {
	tests := []struct {
		s    domain.OrderSide
		want string
	}{
		{domain.OrderSideBuy, "buy"},
		{domain.OrderSideSell, "sell"},
	}

	for _, tc := range tests {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("OrderSide(%q).String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestOrderTypeString(t *testing.T) {
	tests := []struct {
		t    domain.OrderType
		want string
	}{
		{domain.OrderTypeMarket, "market"},
		{domain.OrderTypeLimit, "limit"},
		{domain.OrderTypeStop, "stop"},
		{domain.OrderTypeStopLimit, "stop_limit"},
	}

	for _, tc := range tests {
		if got := tc.t.String(); got != tc.want {
			t.Errorf("OrderType(%q).String() = %q, want %q", tc.t, got, tc.want)
		}
	}
}

func TestOrderStatusString(t *testing.T) {
	tests := []struct {
		s    domain.OrderStatus
		want string
	}{
		{domain.OrderStatusPending, "pending"},
		{domain.OrderStatusSubmitted, "submitted"},
		{domain.OrderStatusPartial, "partial"},
		{domain.OrderStatusFilled, "filled"},
		{domain.OrderStatusCancelled, "cancelled"},
		{domain.OrderStatusRejected, "rejected"},
	}

	for _, tc := range tests {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("OrderStatus(%q).String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestPositionSideString(t *testing.T) {
	tests := []struct {
		s    domain.PositionSide
		want string
	}{
		{domain.PositionSideLong, "long"},
		{domain.PositionSideShort, "short"},
	}

	for _, tc := range tests {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("PositionSide(%q).String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestRiskStatusString(t *testing.T) {
	tests := []struct {
		s    domain.RiskStatus
		want string
	}{
		{domain.RiskStatusNormal, "normal"},
		{domain.RiskStatusWarning, "warning"},
		{domain.RiskStatusBreached, "breached"},
	}

	for _, tc := range tests {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("RiskStatus(%q).String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestCircuitBreakerStateString(t *testing.T) {
	tests := []struct {
		s    domain.CircuitBreakerState
		want string
	}{
		{domain.CircuitBreakerClosed, "closed"},
		{domain.CircuitBreakerOpen, "open"},
		{domain.CircuitBreakerHalfOpen, "half_open"},
	}

	for _, tc := range tests {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("CircuitBreakerState(%q).String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}
