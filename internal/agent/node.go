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
