---
title: Factor Investing
description: Systematic portfolio construction tilted toward characteristics that explain cross-sectional returns
type: strategy
asset_classes: [equities]
time_horizon: years
tags: [factors, fama-french, CAPM, multi-factor]
created: 2026-03-20
---

# Factor Investing

Systematically constructs portfolios tilted toward characteristics that explain cross-sectional return variation.

## Evolution of Factor Models

| Year | Model                | Factors                                                  |
| ---- | -------------------- | -------------------------------------------------------- |
| 1964 | CAPM                 | Market (MKT)                                             |
| 1993 | Fama-French 3-Factor | MKT + Size (SMB) + Value (HML)                           |
| 1997 | Carhart 4-Factor     | + Momentum (UMD)                                         |
| 2015 | Fama-French 5-Factor | MKT + SMB + HML + Profitability (RMW) + Investment (CMA) |

## Key References

- **Fama & French (1993, JFE 33(1), 3-56)**: Three-factor model adding size and value
- **Carhart (1997, JF)**: Added momentum factor
- **Fama & French (2015, JFE 116(1), 1-22)**: Five-factor model adding profitability and investment

## Five-Factor Model Results

- Explains **71-94%** of cross-sectional return variance
- With profitability and investment included, HML (value) becomes redundant
- Primary failure: small stocks behaving like unprofitable firms investing aggressively

## Core Factors

- **Market (MKT)**: Equity risk premium
- **Size (SMB)**: Small-cap premium
- **Value (HML)**: See [[value-investing]]
- **Profitability (RMW)**: See [[quality-investing]]
- **Investment (CMA)**: Conservative minus aggressive investment
- **Momentum (UMD)**: See [[momentum]]
- **Low Volatility (BAB)**: See [[low-volatility-anomaly]]

## Practical Implications

- Diversifying across multiple uncorrelated factors significantly improves risk-adjusted returns
- Whether factors represent risk compensation or [[risk-vs-mispricing-debate|behavioral mispricing]] remains the central debate
- Factor ETFs or long-only tilts have modest trading costs (quarterly/annual rebalancing)
- Factors can suffer cyclical droughts (e.g. value underperformance in growth markets)
- Crowding is a concern as many quant strategies target the same factors

## Related

- [[strategy-diversification]] - Combining factors is the core insight
- [[risk-vs-mispricing-debate]] - Fundamental question for all factor premia
