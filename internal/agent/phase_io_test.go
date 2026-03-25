package agent

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
)

func TestAnalysisInputFromState(t *testing.T) {
	market := &MarketData{}
	news := []data.NewsArticle{{Title: "headline"}}
	fundamentals := &data.Fundamentals{}
	social := &data.SocialSentiment{}

	state := &PipelineState{
		Ticker:       "AAPL",
		Market:       market,
		News:         news,
		Fundamentals: fundamentals,
		Social:       social,
	}

	input := analysisInputFromState(state)

	if input.Ticker != "AAPL" {
		t.Errorf("Ticker = %q, want %q", input.Ticker, "AAPL")
	}
	if input.Market != market {
		t.Error("Market pointer mismatch")
	}
	if len(input.News) != 1 || input.News[0].Title != "headline" {
		t.Error("News not copied correctly")
	}
	if input.Fundamentals != fundamentals {
		t.Error("Fundamentals pointer mismatch")
	}
	if input.Social != social {
		t.Error("Social pointer mismatch")
	}
}

func TestAnalysisInputFromState_NilFields(t *testing.T) {
	state := &PipelineState{
		Ticker: "TSLA",
	}

	input := analysisInputFromState(state)

	if input.Ticker != "TSLA" {
		t.Errorf("Ticker = %q, want %q", input.Ticker, "TSLA")
	}
	if input.Market != nil {
		t.Error("Market should be nil")
	}
	if input.News != nil {
		t.Error("News should be nil")
	}
	if input.Fundamentals != nil {
		t.Error("Fundamentals should be nil")
	}
	if input.Social != nil {
		t.Error("Social should be nil")
	}
}

func TestApplyAnalysisOutput(t *testing.T) {
	state := &PipelineState{
		mu: &sync.Mutex{},
	}

	output := AnalysisOutput{
		Report:      "test report",
		LLMResponse: &DecisionLLMResponse{Provider: "test"},
	}

	applyAnalysisOutput(state, AgentRoleMarketAnalyst, output)

	if got := state.GetAnalystReport(AgentRoleMarketAnalyst); got != "test report" {
		t.Errorf("GetAnalystReport = %q, want %q", got, "test report")
	}

	d, ok := state.Decision(AgentRoleMarketAnalyst, PhaseAnalysis, nil)
	if !ok {
		t.Fatal("decision not found")
	}
	if d.OutputText != "test report" {
		t.Errorf("OutputText = %q, want %q", d.OutputText, "test report")
	}
	if d.LLMResponse == nil || d.LLMResponse.Provider != "test" {
		t.Error("LLMResponse not recorded correctly")
	}
}

func TestApplyAnalysisOutput_NilLLMResponse(t *testing.T) {
	state := &PipelineState{
		mu: &sync.Mutex{},
	}

	output := AnalysisOutput{
		Report:      "report without llm",
		LLMResponse: nil,
	}

	applyAnalysisOutput(state, AgentRoleNewsAnalyst, output)

	if got := state.GetAnalystReport(AgentRoleNewsAnalyst); got != "report without llm" {
		t.Errorf("GetAnalystReport = %q, want %q", got, "report without llm")
	}

	// When LLMResponse is nil, no decision should be recorded.
	_, ok := state.Decision(AgentRoleNewsAnalyst, PhaseAnalysis, nil)
	if ok {
		t.Error("decision should not be recorded when LLMResponse is nil")
	}
}

// typedAnalystNode implements both Node and AnalystNode for testing.
type typedAnalystNode struct {
	name string
	role AgentRole
	fn   func(ctx context.Context, input AnalysisInput) (AnalysisOutput, error)
}

func (n *typedAnalystNode) Name() string    { return n.name }
func (n *typedAnalystNode) Role() AgentRole { return n.role }
func (n *typedAnalystNode) Phase() Phase    { return PhaseAnalysis }
func (n *typedAnalystNode) Execute(_ context.Context, _ *PipelineState) error {
	panic("Execute should not be called on a typed AnalystNode")
}
func (n *typedAnalystNode) Analyze(ctx context.Context, input AnalysisInput) (AnalysisOutput, error) {
	return n.fn(ctx, input)
}

