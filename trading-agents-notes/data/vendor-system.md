---
title: Data Vendor System
description: Vendor routing, fallback logic, and tool registration for market data access
type: architecture
source_file: TradingAgents/tradingagents/dataflows/interface.py
created: 2026-03-20
---

# Data Vendor System

`interface.py` provides a routing layer that maps abstract tool names to concrete data vendor implementations. This decouples agents from specific data providers.

## Vendor Routing

Tools are organized into four categories, each configurable to a specific vendor:

| Category               | Tools                                                                                                       | Default Vendor    |
| ---------------------- | ----------------------------------------------------------------------------------------------------------- | ----------------- |
| `core_stock_apis`      | `get_stock_data`                                                                                            | [[yahoo-finance]] |
| `technical_indicators` | `get_indicators`                                                                                            | [[yahoo-finance]] |
| `fundamental_data`     | `get_fundamentals`, `get_balance_sheet`, `get_cashflow`, `get_income_statement`, `get_insider_transactions` | [[yahoo-finance]] |
| `news_data`            | `get_news`, `get_global_news`                                                                               | [[yahoo-finance]] |

Configured via the `data_vendors` key in [[configuration]]:

```python
"data_vendors": {
    "core_stock_apis": "yfinance",
    "technical_indicators": "alpha_vantage",  # Override just this one
    "fundamental_data": "yfinance",
    "news_data": "yfinance",
}
```

## Supported Vendors

| Vendor        | Config value      | Details                                               |
| ------------- | ----------------- | ----------------------------------------------------- |
| Yahoo Finance | `"yfinance"`      | Free, no API key needed. See [[yahoo-finance]]        |
| Alpha Vantage | `"alpha_vantage"` | Requires API key, rate-limited. See [[alpha-vantage]] |

## Fallback Behavior

When Alpha Vantage hits rate limits, the system automatically falls back to yfinance for that request. This is handled at the individual tool level in the Alpha Vantage implementation files.

## Tool Registration

Tools are created as LangChain `@tool` decorated functions. The vendor system returns the appropriate function based on config, which is then bound to agent tool nodes in the [[langgraph-orchestration|graph setup]].

## Related

- [[yahoo-finance]] - Primary vendor implementation
- [[alpha-vantage]] - Secondary vendor with rate limit handling
- [[analyst-team]] - Agents that consume these tools
- [[configuration]] - How to configure vendors
