package agent

// Hardcoded defaults used as the final fallback in ResolveConfig.
const (
	defaultLLMProvider               = "openai"
	defaultLLMDeepThinkModel         = "gpt-5.2"
	defaultLLMQuickThinkModel        = "gpt-5-mini"
	defaultPipelineDebateRounds      = 3
	defaultAnalysisTimeoutSeconds    = 30
	defaultDebateTimeoutSeconds      = 60
	defaultPositionSizePct           = 5.0
	defaultStopLossMultiplier        = 1.5
	defaultTakeProfitMultiplier      = 2.0
	defaultMinConfidence             = 0.65
)

// GlobalSettings holds optional system-wide defaults for strategy execution.
// Fields use the same pointer-based convention as StrategyConfig: a nil pointer
// means "no global setting; fall through to the hardcoded default".
type GlobalSettings struct {
	// LLMConfig provides system-wide LLM provider and model defaults.
	LLMConfig *StrategyLLMConfig
	// PipelineConfig provides system-wide debate and timeout defaults.
	PipelineConfig *StrategyPipelineConfig
	// RiskConfig provides system-wide position-sizing and risk-limit defaults.
	RiskConfig *StrategyRiskConfig
	// AnalystSelection restricts which analyst roles run across all strategies.
	// A nil slice means all analysts are enabled.
	AnalystSelection []AgentRole
	// PromptOverrides provides system-wide prompt replacements for specific roles.
	// A nil map means no system-wide overrides are applied.
	PromptOverrides map[AgentRole]string
}

// ResolvedLLMConfig is the fully-resolved LLM configuration with no pointer fields.
type ResolvedLLMConfig struct {
	// Provider is the LLM provider name (e.g. "openai", "anthropic").
	Provider string
	// DeepThinkModel is the model used for deep-reasoning tasks.
	DeepThinkModel string
	// QuickThinkModel is the model used for fast-response tasks.
	QuickThinkModel string
}

// ResolvedPipelineConfig is the fully-resolved pipeline configuration with no pointer fields.
type ResolvedPipelineConfig struct {
	// DebateRounds is the number of research/risk debate rounds.
	DebateRounds int
	// AnalysisTimeoutSeconds is the per-agent analysis timeout in seconds.
	AnalysisTimeoutSeconds int
	// DebateTimeoutSeconds is the per-round debate timeout in seconds.
	DebateTimeoutSeconds int
}

// ResolvedRiskConfig is the fully-resolved risk configuration with no pointer fields.
type ResolvedRiskConfig struct {
	// PositionSizePct is the fraction of portfolio to allocate (0–100, percent).
	PositionSizePct float64
	// StopLossMultiplier scales the default stop-loss distance.
	StopLossMultiplier float64
	// TakeProfitMultiplier scales the default take-profit distance.
	TakeProfitMultiplier float64
	// MinConfidence is the minimum signal confidence required to enter a trade (0–1).
	MinConfidence float64
}

// ResolvedConfig is the fully-resolved strategy configuration. Every field has a
// concrete value; no pointer fields are present. Use ResolveConfig to obtain one.
type ResolvedConfig struct {
	// LLMConfig holds the resolved LLM provider and model selection.
	LLMConfig ResolvedLLMConfig
	// PipelineConfig holds the resolved debate round and timeout settings.
	PipelineConfig ResolvedPipelineConfig
	// RiskConfig holds the resolved position-sizing and risk-limit settings.
	RiskConfig ResolvedRiskConfig
	// AnalystSelection lists which analyst roles run in the pipeline.
	// A nil slice means all analysts are enabled.
	AnalystSelection []AgentRole
	// PromptOverrides maps agent roles to their overridden system prompts.
	// A nil map means no overrides are applied.
	PromptOverrides map[AgentRole]string
}

