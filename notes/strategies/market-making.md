---
title: Market Making
description: Earning bid-ask spreads through continuous liquidity provision
type: strategy
asset_classes: [equities, futures]
time_horizon: milliseconds to minutes
tags: [HFT, liquidity, bid-ask-spread, market-microstructure]
created: 2026-03-20
---

# Market Making

Continuously posts limit orders on both bid and ask sides to earn the bid-ask spread. Profits arise from supplying liquidity, offsetting adverse selection risk (trading against better-informed counterparties).

## Mechanism

- Post buy and sell limit orders around mid-price
- Profit per trade is tiny; requires massive volume
- The Avellaneda-Stoikov (2008) model sets quotes dynamically based on current inventory and market activity
- Adapt spread and sizes dynamically based on conditions

## Requirements

- Ultra-liquid instruments (large-cap stocks, futures)
- Low-latency infrastructure (co-location, optimized networking)
- Sophisticated inventory and risk controls
- Real-time delta hedging

## Risks

- **Adverse selection**: Trading against informed counterparties
- **Inventory risk**: Accumulating one-sided positions during directional flow
- Volatile markets can widen spreads or cause losses
- Technology and connectivity costs are significant

## Performance

- Aim for Sharpe ratios well above 1.0 (leveraged)
- Very low returns per trade, high volume
- Firms like Citadel/Optiver report multi-billion USD revenues with thin margins
- Even 1-cent average spread capture per share yields high returns at scale

## Related

- [[polymarket-execution]] - Market-making bots on prediction markets
- [[position-sizing]] - Inventory management is a form of position sizing
