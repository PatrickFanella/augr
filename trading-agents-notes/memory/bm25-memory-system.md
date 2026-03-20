---
title: BM25 Memory System
description: BM25-based retrieval memory for learning from past trading outcomes and reflection
type: architecture
source_files:
  - TradingAgents/tradingagents/agents/utils/memory.py
  - TradingAgents/tradingagents/graph/reflection.py
created: 2026-03-20
---

# BM25 Memory System

TradingAgents uses BM25 (Best Matching 25) for lexical similarity retrieval, allowing agents to learn from past trading situations without requiring API calls or vector embeddings.

## How It Works

### Memory Class (`memory.py`)

```python
class Memory:
    def add_situations(self, situations: list[tuple[str, str]])
    def get_memories(self, current_situation: str, n_matches: int) -> list[str]
```

- **Storage**: List of `(situation_description, recommendation)` tuples
- **Retrieval**: BM25 lexical matching ranks past situations by relevance to the current one
- **Returns**: Top-n most similar past situations with their outcomes/recommendations

### Key Properties

- **No API calls**: Works entirely offline using `rank_bm25` library
- **Provider-agnostic**: Works with any LLM provider since retrieval is separate from generation
- **Per-agent stores**: Each agent type has its own independent memory

## Separate Memory Stores

Five independent memory stores, one per agent type:

| Memory Store            | Owner            | Learns From                        |
| ----------------------- | ---------------- | ---------------------------------- |
| Bull memory             | Bull Researcher  | Past bullish analyses and outcomes |
| Bear memory             | Bear Researcher  | Past bearish analyses and outcomes |
| Trader memory           | Trader Agent     | Past trading plans and results     |
| Investment Judge memory | Research Manager | Past debate judgments and outcomes |
| Risk Manager memory     | Risk Manager     | Past risk assessments and outcomes |

## Memory Usage in Agents

The [[research-team|bull and bear researchers]] use memory during debates:

1. Agent receives current market situation
2. Queries memory: `memory.get_memories(current_situation, n_matches=3)`
3. Retrieves similar past situations and what happened
4. Incorporates past experience into arguments

## Reflection

`reflection.py` (122 lines) updates memories after trading outcomes:

```python
ta.reflect_and_remember(position_returns=0.05)
```

### Reflection Process

1. Takes the position return (positive or negative)
2. For each agent type, uses the LLM to:
   - Compare the agent's original recommendation to the actual outcome
   - Generate an actionable improvement or lesson
   - Format as a `(situation, recommendation)` tuple
3. Adds the new tuple to the appropriate memory store

This creates a **feedback loop**: trading outcomes refine future agent arguments, gradually improving decision quality.

## Example Flow

```
Day 1: Analyze AAPL → Bull argues buy → System buys
Day 2: AAPL drops 5% → reflect_and_remember(-0.05)
  → Bull memory gets: ("AAPL showed X indicators",
     "Overweighted momentum signals; should have noted Y risk")
Day 3: Analyze MSFT (similar to AAPL situation)
  → Bull retrieves AAPL memory → Tempers argument with past lesson
```

## Related

- [[research-team]] - Primary memory consumers (bull/bear researchers)
- [[risk-management-team]] - Risk manager uses its own memory
- [[trader-agent]] - Trader uses its own memory
- [[architecture]] - Where reflection fits in the system lifecycle
