---
title: News Analyst
description: News sentiment and macroeconomic analysis agent
type: agent
source_file: TradingAgents/tradingagents/agents/analysts/news_analyst.py
created: 2026-03-20
---

# News Analyst

Analyzes recent company-specific news and global macroeconomic developments to assess their impact on the trading decision.

## Tools

| Tool              | Data Returned                                      |
| ----------------- | -------------------------------------------------- |
| `get_news`        | Company-specific news articles and headlines       |
| `get_global_news` | Global macroeconomic news and market-moving events |

News data sourced via [[yahoo-finance]] (`yfinance_news.py`) or [[alpha-vantage]].

## System Prompt Behavior

The news analyst evaluates:

- Recent company-specific developments (earnings, product launches, management changes)
- Sector and industry news affecting the company
- Macroeconomic factors (Fed policy, economic indicators, geopolitical events)
- News sentiment (positive/negative/neutral tone)
- Potential catalysts or headwinds

## Output

A news impact assessment covering recent developments, their likely effect on the stock, and any upcoming catalysts. Consumed by the [[research-team]].

## Related

- [[analyst-team]] - Overview of all analysts
- [[social-media-analyst]] - Complementary sentiment source
