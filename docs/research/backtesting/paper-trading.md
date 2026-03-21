---
title: Paper Trading
description: Simulated live validation with virtual funds before deploying real capital
type: reference
tags: [paper-trading, simulation, validation, Alpaca, testnet]
created: 2026-03-20
---

# Paper Trading

Validates strategy on live market data without real capital. Bridges the gap between [[backtesting-methodology|backtesting]] and live deployment.

## Platform Options

| Platform            | Details                                                              |
| ------------------- | -------------------------------------------------------------------- |
| **Alpaca**          | $100K virtual funds, identical API (`paper=True`), gold standard     |
| **Binance Testnet** | $3K-$15K virtual USDT, up to 125x leverage                           |
| **Bybit/BitMEX**    | Comparable testnet environments                                      |
| **Polymarket bots** | Multi-model ensemble paper mode (GPT-4o 40%, Claude 35%, Gemini 25%) |
| **OctoBot**         | Open-source prediction market paper trading with arbitrage support   |

## User Behavior Data (Alpaca, June 2024 - May 2025)

- **67.2%** of users who eventually traded live started directly without paper trading
- Among paper traders, **57.1%** went live within 30 days, 75.1% within 60 days
- Paper trading functions more as environmental testing (API integration, order flow, error handling) than long-term simulation

## Recommended Timeline

**30-60 days** of paper trading for algorithmic systems, targeting consistent profitability across bull, bear, and sideways conditions.

## What Paper Trading Reveals

- Slower fills or more frequent stop-outs than backtests predicted
- How bot handles unexpected situations (API downtime, partial fills, lag)
- Whether LLM-triggered signals need additional confirmation filters
- Behavior during volatile events (e.g. Fed announcements)

## Limitations

- **Idealized fills**: Instant execution at limit price with zero slippage
- **No market impact**: Paper orders don't move the market
- **API behavior differences**: Between paper and live environments

## Transition to Live Checklist

1. Consistent paper profitability across multiple market conditions
2. Understand real slippage, partial fills, and latency will differ
3. Start with **minimum position sizes** (0.1% risk per trade)
4. Deposit only a fraction of intended capital
5. All [[portfolio-risk-controls|circuit breakers and kill switches]] function correctly
6. Start live with **10-20% of intended capital**
7. Human-in-the-loop approval for trades above certain thresholds initially

## Related

- [[backtesting-methodology]] - Previous validation step
- [[llm-backtesting-challenges]] - LLM-specific testing issues
- [[portfolio-risk-controls]] - Safety systems to verify
- [[stock-market-execution]] - Alpaca paper trading details
