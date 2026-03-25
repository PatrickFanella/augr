package domain

import (
	"testing"
)

func TestRequireNonEmpty(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		value   string
		wantErr bool
	}{
		{"empty returns error", "name", "", true},
		{"non-empty returns nil", "name", "hello", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := requireNonEmpty(tc.field, tc.value)
			if (err != nil) != tc.wantErr {
				t.Errorf("requireNonEmpty(%q, %q) error = %v, wantErr %v", tc.field, tc.value, err, tc.wantErr)
			}
		})
	}
}

func TestRequirePositive(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		value   float64
		wantErr bool
	}{
		{"zero returns error", "qty", 0, true},
		{"negative returns error", "qty", -5.0, true},
		{"positive returns nil", "qty", 1.5, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := requirePositive(tc.field, tc.value)
			if (err != nil) != tc.wantErr {
				t.Errorf("requirePositive(%q, %v) error = %v, wantErr %v", tc.field, tc.value, err, tc.wantErr)
			}
		})
	}
}

func TestAgentRoleIsValid(t *testing.T) {
	validRoles := []AgentRole{
		AgentRoleMarketAnalyst,
		AgentRoleFundamentalsAnalyst,
		AgentRoleNewsAnalyst,
		AgentRoleSocialMediaAnalyst,
		AgentRoleBullResearcher,
		AgentRoleBearResearcher,
		AgentRoleTrader,
		AgentRoleInvestJudge,
		AgentRoleRiskManager,
		AgentRoleAggressiveAnalyst,
		AgentRoleConservativeAnalyst,
		AgentRoleNeutralAnalyst,
		AgentRoleAggressiveRisk,
		AgentRoleConservativeRisk,
		AgentRoleNeutralRisk,
	}
	for _, r := range validRoles {
		if !r.IsValid() {
			t.Errorf("AgentRole(%q).IsValid() = false, want true", r)
		}
	}
	for _, r := range []AgentRole{"unknown", ""} {
		if r.IsValid() {
			t.Errorf("AgentRole(%q).IsValid() = true, want false", r)
		}
	}
}

func TestPhaseIsValid(t *testing.T) {
	validPhases := []Phase{PhaseAnalysis, PhaseResearchDebate, PhaseTrading, PhaseRiskDebate}
	for _, p := range validPhases {
		if !p.IsValid() {
			t.Errorf("Phase(%q).IsValid() = false, want true", p)
		}
	}
	if Phase("unknown").IsValid() {
		t.Error("Phase(\"unknown\").IsValid() = true, want false")
	}
}

func TestPipelineStatusIsValid(t *testing.T) {
	valid := []PipelineStatus{PipelineStatusRunning, PipelineStatusCompleted, PipelineStatusFailed, PipelineStatusCancelled}
	for _, s := range valid {
		if !s.IsValid() {
			t.Errorf("PipelineStatus(%q).IsValid() = false, want true", s)
		}
	}
	if PipelineStatus("unknown").IsValid() {
		t.Error("PipelineStatus(\"unknown\").IsValid() = true, want false")
	}
}

