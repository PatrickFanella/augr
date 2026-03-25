package agent

import "context"

// Node represents a single executable step in the agent pipeline.
type Node interface {
	Name() string
	// Role returns the agent role that uniquely identifies this node. It must
	// correspond to one of the defined AgentRole constants so that pipeline
	// events and state reports carry a valid, meaningful role.
	Role() AgentRole
	Phase() Phase
	Execute(ctx context.Context, state *PipelineState) error
}

// AnalystNode is an optional interface for analysis-phase nodes that prefer
// typed input/output over direct state mutation.
type AnalystNode interface {
	Node
	Analyze(ctx context.Context, input AnalysisInput) (AnalysisOutput, error)
}

// DebaterNode is an optional interface for debate-phase nodes.
type DebaterNode interface {
	Node
	Debate(ctx context.Context, input DebateInput) (DebateOutput, error)
}

// TraderNode is an optional interface for trading-phase nodes.
type TraderNode interface {
	Node
	Trade(ctx context.Context, input TradingInput) (TradingOutput, error)
}

// RiskJudgeNode is an optional interface for the risk manager node.
type RiskJudgeNode interface {
	Node
	JudgeRisk(ctx context.Context, input RiskJudgeInput) (RiskJudgeOutput, error)
}
