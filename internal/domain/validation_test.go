package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
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

func TestAgentDecisionJSONOmitsPromptText(t *testing.T) {
	decision := AgentDecision{
		ID:         uuid.New(),
		OutputText: "final output",
		PromptText: "sensitive prompt contents",
	}

	payload, err := json.Marshal(decision)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	if string(payload) == "" {
		t.Fatal("expected non-empty JSON payload")
	}
	if !json.Valid(payload) {
		t.Fatalf("expected valid JSON, got %q", string(payload))
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if _, ok := decoded["prompt_text"]; ok {
		t.Fatalf("expected prompt_text to be omitted from JSON, got %q", string(payload))
	}
	if got := decoded["output_text"]; got != "final output" {
		t.Fatalf("output_text = %v, want %q", got, "final output")
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

func TestBacktestConfigValidate(t *testing.T) {
	valid := &BacktestConfig{
		StrategyID: uuid.New(),
		Name:       "baseline",
		StartDate:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:    time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
		Simulation: BacktestSimulationParameters{
			InitialCapital: 100000,
			MaxVolumePct:   0.25,
		},
	}

	tests := []struct {
		name   string
		config *BacktestConfig
	}{
		{name: "valid config", config: valid},
		{name: "nil config", config: nil},
		{name: "missing strategy id", config: &BacktestConfig{
			Name:      "baseline",
			StartDate: valid.StartDate,
			EndDate:   valid.EndDate,
			Simulation: BacktestSimulationParameters{
				InitialCapital: 100000,
			},
		}},
		{name: "empty name", config: &BacktestConfig{
			StrategyID: valid.StrategyID,
			StartDate:  valid.StartDate,
			EndDate:    valid.EndDate,
			Simulation: BacktestSimulationParameters{InitialCapital: 100000},
		}},
		{name: "missing start date", config: &BacktestConfig{
			StrategyID: valid.StrategyID,
			Name:       "baseline",
			EndDate:    valid.EndDate,
			Simulation: BacktestSimulationParameters{InitialCapital: 100000},
		}},
		{name: "missing end date", config: &BacktestConfig{
			StrategyID: valid.StrategyID,
			Name:       "baseline",
			StartDate:  valid.StartDate,
			Simulation: BacktestSimulationParameters{InitialCapital: 100000},
		}},
		{name: "end before start", config: &BacktestConfig{
			StrategyID: valid.StrategyID,
			Name:       "baseline",
			StartDate:  valid.EndDate,
			EndDate:    valid.StartDate,
			Simulation: BacktestSimulationParameters{InitialCapital: 100000},
		}},
		{name: "non-positive capital", config: &BacktestConfig{
			StrategyID: valid.StrategyID,
			Name:       "baseline",
			StartDate:  valid.StartDate,
			EndDate:    valid.EndDate,
			Simulation: BacktestSimulationParameters{},
		}},
		{name: "negative max volume", config: &BacktestConfig{
			StrategyID: valid.StrategyID,
			Name:       "baseline",
			StartDate:  valid.StartDate,
			EndDate:    valid.EndDate,
			Simulation: BacktestSimulationParameters{
				InitialCapital: 100000,
				MaxVolumePct:   -0.1,
			},
		}},
		{name: "max volume above one", config: &BacktestConfig{
			StrategyID: valid.StrategyID,
			Name:       "baseline",
			StartDate:  valid.StartDate,
			EndDate:    valid.EndDate,
			Simulation: BacktestSimulationParameters{
				InitialCapital: 100000,
				MaxVolumePct:   1.1,
			},
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()
			if tc.name == "valid config" {
				if err != nil {
					t.Fatalf("BacktestConfig.Validate() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("BacktestConfig.Validate() expected error, got nil")
			}
		})
	}
}

func TestBacktestRunValidate(t *testing.T) {
	valid := &BacktestRun{
		BacktestConfigID:  uuid.New(),
		Metrics:           json.RawMessage(`{"total_return":0.12}`),
		TradeLog:          json.RawMessage(`[]`),
		EquityCurve:       json.RawMessage(`[]`),
		RunTimestamp:      time.Date(2024, 1, 3, 21, 0, 0, 0, time.UTC),
		Duration:          37 * time.Minute,
		PromptVersion:     "prompt-v1",
		PromptVersionHash: testPromptVersionHash("prompt-v1"),
	}

	tests := []struct {
		name string
		run  *BacktestRun
	}{
		{name: "valid run", run: valid},
		{name: "nil run", run: nil},
		{name: "missing backtest config id", run: &BacktestRun{
			Metrics:           valid.Metrics,
			TradeLog:          valid.TradeLog,
			EquityCurve:       valid.EquityCurve,
			RunTimestamp:      valid.RunTimestamp,
			Duration:          valid.Duration,
			PromptVersion:     valid.PromptVersion,
			PromptVersionHash: valid.PromptVersionHash,
		}},
		{name: "missing metrics", run: &BacktestRun{
			BacktestConfigID:  valid.BacktestConfigID,
			TradeLog:          valid.TradeLog,
			EquityCurve:       valid.EquityCurve,
			RunTimestamp:      valid.RunTimestamp,
			Duration:          valid.Duration,
			PromptVersion:     valid.PromptVersion,
			PromptVersionHash: valid.PromptVersionHash,
		}},
		{name: "invalid metrics json", run: &BacktestRun{
			BacktestConfigID:  valid.BacktestConfigID,
			Metrics:           json.RawMessage(`{"total_return":`),
			TradeLog:          valid.TradeLog,
			EquityCurve:       valid.EquityCurve,
			RunTimestamp:      valid.RunTimestamp,
			Duration:          valid.Duration,
			PromptVersion:     valid.PromptVersion,
			PromptVersionHash: valid.PromptVersionHash,
		}},
		{name: "missing trade log", run: &BacktestRun{
			BacktestConfigID:  valid.BacktestConfigID,
			Metrics:           valid.Metrics,
			EquityCurve:       valid.EquityCurve,
			RunTimestamp:      valid.RunTimestamp,
			Duration:          valid.Duration,
			PromptVersion:     valid.PromptVersion,
			PromptVersionHash: valid.PromptVersionHash,
		}},
		{name: "invalid trade log json", run: &BacktestRun{
			BacktestConfigID:  valid.BacktestConfigID,
			Metrics:           valid.Metrics,
			TradeLog:          json.RawMessage(`[}`),
			EquityCurve:       valid.EquityCurve,
			RunTimestamp:      valid.RunTimestamp,
			Duration:          valid.Duration,
			PromptVersion:     valid.PromptVersion,
			PromptVersionHash: valid.PromptVersionHash,
		}},
		{name: "missing equity curve", run: &BacktestRun{
			BacktestConfigID:  valid.BacktestConfigID,
			Metrics:           valid.Metrics,
			TradeLog:          valid.TradeLog,
			RunTimestamp:      valid.RunTimestamp,
			Duration:          valid.Duration,
			PromptVersion:     valid.PromptVersion,
			PromptVersionHash: valid.PromptVersionHash,
		}},
		{name: "invalid equity curve json", run: &BacktestRun{
			BacktestConfigID:  valid.BacktestConfigID,
			Metrics:           valid.Metrics,
			TradeLog:          valid.TradeLog,
			EquityCurve:       json.RawMessage(`[`),
			RunTimestamp:      valid.RunTimestamp,
			Duration:          valid.Duration,
			PromptVersion:     valid.PromptVersion,
			PromptVersionHash: valid.PromptVersionHash,
		}},
		{name: "missing run timestamp", run: &BacktestRun{
			BacktestConfigID:  valid.BacktestConfigID,
			Metrics:           valid.Metrics,
			TradeLog:          valid.TradeLog,
			EquityCurve:       valid.EquityCurve,
			Duration:          valid.Duration,
			PromptVersion:     valid.PromptVersion,
			PromptVersionHash: valid.PromptVersionHash,
		}},
		{name: "negative duration", run: &BacktestRun{
			BacktestConfigID:  valid.BacktestConfigID,
			Metrics:           valid.Metrics,
			TradeLog:          valid.TradeLog,
			EquityCurve:       valid.EquityCurve,
			RunTimestamp:      valid.RunTimestamp,
			Duration:          -time.Second,
			PromptVersion:     valid.PromptVersion,
			PromptVersionHash: valid.PromptVersionHash,
		}},
		{name: "empty prompt version", run: &BacktestRun{
			BacktestConfigID:  valid.BacktestConfigID,
			Metrics:           valid.Metrics,
			TradeLog:          valid.TradeLog,
			EquityCurve:       valid.EquityCurve,
			RunTimestamp:      valid.RunTimestamp,
			Duration:          valid.Duration,
			PromptVersionHash: valid.PromptVersionHash,
		}},
		{name: "empty prompt version hash", run: &BacktestRun{
			BacktestConfigID: valid.BacktestConfigID,
			Metrics:          valid.Metrics,
			TradeLog:         valid.TradeLog,
			EquityCurve:      valid.EquityCurve,
			RunTimestamp:     valid.RunTimestamp,
			Duration:         valid.Duration,
			PromptVersion:    valid.PromptVersion,
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run.Validate()
			if tc.name == "valid run" {
				if err != nil {
					t.Fatalf("BacktestRun.Validate() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("BacktestRun.Validate() expected error, got nil")
			}
		})
	}
}

func testPromptVersionHash(version string) string {
	sum := sha256.Sum256([]byte(version))
	return hex.EncodeToString(sum[:])
}
