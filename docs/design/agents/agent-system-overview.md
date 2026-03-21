---
title: "Agent System Overview"
date: 2026-03-20
tags: [agents, architecture, multi-agent, roles]
---

# Agent System Overview

The system decomposes trading decisions into specialized agent roles, mirroring the structure of a real trading firm. This design is based on the TradingAgents framework and informed by research showing multi-agent cross-checking improves decision quality.

## Agent Roster

```
┌─────────────────────────────────────────────────────────┐
│  PHASE 1: ANALYSIS                                       │
│  ┌────────────┐ ┌──────────────┐ ┌──────┐ ┌──────────┐ │
│  │  Market    │ │ Fundamentals │ │ News │ │  Social  │ │
│  │  Analyst   │ │   Analyst    │ │Analyst│ │  Analyst │ │
│  └────────────┘ └──────────────┘ └──────┘ └──────────┘ │
└─────────────────────────┬───────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────┐
│  PHASE 2: RESEARCH DEBATE                                │
│  ┌──────────────┐   ┌──────────────┐                    │
│  │    Bull      │◄─►│    Bear      │  (N rounds)        │
│  │  Researcher  │   │  Researcher  │                    │
│  └──────────────┘   └──────────────┘                    │
│               ┌──────────────┐                          │
│               │   Research   │  (judge)                  │
│               │   Manager    │                          │
│               └──────────────┘                          │
└─────────────────────────┬───────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────┐
│  PHASE 3: TRADING                                        │
│               ┌──────────────┐                          │
│               │   Trader     │                          │
│               │   Agent      │                          │
│               └──────────────┘                          │
└─────────────────────────┬───────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────┐
│  PHASE 4: RISK DEBATE                                    │
│  ┌────────────┐ ┌──────────────┐ ┌─────────┐           │
│  │ Aggressive │ │ Conservative │ │ Neutral │ (N rounds) │
│  └────────────┘ └──────────────┘ └─────────┘           │
│               ┌──────────────┐                          │
│               │    Risk      │  (final judge)            │
│               │   Manager    │                          │
│               └──────────────┘                          │
└─────────────────────────────────────────────────────────┘
```

## Agent Interface

All agents implement the same Go interface:

```go
// internal/agent/node.go
type Node interface {
    Name() string
    Execute(ctx context.Context, state *PipelineState) error
}

// Base agent that all concrete agents embed
type BaseAgent struct {
    name        string
    llm         llm.Provider
    memoryStore memory.Store
    tier        AgentTier // QuickThink or DeepThink
}

func (b *BaseAgent) Name() string { return b.name }

func (b *BaseAgent) buildPrompt(systemPrompt string, state *PipelineState) llm.CompletionRequest {
    // Inject relevant memories into system prompt
    memories, _ := b.memoryStore.GetRelevantMemories(context.Background(), memory.MemoryQuery{
        Situation: fmt.Sprintf("%s analysis on %s", state.Ticker, state.TradeDate),
        AgentRole: b.name,
        Limit:     5,
    })

    enrichedPrompt := systemPrompt
    if len(memories) > 0 {
        enrichedPrompt += "\n\nRELEVANT PAST EXPERIENCE:\n"
        for _, m := range memories {
            enrichedPrompt += fmt.Sprintf("- Situation: %s\n  Lesson: %s\n", m.Situation, m.Recommendation)
        }
    }

    return llm.CompletionRequest{
        SystemPrompt: enrichedPrompt,
        Temperature:  0.3,
    }
}
```

## Agent Lifecycle

1. **Initialization** — Agent is created by the orchestrator with LLM provider and memory store
2. **Context Assembly** — Agent gathers relevant data (market data, previous reports, memories)
3. **LLM Call** — Agent sends prompt to LLM and receives response
4. **State Update** — Agent writes its output to the shared `PipelineState`
5. **Persistence** — Orchestrator saves the decision to `agent_decisions` table
6. **Event Emission** — Decision is broadcast via WebSocket for frontend visualization

## LLM Tier Assignment

| Agent                           | Tier        | Rationale                                  |
| ------------------------------- | ----------- | ------------------------------------------ |
| Market Analyst                  | Quick Think | Structured data analysis, pattern matching |
| Fundamentals Analyst            | Quick Think | Financial statement summarization          |
| News Analyst                    | Quick Think | Sentiment classification                   |
| Social Media Analyst            | Quick Think | Social signal aggregation                  |
| Bull Researcher                 | Deep Think  | Complex argumentative reasoning            |
| Bear Researcher                 | Deep Think  | Counter-argument construction              |
| Research Manager                | Deep Think  | Judgment under conflicting information     |
| Trader Agent                    | Deep Think  | Multi-factor trade plan synthesis          |
| Aggressive/Conservative/Neutral | Deep Think  | Risk perspective reasoning                 |
| Risk Manager                    | Deep Think  | Final judgment with full context           |

## Configurable Analyst Selection

Not all analysts need to run for every pipeline. Users configure which analysts to include:

```go
type StrategyConfig struct {
    SelectedAnalysts []AnalystType // e.g., ["market", "fundamentals", "news"]
    // ...
}
```

This allows:

- Running only market + news for crypto (no fundamentals available)
- Skipping social media for less-discussed tickers
- Faster pipelines with fewer analysts

---

**Related:** [[analyst-agents]] · [[research-debate-system]] · [[trader-agent]] · [[risk-management-agents]] · [[agent-orchestration-engine]]
