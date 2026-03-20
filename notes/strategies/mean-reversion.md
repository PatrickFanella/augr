---
title: Mean Reversion
description: Contrarian strategies exploiting price reversion toward historical averages across multiple horizons
type: strategy
asset_classes: [equities, ETFs]
time_horizon: days to years (varies by horizon)
tags: [contrarian, reversal, overreaction]
created: 2026-03-20
---

# Mean Reversion

The tendency of prices to revert toward historical averages. Manifests differently at different time horizons.

## Time Horizon Effects

| Horizon                    | Behavior                                                             | Evidence                            |
| -------------------------- | -------------------------------------------------------------------- | ----------------------------------- |
| Short (weekly/monthly)     | Strong negative serial correlation; ~2% per month contrarian profits | Jegadeesh (1990, JF 45(3), 881-898) |
| Intermediate (3-12 months) | **Momentum dominates** -- see [[momentum]]                           | Jegadeesh & Titman (1993)           |
| Long (3-5 years)           | Significant negative autocorrelation; overreaction reversal          | De Bondt & Thaler (1985)            |

## Key Evidence

- **De Bondt & Thaler (1985, JF 40(3), 793-805)**: Prior 3-5 year losers outperformed prior winners by wide margins, consistent with investor overreaction
- **Fama & French (1988, JPE)**: A slowly mean-reverting component accounts for ~25-40% of 3-5 year return variance
- **Poterba & Summers (1988, JFE)**: Confirmed using variance ratio tests across US and 17 other countries
- **Lo & MacKinlay (1990, RFS)**: Contrarian profits partly arise from lead-lag effects between large and small stocks, not purely overreaction

## Implementation Considerations

- Must differentiate genuine overreaction from cross-serial correlation effects
- Extremely high turnover and market friction
- Carries "value trap" risk (further decline)
- Small profit per trade, requires leverage or high volume
- Sharpe ~0.3-0.5 after costs
- Need liquidity filters and transaction cost awareness

## Related

- [[momentum]] - Dominates at intermediate horizons; reversal dominates at short and long horizons
- [[statistical-arbitrage]] - Pairs trading is a form of relative mean reversion
- [[behavioral-biases-in-markets]] - Overreaction drives long-horizon reversal
