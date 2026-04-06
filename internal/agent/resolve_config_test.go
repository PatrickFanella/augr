package agent_test

import (
	"reflect"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
)

// TestResolveConfig_StrategyOverrides verifies that strategy-level values take
// precedence over global settings and hardcoded defaults for every field.
func TestResolveConfig_StrategyOverrides(t *testing.T) {
	tests := []struct {
		name     string
		strategy agent.StrategyConfig
		global   agent.GlobalSettings
		check    func(t *testing.T, got agent.ResolvedConfig)
	}{
		{
			name: "LLMConfig.Provider from strategy",
			strategy: agent.StrategyConfig{
				LLMConfig: &agent.StrategyLLMConfig{Provider: strPtr("anthropic")},
			},
			global: agent.GlobalSettings{
				LLMConfig: &agent.StrategyLLMConfig{Provider: strPtr("google")},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.LLMConfig.Provider != "anthropic" {
					t.Errorf("Provider = %q, want %q", got.LLMConfig.Provider, "anthropic")
				}
			},
		},
		{
			name: "LLMConfig.DeepThinkModel from strategy",
			strategy: agent.StrategyConfig{
				LLMConfig: &agent.StrategyLLMConfig{DeepThinkModel: strPtr("claude-3-7-sonnet-latest")},
			},
			global: agent.GlobalSettings{
				LLMConfig: &agent.StrategyLLMConfig{DeepThinkModel: strPtr("gemini-2.5-flash")},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.LLMConfig.DeepThinkModel != "claude-3-7-sonnet-latest" {
					t.Errorf("DeepThinkModel = %q, want %q", got.LLMConfig.DeepThinkModel, "claude-3-7-sonnet-latest")
				}
			},
		},
		{
			name: "LLMConfig.QuickThinkModel from strategy",
			strategy: agent.StrategyConfig{
				LLMConfig: &agent.StrategyLLMConfig{QuickThinkModel: strPtr("gpt-4.1-mini")},
			},
			global: agent.GlobalSettings{
				LLMConfig: &agent.StrategyLLMConfig{QuickThinkModel: strPtr("grok-3-mini")},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.LLMConfig.QuickThinkModel != "gpt-4.1-mini" {
					t.Errorf("QuickThinkModel = %q, want %q", got.LLMConfig.QuickThinkModel, "gpt-4.1-mini")
				}
			},
		},
		{
			name: "PipelineConfig.DebateRounds from strategy",
			strategy: agent.StrategyConfig{
				PipelineConfig: &agent.StrategyPipelineConfig{DebateRounds: intPtr(5)},
			},
			global: agent.GlobalSettings{
				PipelineConfig: &agent.StrategyPipelineConfig{DebateRounds: intPtr(2)},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.PipelineConfig.DebateRounds != 5 {
					t.Errorf("DebateRounds = %d, want %d", got.PipelineConfig.DebateRounds, 5)
				}
			},
		},
		{
			name: "PipelineConfig.AnalysisTimeoutSeconds from strategy",
			strategy: agent.StrategyConfig{
				PipelineConfig: &agent.StrategyPipelineConfig{AnalysisTimeoutSeconds: intPtr(90)},
			},
			global: agent.GlobalSettings{
				PipelineConfig: &agent.StrategyPipelineConfig{AnalysisTimeoutSeconds: intPtr(45)},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.PipelineConfig.AnalysisTimeoutSeconds != 90 {
					t.Errorf("AnalysisTimeoutSeconds = %d, want %d", got.PipelineConfig.AnalysisTimeoutSeconds, 90)
				}
			},
		},
		{
			name: "PipelineConfig.DebateTimeoutSeconds from strategy",
			strategy: agent.StrategyConfig{
				PipelineConfig: &agent.StrategyPipelineConfig{DebateTimeoutSeconds: intPtr(120)},
			},
			global: agent.GlobalSettings{
				PipelineConfig: &agent.StrategyPipelineConfig{DebateTimeoutSeconds: intPtr(30)},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.PipelineConfig.DebateTimeoutSeconds != 120 {
					t.Errorf("DebateTimeoutSeconds = %d, want %d", got.PipelineConfig.DebateTimeoutSeconds, 120)
				}
			},
		},
		{
			name: "RiskConfig.PositionSizePct from strategy",
			strategy: agent.StrategyConfig{
				RiskConfig: &agent.StrategyRiskConfig{PositionSizePct: float64Ptr(10.0)},
			},
			global: agent.GlobalSettings{
				RiskConfig: &agent.StrategyRiskConfig{PositionSizePct: float64Ptr(3.0)},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.RiskConfig.PositionSizePct != 10.0 {
					t.Errorf("PositionSizePct = %g, want %g", got.RiskConfig.PositionSizePct, 10.0)
				}
			},
		},
		{
			name: "RiskConfig.StopLossMultiplier from strategy",
			strategy: agent.StrategyConfig{
				RiskConfig: &agent.StrategyRiskConfig{StopLossMultiplier: float64Ptr(2.5)},
			},
			global: agent.GlobalSettings{
				RiskConfig: &agent.StrategyRiskConfig{StopLossMultiplier: float64Ptr(1.0)},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.RiskConfig.StopLossMultiplier != 2.5 {
					t.Errorf("StopLossMultiplier = %g, want %g", got.RiskConfig.StopLossMultiplier, 2.5)
				}
			},
		},
		{
			name: "RiskConfig.TakeProfitMultiplier from strategy",
			strategy: agent.StrategyConfig{
				RiskConfig: &agent.StrategyRiskConfig{TakeProfitMultiplier: float64Ptr(3.0)},
			},
			global: agent.GlobalSettings{
				RiskConfig: &agent.StrategyRiskConfig{TakeProfitMultiplier: float64Ptr(1.5)},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.RiskConfig.TakeProfitMultiplier != 3.0 {
					t.Errorf("TakeProfitMultiplier = %g, want %g", got.RiskConfig.TakeProfitMultiplier, 3.0)
				}
			},
		},
		{
			name: "RiskConfig.MinConfidence from strategy",
			strategy: agent.StrategyConfig{
				RiskConfig: &agent.StrategyRiskConfig{MinConfidence: float64Ptr(0.8)},
			},
			global: agent.GlobalSettings{
				RiskConfig: &agent.StrategyRiskConfig{MinConfidence: float64Ptr(0.5)},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.RiskConfig.MinConfidence != 0.8 {
					t.Errorf("MinConfidence = %g, want %g", got.RiskConfig.MinConfidence, 0.8)
				}
			},
		},
		{
			name: "AnalystSelection from strategy",
			strategy: agent.StrategyConfig{
				AnalystSelection: []agent.AgentRole{agent.AgentRoleMarketAnalyst},
			},
			global: agent.GlobalSettings{
				AnalystSelection: []agent.AgentRole{agent.AgentRoleFundamentalsAnalyst, agent.AgentRoleNewsAnalyst},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				want := []agent.AgentRole{agent.AgentRoleMarketAnalyst}
				if !reflect.DeepEqual(got.AnalystSelection, want) {
					t.Errorf("AnalystSelection = %v, want %v", got.AnalystSelection, want)
				}
			},
		},
		{
			name: "PromptOverrides from strategy",
			strategy: agent.StrategyConfig{
				PromptOverrides: map[agent.AgentRole]string{
					agent.AgentRoleTrader: "strategy prompt",
				},
			},
			global: agent.GlobalSettings{
				PromptOverrides: map[agent.AgentRole]string{
					agent.AgentRoleTrader: "global prompt",
				},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				want := map[agent.AgentRole]string{agent.AgentRoleTrader: "strategy prompt"}
				if !reflect.DeepEqual(got.PromptOverrides, want) {
					t.Errorf("PromptOverrides = %v, want %v", got.PromptOverrides, want)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := agent.ResolveConfig(&tc.strategy, tc.global)
			tc.check(t, got)
		})
	}
}

// TestResolveConfig_GlobalFallback verifies that when strategy-level values are
// absent (nil), global settings are used for every field.
func TestResolveConfig_GlobalFallback(t *testing.T) {
	tests := []struct {
		name     string
		strategy agent.StrategyConfig
		global   agent.GlobalSettings
		check    func(t *testing.T, got agent.ResolvedConfig)
	}{
		{
			name:     "LLMConfig.Provider from global",
			strategy: agent.StrategyConfig{},
			global: agent.GlobalSettings{
				LLMConfig: &agent.StrategyLLMConfig{Provider: strPtr("google")},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.LLMConfig.Provider != "google" {
					t.Errorf("Provider = %q, want %q", got.LLMConfig.Provider, "google")
				}
			},
		},
		{
			name:     "LLMConfig.DeepThinkModel from global",
			strategy: agent.StrategyConfig{},
			global: agent.GlobalSettings{
				LLMConfig: &agent.StrategyLLMConfig{DeepThinkModel: strPtr("gemini-2.5-flash")},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.LLMConfig.DeepThinkModel != "gemini-2.5-flash" {
					t.Errorf("DeepThinkModel = %q, want %q", got.LLMConfig.DeepThinkModel, "gemini-2.5-flash")
				}
			},
		},
		{
			name:     "LLMConfig.QuickThinkModel from global",
			strategy: agent.StrategyConfig{},
			global: agent.GlobalSettings{
				LLMConfig: &agent.StrategyLLMConfig{QuickThinkModel: strPtr("grok-3-mini")},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.LLMConfig.QuickThinkModel != "grok-3-mini" {
					t.Errorf("QuickThinkModel = %q, want %q", got.LLMConfig.QuickThinkModel, "grok-3-mini")
				}
			},
		},
		{
			name:     "PipelineConfig.DebateRounds from global",
			strategy: agent.StrategyConfig{},
			global: agent.GlobalSettings{
				PipelineConfig: &agent.StrategyPipelineConfig{DebateRounds: intPtr(7)},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.PipelineConfig.DebateRounds != 7 {
					t.Errorf("DebateRounds = %d, want %d", got.PipelineConfig.DebateRounds, 7)
				}
			},
		},
		{
			name:     "PipelineConfig.AnalysisTimeoutSeconds from global",
			strategy: agent.StrategyConfig{},
			global: agent.GlobalSettings{
				PipelineConfig: &agent.StrategyPipelineConfig{AnalysisTimeoutSeconds: intPtr(45)},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.PipelineConfig.AnalysisTimeoutSeconds != 45 {
					t.Errorf("AnalysisTimeoutSeconds = %d, want %d", got.PipelineConfig.AnalysisTimeoutSeconds, 45)
				}
			},
		},
		{
			name:     "PipelineConfig.DebateTimeoutSeconds from global",
			strategy: agent.StrategyConfig{},
			global: agent.GlobalSettings{
				PipelineConfig: &agent.StrategyPipelineConfig{DebateTimeoutSeconds: intPtr(90)},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.PipelineConfig.DebateTimeoutSeconds != 90 {
					t.Errorf("DebateTimeoutSeconds = %d, want %d", got.PipelineConfig.DebateTimeoutSeconds, 90)
				}
			},
		},
		{
			name:     "RiskConfig.PositionSizePct from global",
			strategy: agent.StrategyConfig{},
			global: agent.GlobalSettings{
				RiskConfig: &agent.StrategyRiskConfig{PositionSizePct: float64Ptr(8.0)},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.RiskConfig.PositionSizePct != 8.0 {
					t.Errorf("PositionSizePct = %g, want %g", got.RiskConfig.PositionSizePct, 8.0)
				}
			},
		},
		{
			name:     "RiskConfig.StopLossMultiplier from global",
			strategy: agent.StrategyConfig{},
			global: agent.GlobalSettings{
				RiskConfig: &agent.StrategyRiskConfig{StopLossMultiplier: float64Ptr(1.2)},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.RiskConfig.StopLossMultiplier != 1.2 {
					t.Errorf("StopLossMultiplier = %g, want %g", got.RiskConfig.StopLossMultiplier, 1.2)
				}
			},
		},
		{
			name:     "RiskConfig.TakeProfitMultiplier from global",
			strategy: agent.StrategyConfig{},
			global: agent.GlobalSettings{
				RiskConfig: &agent.StrategyRiskConfig{TakeProfitMultiplier: float64Ptr(1.8)},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.RiskConfig.TakeProfitMultiplier != 1.8 {
					t.Errorf("TakeProfitMultiplier = %g, want %g", got.RiskConfig.TakeProfitMultiplier, 1.8)
				}
			},
		},
		{
			name:     "RiskConfig.MinConfidence from global",
			strategy: agent.StrategyConfig{},
			global: agent.GlobalSettings{
				RiskConfig: &agent.StrategyRiskConfig{MinConfidence: float64Ptr(0.7)},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.RiskConfig.MinConfidence != 0.7 {
					t.Errorf("MinConfidence = %g, want %g", got.RiskConfig.MinConfidence, 0.7)
				}
			},
		},
		{
			name:     "AnalystSelection from global",
			strategy: agent.StrategyConfig{},
			global: agent.GlobalSettings{
				AnalystSelection: []agent.AgentRole{agent.AgentRoleNewsAnalyst, agent.AgentRoleSocialMediaAnalyst},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				want := []agent.AgentRole{agent.AgentRoleNewsAnalyst, agent.AgentRoleSocialMediaAnalyst}
				if !reflect.DeepEqual(got.AnalystSelection, want) {
					t.Errorf("AnalystSelection = %v, want %v", got.AnalystSelection, want)
				}
			},
		},
		{
			name:     "PromptOverrides from global",
			strategy: agent.StrategyConfig{},
			global: agent.GlobalSettings{
				PromptOverrides: map[agent.AgentRole]string{
					agent.AgentRoleRiskManager: "global risk prompt",
				},
			},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				want := map[agent.AgentRole]string{agent.AgentRoleRiskManager: "global risk prompt"}
				if !reflect.DeepEqual(got.PromptOverrides, want) {
					t.Errorf("PromptOverrides = %v, want %v", got.PromptOverrides, want)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := agent.ResolveConfig(&tc.strategy, tc.global)
			tc.check(t, got)
		})
	}
}

// TestResolveConfig_HardcodedDefaults verifies that when both strategy and global
// settings are absent, hardcoded defaults are used for every field.
func TestResolveConfig_HardcodedDefaults(t *testing.T) {
	got := agent.ResolveConfig(nil, agent.GlobalSettings{})

	if got.LLMConfig.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", got.LLMConfig.Provider, "openai")
	}
	if got.LLMConfig.DeepThinkModel != "gpt-5.2" {
		t.Errorf("DeepThinkModel = %q, want %q", got.LLMConfig.DeepThinkModel, "gpt-5.2")
	}
	if got.LLMConfig.QuickThinkModel != "gpt-5-mini" {
		t.Errorf("QuickThinkModel = %q, want %q", got.LLMConfig.QuickThinkModel, "gpt-5-mini")
	}
	if got.PipelineConfig.DebateRounds != 3 {
		t.Errorf("DebateRounds = %d, want %d", got.PipelineConfig.DebateRounds, 3)
	}
	if got.PipelineConfig.AnalysisTimeoutSeconds != 300 {
		t.Errorf("AnalysisTimeoutSeconds = %d, want %d", got.PipelineConfig.AnalysisTimeoutSeconds, 300)
	}
	if got.PipelineConfig.DebateTimeoutSeconds != 600 {
		t.Errorf("DebateTimeoutSeconds = %d, want %d", got.PipelineConfig.DebateTimeoutSeconds, 600)
	}
	if got.RiskConfig.PositionSizePct != 5.0 {
		t.Errorf("PositionSizePct = %g, want %g", got.RiskConfig.PositionSizePct, 5.0)
	}
	if got.RiskConfig.StopLossMultiplier != 1.5 {
		t.Errorf("StopLossMultiplier = %g, want %g", got.RiskConfig.StopLossMultiplier, 1.5)
	}
	if got.RiskConfig.TakeProfitMultiplier != 2.0 {
		t.Errorf("TakeProfitMultiplier = %g, want %g", got.RiskConfig.TakeProfitMultiplier, 2.0)
	}
	if got.RiskConfig.MinConfidence != 0.65 {
		t.Errorf("MinConfidence = %g, want %g", got.RiskConfig.MinConfidence, 0.65)
	}
	if got.AnalystSelection != nil {
		t.Errorf("AnalystSelection = %v, want nil (all analysts enabled)", got.AnalystSelection)
	}
	if got.PromptOverrides != nil {
		t.Errorf("PromptOverrides = %v, want nil (no overrides)", got.PromptOverrides)
	}
}

