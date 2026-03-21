---
title: System Architecture
description: End-to-end execution flow, component interactions, and design patterns in TradingAgents
type: architecture
source_files:
  - TradingAgents/tradingagents/graph/trading_graph.py
  - TradingAgents/tradingagents/graph/setup.py
created: 2026-03-20
---

# System Architecture

## Execution Flow

The main entry point is `TradingAgentsGraph.propagate(company_name, trade_date)`, which drives the following pipeline:

### Phase 1: Analysis

Each selected analyst (configurable) runs in sequence, calling real data tools:

1. **[[market-analyst]]** → `get_stock_data`, `get_indicators` → Technical analysis report
2. **[[fundamentals-analyst]]** → `get_fundamentals`, `get_balance_sheet`, `get_cashflow`, `get_income_statement` → Fundamentals report
3. **[[news-analyst]]** → `get_news`, `get_global_news` → News impact report
4. **[[social-media-analyst]]** → `get_news` (for sentiment) → Sentiment report

### Phase 2: Research Debate

- **Bull Researcher** builds the bullish case from analyst reports + [[bm25-memory-system|memory]]
- **Bear Researcher** builds the bearish case from analyst reports + memory
- They debate for `max_debate_rounds` iterations (default: 1)
- **Research Manager** judges the debate and produces an investment plan

### Phase 3: Trading

- **[[trader-agent]]** takes the investment plan + market data and creates a concrete trading plan with entry/exit points, position sizing, and risk parameters

### Phase 4: Risk Management

- Three risk debators ([[risk-management-team]]) argue from aggressive, conservative, and neutral perspectives
- They debate for `max_risk_discuss_rounds` iterations (default: 1)
- **Risk Manager** judges the risk debate and issues the **final BUY/SELL/HOLD decision**

### Phase 5: Signal Extraction

- [[signal-processing]] extracts the clean decision from verbose output

## Key Design Patterns

| Pattern              | Usage                                                                     |
| -------------------- | ------------------------------------------------------------------------- | ------------------------------------------------- |
| **Factory**          | `create_llm_client()` for [[multi-provider-system                         | provider-agnostic LLM creation]]                  |
| **Strategy**         | Different analyst/debator strategies (bull/bear, aggressive/conservative) |
| **State Machine**    | LangGraph-based workflow with [[conditional-routing]]                     |
| **Fallback**         | [[vendor-system                                                           | Data vendor fallback]] (yfinance → Alpha Vantage) |
| **Observer**         | Callbacks for tracking LLM and tool usage statistics                      |
| **Tool-Agent**       | LangChain tools for structured data access                                |
| **Memory/Retrieval** | BM25-based [[bm25-memory-system                                           | learning from past outcomes]]                     |

## Component Interaction

```
TradingAgentsGraph (orchestrator)
├── LLM Clients (factory-created per provider)
├── Memory Systems (one per agent type)
├── Tool Nodes (data access via vendor routing)
└── StateGraph (LangGraph workflow)
    ├── Analyst nodes (1-4, configurable)
    ├── Research debate subgraph
    ├── Trader node
    └── Risk debate subgraph
```

## Error Handling and Resilience

- **Rate limits**: Alpha Vantage triggers fallback to yfinance
- **Date validation**: Strict YYYY-MM-DD checking
- **Missing data**: Graceful returns when ticker data unavailable
- **Recursion limits**: Configurable `max_recur_limit` for graph execution

## Related

- [[langgraph-orchestration]] - How the StateGraph is built
- [[state-management]] - Data flow between agents
- [[configuration]] - Tunable parameters
