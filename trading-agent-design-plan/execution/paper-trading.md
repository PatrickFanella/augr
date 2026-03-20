---
title: "Paper Trading"
date: 2026-03-20
tags: [execution, paper-trading, simulation, validation]
---

# Paper Trading

Built-in simulated trading engine for validating strategies before deploying real capital.

## Two Modes

### 1. Alpaca Paper Trading

Alpaca provides a native paper trading environment:

- $100K virtual account
- Same API as live (`paper-api.alpaca.markets`)
- Realistic fills against real market data
- Separate API keys for paper vs. live

**Pros:** Most realistic simulation — uses real order book data
**Cons:** Only for US equities; requires Alpaca account

### 2. Internal Paper Engine

For crypto and prediction markets (or when Alpaca is unavailable):

```go
// internal/execution/paper.go
type PaperBroker struct {
    mu           sync.RWMutex
    orders       map[string]*PaperOrder
    positions    map[string]*PaperPosition
    balance      float64
    initialBalance float64
    dataProvider data.MarketDataProvider
    config       PaperConfig
}

type PaperConfig struct {
    InitialBalance float64 // default: $100,000
    SlippageBps    float64 // default: 5 basis points
    FeeRate        float64 // default: 0.001 (0.1% — Binance-like)
    PartialFillPct float64 // default: 1.0 (100% fill)
}
```

### Simulated Fill Logic

```go
func (p *PaperBroker) simulateFill(order *PaperOrder) (*FillEvent, error) {
    // Get current market price
    price, err := p.dataProvider.GetLatestPrice(context.Background(), order.Ticker)
    if err != nil {
        return nil, err
    }

    // Check if order can fill
    switch order.OrderType {
    case "market":
        // Always fills at market + slippage
    case "limit":
        if order.Side == "buy" && price > order.LimitPrice {
            return nil, nil // not fillable yet
        }
        if order.Side == "sell" && price < order.LimitPrice {
            return nil, nil
        }
        price = order.LimitPrice // limit orders fill at limit price
    }

    // Apply slippage
    slippage := price * (p.config.SlippageBps / 10000)
    if order.Side == "buy" {
        price += slippage
    } else {
        price -= slippage
    }

    // Calculate fee
    fee := price * order.Quantity * p.config.FeeRate

    // Partial fill simulation
    fillQty := order.Quantity * p.config.PartialFillPct

    return &FillEvent{
        OrderID:  order.ID,
        Ticker:   order.Ticker,
        Side:     order.Side,
        Quantity: fillQty,
        Price:    price,
        Fee:      fee,
    }, nil
}
```

## Validation Protocol

Before transitioning to live trading, strategies must pass a paper trading validation period:

### Criteria

| Metric           | Minimum Requirement                  |
| ---------------- | ------------------------------------ |
| Duration         | 60 days (or 30 days for crypto)      |
| Number of trades | At least 20 round-trip trades        |
| Sharpe ratio     | > 1.0 (annualized)                   |
| Max drawdown     | < 15%                                |
| Win rate         | > 40% (with appropriate risk/reward) |
| Profit factor    | > 1.5                                |

### Transition Checklist

- [ ] Strategy has been profitable in paper trading
- [ ] Max drawdown within acceptable limits
- [ ] Sharpe ratio meets minimum threshold
- [ ] Understand difference between paper and live fills
- [ ] Set initial live position size to 25% of paper size
- [ ] Circuit breakers configured for live trading
- [ ] Kill switch tested and working
- [ ] Alerting (Telegram/Discord) configured and tested

## Limitations of Paper Trading

| Limitation             | Impact                                    | Mitigation                                      |
| ---------------------- | ----------------------------------------- | ----------------------------------------------- |
| Idealized fills        | Paper fills at exact price; live may slip | Add slippage simulation (5-10 bps)              |
| No market impact       | Large orders don't move price in paper    | Limit position sizes relative to volume         |
| Different API behavior | Subtle API differences between paper/live | Shared broker interface abstracts this          |
| No emotional pressure  | Paper trading removes fear/greed          | Start live with small positions                 |
| No counterparty risk   | Paper never has exchange failures         | Include exchange diversification in live config |

## Performance Tracking

Paper trading results are stored in the same tables as live trades (`orders`, `trades`, `positions`) with `is_paper = true` on the strategy. This enables:

- Identical performance metrics calculation
- Side-by-side comparison of paper vs. live
- Historical paper results preserved after going live

---

**Related:** [[execution-overview]] · [[execution-engine]] · [[risk-management-engine]] · [[implementation-roadmap]]