// TestResolveConfig_NilStrategyConfig verifies that passing nil as strategyConfig
// behaves identically to passing an empty StrategyConfig.
func TestResolveConfig_NilStrategyConfig(t *testing.T) {
	global := agent.GlobalSettings{
		LLMConfig: &agent.StrategyLLMConfig{Provider: strPtr("xai")},
	}
	fromNil := agent.ResolveConfig(nil, global)
	fromEmpty := agent.ResolveConfig(&agent.StrategyConfig{}, global)

	if fromNil.LLMConfig.Provider != fromEmpty.LLMConfig.Provider {
		t.Errorf("nil vs empty StrategyConfig produced different Provider: %q vs %q",
			fromNil.LLMConfig.Provider, fromEmpty.LLMConfig.Provider)
	}
}

// TestResolveConfig_PartialStrategyOverride verifies that individual nil sub-fields
// within a non-nil strategy section fall through to global or default correctly.
func TestResolveConfig_PartialStrategyOverride(t *testing.T) {
	// Strategy sets Provider but not DeepThinkModel or QuickThinkModel.
	// Global sets DeepThinkModel. QuickThinkModel should come from hardcoded default.
	strategy := agent.StrategyConfig{
		LLMConfig: &agent.StrategyLLMConfig{
			Provider: strPtr("anthropic"),
			// DeepThinkModel: nil
			// QuickThinkModel: nil
		},
	}
	global := agent.GlobalSettings{
		LLMConfig: &agent.StrategyLLMConfig{
			DeepThinkModel: strPtr("claude-3-7-sonnet-latest"),
		},
	}

	got := agent.ResolveConfig(&strategy, global)

	if got.LLMConfig.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", got.LLMConfig.Provider, "anthropic")
	}
	if got.LLMConfig.DeepThinkModel != "claude-3-7-sonnet-latest" {
		t.Errorf("DeepThinkModel = %q, want %q", got.LLMConfig.DeepThinkModel, "claude-3-7-sonnet-latest")
	}
	if got.LLMConfig.QuickThinkModel != "gpt-5-mini" {
		t.Errorf("QuickThinkModel = %q, want %q (hardcoded default)", got.LLMConfig.QuickThinkModel, "gpt-5-mini")
	}
}

