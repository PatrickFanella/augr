---
title: Volatility Selling
description: Harvesting the volatility risk premium by selling options, variance swaps, or VIX futures
type: strategy
asset_classes: [equity-index-options, VIX-futures, variance-swaps]
time_horizon: days to weeks
tags: [volatility, options, VRP, insurance-premium, negative-skew]
created: 2026-03-20
---

# Volatility Selling

Exploits the persistent tendency for implied volatility to exceed realized volatility. For S&P 500 index options, this gap averages **3-4 percentage points**.

## Mechanism

- Sell options, variance swaps, or VIX futures
- Common vehicle: CBOE S&P PutWrite (sell 1-month ATM put, collect premium, roll monthly)
- The premium exists because option buyers pay for crash protection ([[behavioral-biases-in-markets|insurance demand]])
- Institutional demand for portfolio insurance since the 1987 crash creates persistent buying pressure on index puts

## Key Evidence

- **Coval & Shumway (2001, JF 56(3), 983-1009)**: Zero-beta ATM straddle positions produce average losses of ~3% per week for option _buyers_
- **Bakshi & Kapadia (2003, RFS)**: Delta-hedged option portfolios systematically underperform zero; VRP accounts for ~16% of call option prices
- **CBOE PutWrite Index**: Returned 1835% (since 1986) vs 708% for S&P 500, with higher Sharpe (~1.0)
- Average implied-realized vol gap: ~4.2% per year

## Ilmanen's Framework

Ilmanen (2012, FAJ): Volatility selling is part of a broader pattern -- strategies that resemble "selling insurance" are systematically rewarded, while "buying lottery tickets" is systematically costly. See also [[carry-trade]] for similar dynamics.

## Risks

- **Extreme negative skewness**: Most small gains vs. infrequent catastrophic losses
- 2008 financial crisis and March 2020 COVID crash caused devastating losses
- Option margin requirements spike during crises
- Strategies range from aggressive (naked puts) to conservative (vertical put spreads, iron condors)

## Related

- [[carry-trade]] - Similar "insurance seller" return profile with negative skew
- [[trend-following]] - Opposite profile; strong diversification partner
- [[low-volatility-anomaly]] - Related through leverage constraints
- [[strategy-diversification]] - Key component of multi-strategy portfolios
