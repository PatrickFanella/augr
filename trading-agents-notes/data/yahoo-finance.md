---
title: Yahoo Finance Data
description: yfinance implementation providing price data, indicators, fundamentals, and news
type: data-source
source_files:
  - TradingAgents/tradingagents/dataflows/y_finance.py
  - TradingAgents/tradingagents/dataflows/yfinance_news.py
created: 2026-03-20
---

# Yahoo Finance Data

The default data vendor. Uses the `yfinance` Python package. Free, no API key required.

## Available Functions

### Price Data (`y_finance.py`, ~280 lines)

| Function                             | Returns                                                             |
| ------------------------------------ | ------------------------------------------------------------------- |
| `get_YFin_data_online(ticker, date)` | OHLCV (Open, High, Low, Close, Volume) price data for a date window |

### Technical Indicators

| Function                                          | Returns                       |
| ------------------------------------------------- | ----------------------------- |
| `get_stock_stats_indicators_window(ticker, date)` | 14 technical indicator values |

See [[technical-indicators]] for the full list.

### Financial Statements

| Function                           | Returns                                     |
| ---------------------------------- | ------------------------------------------- |
| `get_fundamentals(ticker)`         | Key ratios and company overview             |
| `get_balance_sheet(ticker)`        | Assets, liabilities, shareholders' equity   |
| `get_cashflow(ticker)`             | Operating, investing, financing cash flows  |
| `get_income_statement(ticker)`     | Revenue, COGS, operating income, net income |
| `get_insider_transactions(ticker)` | Recent insider buys/sells                   |

### News (`yfinance_news.py`)

| Function                     | Returns                        |
| ---------------------------- | ------------------------------ |
| `get_news_yfinance(ticker)`  | Company-specific news articles |
| `get_global_news_yfinance()` | Global macroeconomic news      |

## Usage in Pipeline

Accessed indirectly through the [[vendor-system]]. Agents never call yfinance directly -- they use abstract tool names that route through the vendor interface.

## Related

- [[vendor-system]] - How tools route to this implementation
- [[alpha-vantage]] - Alternative vendor
- [[technical-indicators]] - Indicator calculations built on yfinance data