// TestResolveConfig_AnalystSelectionIsCopied verifies that mutating the
// AnalystSelection slice in the returned ResolvedConfig does not affect the
// original StrategyConfig input.
func TestResolveConfig_AnalystSelectionIsCopied(t *testing.T) {
	strategy := agent.StrategyConfig{
		AnalystSelection: []agent.AgentRole{agent.AgentRoleMarketAnalyst, agent.AgentRoleFundamentalsAnalyst},
	}
	got := agent.ResolveConfig(&strategy, agent.GlobalSettings{})

	// Mutate the returned slice.
	got.AnalystSelection[0] = agent.AgentRoleTrader

	// Original must be unchanged.
	if strategy.AnalystSelection[0] != agent.AgentRoleMarketAnalyst {
		t.Errorf("original AnalystSelection[0] was mutated; got %q, want %q",
			strategy.AnalystSelection[0], agent.AgentRoleMarketAnalyst)
	}
}

// TestResolveConfig_PromptOverridesIsCopied verifies that mutating the
// PromptOverrides map in the returned ResolvedConfig does not affect the
// original StrategyConfig input.
func TestResolveConfig_PromptOverridesIsCopied(t *testing.T) {
	strategy := agent.StrategyConfig{
		PromptOverrides: map[agent.AgentRole]string{
			agent.AgentRoleTrader: "original prompt",
		},
	}
	got := agent.ResolveConfig(&strategy, agent.GlobalSettings{})

	// Mutate the returned map.
	got.PromptOverrides[agent.AgentRoleTrader] = "mutated prompt"

	// Original must be unchanged.
	if strategy.PromptOverrides[agent.AgentRoleTrader] != "original prompt" {
		t.Errorf("original PromptOverrides was mutated; got %q, want %q",
			strategy.PromptOverrides[agent.AgentRoleTrader], "original prompt")
	}
}

