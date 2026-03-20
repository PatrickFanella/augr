---
title: Technical Indicators
description: All 14 technical indicators available in TradingAgents and their calculations
type: reference
source_files:
  - TradingAgents/tradingagents/dataflows/y_finance.py
  - TradingAgents/tradingagents/dataflows/stockstats_utils.py
  - TradingAgents/tradingagents/agents/utils/technical_indicators_tools.py
created: 2026-03-20
---

# Technical Indicators

TradingAgents supports 14 technical indicators, computed via `stockstats` and custom calculations in `StockstatsUtils`. Used by the [[market-analyst]].

## Available Indicators

### Trend Indicators

| Indicator                                        | Description                                                                    |
| ------------------------------------------------ | ------------------------------------------------------------------------------ |
| **SMA** (Simple Moving Average)                  | Average price over a period; trend direction                                   |
| **EMA** (Exponential Moving Average)             | Weighted average favoring recent prices; faster trend response                 |
| **MACD** (Moving Average Convergence Divergence) | Difference between 12 and 26-period EMAs; trend momentum and crossover signals |

### Momentum Indicators

| Indicator                         | Description                                                                      |
| --------------------------------- | -------------------------------------------------------------------------------- |
| **RSI** (Relative Strength Index) | Measures speed and magnitude of price changes; overbought (>70) / oversold (<30) |
| **MFI** (Money Flow Index)        | Volume-weighted RSI; incorporates buying/selling pressure                        |
| **Stochastic Oscillator**         | Compares closing price to price range over a period                              |
| **Williams %R**                   | Similar to stochastic; momentum oscillator                                       |
| **CCI** (Commodity Channel Index) | Measures deviation from statistical mean                                         |
| **ROC** (Rate of Change)          | Percentage price change over a period                                            |

### Volatility Indicators

| Indicator                    | Description                                                                        |
| ---------------------------- | ---------------------------------------------------------------------------------- |
| **Bollinger Bands**          | Upper/lower bands at 2 standard deviations from SMA; volatility and mean reversion |
| **ATR** (Average True Range) | Measures volatility as average of true ranges                                      |

### Volume Indicators

| Indicator                                 | Description                                              |
| ----------------------------------------- | -------------------------------------------------------- |
| **VWMA** (Volume Weighted Moving Average) | Price average weighted by volume                         |
| **OBV** (On-Balance Volume)               | Cumulative volume flow; confirms price trends            |
| **ADL** (Accumulation/Distribution Line)  | Measures money flow based on close position within range |

## Calculation

`get_stock_stats_indicators_window(ticker, date)` in `y_finance.py`:

1. Fetches OHLCV data for the date window
2. Uses `StockstatsUtils` (wrapper around `stockstats` library) to compute all 14 indicators
3. Returns a structured dict with current values for each

## Usage

The [[market-analyst]] receives these via the `get_indicators` tool and interprets them in context of the overall technical picture.

## Related

- [[market-analyst]] - Primary consumer
- [[yahoo-finance]] - Underlying price data
- [[vendor-system]] - Tool routing
