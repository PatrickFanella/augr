---
title: Market Analyst
description: Technical analysis agent using price data and indicators (MACD, RSI, Bollinger Bands, etc.)
type: agent
source_file: TradingAgents/tradingagents/agents/analysts/market_analyst.py
created: 2026-03-20
---

# Market Analyst

Analyzes technical indicators and price action to assess market conditions and identify trading signals.

## Tools

| Tool             | Source                                 | Data Returned                        |
| ---------------- | -------------------------------------- | ------------------------------------ |
| `get_stock_data` | [[yahoo-finance]] or [[alpha-vantage]] | OHLCV price data for the date window |
| `get_indicators` | [[technical-indicators]]               | 14 technical indicator values        |

## System Prompt Behavior

The market analyst is instructed to:

- Analyze recent price trends and volume patterns
- Evaluate technical indicator signals (bullish/bearish/neutral)
- Identify support/resistance levels
- Assess momentum and trend strength
- Provide a structured technical analysis report

## Indicators Analyzed

See [[technical-indicators]] for the full list. Key indicators include MACD, RSI, Bollinger Bands, ATR, SMA/EMA crossovers, VWMA, and MFI.

## Output

A technical analysis report containing:

- Current price context and trend direction
- Indicator readings and their implications
- Key levels (support/resistance)
- Overall technical sentiment (bullish/bearish/neutral)

This report is consumed by the [[research-team]] as evidence.

## Related

- [[analyst-team]] - Overview of all analysts
- [[technical-indicators]] - Available indicator calculations
- [[yahoo-finance]] - Primary data source