func TestResolveConfig_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		strategy *agent.StrategyConfig
		global   agent.GlobalSettings
		check    func(t *testing.T, got agent.ResolvedConfig)
	}{
		{
			name:     "empty strategy and global uses hardcoded defaults",
			strategy: &agent.StrategyConfig{},
			global:   agent.GlobalSettings{},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.LLMConfig.Provider != "openai" || got.LLMConfig.DeepThinkModel != "gpt-5.2" || got.LLMConfig.QuickThinkModel != "gpt-5-mini" {
					t.Fatalf("LLM defaults = %+v", got.LLMConfig)
				}
				if got.PipelineConfig.DebateRounds != 3 || got.PipelineConfig.AnalysisTimeoutSeconds != 300 || got.PipelineConfig.DebateTimeoutSeconds != 600 {
					t.Fatalf("pipeline defaults = %+v", got.PipelineConfig)
				}
				if got.RiskConfig.PositionSizePct != 5.0 || got.RiskConfig.StopLossMultiplier != 1.5 || got.RiskConfig.TakeProfitMultiplier != 2.0 || got.RiskConfig.MinConfidence != 0.65 {
					t.Fatalf("risk defaults = %+v", got.RiskConfig)
				}
			},
		},
		{
			name: "prompt overrides propagate from strategy",
			strategy: &agent.StrategyConfig{
				PromptOverrides: map[agent.AgentRole]string{
					agent.AgentRoleTrader:      "trader override",
					agent.AgentRoleRiskManager: "risk override",
				},
			},
			global: agent.GlobalSettings{},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				want := map[agent.AgentRole]string{
					agent.AgentRoleTrader:      "trader override",
					agent.AgentRoleRiskManager: "risk override",
				}
				if !reflect.DeepEqual(got.PromptOverrides, want) {
					t.Fatalf("PromptOverrides = %v, want %v", got.PromptOverrides, want)
				}
			},
		},
		{
			name: "analyst selection copies exact subset",
			strategy: &agent.StrategyConfig{
				AnalystSelection: []agent.AgentRole{agent.AgentRoleMarketAnalyst, agent.AgentRoleNewsAnalyst},
			},
			global: agent.GlobalSettings{},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				want := []agent.AgentRole{agent.AgentRoleMarketAnalyst, agent.AgentRoleNewsAnalyst}
				if !reflect.DeepEqual(got.AnalystSelection, want) {
					t.Fatalf("AnalystSelection = %v, want %v", got.AnalystSelection, want)
				}
			},
		},
		{
			name: "zero and boundary values survive resolution",
			strategy: &agent.StrategyConfig{
				PipelineConfig: &agent.StrategyPipelineConfig{
					DebateRounds:           intPtr(0),
					AnalysisTimeoutSeconds: intPtr(1),
					DebateTimeoutSeconds:   intPtr(0),
				},
				RiskConfig: &agent.StrategyRiskConfig{
					PositionSizePct:    float64Ptr(0.0),
					MinConfidence:      float64Ptr(1.0),
					StopLossMultiplier: float64Ptr(0.1),
				},
			},
			global: agent.GlobalSettings{},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.PipelineConfig.DebateRounds != 0 || got.PipelineConfig.AnalysisTimeoutSeconds != 1 || got.PipelineConfig.DebateTimeoutSeconds != 0 {
					t.Fatalf("pipeline boundaries = %+v", got.PipelineConfig)
				}
				if got.RiskConfig.PositionSizePct != 0.0 || got.RiskConfig.MinConfidence != 1.0 || got.RiskConfig.StopLossMultiplier != 0.1 {
					t.Fatalf("risk boundaries = %+v", got.RiskConfig)
				}
			},
		},
		{
			name: "partial strategy llm override preserves defaults elsewhere",
			strategy: &agent.StrategyConfig{
				LLMConfig: &agent.StrategyLLMConfig{
					Provider:        strPtr("anthropic"),
					DeepThinkModel:  strPtr("claude-3-7-sonnet-latest"),
					QuickThinkModel: strPtr("gpt-5-mini"),
				},
			},
			global: agent.GlobalSettings{},
			check: func(t *testing.T, got agent.ResolvedConfig) {
				t.Helper()
				if got.LLMConfig.Provider != "anthropic" || got.LLMConfig.DeepThinkModel != "claude-3-7-sonnet-latest" || got.LLMConfig.QuickThinkModel != "gpt-5-mini" {
					t.Fatalf("LLM override = %+v", got.LLMConfig)
				}
				if got.RiskConfig.MinConfidence != 0.65 {
					t.Fatalf("risk defaults changed unexpectedly: %+v", got.RiskConfig)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := agent.ResolveConfig(tc.strategy, tc.global)
			tc.check(t, got)
		})
	}
}