func TestPipelineAnalysisPhase_AnalystNodeInterface(t *testing.T) {
	runID := uuid.New()
	stratID := uuid.New()

	node := &typedAnalystNode{
		name: "typed_market_analyst",
		role: AgentRoleMarketAnalyst,
		fn: func(_ context.Context, input AnalysisInput) (AnalysisOutput, error) {
			if input.Ticker != "AAPL" {
				t.Errorf("input.Ticker = %q, want %q", input.Ticker, "AAPL")
			}
			return AnalysisOutput{
				Report:      "typed analysis report",
				LLMResponse: &DecisionLLMResponse{Provider: "test-provider"},
			}, nil
		},
	}

	events := make(chan PipelineEvent, 10)
	pipeline := NewPipeline(
		PipelineConfig{},
		NoopPersister{}, events, slog.Default(),
	)
	pipeline.RegisterNode(node)

	state := &PipelineState{
		PipelineRunID: runID,
		StrategyID:    stratID,
		Ticker:        "AAPL",
		Market:        &MarketData{},
		mu:            &sync.Mutex{},
	}

	err := pipeline.executeAnalysisPhase(context.Background(), state)
	if err != nil {
		t.Fatalf("executeAnalysisPhase() error = %v, want nil", err)
	}

	// Verify the report was set via applyAnalysisOutput.
	if got := state.GetAnalystReport(AgentRoleMarketAnalyst); got != "typed analysis report" {
		t.Errorf("GetAnalystReport = %q, want %q", got, "typed analysis report")
	}

	// Verify the decision was recorded (so decisionPayload can find it).
	d, ok := state.Decision(AgentRoleMarketAnalyst, PhaseAnalysis, nil)
	if !ok {
		t.Fatal("decision not found after AnalystNode execution")
	}
	if d.OutputText != "typed analysis report" {
		t.Errorf("OutputText = %q, want %q", d.OutputText, "typed analysis report")
	}

	// Verify an AgentDecisionMade event was emitted.
	close(events)
	var emitted []PipelineEvent
	for e := range events {
		emitted = append(emitted, e)
	}
	if len(emitted) != 1 {
		t.Fatalf("got %d events, want 1", len(emitted))
	}
	if emitted[0].Type != AgentDecisionMade {
		t.Errorf("event Type = %q, want %q", emitted[0].Type, AgentDecisionMade)
	}
	if emitted[0].AgentRole != AgentRoleMarketAnalyst {
		t.Errorf("event AgentRole = %q, want %q", emitted[0].AgentRole, AgentRoleMarketAnalyst)
	}
}

func TestPipelineAnalysisPhase_MixedNodeTypes(t *testing.T) {
	runID := uuid.New()
	stratID := uuid.New()

	// A typed AnalystNode.
	typedNode := &typedAnalystNode{
		name: "typed_news_analyst",
		role: AgentRoleNewsAnalyst,
		fn: func(_ context.Context, _ AnalysisInput) (AnalysisOutput, error) {
			return AnalysisOutput{
				Report:      "typed news report",
				LLMResponse: &DecisionLLMResponse{Provider: "typed"},
			}, nil
		},
	}

	// A legacy Node (uses Execute).
	legacyNode := &mockAnalystNode{
		name: "legacy_market_analyst",
		role: AgentRoleMarketAnalyst,
		execute: func(_ context.Context, state *PipelineState) error {
			state.SetAnalystReport(AgentRoleMarketAnalyst, "legacy market report")
			return nil
		},
	}

	events := make(chan PipelineEvent, 10)
	pipeline := NewPipeline(
		PipelineConfig{},
		NoopPersister{}, events, slog.Default(),
	)
	pipeline.RegisterNode(typedNode)
	pipeline.RegisterNode(legacyNode)

	state := &PipelineState{
		PipelineRunID: runID,
		StrategyID:    stratID,
		Ticker:        "GOOG",
		mu:            &sync.Mutex{},
	}

	err := pipeline.executeAnalysisPhase(context.Background(), state)
	if err != nil {
		t.Fatalf("executeAnalysisPhase() error = %v, want nil", err)
	}

	// Both reports should be set.
	if got := state.GetAnalystReport(AgentRoleNewsAnalyst); got != "typed news report" {
		t.Errorf("typed report = %q, want %q", got, "typed news report")
	}
	if got := state.GetAnalystReport(AgentRoleMarketAnalyst); got != "legacy market report" {
		t.Errorf("legacy report = %q, want %q", got, "legacy market report")
	}

	// Two events should be emitted (one per successful node).
	close(events)
	var count int
	for range events {
		count++
	}
	if count != 2 {
		t.Errorf("got %d events, want 2", count)
	}
}

func TestPipelineAnalysisPhase_AnalystNodeError(t *testing.T) {
	node := &typedAnalystNode{
		name: "failing_analyst",
		role: AgentRoleSocialMediaAnalyst,
		fn: func(_ context.Context, _ AnalysisInput) (AnalysisOutput, error) {
			return AnalysisOutput{}, context.DeadlineExceeded
		},
	}

	events := make(chan PipelineEvent, 10)
	pipeline := NewPipeline(
		PipelineConfig{},
		NoopPersister{}, events, slog.Default(),
	)
	pipeline.RegisterNode(node)

	state := &PipelineState{
		PipelineRunID: uuid.New(),
		StrategyID:    uuid.New(),
		Ticker:        "MSFT",
		mu:            &sync.Mutex{},
	}

	err := pipeline.executeAnalysisPhase(context.Background(), state)
	if err != nil {
		t.Fatalf("executeAnalysisPhase() error = %v, want nil (partial failure tolerated)", err)
	}

	// No report should be set for the failing node.
	if got := state.GetAnalystReport(AgentRoleSocialMediaAnalyst); got != "" {
		t.Errorf("report = %q, want empty", got)
	}

	// No events should be emitted for the failing node.
	close(events)
	var count int
	for range events {
		count++
	}
	if count != 0 {
		t.Errorf("got %d events, want 0", count)
	}
}
