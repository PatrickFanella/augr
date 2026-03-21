---
title: Fundamentals Analyst
description: Financial statement analysis agent covering balance sheet, cash flow, and income statement
type: agent
source_file: TradingAgents/tradingagents/agents/analysts/fundamentals_analyst.py
created: 2026-03-20
---

# Fundamentals Analyst

Analyzes company financial health through balance sheets, cash flow statements, income statements, and key financial ratios.

## Tools

| Tool                   | Data Returned                              |
| ---------------------- | ------------------------------------------ |
| `get_fundamentals`     | Key ratios and company overview            |
| `get_balance_sheet`    | Assets, liabilities, equity                |
| `get_cashflow`         | Operating, investing, financing cash flows |
| `get_income_statement` | Revenue, expenses, net income              |

All tools route through the [[vendor-system]] to [[yahoo-finance]] (default) or [[alpha-vantage]].

## System Prompt Behavior

The fundamentals analyst evaluates:

- Revenue trends and growth rates
- Profitability margins (gross, operating, net)
- Balance sheet strength (debt levels, liquidity ratios)
- Cash flow quality (operating vs financing)
- Valuation metrics relative to peers
- Insider transaction patterns

## Output

A fundamental analysis report assessing the company's financial position, growth trajectory, and valuation. Consumed by the [[research-team]].

## Related

- [[analyst-team]] - Overview of all analysts
- [[yahoo-finance]] - Primary data source for financial statements
