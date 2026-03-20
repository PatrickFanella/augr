---
title: Backtesting Methodology
description: Frameworks, data sources, and common pitfalls for strategy validation
type: reference
tags:
  [
    backtesting,
    vectorbt,
    Backtrader,
    QuantConnect,
    overfitting,
    survivorship-bias,
  ]
created: 2026-03-20
---

# Backtesting Methodology

## Frameworks

| Framework             | Strength                                 | Best For                                     |
| --------------------- | ---------------------------------------- | -------------------------------------------- |
| **vectorbt**          | Fastest (millions of trades/sec)         | Large-scale parameter optimization           |
| **Backtrader**        | Simplest event-driven                    | Beginner-friendly, native broker integration |
| **QuantConnect/LEAN** | Institutional-grade, TB of included data | Professional research, cloud execution       |
| **Freqtrade**         | Built-in FreqAI module                   | Crypto ML strategies                         |
| **Backtesting.py**    | Lightweight                              | Quick prototyping                            |
| **Zipline**           | Algorithmic trading library              | Research pipelines                           |

## Historical Data Sources

| Source                   | Coverage                             |
| ------------------------ | ------------------------------------ |
| Yahoo Finance (yfinance) | Free, delayed                        |
| Polygon.io               | Real-time + historical stocks/crypto |
| Alpha Vantage            | Free tier                            |
| CCXT                     | Unified crypto exchange data         |
| Polymarket Gamma API     | Prediction market data               |

## Critical Pitfalls

### Look-Ahead Bias

All data inputs must use only information available before each decision point. Strict timestamp alignment required.

### Survivorship Bias

Must include delisted stocks. Most LLM papers test only on narrow, cherry-picked universes (often just FAANG stocks).

### Overfitting

Amplified by testing many prompt variations on the same dataset. Use walk-forward (rolling window) approach: calibrate on one year, test on next quarter, iterate.

### Optimistic Fills

Backtests assume instant execution at limit price with zero slippage. Live trading has real slippage, partial fills, and latency. Simulate partial fills and worst-case slippage.

### Transaction Costs

Model commissions, bid-ask spreads, and platform-specific fees. Crypto exchanges have fees and wide spreads.

## Best Practices

- Use the **same execution engine** for backtesting and live trading
- Test across **multiple market regimes** spanning 1-5+ years
- Log each hypothetical trade with reasons; manually review
- Cross-check that bot wouldn't have violated exchange rules (margin limits)
- Walk-forward and out-of-sample validation are mandatory

## Key Metrics

| Metric           | Description                     |
| ---------------- | ------------------------------- |
| Sharpe ratio     | Most universally reported       |
| Sortino ratio    | Downside-risk-adjusted          |
| Maximum drawdown | Worst peak-to-trough decline    |
| Calmar ratio     | Annual return / max drawdown    |
| Win rate         | Percentage of profitable trades |
| Profit factor    | Gross profit / gross loss       |

## Related

- [[llm-backtesting-challenges]] - LLM-specific testing problems
- [[paper-trading]] - Next validation step after backtesting
- [[llm-trading-tools]] - Specific tool recommendations
