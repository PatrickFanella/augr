---
title: Risk Parity
description: Portfolio allocation where each asset class contributes equally to total portfolio risk
type: strategy
asset_classes: [multi-asset, equities, bonds, commodities, TIPS]
time_horizon: years (monthly/quarterly rebalancing)
tags: [asset-allocation, portfolio-construction, leverage, equal-risk]
created: 2026-03-20
---

# Risk Parity

Allocates capital so each asset class contributes equally to portfolio risk (usually measured by volatility). Overweights low-vol assets (bonds) and underweights high-vol (stocks), then leverages to target overall volatility.

## Mechanism

- If stocks have 3x the vol of bonds, allocate 1/3 to stocks and 2/3 to bonds (on a risk basis)
- Leverage used to boost returns to target an aggressive volatility (common: 8-12% annual vol)
- Rebalance monthly or quarterly
- Typically applied to 3-5 asset mix (global stocks, government bonds, TIPS, commodities)

## Performance

- Sharpe ~0.6-0.8 over past 20 years
- Filho & Gaspar (2024, JPM) compare to mean-variance portfolios (1990-2019): risk parity does well over shorter horizons (<10 years) but not necessarily over 20-year spans
- Smoothed returns vs traditional 60/40 portfolio
- 2021-2022 challenges: low bond yields and rising rates

## Risks

- Vulnerable when low-vol assets (bonds) get hit (inflation spikes, rising rates)
- Leverage costs and margin requirements
- Joint regime shifts can cause drawdowns
- Requires robust volatility estimates (1-2 year rolling vol)

## Implementation

- Allow dynamic leverage (de-lever when vol spikes)
- Consider tail hedges (inflation, deflation)
- Connection to [[low-volatility-anomaly]]: effectively bets on the low-vol anomaly by leveraging low-vol assets

## Related

- [[low-volatility-anomaly]] - Core theoretical connection
- [[portfolio-risk-controls]] - Volatility targeting is a shared technique
- [[strategy-diversification]] - Risk parity is a diversification framework
