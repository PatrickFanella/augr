---
title: Trend Following & Managed Futures
description: Time-series momentum applied across diversified futures markets, delivering crisis alpha
type: strategy
asset_classes: [futures, commodities, FX, bonds, equity-indices]
time_horizon: weeks to months
tags:
  [trend-following, CTA, managed-futures, crisis-alpha, time-series-momentum]
created: 2026-03-20
---

# Trend Following & Managed Futures

The practical multi-asset implementation of time-series momentum. If an asset's excess return over a lookback period is positive, go long; if negative, go short. Each asset is evaluated against its own history (unlike [[momentum]] which ranks assets against each other).

## Mechanism

- Calculate past return over lookback periods (commonly 1, 3, or 12 months)
- Position sizes scaled by inverse volatility to equalize risk contributions
- Typically targets ~10% annualized portfolio volatility
- Combines multiple lookback horizons (1m/3m/12m) for robustness
- Common signals: moving average crossovers, breakout signals, blended lookback returns

## Why It Works

- Slow-moving institutional capital
- Central bank policy creating sustained directional moves
- Risk transfer from hedgers to speculators
- [[behavioral-biases-in-markets|Behavioral biases]]: anchoring, herding, gradual information diffusion

## Key Evidence

- **Moskowitz, Ooi & Pedersen (2012, JFE 104(2), 228-250)**: Significant effects across all 58 liquid futures contracts studied
- **Hurst, Ooi & Pedersen (2017, JPM 44(1), 15-29)**: Positive returns in every decade since 1880; performed well in 8 of 10 largest crisis periods
- SG CTA Index gained **+20.1% in 2022** while both stocks and bonds suffered
- CTA returns almost entirely explained by TSMOM strategies (Hurst, Ooi & Pedersen, 2013)

## Performance

| Metric                            | Value                 |
| --------------------------------- | --------------------- |
| Gross Sharpe                      | ~1.8                  |
| Net Sharpe (after 2/20 fees)      | ~0.7                  |
| Net annualized return (1880-2016) | ~7.3%                 |
| Correlation to stocks/bonds       | Near zero or negative |

## Risks

- Underperforms in range-bound, non-trending markets (~flat in 2023)
- Roll costs in futures and moderate trading costs
- Extended drawdowns during sudden market reversals
- CTAs often underperform their trend signals due to fees and suboptimal execution

## CTA / Managed Futures Funds

Professional trend-following vehicles. Diversified across 50-100+ liquid futures. Charge 2%/20% fees which significantly reduce net returns. Simple equal-weight of trend horizons and volatility scaling often outperforms complex models after costs.

## Related

- [[momentum]] - Cross-sectional variant
- [[strategy-diversification]] - Trend following provides crisis hedging, opposite profile to [[carry-trade]] and [[volatility-selling]]
- [[risk-parity]] - Often combined with trend in portfolio construction
