---
title: "Agent Orchestration Engine"
date: 2026-03-20
tags: [backend, orchestration, dag, state-machine, agents]
---

# Agent Orchestration Engine

The orchestration engine replaces LangGraph with a custom Go DAG executor. It manages the full pipeline lifecycle: analysts → research debate → trader → risk debate → signal extraction.

## Why Not Use LangGraph (or Port It)

LangGraph is Python-only with no Go equivalent. Rather than wrapping Python, we build a purpose-specific DAG engine that:

- Leverages goroutines for parallel node execution
- Uses Go interfaces for compile-time agent contracts
- Streams events via channels (natural fit for WebSocket push)
- Is testable without LLM calls (mock providers)

## Core Abstractions

### Node Interface

Every agent in the pipeline implements `Node`:

```go
// internal/agent/node.go
type Node interface {
    // Name returns the unique identifier for this node
    Name() string

    // Execute runs the agent and returns updated state
    Execute(ctx context.Context, state *PipelineState) error
}
```

### Pipeline State

Shared mutable state that flows through the pipeline:

```go
// internal/agent/state.go
type PipelineState struct {
    // Inputs
    Ticker    string
    TradeDate time.Time
    Config    StrategyConfig

    // Phase 1: Analysis
    MarketReport       string
    FundamentalsReport string
    NewsReport         string
    SocialReport       string

    // Phase 2: Research Debate
    InvestDebate InvestDebateState

    // Phase 3: Trading
    TradingPlan string

    // Phase 4: Risk Debate
    RiskDebate RiskDebateState

    // Phase 5: Signal
    Signal     string // "buy", "sell", "hold"
    Confidence float64

    // Metadata
    RunID     uuid.UUID
    Events    chan<- PipelineEvent // stream to WebSocket
    StartedAt time.Time
    Errors    []error
}

type InvestDebateState struct {
    Messages     []DebateMessage
    JudgeResponse string
    RoundCount    int
}

type RiskDebateState struct {
    Messages     []DebateMessage
    JudgeResponse string
    RoundCount    int
}

type DebateMessage struct {
    Role    string // "bull", "bear", "aggressive", "conservative", "neutral"
    Content string
    Round   int
}
```

### Pipeline Event

Events are emitted to a channel for WebSocket streaming:

```go
type PipelineEvent struct {
    Type      string    // "agent_decision", "debate_round", "signal", etc.
    RunID     uuid.UUID
    AgentRole string
    Phase     string
    Round     int
    Data      any
    Timestamp time.Time
}
```

## DAG Definition

The pipeline is a directed acyclic graph with conditional edges:

```
                    ┌── MarketAnalyst ──┐
                    ├── FundAnalyst ────┤
  START ──(parallel)├── NewsAnalyst ────┼──► BullResearcher ◄─┐
                    └── SocialAnalyst ──┘        │            │
                                                 ▼            │
                                           BearResearcher ────┘
                                                 │      (loop N rounds)
                                                 ▼
                                          ResearchManager
                                                 │
                                                 ▼
                                            TraderAgent
                                                 │
                                                 ▼
                                       AggressiveAnalyst ◄─┐
                                                │          │
                                       ConservativeAnalyst │
                                                │          │
                                         NeutralAnalyst ───┘
                                                │    (loop N rounds)
                                                ▼
                                           RiskManager
                                                │
                                                ▼
                                        SignalExtractor
                                                │
                                               END
```

## Orchestrator Implementation

