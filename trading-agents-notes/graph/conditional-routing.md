---
title: Conditional Routing
description: Decision logic for tool calls, debate loops, and analyst sequencing
type: architecture
source_file: TradingAgents/tradingagents/graph/conditional_logic.py
created: 2026-03-20
---

# Conditional Routing

`conditional_logic.py` (68 lines) defines the routing functions that control flow through the [[langgraph-orchestration|LangGraph workflow]].

## Three Routing Decisions

### 1. Tool Call Routing

After an analyst node runs, determines whether to call a tool or proceed:

```python
def should_call_tool(state) -> "tool_node" | "next_analyst"
```

- If the agent's last message contains tool call requests → route to tool node
- Tool node executes the data retrieval → routes back to agent
- If no tool calls → move to next analyst or debate phase

This creates the analyst ↔ tool loop where agents iteratively request data.

### 2. Debate Round Control

Controls how many rounds the bull/bear and risk debates run:

```python
def should_continue_invest_debate(state) -> "bull" | "research_manager"
def should_continue_risk_debate(state) -> "aggressive" | "risk_manager"
```

- Checks `invest_debate_count` / `risk_debate_count` against `max_debate_rounds` / `max_risk_discuss_rounds` from [[configuration]]
- If under the limit → route to next debator for another round
- If limit reached → route to the judge (manager) for final decision

### 3. Analyst Sequencing

Routes between analysts in the configured order:

```python
def route_after_analyst(state) -> "next_analyst" | "bull_researcher"
```

- Tracks which analysts have completed
- Routes to next selected analyst, or to the research debate when all are done

## Routing Flow Diagram

```
analyst → should_call_tool? ──yes──→ tool_node → analyst
                             └─no──→ next_analyst / bull_researcher

bear_researcher → should_continue_debate? ──yes──→ bull_researcher
                                           └─no──→ research_manager

neutral_analyst → should_continue_risk? ──yes──→ aggressive_analyst
                                         └─no──→ risk_manager
```

## Related

- [[langgraph-orchestration]] - Overall graph structure
- [[state-management]] - Debate count fields used for routing
- [[analyst-team]] - Analyst sequencing
- [[research-team]] - Debate loop control
- [[risk-management-team]] - Risk debate loop control
