---
title: LangGraph Orchestration
description: How TradingAgents uses LangGraph StateGraph to wire agents, tools, and debate loops
type: architecture
source_files:
  - TradingAgents/tradingagents/graph/trading_graph.py
  - TradingAgents/tradingagents/graph/setup.py
created: 2026-03-20
---

# LangGraph Orchestration

The entire agent pipeline is implemented as a LangGraph `StateGraph`, allowing declarative workflow definition with conditional routing, tool nodes, and nested subgraphs.

## Graph Construction

Defined in `setup.py`, the graph is built dynamically based on [[configuration]]:

### Nodes Added

1. **Analyst nodes** (1-4, based on `selected_analysts`): `market_analyst`, `fundamentals_analyst`, `news_analyst`, `social_media_analyst`
2. **Tool nodes**: One per analyst for data retrieval
3. **Research debate nodes**: `bull_researcher`, `bear_researcher`, `research_manager`
4. **Trader node**: `trader`
5. **Risk debate nodes**: `aggressive_analyst`, `conservative_analyst`, `neutral_analyst`, `risk_manager`

### Edge Wiring

```
START → analyst_1 → tool_1 ↔ analyst_1 → analyst_2 → ... → bull_researcher
                                                                  ↕
                                                          bear_researcher
                                                                  ↓
                                                         research_manager
                                                                  ↓
                                                              trader
                                                                  ↓
                                                      aggressive_analyst
                                                                  ↕
                                                     conservative_analyst
                                                                  ↕
                                                       neutral_analyst
                                                                  ↓
                                                          risk_manager
                                                                  ↓
                                                                END
```

Debate loops (↕) are controlled by [[conditional-routing]].

## TradingAgentsGraph Class

`trading_graph.py` (288 lines) -- the main orchestrator:

```python
class TradingAgentsGraph:
    def __init__(self, selected_analysts, debug, config, callbacks)
    def propagate(company_name, trade_date)  # Run full pipeline
    def reflect_and_remember(returns_losses)  # Learn from outcome
    def process_signal(full_signal)           # Extract decision
```

### Initialization

1. Creates LLM clients via [[multi-provider-system|factory]]
2. Initializes [[bm25-memory-system|memory]] stores (one per agent type)
3. Creates tool nodes for [[vendor-system|data access]]
4. Builds and compiles the StateGraph

### Execution

`propagate()` invokes the compiled graph with the initial [[state-management|state]], then calls [[signal-processing]] on the result.

## Debug Mode

When `debug=True`, intermediate state is logged at each node for inspection.

## Related

- [[state-management]] - The data flowing through the graph
- [[conditional-routing]] - How decisions between nodes are made
- [[architecture]] - High-level system design
