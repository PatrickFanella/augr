---
title: Research Team
description: Bull/bear adversarial debate and research manager judgment
type: agents
source_files:
  - TradingAgents/tradingagents/agents/researchers/bull_researcher.py
  - TradingAgents/tradingagents/agents/researchers/bear_researcher.py
  - TradingAgents/tradingagents/agents/managers/research_manager.py
created: 2026-03-20
---

# Research Team

The second phase of the [[architecture|pipeline]]. Takes analyst reports from the [[analyst-team]] and runs an adversarial debate to produce a balanced investment plan.

## Roles

### Bull Researcher

Advocates for **buying** the stock:

- Highlights positive signals from analyst reports
- References past similar situations via [[bm25-memory-system|memory]] where buying worked
- Builds the strongest possible bullish case
- Responds to bear arguments with counterpoints

### Bear Researcher

Advocates **against** buying:

- Highlights risks, red flags, and negative signals
- References past situations via memory where caution was warranted
- Builds the strongest possible bearish case
- Responds to bull arguments with counterpoints

### Research Manager (Judge)

**Judges** the bull/bear debate:

- Evaluates the strength of both arguments
- Weighs evidence quality and relevance
- Produces an **investment plan** (not a direct buy/sell signal)
- The plan includes rationale, conviction level, and key factors

## Debate Mechanism

```
Bull → Bear → Bull → Bear → ... → Research Manager
         ↑                              ↑
    max_debate_rounds           judges and decides
      (default: 1)
```

The number of debate rounds is controlled by `max_debate_rounds` in [[configuration]]. Each round:

1. Bull presents/updates bullish case
2. Bear presents/updates bearish case
3. Both respond to each other's arguments

After all rounds complete, the Research Manager reads the full debate transcript and makes a judgment.

## Memory Integration

Both researchers access the [[bm25-memory-system]]:

- Before arguing, they query memory for similar past situations
- Relevant past experiences inform their arguments
- After trade outcomes are known, [[bm25-memory-system#Reflection|reflection]] updates their memories

Each researcher has a **separate memory store**, so bull and bear memories evolve independently.

## State Flow

Uses `InvestDebateState` (see [[state-management]]):

- `invest_debate_messages`: Debate conversation history
- `invest_judge_response`: Research manager's judgment
- `invest_debate_count`: Current round number

## Related

- [[analyst-team]] - Upstream data source
- [[trader-agent]] - Downstream consumer of the investment plan
- [[bm25-memory-system]] - How researchers learn from past trades
- [[conditional-routing]] - How debate rounds are controlled