func TestPipelineStatusCanTransitionTo(t *testing.T) {
	tests := []struct {
		from PipelineStatus
		to   PipelineStatus
		want bool
	}{
		{PipelineStatusRunning, PipelineStatusCompleted, true},
		{PipelineStatusRunning, PipelineStatusFailed, true},
		{PipelineStatusRunning, PipelineStatusCancelled, true},
		{PipelineStatusCompleted, PipelineStatusRunning, false},
		{PipelineStatusCompleted, PipelineStatusFailed, false},
		{PipelineStatusFailed, PipelineStatusRunning, false},
		{PipelineStatusCancelled, PipelineStatusCompleted, false},
		{PipelineStatus("invalid"), PipelineStatusRunning, false},
	}
	for _, tc := range tests {
		got := tc.from.CanTransitionTo(tc.to)
		if got != tc.want {
			t.Errorf("PipelineStatus(%q).CanTransitionTo(%q) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestPipelineSignalIsValid(t *testing.T) {
	for _, s := range []PipelineSignal{PipelineSignalBuy, PipelineSignalSell, PipelineSignalHold} {
		if !s.IsValid() {
			t.Errorf("PipelineSignal(%q).IsValid() = false, want true", s)
		}
	}
	if PipelineSignal("unknown").IsValid() {
		t.Error("PipelineSignal(\"unknown\").IsValid() = true, want false")
	}
}

func TestOrderStatusIsValid(t *testing.T) {
	valid := []OrderStatus{
		OrderStatusPending, OrderStatusSubmitted, OrderStatusPartial,
		OrderStatusFilled, OrderStatusCancelled, OrderStatusRejected,
	}
	for _, s := range valid {
		if !s.IsValid() {
			t.Errorf("OrderStatus(%q).IsValid() = false, want true", s)
		}
	}
	if OrderStatus("unknown").IsValid() {
		t.Error("OrderStatus(\"unknown\").IsValid() = true, want false")
	}
}

func TestOrderStatusCanTransitionTo(t *testing.T) {
	tests := []struct {
		from OrderStatus
		to   OrderStatus
		want bool
	}{
		{OrderStatusPending, OrderStatusSubmitted, true},
		{OrderStatusPending, OrderStatusCancelled, true},
		{OrderStatusPending, OrderStatusRejected, true},
		{OrderStatusPending, OrderStatusFilled, false},
		{OrderStatusSubmitted, OrderStatusFilled, true},
		{OrderStatusSubmitted, OrderStatusPartial, true},
		{OrderStatusPartial, OrderStatusFilled, true},
		{OrderStatusPartial, OrderStatusCancelled, true},
		{OrderStatusPartial, OrderStatusPending, false},
		{OrderStatusFilled, OrderStatusPending, false},
		{OrderStatusCancelled, OrderStatusPending, false},
		{OrderStatusRejected, OrderStatusPending, false},
	}
	for _, tc := range tests {
		got := tc.from.CanTransitionTo(tc.to)
		if got != tc.want {
			t.Errorf("OrderStatus(%q).CanTransitionTo(%q) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestOrderSideIsValid(t *testing.T) {
	for _, s := range []OrderSide{OrderSideBuy, OrderSideSell} {
		if !s.IsValid() {
			t.Errorf("OrderSide(%q).IsValid() = false, want true", s)
		}
	}
	if OrderSide("unknown").IsValid() {
		t.Error("OrderSide(\"unknown\").IsValid() = true, want false")
	}
}

func TestOrderTypeIsValid(t *testing.T) {
	valid := []OrderType{OrderTypeMarket, OrderTypeLimit, OrderTypeStop, OrderTypeStopLimit, OrderTypeTrailingStop}
	for _, ot := range valid {
		if !ot.IsValid() {
			t.Errorf("OrderType(%q).IsValid() = false, want true", ot)
		}
	}
	if OrderType("unknown").IsValid() {
		t.Error("OrderType(\"unknown\").IsValid() = true, want false")
	}
}

func TestRiskStatusIsValid(t *testing.T) {
	for _, s := range []RiskStatus{RiskStatusNormal, RiskStatusWarning, RiskStatusBreached} {
		if !s.IsValid() {
			t.Errorf("RiskStatus(%q).IsValid() = false, want true", s)
		}
	}
	if RiskStatus("unknown").IsValid() {
		t.Error("RiskStatus(\"unknown\").IsValid() = true, want false")
	}
}

func TestCircuitBreakerStateIsValid(t *testing.T) {
	for _, s := range []CircuitBreakerState{CircuitBreakerClosed, CircuitBreakerOpen, CircuitBreakerHalfOpen} {
		if !s.IsValid() {
			t.Errorf("CircuitBreakerState(%q).IsValid() = false, want true", s)
		}
	}
	if CircuitBreakerState("unknown").IsValid() {
		t.Error("CircuitBreakerState(\"unknown\").IsValid() = true, want false")
	}
}

func TestCircuitBreakerCanTransitionTo(t *testing.T) {
	tests := []struct {
		from CircuitBreakerState
		to   CircuitBreakerState
		want bool
	}{
		{CircuitBreakerClosed, CircuitBreakerOpen, true},
		{CircuitBreakerClosed, CircuitBreakerHalfOpen, false},
		{CircuitBreakerOpen, CircuitBreakerHalfOpen, true},
		{CircuitBreakerOpen, CircuitBreakerClosed, false},
		{CircuitBreakerHalfOpen, CircuitBreakerClosed, true},
		{CircuitBreakerHalfOpen, CircuitBreakerOpen, true},
	}
	for _, tc := range tests {
		got := tc.from.CanTransitionTo(tc.to)
		if got != tc.want {
			t.Errorf("CircuitBreakerState(%q).CanTransitionTo(%q) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestPositionSideIsValid(t *testing.T) {
	for _, s := range []PositionSide{PositionSideLong, PositionSideShort} {
		if !s.IsValid() {
			t.Errorf("PositionSide(%q).IsValid() = false, want true", s)
		}
	}
	if PositionSide("unknown").IsValid() {
		t.Error("PositionSide(\"unknown\").IsValid() = true, want false")
	}
}

func TestNewPosition(t *testing.T) {
	t.Run("valid construction", func(t *testing.T) {
		p, err := NewPosition("AAPL", PositionSideLong, 10, 150.0)
		if err != nil {
			t.Fatalf("NewPosition() unexpected error: %v", err)
		}
		if p.Ticker != "AAPL" {
			t.Errorf("Ticker = %q, want %q", p.Ticker, "AAPL")
		}
		if p.Side != PositionSideLong {
			t.Errorf("Side = %q, want %q", p.Side, PositionSideLong)
		}
		if p.Quantity != 10 {
			t.Errorf("Quantity = %v, want %v", p.Quantity, 10.0)
		}
		if p.AvgEntry != 150.0 {
			t.Errorf("AvgEntry = %v, want %v", p.AvgEntry, 150.0)
		}
	})
	t.Run("empty ticker error", func(t *testing.T) {
		_, err := NewPosition("", PositionSideLong, 10, 150.0)
		if err == nil {
			t.Fatal("NewPosition() expected error for empty ticker")
		}
	})
	t.Run("invalid side error", func(t *testing.T) {
		_, err := NewPosition("AAPL", PositionSide("bad"), 10, 150.0)
		if err == nil {
			t.Fatal("NewPosition() expected error for invalid side")
		}
	})
	t.Run("zero quantity error", func(t *testing.T) {
		_, err := NewPosition("AAPL", PositionSideLong, 0, 150.0)
		if err == nil {
			t.Fatal("NewPosition() expected error for zero quantity")
		}
	})
	t.Run("negative avgEntry error", func(t *testing.T) {
		_, err := NewPosition("AAPL", PositionSideLong, 10, -1.0)
		if err == nil {
			t.Fatal("NewPosition() expected error for negative avgEntry")
		}
	})
}

func TestMarketTypeIsValid(t *testing.T) {
	for _, m := range []MarketType{MarketTypeStock, MarketTypeCrypto, MarketTypePolymarket} {
		if !m.IsValid() {
			t.Errorf("MarketType(%q).IsValid() = false, want true", m)
		}
	}
	if MarketType("unknown").IsValid() {
		t.Error("MarketType(\"unknown\").IsValid() = true, want false")
	}
}

func TestStrategyValidate(t *testing.T) {
	t.Run("valid strategy", func(t *testing.T) {
		s := &Strategy{Name: "test", Ticker: "AAPL", MarketType: MarketTypeStock}
		if err := s.Validate(); err != nil {
			t.Errorf("Strategy.Validate() unexpected error: %v", err)
		}
	})
	t.Run("empty name error", func(t *testing.T) {
		s := &Strategy{Name: "", Ticker: "AAPL", MarketType: MarketTypeStock}
		if err := s.Validate(); err == nil {
			t.Error("Strategy.Validate() expected error for empty name")
		}
	})
	t.Run("empty ticker error", func(t *testing.T) {
		s := &Strategy{Name: "test", Ticker: "", MarketType: MarketTypeStock}
		if err := s.Validate(); err == nil {
			t.Error("Strategy.Validate() expected error for empty ticker")
		}
	})
	t.Run("invalid market type error", func(t *testing.T) {
		s := &Strategy{Name: "test", Ticker: "AAPL", MarketType: MarketType("bad")}
		if err := s.Validate(); err == nil {
			t.Error("Strategy.Validate() expected error for invalid market type")
		}
	})
}