```go
// internal/agent/orchestrator.go
type Orchestrator struct {
    llmFactory  llm.Factory
    memoryStore memory.Store
    dataProviders []data.MarketDataProvider
    eventBus    chan<- PipelineEvent
    config      OrchestratorConfig
}

type OrchestratorConfig struct {
    MaxDebateRounds     int
    MaxRiskDebateRounds int
    SelectedAnalysts    []string
    DeepThinkLLM        llm.ProviderConfig
    QuickThinkLLM       llm.ProviderConfig
}

func (o *Orchestrator) Run(ctx context.Context, state *PipelineState) error {
    // Phase 1: Analysts (parallel)
    if err := o.runAnalysts(ctx, state); err != nil {
        return fmt.Errorf("analyst phase: %w", err)
    }

    // Phase 2: Research Debate
    if err := o.runResearchDebate(ctx, state); err != nil {
        return fmt.Errorf("research debate: %w", err)
    }

    // Phase 3: Trader
    if err := o.runTrader(ctx, state); err != nil {
        return fmt.Errorf("trader: %w", err)
    }

    // Phase 4: Risk Debate
    if err := o.runRiskDebate(ctx, state); err != nil {
        return fmt.Errorf("risk debate: %w", err)
    }

    // Phase 5: Signal Extraction
    return o.extractSignal(state)
}
```

### Parallel Analyst Execution

```go
func (o *Orchestrator) runAnalysts(ctx context.Context, state *PipelineState) error {
    analysts := o.buildAnalysts(state.Config.SelectedAnalysts)

    g, ctx := errgroup.WithContext(ctx)
    for _, analyst := range analysts {
        a := analyst // capture loop variable
        g.Go(func() error {
            if err := a.Execute(ctx, state); err != nil {
                return fmt.Errorf("%s: %w", a.Name(), err)
            }
            o.emitEvent(PipelineEvent{
                Type:      "agent_decision",
                RunID:     state.RunID,
                AgentRole: a.Name(),
                Phase:     "analysis",
            })
            return nil
        })
    }
    return g.Wait()
}
```

### Debate Loop

```go
func (o *Orchestrator) runResearchDebate(ctx context.Context, state *PipelineState) error {
    bull := research.NewBullResearcher(o.llmFactory, o.memoryStore)
    bear := research.NewBearResearcher(o.llmFactory, o.memoryStore)
    manager := research.NewResearchManager(o.llmFactory, o.memoryStore)

    for round := 1; round <= o.config.MaxDebateRounds; round++ {
        // Bull argues
        if err := bull.Execute(ctx, state); err != nil {
            return err
        }
        state.InvestDebate.RoundCount = round

        // Bear counters
        if err := bear.Execute(ctx, state); err != nil {
            return err
        }

        o.emitEvent(PipelineEvent{
            Type:  "debate_round",
            Phase: "research_debate",
            Round: round,
        })
    }

    // Judge renders verdict
    return manager.Execute(ctx, state)
}
```

## Persistence Integration

The orchestrator saves each agent's output to the database after execution:

```go
func (o *Orchestrator) persistDecision(ctx context.Context, state *PipelineState, node Node, output string) {
    o.decisionRepo.Create(ctx, &domain.AgentDecision{
        PipelineRunID: state.RunID,
        AgentRole:     node.Name(),
        Phase:         currentPhase(node),
        OutputText:    output,
        CreatedAt:     time.Now(),
    })
}
```

## Testing Strategy

1. **Unit tests** — Mock `llm.Provider` to return canned responses; verify state transitions
2. **Integration tests** — Real PostgreSQL, mock LLM; verify full pipeline persistence
3. **End-to-end tests** — Real LLM (with test API key); verify signal extraction

```go
func TestOrchestrator_FullPipeline(t *testing.T) {
    mockLLM := &MockProvider{
        Responses: map[string]string{
            "market_analyst":    "Bullish trend detected...",
            "bull_researcher":   "Strong momentum...",
            // ...
        },
    }
    orch := NewOrchestrator(mockLLM, ...)
    state := &PipelineState{Ticker: "AAPL", TradeDate: time.Now()}

    err := orch.Run(context.Background(), state)
    require.NoError(t, err)
    assert.Contains(t, []string{"buy", "sell", "hold"}, state.Signal)
}
```

---

**Related:** [[agent-system-overview]] · [[research-debate-system]] · [[risk-management-agents]] · [[go-project-structure]]
