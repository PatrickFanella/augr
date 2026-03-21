---
title: Cross-Sectional Momentum
description: Buying past winners and selling past losers based on relative 3-12 month returns
type: strategy
asset_classes: [equities, commodities, FX, bonds]
time_horizon: 1-12 months
tags: [momentum, anomaly, behavioral-finance]
created: 2026-03-20
---

# Cross-Sectional Momentum

Ranks assets by past 3-12 month returns, goes long top performers and short bottom performers. Always dollar-neutral (unlike [[trend-following]] which can be net long or short an entire asset class).

## Mechanism

- Formation period: typically 3-12 months of prior returns
- Common implementation: "6-1" strategy (6-month formation, skip 1 month, hold 6 months)
- Skip the most recent month to avoid short-term reversal effects

## Why It Works

Driven by investor underreaction to new information:

- **Information diffusion**: Hong & Stein (1999) modeled how information spreads gradually across "newswatchers," causing slow price adjustment
- **Conservatism bias**: Barberis, Shleifer & Vishny (1998) showed investors underweight new evidence
- **Disposition effect**: Selling winners too early, holding losers too long

See [[behavioral-biases-in-markets]] for more on these dynamics.

## Key Evidence

- **Jegadeesh & Titman (1993, JF 48(1), 65-91)**: ~1-1.5% per month excess returns, not explained by systematic risk
- **Asness, Moskowitz & Pedersen (2013, JF 68(3), 929-985)**: Consistent premia across 8 diverse markets and asset classes; **-0.41 correlation with value** (see [[strategy-diversification]])
- **Rouwenhorst (1998)**: Confirmed across 12 European markets

## Risks

- **Momentum crashes**: Occur during bear market rebounds when past losers rally violently (Daniel & Moskowitz, 2016)
- **Risk-managed momentum**: Scaling by recent realized volatility nearly doubles the Sharpe ratio from 0.53 to 0.97 (Barroso & Santa-Clara, 2015)
- High turnover and transaction costs

## Performance

- Gross returns: ~1-1.5% per month (12-18% annualized)
- Sharpe ratio: ~0.6-0.8 gross, ~0.3-0.5 net after costs

## Related

- [[trend-following]] - Time-series variant of momentum
- [[value-investing]] - Negatively correlated (-0.41), strong diversification pair
- [[factor-investing]] - Momentum is Carhart's fourth factor
