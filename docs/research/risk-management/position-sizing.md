---
title: Position Sizing
description: Kelly criterion, ATR-based stops, fixed fractional, and volatility targeting methods
type: reference
tags:
  [position-sizing, Kelly-criterion, ATR, volatility-targeting, risk-per-trade]
created: 2026-03-20
---

# Position Sizing

## ATR-Based Volatility Stops

Adapt to market conditions rather than fixed percentages:

```
stop = entry_price - (1.5 x ATR_14)
take_profit = entry_price + (3.0 x ATR_14)
```

Maintains 1:2 risk-reward ratio that adjusts automatically. Essential for crypto where daily moves of 10-20% are common.

## Kelly Criterion

Mathematically optimal bet size:

```
f = W - (1-W) / R
```

Where W = win rate, R = average win/loss ratio.

**Production systems should use Half-Kelly (50%) or Quarter-Kelly (25%)** to account for estimation error and tail risks. Full Kelly is dangerously aggressive.

## Fixed Fractional Method

Risk a fixed percentage (commonly 1-2%) of account equity per trade:

```
position_size = (account_value x 0.02) / stop_distance
```

Example: Risking 1% of $100K ($1K) with $10 stop-loss = 100 shares.

## Volatility Targeting

Adjust position weights inversely to current volatility, maintaining consistent portfolio-level risk. Used extensively in [[trend-following]] (targeting ~10% annualized portfolio vol).

## Platform-Specific Examples

- **Polymarket**: `MAX_BET_USD=100`, `MAX_POSITION_PER_MARKET_USD=500`
- **Alpaca**: Fractional shares via notional dollar amounts enable precise sizing
- **Crypto**: Smaller sizes during high vol; account for funding rates on perpetuals

## Related

- [[portfolio-risk-controls]] - Portfolio-level risk limits
- [[position-management]] - Exit execution using stop calculations
- [[market-specific-risks]] - Context-dependent sizing rules
