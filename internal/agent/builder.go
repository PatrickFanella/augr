package agent

import (
	"fmt"
	"log/slog"
	"strings"
)

// PipelineBuilder validates that all required nodes are registered before
// constructing a Pipeline. Use Build to obtain a ready-to-execute Pipeline.
type PipelineBuilder struct {
	config    PipelineConfig
	persister DecisionPersister
	events    chan<- PipelineEvent
	logger    *slog.Logger
	nodes     []Node
}

// NewPipelineBuilder returns a builder for constructing validated Pipelines.
func NewPipelineBuilder(config PipelineConfig, persister DecisionPersister, events chan<- PipelineEvent, logger *slog.Logger) *PipelineBuilder {
	return &PipelineBuilder{
		config:    config,
		persister: persister,
		events:    events,
		logger:    logger,
	}
}

// RegisterNode adds a node to the builder. It is chainable.
func (b *PipelineBuilder) RegisterNode(node Node) *PipelineBuilder {
	b.nodes = append(b.nodes, node)
	return b
}

// requiredRoles lists the phase and role that must be present for a valid pipeline.
var requiredRoles = []struct {
	phase Phase
	role  AgentRole
}{
	{PhaseResearchDebate, AgentRoleBullResearcher},
	{PhaseResearchDebate, AgentRoleBearResearcher},
	{PhaseResearchDebate, AgentRoleInvestJudge},
	{PhaseTrading, AgentRoleTrader},
	{PhaseRiskDebate, AgentRoleAggressiveAnalyst},
	{PhaseRiskDebate, AgentRoleConservativeAnalyst},
	{PhaseRiskDebate, AgentRoleNeutralAnalyst},
	{PhaseRiskDebate, AgentRoleRiskManager},
}

// Build validates that all required nodes are registered and returns a Pipeline.
// It returns a descriptive error listing every missing role if validation fails.
func (b *PipelineBuilder) Build() (*Pipeline, error) {
	type phaseRole struct {
		phase Phase
		role  AgentRole
	}
	registered := make(map[phaseRole]bool, len(b.nodes))
	hasAnalyst := false
	for _, n := range b.nodes {
		registered[phaseRole{n.Phase(), n.Role()}] = true
		if n.Phase() == PhaseAnalysis {
			hasAnalyst = true
		}
	}

	var missing []string
	if !hasAnalyst {
		missing = append(missing, "at least one analysis node")
	}
	for _, req := range requiredRoles {
		if !registered[phaseRole{req.phase, req.role}] {
			missing = append(missing, fmt.Sprintf("%s/%s", req.phase, req.role))
		}
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("agent/pipeline: missing required nodes: %s", strings.Join(missing, ", "))
	}

	p := NewPipeline(b.config, b.persister, b.events, b.logger)
	for _, n := range b.nodes {
		p.RegisterNode(n)
	}
	return p, nil
}
