---
title: Analyst Team
description: Overview of the four specialized analyst agents that gather and interpret market data
type: agents
source_dir: TradingAgents/tradingagents/agents/analysts/
created: 2026-03-20
---

# Analyst Team

The first phase of the [[architecture|execution pipeline]]. Four specialized analyst agents each gather data through LangChain tools and produce structured reports. Analysts are **selectable** -- any subset can be enabled via [[configuration]].

## The Four Analysts

| Analyst                  | Focus                              | Tools Used                                                                      | Report Output                        |
| ------------------------ | ---------------------------------- | ------------------------------------------------------------------------------- | ------------------------------------ |
| [[market-analyst]]       | Technical indicators, price action | `get_stock_data`, `get_indicators`                                              | Technical analysis with signals      |
| [[fundamentals-analyst]] | Financial health, valuation        | `get_fundamentals`, `get_balance_sheet`, `get_cashflow`, `get_income_statement` | Fundamental analysis                 |
| [[news-analyst]]         | News impact, macro context         | `get_news`, `get_global_news`                                                   | News sentiment and impact assessment |
| [[social-media-analyst]] | Public sentiment, social buzz      | `get_news` (sentiment-focused)                                                  | Sentiment analysis                   |

## How Analysts Work

Each analyst is a LangChain agent created with `create_react_agent()`:

1. Receives the current [[state-management|AgentState]] containing ticker and date
2. Has access to specific tools via [[vendor-system|data vendor routing]]
3. Calls tools to fetch real market data (not LLM knowledge)
4. Produces a structured analysis report
5. Report is appended to state for downstream agents

## Selection

```python
TradingAgentsGraph(
    selected_analysts=["market", "social", "news", "fundamentals"]
)
```

The [[langgraph-orchestration|graph setup]] dynamically adds only selected analyst nodes and wires their edges accordingly.

## Downstream Consumers

Analyst reports flow to the [[research-team]], where bull and bear researchers use them as evidence for their arguments.

## Related

- [[architecture]] - Where analysts fit in the pipeline
- [[vendor-system]] - How tools route to data providers
- [[research-team]] - Next phase after analysis
