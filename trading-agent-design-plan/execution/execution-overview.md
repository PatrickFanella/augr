---
title: "Execution Overview"
date: 2026-03-20
tags: [execution, trading, multi-market, orders]
---

# Execution Overview

Multi-market execution architecture supporting US equities, crypto, and prediction markets through a unified broker interface.

## Market-Specific Implementation

### US Equities — Alpaca

| Feature           | Details                                                            |
| ----------------- | ------------------------------------------------------------------ |
| API               | REST `https://api.alpaca.markets/v2`                               |
| Paper Trading     | `https://paper-api.alpaca.markets/v2` — $100K virtual account      |
| Commission        | Zero commission                                                    |
| Order Types       | Market, limit, stop, stop-limit, trailing stop                     |
| Fractional Shares | Supported — enables precise position sizing                        |
| Streaming         | WebSocket for trade updates                                        |
| Regulation        | Pattern Day Trader rule ($25K minimum for 4+ day trades in 5 days) |

**Implementation priority:** Phase 4 first target — best paper trading support.

### Crypto — Exchange APIs

| Exchange | Fee        | Testnet                  | Primary Use              |
| -------- | ---------- | ------------------------ | ------------------------ |
| Binance  | ~0.1%      | `testnet.binance.vision` | Primary for major pairs  |
| Coinbase | 0.4–0.6%   | N/A                      | US-regulated alternative |
| Kraken   | 0.16–0.26% | N/A                      | EU alternative           |

**Key Differences from Equities:**

- 24/7 operation — system must handle weekends and holidays
- Higher volatility — wider stops and smaller position sizes
- Funding rates (futures) — must track cost of carry
- Gas fees (DEX) — Ethereum ~$0.44 per tx, Solana ~$0.00025
- Counterparty risk — exchange may become insolvent (FTX precedent)

### Prediction Markets — Polymarket

| Feature       | Details                                           |
| ------------- | ------------------------------------------------- |
| Architecture  | Hybrid CLOB on Polygon L2                         |
| Order signing | EIP-712 signatures                                |
| Rate limits   | 60 orders/minute                                  |
| Order types   | GTC, FOK, GTD                                     |
| Regulatory    | CFTC-approved DCM (Nov 2025)                      |
| Resolution    | Polymarket team, Chainlink, UMA Optimistic Oracle |

**Unique Risks:**

- Binary outcomes — total loss possible on any single market
- Resolution risk — oracle may resolve ambiguously
- Liquidity — thin markets can have wide spreads
- Limit: < 5% of portfolio per market

## Broker Selection Logic

The system selects the appropriate broker based on strategy `market_type`:

```go
func (e *ExecutionEngine) getBroker(strategy domain.Strategy) (Broker, error) {
    if strategy.IsPaper {
        return e.paperBroker, nil
    }
    switch strategy.MarketType {
    case "stock":
        return e.alpacaBroker, nil
    case "crypto":
        return e.cryptoBroker, nil // Binance by default
    case "polymarket":
        return e.polymarketBroker, nil
    default:
        return nil, fmt.Errorf("unknown market type: %s", strategy.MarketType)
    }
}
```

## Order Routing Flow

```
Signal (BUY AAPL)
     │
     ▼
Risk Engine ──► Pre-trade checks (limits, circuit breakers)
     │
     ▼
Position Sizer ──► Calculate quantity (ATR/Kelly/fixed)
     │
     ▼
Order Builder ──► Create order (type, price, TIF)
     │
     ▼
Broker Adapter ──► Submit to Alpaca/Binance/Polymarket
     │
     ▼
Fill Monitor ──► Track fills via polling or WebSocket
     │
     ▼
Position Tracker ──► Update positions table, calculate P&L
     │
     ▼
Audit Logger ──► Record in audit_log table
```

## Fee Tracking

All fees are recorded per trade:

```sql
INSERT INTO trades (order_id, ticker, side, quantity, price, fee, executed_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);
```

Fee schedules per broker are configured and used to estimate costs during position sizing.

## Market Hours Awareness

| Market      | Hours                  | System Behavior                                            |
| ----------- | ---------------------- | ---------------------------------------------------------- |
| US Equities | 9:30–16:00 ET, Mon–Fri | Pipeline scheduled at market open; no orders outside hours |
| Crypto      | 24/7                   | Continuous operation; scheduler runs on fixed intervals    |
| Polymarket  | 24/7                   | Same as crypto                                             |

The scheduler respects market hours — equity strategies only trigger during trading hours.

---

**Related:** [[execution-engine]] · [[risk-management-engine]] · [[paper-trading]] · [[system-architecture]]
