---
title: Stock Market Execution
description: Alpaca and Interactive Brokers APIs for equity order execution
type: reference
tags: [stocks, Alpaca, IBKR, equities, broker-API]
created: 2026-03-20
---

# Stock Market Execution

## Alpaca

Most accessible entry point for stock trading bots:

- Python SDK: `alpaca-py`
- Commission-free trading
- Order types: market, limit, stop, stop-limit, trailing stop, bracket
- Paper trading: $100K virtual funds, identical API (`paper=True`)
- Rate limits: 200 requests/minute
- Extended hours: pre-market 4:00 AM, after-hours 8:00 PM ET (limit orders only)
- `TradingStream` for real-time WebSocket order updates
- Fractional shares via notional dollar amounts
- Official MCP Server for natural language trading

## Interactive Brokers (IBKR)

Institutional-grade capabilities:

- Python library: `ib_async` (successor to `ib_insync`)
- 60+ order types: adaptive, TWAP, VWAP algorithms
- Global market access
- Requires running TWS or IB Gateway locally (port 7497 paper, 7496 live)
- Higher complexity: connection management, contract qualification, nightly system resets

## Execution Best Practices

- Use **limit orders with a slight price buffer** (mid-price + one tick) rather than market orders
- Monitor bid-ask spreads before placing orders
- Implement fill-rate tracking to detect execution quality degradation
- Real-time Level 1 data often free through brokers (Alpaca, IBKR provide IEX, Cboe One feeds)
- Level 2 depth-of-book requires paid subscriptions

## Regulatory Considerations

See [[market-specific-risks]] for:

- Pattern Day Trader rule ($25K equity requirement for 4+ day trades in 5 days)
- Wash sale rule (particularly dangerous for bots)
- Short Sale Restriction (SSR) -- check before any short order
- Reg SHO locate/borrow requirements for short selling

## Related

- [[position-management]] - Exit strategies across platforms
- [[paper-trading]] - Validating before live deployment
- [[market-specific-risks]] - Regulatory environment
- [[llm-trading-tools]] - Full tool ecosystem
