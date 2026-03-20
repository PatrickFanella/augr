---
title: "Memory and Learning System"
date: 2026-03-20
tags: [backend, memory, learning, full-text-search, postgresql]
---

# Memory and Learning System

Replaces the TradingAgents in-memory BM25 retrieval with PostgreSQL full-text search. Agents learn from past trading outcomes and recall relevant situations during future decisions.

## Why PostgreSQL FTS Over BM25

| Aspect           | In-Memory BM25 (Reference) | PostgreSQL FTS (Ours)                    |
| ---------------- | -------------------------- | ---------------------------------------- |
| Persistence      | Lost on restart            | Durable                                  |
| Scalability      | Memory-bound               | Disk-backed, indexed                     |
| Maintenance      | Custom implementation      | Battle-tested engine                     |
| Integration      | Separate library           | Same database as everything else         |
| Ranking          | BM25 only                  | `ts_rank` + `ts_rank_cd` (cover density) |
| Language support | Basic tokenization         | Full stemming, stop words, dictionaries  |

PostgreSQL's `tsvector`/`tsquery` provides relevance-ranked full-text search that is functionally equivalent to BM25 for our use case.

## Memory Store Interface

```go
// internal/memory/store.go
type Store interface {
    // AddMemory stores a new (situation, recommendation) pair
    AddMemory(ctx context.Context, mem Memory) error
    // GetRelevantMemories retrieves the most relevant memories for a situation
    GetRelevantMemories(ctx context.Context, query MemoryQuery) ([]Memory, error)
    // DeleteMemory removes a memory by ID
    DeleteMemory(ctx context.Context, id uuid.UUID) error
}

type Memory struct {
    ID              uuid.UUID
    AgentRole       string    // "bull", "bear", "trader", "invest_judge", "risk_manager"
    Situation       string    // description of the trading scenario
    Recommendation  string    // what the agent recommended
    Outcome         string    // what actually happened (filled after trade resolves)
    PipelineRunID   uuid.UUID
    CreatedAt       time.Time
}

type MemoryQuery struct {
    Situation string   // free-text description of current scenario
    AgentRole string   // filter by agent role
    Limit     int      // max results (default 5)
}
```

## PostgreSQL Implementation

```go
// internal/memory/postgres.go
type PostgresStore struct {
    db *pgxpool.Pool
}

func (s *PostgresStore) AddMemory(ctx context.Context, mem Memory) error {
    _, err := s.db.Exec(ctx, `
        INSERT INTO agent_memories (id, agent_role, situation, recommendation, outcome, pipeline_run_id)
        VALUES ($1, $2, $3, $4, $5, $6)
    `, mem.ID, mem.AgentRole, mem.Situation, mem.Recommendation, mem.Outcome, mem.PipelineRunID)
    return err
}

func (s *PostgresStore) GetRelevantMemories(ctx context.Context, q MemoryQuery) ([]Memory, error) {
    rows, err := s.db.Query(ctx, `
        SELECT id, agent_role, situation, recommendation, outcome, created_at,
               ts_rank(situation_tsv, plainto_tsquery('english', $1)) AS rank
        FROM agent_memories
        WHERE agent_role = $2
          AND situation_tsv @@ plainto_tsquery('english', $1)
        ORDER BY rank DESC
        LIMIT $3
    `, q.Situation, q.AgentRole, q.Limit)
    // scan rows into []Memory...
}
```

The `situation_tsv` column is a generated `TSVECTOR` column (see [[database-schema]]) that automatically indexes the `situation` text.

## Five Memory Stores

Each agent role has its own logical memory space (same table, filtered by `agent_role`):

| Agent Role     | Memory Contains                           | Example                                                                    |
| -------------- | ----------------------------------------- | -------------------------------------------------------------------------- |
| `bull`         | Bullish arguments that proved right/wrong | "AAPL momentum thesis validated — stock gained 12% in 3 weeks"             |
| `bear`         | Bearish arguments and their outcomes      | "Warning about TSLA overvaluation was correct — dropped 8% after earnings" |
| `trader`       | Trade execution lessons                   | "Tighter stop-loss on NVDA would have prevented 4% loss on reversal"       |
| `invest_judge` | Research judgment calls                   | "Should have weighed fundamentals more than technicals for XOM"            |
| `risk_manager` | Risk assessment outcomes                  | "Conservative stance on crypto during rate hike was correct"               |

## Reflection Process

After a trade resolves (position closed or target date reached), the system generates new memories:

```go
// internal/memory/reflection.go
type Reflector struct {
    llm    llm.Provider
    store  Store
}

func (r *Reflector) ReflectOnOutcome(ctx context.Context, run PipelineRun, outcome TradeOutcome) error {
    // Build reflection prompt with original decisions and actual outcome
    prompt := buildReflectionPrompt(run, outcome)

    resp, err := r.llm.Complete(ctx, llm.CompletionRequest{
        SystemPrompt: reflectionSystemPrompt,
        Messages: []llm.Message{
            {Role: "user", Content: prompt},
        },
    })
    if err != nil {
        return err
    }

    // Parse response into memories for each relevant agent role
    memories := parseReflectionResponse(resp.Content)
    for _, mem := range memories {
        mem.PipelineRunID = run.ID
        if err := r.store.AddMemory(ctx, mem); err != nil {
            return err
        }
    }
    return nil
}
```

### Reflection Prompt Structure

```
Given the following trading analysis and outcome:

TICKER: AAPL
DATE: 2026-03-15
SIGNAL: BUY

ANALYST REPORTS:
[market analyst report]
[fundamentals report]

RESEARCH DEBATE:
[bull arguments]
[bear arguments]
[research manager judgment]

TRADE PLAN:
[trader agent plan]

RISK ASSESSMENT:
[risk debate]
[risk manager decision]

ACTUAL OUTCOME:
Entry: $182.50, Exit: $178.30, P&L: -2.3%, Held: 5 days

For each agent role (bull, bear, trader, invest_judge, risk_manager),
generate a concise lesson learned in the format:
- Situation: [brief description of the scenario]
- Recommendation: [what the agent should do differently or continue doing]
```

## Memory Injection into Prompts

During pipeline execution, relevant memories are retrieved and injected into each agent's system prompt:

```go
func (a *BullResearcher) Execute(ctx context.Context, state *PipelineState) error {
    // Retrieve relevant memories
    memories, _ := a.memoryStore.GetRelevantMemories(ctx, memory.MemoryQuery{
        Situation: fmt.Sprintf("%s on %s", state.Ticker, state.TradeDate.Format("2006-01-02")),
        AgentRole: "bull",
        Limit:     5,
    })

    // Build prompt with memories
    systemPrompt := buildBullPrompt(memories)
    // ...
}
```

## Memory Maintenance

- **Pruning**: Memories older than 1 year with no recent retrievals are archived
- **Deduplication**: Similar situations (cosine similarity > 0.9 on tsvector) are merged
- **Manual curation**: Memories can be deleted via REST API if they encode incorrect lessons

---

**Related:** [[database-schema]] · [[agent-orchestration-engine]] · [[agent-system-overview]] · [[research-debate-system]]
