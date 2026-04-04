package rules

import (
	"log/slog"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// NewRulesPipeline constructs a Pipeline pre-configured for deterministic
// rules-only execution. Only the indicator analyst and rules trader nodes
// are registered; research debate and risk debate phases are skipped.
func NewRulesPipeline(
	config RulesEngineConfig,
	bars []domain.OHLCV,
	equity float64,
	persister agent.DecisionPersister,
	events chan<- agent.PipelineEvent,
	logger *slog.Logger,
) *agent.Pipeline {
	pipeline := agent.NewPipeline(agent.PipelineConfig{
		SkipPhases: map[agent.Phase]bool{
			agent.PhaseResearchDebate: true,
			agent.PhaseRiskDebate:     true,
		},
	}, persister, events, logger)

	pipeline.RegisterNode(NewIndicatorAnalystNode(bars, logger))
	pipeline.RegisterNode(NewRulesTraderNode(config, equity, logger))
	return pipeline
}
