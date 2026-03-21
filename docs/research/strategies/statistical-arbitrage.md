---
title: Statistical Arbitrage & Pairs Trading
description: Market-neutral strategies exploiting temporary mispricings between co-moving securities
type: strategy
asset_classes: [equities, futures, ETFs]
time_horizon: weeks to months
tags: [stat-arb, pairs-trading, market-neutral, cointegration]
created: 2026-03-20
---

# Statistical Arbitrage & Pairs Trading

Identifies securities with historically co-moving prices, waits for divergence beyond a threshold, then goes long the underperformer and short the outperformer.

## Three Approaches

1. **Distance method**: Minimize Euclidean distance between normalized prices
2. **Cointegration method**: Based on Engle & Granger's (1987) framework in _Econometrica_
3. **Factor-model stat arb**: Decompose returns into systematic and idiosyncratic components (PCA-based)

## Key Evidence

- **Gatev, Goetzmann & Rouwenhorst (2006, RFS 19(3), 797-827)**: Top pairs generated ~11% annualized excess returns over 1962-2002, exceeding conservative transaction cost estimates
- **Avellaneda & Lee (2010, Quantitative Finance)**: PCA-based stat arb achieving Sharpe ratio of 1.44 during 1997-2007

## Declining Profitability

- Do & Faff (2010, FAJ): Mean excess returns fell from **0.86%/month** (1962-1988) to **0.24%/month** (2003-2009)
- August 2007 quant crisis: Many stat-arb funds suffered simultaneous large losses from crowded, correlated positions
- Basic distance-method pairs trading now marginally profitable at best after costs
- More sophisticated approaches (cointegration, ML, volume signals) and within-industry pairs still show promise

## Implementation

- Trading signals based on z-scores of the price spread (typically 2 standard deviation threshold)
- Look-back windows of 1-2 years to find cointegration
- Requires careful cointegration checks and model risk management
- Out-of-sample degradation is a major concern

## Related

- [[mean-reversion]] - Pairs trading is relative mean reversion
- [[market-making]] - Also involves two-sided positioning
- [[llm-strategy-limitations]] - Example of a strategy weakened by capital inflows post-publication
