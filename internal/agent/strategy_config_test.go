package agent_test

import (
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
)

// validStrategyConfig returns a fully-populated StrategyConfig that must pass
// ValidateStrategyConfig without error.
func validStrategyConfig() agent.StrategyConfig {
	return agent.StrategyConfig{
		LLMConfig: &agent.StrategyLLMConfig{
			Provider:        strPtr("openai"),
			DeepThinkModel:  strPtr("gpt-5.2"),
			QuickThinkModel: strPtr("gpt-5-mini"),
		},
		PipelineConfig: &agent.StrategyPipelineConfig{
			DebateRounds:           intPtr(3),
			AnalysisTimeoutSeconds: intPtr(30),
			DebateTimeoutSeconds:   intPtr(60),
		},
		RiskConfig: &agent.StrategyRiskConfig{
			PositionSizePct:      float64Ptr(5.0),
			StopLossMultiplier:   float64Ptr(1.5),
			TakeProfitMultiplier: float64Ptr(2.0),
			MinConfidence:        float64Ptr(0.65),
		},
		AnalystSelection: []agent.AgentRole{
			agent.AgentRoleMarketAnalyst,
			agent.AgentRoleFundamentalsAnalyst,
		},
		PromptOverrides: map[agent.AgentRole]string{
			agent.AgentRoleTrader: "You are a conservative trader.",
		},
	}
}

func TestValidateStrategyConfig_ValidFull(t *testing.T) {
	cfg := validStrategyConfig()
	if err := agent.ValidateStrategyConfig(cfg); err != nil {
		t.Fatalf("expected no error for a valid config, got: %v", err)
	}
}

func TestValidateStrategyConfig_EmptyConfig(t *testing.T) {
	cfg := agent.StrategyConfig{}
	if err := agent.ValidateStrategyConfig(cfg); err != nil {
		t.Fatalf("expected no error for an empty config, got: %v", err)
	}
}

func TestValidateStrategyConfig_InvalidDeepThinkModel(t *testing.T) {
	cfg := validStrategyConfig()
	cfg.LLMConfig.DeepThinkModel = strPtr("unknown-model-xyz")

	err := agent.ValidateStrategyConfig(cfg)
	if err == nil {
		t.Fatal("expected error for unknown deep_think_model, got nil")
	}
	if !strings.Contains(err.Error(), "deep_think_model") {
		t.Fatalf("error should mention 'deep_think_model', got: %v", err)
	}
	if !strings.Contains(err.Error(), "unknown-model-xyz") {
		t.Fatalf("error should include the bad model name, got: %v", err)
	}
}

func TestValidateStrategyConfig_InvalidQuickThinkModel(t *testing.T) {
	cfg := validStrategyConfig()
	cfg.LLMConfig.QuickThinkModel = strPtr("bad-model")

	err := agent.ValidateStrategyConfig(cfg)
	if err == nil {
		t.Fatal("expected error for unknown quick_think_model, got nil")
	}
	if !strings.Contains(err.Error(), "quick_think_model") {
		t.Fatalf("error should mention 'quick_think_model', got: %v", err)
	}
}

func TestValidateStrategyConfig_PositionSizePctOutOfRange(t *testing.T) {
	tests := []struct {
		name string
		val  float64
	}{
		{"negative", -1.0},
		{"over 100", 101.0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validStrategyConfig()
			cfg.RiskConfig.PositionSizePct = float64Ptr(tc.val)

			err := agent.ValidateStrategyConfig(cfg)
			if err == nil {
				t.Fatalf("expected error for position_size_pct=%g, got nil", tc.val)
			}
			if !strings.Contains(err.Error(), "position_size_pct") {
				t.Fatalf("error should mention 'position_size_pct', got: %v", err)
			}
		})
	}
}

func TestValidateStrategyConfig_MinConfidenceOutOfRange(t *testing.T) {
	tests := []struct {
		name string
		val  float64
	}{
		{"negative", -0.1},
		{"above 1", 1.1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validStrategyConfig()
			cfg.RiskConfig.MinConfidence = float64Ptr(tc.val)

			err := agent.ValidateStrategyConfig(cfg)
			if err == nil {
				t.Fatalf("expected error for min_confidence=%g, got nil", tc.val)
			}
			if !strings.Contains(err.Error(), "min_confidence") {
				t.Fatalf("error should mention 'min_confidence', got: %v", err)
			}
		})
	}
}

func TestValidateStrategyConfig_StopLossMultiplierNonPositive(t *testing.T) {
	cfg := validStrategyConfig()
	cfg.RiskConfig.StopLossMultiplier = float64Ptr(0)

	err := agent.ValidateStrategyConfig(cfg)
	if err == nil {
		t.Fatal("expected error for stop_loss_multiplier=0, got nil")
	}
	if !strings.Contains(err.Error(), "stop_loss_multiplier") {
		t.Fatalf("error should mention 'stop_loss_multiplier', got: %v", err)
	}
}

func TestValidateStrategyConfig_TakeProfitMultiplierNonPositive(t *testing.T) {
	cfg := validStrategyConfig()
	cfg.RiskConfig.TakeProfitMultiplier = float64Ptr(-1.0)

	err := agent.ValidateStrategyConfig(cfg)
	if err == nil {
		t.Fatal("expected error for take_profit_multiplier=-1, got nil")
	}
	if !strings.Contains(err.Error(), "take_profit_multiplier") {
		t.Fatalf("error should mention 'take_profit_multiplier', got: %v", err)
	}
}

func TestValidateStrategyConfig_UnknownAnalystRole(t *testing.T) {
	cfg := validStrategyConfig()
	cfg.AnalystSelection = []agent.AgentRole{"not_a_real_role"}

	err := agent.ValidateStrategyConfig(cfg)
	if err == nil {
		t.Fatal("expected error for unknown analyst role, got nil")
	}
	if !strings.Contains(err.Error(), "analyst_selection") {
		t.Fatalf("error should mention 'analyst_selection', got: %v", err)
	}
	if !strings.Contains(err.Error(), "not_a_real_role") {
		t.Fatalf("error should include the bad role name, got: %v", err)
	}
}

func TestValidateStrategyConfig_UnknownPromptOverrideRole(t *testing.T) {
	cfg := validStrategyConfig()
	cfg.PromptOverrides = map[agent.AgentRole]string{
		"ghost_role": "some prompt",
	}

	err := agent.ValidateStrategyConfig(cfg)
	if err == nil {
		t.Fatal("expected error for unknown role in prompt_overrides, got nil")
	}
	if !strings.Contains(err.Error(), "prompt_overrides") {
		t.Fatalf("error should mention 'prompt_overrides', got: %v", err)
	}
}

func TestValidateStrategyConfig_DebateRoundsZero(t *testing.T) {
	cfg := validStrategyConfig()
	cfg.PipelineConfig.DebateRounds = intPtr(0)

	err := agent.ValidateStrategyConfig(cfg)
	if err == nil {
		t.Fatal("expected error for debate_rounds=0, got nil")
	}
	if !strings.Contains(err.Error(), "debate_rounds") {
		t.Fatalf("error should mention 'debate_rounds', got: %v", err)
	}
}

// helper pointer constructors used only in tests.
func strPtr(s string) *string     { return &s }
func intPtr(n int) *int            { return &n }
func float64Ptr(f float64) *float64 { return &f }
