package agent

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func builderLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type stubBuilderNode struct {
	name  string
	role  AgentRole
	phase Phase
}

func (n *stubBuilderNode) Name() string    { return n.name }
func (n *stubBuilderNode) Role() AgentRole { return n.role }
func (n *stubBuilderNode) Phase() Phase    { return n.phase }
func (n *stubBuilderNode) Execute(_ context.Context, _ *PipelineState) error {
	return nil
}

// allRequiredNodes returns the minimum set of nodes for a valid pipeline.
func allRequiredNodes() []Node {
	return []Node{
		&stubBuilderNode{"market_analyst", AgentRoleMarketAnalyst, PhaseAnalysis},
		&stubBuilderNode{"bull", AgentRoleBullResearcher, PhaseResearchDebate},
		&stubBuilderNode{"bear", AgentRoleBearResearcher, PhaseResearchDebate},
		&stubBuilderNode{"judge", AgentRoleInvestJudge, PhaseResearchDebate},
		&stubBuilderNode{"trader", AgentRoleTrader, PhaseTrading},
		&stubBuilderNode{"aggressive", AgentRoleAggressiveAnalyst, PhaseRiskDebate},
		&stubBuilderNode{"conservative", AgentRoleConservativeAnalyst, PhaseRiskDebate},
		&stubBuilderNode{"neutral", AgentRoleNeutralAnalyst, PhaseRiskDebate},
		&stubBuilderNode{"risk_mgr", AgentRoleRiskManager, PhaseRiskDebate},
	}
}

func TestPipelineBuilder_AllNodesRegistered(t *testing.T) {
	b := NewPipelineBuilder(PipelineConfig{}, nil, nil, builderLogger())
	for _, n := range allRequiredNodes() {
		b.RegisterNode(n)
	}

	p, err := b.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if p == nil {
		t.Fatal("Build() returned nil pipeline")
	}
}

func TestPipelineBuilder_MissingSingleRole(t *testing.T) {
	nodes := allRequiredNodes()
	// Skip the trader node (index 4).
	b := NewPipelineBuilder(PipelineConfig{}, nil, nil, builderLogger())
	for i, n := range nodes {
		if i == 4 {
			continue
		}
		b.RegisterNode(n)
	}

	_, err := b.Build()
	if err == nil {
		t.Fatal("Build() error = nil, want error for missing trader")
	}
	if !strings.Contains(err.Error(), "trading/trader") {
		t.Fatalf("error = %q, want it to mention trading/trader", err)
	}
}

func TestPipelineBuilder_MissingMultipleRoles(t *testing.T) {
	// Only register analysis node.
	b := NewPipelineBuilder(PipelineConfig{}, nil, nil, builderLogger())
	b.RegisterNode(&stubBuilderNode{"analyst", AgentRoleMarketAnalyst, PhaseAnalysis})

	_, err := b.Build()
	if err == nil {
		t.Fatal("Build() error = nil, want error for missing roles")
	}
	for _, want := range []string{"research_debate/bull_researcher", "trading/trader", "risk_debate/risk_manager"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %s", want, err)
		}
	}
}

func TestPipelineBuilder_NoAnalysisNodes(t *testing.T) {
	b := NewPipelineBuilder(PipelineConfig{}, nil, nil, builderLogger())
	// Register everything except analysis nodes.
	for _, n := range allRequiredNodes()[1:] {
		b.RegisterNode(n)
	}

	_, err := b.Build()
	if err == nil {
		t.Fatal("Build() error = nil, want error for missing analysis node")
	}
	if !strings.Contains(err.Error(), "at least one analysis node") {
		t.Fatalf("error = %q, want analysis node message", err)
	}
}

func TestPipelineBuilder_Chainable(t *testing.T) {
	b := NewPipelineBuilder(PipelineConfig{}, nil, nil, builderLogger())
	result := b.RegisterNode(&stubBuilderNode{"a", AgentRoleMarketAnalyst, PhaseAnalysis})
	if result != b {
		t.Fatal("RegisterNode should return the same builder for chaining")
	}
}
