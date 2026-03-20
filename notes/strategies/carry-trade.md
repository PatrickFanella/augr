---
title: Carry Trade
description: Borrowing in low-yield currencies/assets and investing in high-yield ones to collect differentials
type: strategy
asset_classes: [FX, bonds, commodities, equities, options]
time_horizon: months to years
tags: [carry, yield, forward-premium-puzzle, negative-skew]
created: 2026-03-20
---

# Carry Trade

Borrows in low-interest-rate currencies and invests in high-interest-rate currencies. The strategy generalizes beyond FX to equities, bonds, commodities, and options.

## Forward Premium Puzzle

Under Uncovered Interest Parity (UIP), high-rate currencies should depreciate to offset the differential. Empirically, they often **appreciate** instead. This systematic violation is the foundation of carry trade profits.

## Cross-Asset Carry

**Koijen, Moskowitz, Pedersen & Vrugt (2018, JFE 127(2), 197-225)**: Carry predicts returns cross-sectionally and in time series across equities, bonds, commodities, Treasuries, credit, and options. A global carry timing strategy achieves a **Sharpe ratio of ~0.9**.

## Risk Compensation

- **Lustig & Verdelhan (2007, AER)**: Returns compensate for systematic consumption growth risk; high-rate currencies have higher exposure to aggregate consumption risk during recessions
- **Brunnermeier, Nagel & Pedersen (2009, NBER)**: Significant **negative skewness** from sudden unwinding during declining risk appetite. VIX increases predict carry trade losses.

## Performance

| Metric                        | Value                  |
| ----------------------------- | ---------------------- |
| FX carry annual excess return | ~4-7%                  |
| Long-term Sharpe              | ~0.26-0.38 (1900-2012) |
| Global carry timing Sharpe    | ~0.9                   |

Classic "insurance seller" profile: steady small gains punctuated by infrequent severe drawdowns (e.g. 2008 FX crash).

## Variants

- **FX carry**: JPY/CHF vs AUD/NZD
- **Bond carry**: Long high-yield bonds, financed by short bills
- **Duration carry**: Yield curve slope strategies
- **Commodity carry**: Roll yield in contango/backwardation

## Related

- [[volatility-selling]] - Similar "insurance seller" return profile
- [[trend-following]] - Opposite crisis behavior; strong [[strategy-diversification|diversification]] pair
- [[risk-vs-mispricing-debate]] - Carry has both risk and behavioral explanations
