---
title: Position Management & Exits
description: Monitoring positions, exit strategies, trailing stops, and scaling out across platforms
type: reference
tags: [exits, trailing-stops, profit-targets, position-monitoring]
created: 2026-03-20
---

# Position Management & Exits

## Monitoring Open Positions

| Platform | Method                                                    |
| -------- | --------------------------------------------------------- |
| Alpaca   | `client.get_all_positions()` -- real-time P&L per holding |
| IBKR     | `ib.reqPnL()` -- streaming unrealized/realized P&L        |
| CCXT     | `exchange.fetch_positions()` -- crypto futures            |

## Exit Strategy Types

### Profit Targets

- Limit sell orders at target price
- Static or ATR-based (e.g. 2x ATR from entry)
- See [[position-sizing]] for ATR-based calculations

### Trailing Stops

- Automatically adjust upward as price rises
- Alpaca natively supports `TrailingStopOrderRequest` (percentage or dollar trail)
- Useful in strongly trending markets to maximize profit runs

### Time-Based Exits

- Close positions after defined holding periods
- Critical for prediction markets (manage relative to resolution dates)
- Intraday strategies: close before market close at 3:55 PM ET

### LLM-Driven Exits

- [[multi-agent-trading-systems|Multi-agent frameworks]] detect sentiment shifts, news developments, or technical divergences
- Generate sell signals with natural language justifications and invalidation conditions
- Unique capability of LLM trading systems

## Scaling Out (Partial Exits)

Improves risk-adjusted returns:

1. Close 50% at first profit target
2. Move stop to breakeven on remainder
3. Alpaca handles via partial quantity sell orders

For prediction markets: selling before resolution to lock in partial gains is often preferable to binary $0-or-$1 risk.

## Implementation

- Verify fill status and switch to more aggressive orders if target/stop won't fill
- Log every exit with complete reasoning trace
- Multiple exit criteria (time, profit, technical, news) is safest approach
- Always guard LLM exit recommendations with logic rules

## Related

- [[position-sizing]] - ATR-based stop calculations
- [[portfolio-risk-controls]] - Portfolio-level exit triggers
- [[stock-market-execution]] - Platform-specific exit orders