// ResolveConfig produces a fully-resolved configuration by merging strategyConfig
// with globalSettings and hardcoded defaults. The resolution order per field is:
//  1. Strategy-level value (if non-nil)
//  2. Global setting value (if non-nil)
//  3. Hardcoded default
//
// The returned ResolvedConfig has no nil pointer fields.
func ResolveConfig(strategyConfig *StrategyConfig, globalSettings GlobalSettings) ResolvedConfig {
	var s StrategyConfig
	if strategyConfig != nil {
		s = *strategyConfig
	}

	var sLLM, gLLM StrategyLLMConfig
	if s.LLMConfig != nil {
		sLLM = *s.LLMConfig
	}
	if globalSettings.LLMConfig != nil {
		gLLM = *globalSettings.LLMConfig
	}

	var sPipeline, gPipeline StrategyPipelineConfig
	if s.PipelineConfig != nil {
		sPipeline = *s.PipelineConfig
	}
	if globalSettings.PipelineConfig != nil {
		gPipeline = *globalSettings.PipelineConfig
	}

	var sRisk, gRisk StrategyRiskConfig
	if s.RiskConfig != nil {
		sRisk = *s.RiskConfig
	}
	if globalSettings.RiskConfig != nil {
		gRisk = *globalSettings.RiskConfig
	}

	return ResolvedConfig{
		LLMConfig: ResolvedLLMConfig{
			Provider:        resolveStringPtr(sLLM.Provider, gLLM.Provider, defaultLLMProvider),
			DeepThinkModel:  resolveStringPtr(sLLM.DeepThinkModel, gLLM.DeepThinkModel, defaultLLMDeepThinkModel),
			QuickThinkModel: resolveStringPtr(sLLM.QuickThinkModel, gLLM.QuickThinkModel, defaultLLMQuickThinkModel),
		},
		PipelineConfig: ResolvedPipelineConfig{
			DebateRounds:           resolveIntPtr(sPipeline.DebateRounds, gPipeline.DebateRounds, defaultPipelineDebateRounds),
			AnalysisTimeoutSeconds: resolveIntPtr(sPipeline.AnalysisTimeoutSeconds, gPipeline.AnalysisTimeoutSeconds, defaultAnalysisTimeoutSeconds),
			DebateTimeoutSeconds:   resolveIntPtr(sPipeline.DebateTimeoutSeconds, gPipeline.DebateTimeoutSeconds, defaultDebateTimeoutSeconds),
		},
		RiskConfig: ResolvedRiskConfig{
			PositionSizePct:      resolveFloat64Ptr(sRisk.PositionSizePct, gRisk.PositionSizePct, defaultPositionSizePct),
			StopLossMultiplier:   resolveFloat64Ptr(sRisk.StopLossMultiplier, gRisk.StopLossMultiplier, defaultStopLossMultiplier),
			TakeProfitMultiplier: resolveFloat64Ptr(sRisk.TakeProfitMultiplier, gRisk.TakeProfitMultiplier, defaultTakeProfitMultiplier),
			MinConfidence:        resolveFloat64Ptr(sRisk.MinConfidence, gRisk.MinConfidence, defaultMinConfidence),
		},
		AnalystSelection: resolveAgentRoles(s.AnalystSelection, globalSettings.AnalystSelection),
		PromptOverrides:  resolvePromptOverrides(s.PromptOverrides, globalSettings.PromptOverrides),
	}
}

// resolveStringPtr returns the first non-nil pointer value, or defaultVal if both are nil.
func resolveStringPtr(strategy, global *string, defaultVal string) string {
	if strategy != nil {
		return *strategy
	}
	if global != nil {
		return *global
	}
	return defaultVal
}

// resolveIntPtr returns the first non-nil pointer value, or defaultVal if both are nil.
func resolveIntPtr(strategy, global *int, defaultVal int) int {
	if strategy != nil {
		return *strategy
	}
	if global != nil {
		return *global
	}
	return defaultVal
}

// resolveFloat64Ptr returns the first non-nil pointer value, or defaultVal if both are nil.
func resolveFloat64Ptr(strategy, global *float64, defaultVal float64) float64 {
	if strategy != nil {
		return *strategy
	}
	if global != nil {
		return *global
	}
	return defaultVal
}

// cloneAgentRoles returns a shallow copy of src, or nil if src is nil.
func cloneAgentRoles(src []AgentRole) []AgentRole {
	if src == nil {
		return nil
	}
	dst := make([]AgentRole, len(src))
	copy(dst, src)
	return dst
}

// resolveAgentRoles returns a copy of the strategy slice if non-nil, then a copy of
// the global slice if non-nil, otherwise nil (meaning all analysts are enabled).
func resolveAgentRoles(strategy, global []AgentRole) []AgentRole {
	if strategy != nil {
		return cloneAgentRoles(strategy)
	}
	if global != nil {
		return cloneAgentRoles(global)
	}
	return nil
}

// clonePromptOverrides returns a shallow copy of src, or nil if src is nil.
func clonePromptOverrides(src map[AgentRole]string) map[AgentRole]string {
	if src == nil {
		return nil
	}
	dst := make(map[AgentRole]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// resolvePromptOverrides returns a copy of the strategy map if non-nil, then a copy of
// the global map if non-nil, otherwise nil (meaning no prompt overrides are applied).
func resolvePromptOverrides(strategy, global map[AgentRole]string) map[AgentRole]string {
	if strategy != nil {
		return clonePromptOverrides(strategy)
	}
	if global != nil {
		return clonePromptOverrides(global)
	}
	return nil
}
