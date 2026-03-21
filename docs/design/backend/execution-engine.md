---
title: "Execution Engine"
date: 2026-03-20
tags: [backend, execution, orders, broker, trading]
---

# Execution Engine

The execution engine translates trading signals into orders, routes them to the appropriate broker, and tracks the full order lifecycle.

## Broker Interface

```go
// internal/execution/broker.go
type Broker interface {
    Name() string
    SubmitOrder(ctx context.Context, req OrderRequest) (*OrderResult, error)
    GetOrderStatus(ctx context.Context, orderID string) (*OrderStatus, error)
    CancelOrder(ctx context.Context, orderID string) error
    GetPositions(ctx context.Context) ([]BrokerPosition, error)
    GetAccountBalance(ctx context.Context) (*AccountBalance, error)
}

type OrderRequest struct {
    Ticker    string
    Side      string  // "buy", "sell"
    OrderType string  // "market", "limit", "stop", "stop_limit"
    Quantity  float64
    LimitPrice *float64
    StopPrice  *float64
    TimeInForce string // "day", "gtc", "ioc"
}

type OrderResult struct {
    ExternalID string
    Status     string
    SubmittedAt time.Time
}

type OrderStatus struct {
    ExternalID    string
    Status        string  // "new", "partial", "filled", "cancelled", "rejected"
    FilledQty     float64
    FilledAvgPrice float64
    FilledAt      *time.Time
}
```

## Broker Implementations

### Alpaca (US Equities)

- REST API: `https://paper-api.alpaca.markets/v2` (paper) / `https://api.alpaca.markets/v2` (live)
- Commission-free; supports fractional shares
- Paper trading: $100K virtual account — ideal for validation
- Auth: `APCA-API-KEY-ID` + `APCA-API-SECRET-KEY` headers
- Order types: market, limit, stop, stop-limit, trailing stop
- WebSocket: `wss://stream.data.alpaca.markets/v2` for real-time trade updates

```go
// internal/execution/alpaca.go
type AlpacaBroker struct {
    client  *http.Client
    baseURL string
    apiKey  string
    secret  string
}

func (a *AlpacaBroker) SubmitOrder(ctx context.Context, req OrderRequest) (*OrderResult, error) {
    body := alpacaOrderRequest{
        Symbol:      req.Ticker,
        Qty:         fmt.Sprintf("%.2f", req.Quantity),
        Side:        req.Side,
        Type:        req.OrderType,
        TimeInForce: req.TimeInForce,
    }
    // POST /v2/orders
    resp, err := a.post(ctx, "/v2/orders", body)
    // ...
}
```

### Binance (Crypto)

- REST API: `https://api.binance.com/api/v3`
- Auth: HMAC-SHA256 signed requests
- Fee: ~0.1% maker/taker
- Supports spot and futures
- Testnet available at `https://testnet.binance.vision`

### Polymarket (Prediction Markets)

- CLOB on Polygon L2: EIP-712 signed orders
- Auth: Ethereum wallet signature
- Rate limits: 60 orders/minute
- Order types: GTC, FOK, GTD
- Binary outcomes only — requires understanding of Conditional Token Framework

### Paper Trading Simulator

Built-in paper trading engine that simulates realistic execution:

```go
// internal/execution/paper.go
type PaperBroker struct {
    positions map[string]*PaperPosition
    orders    map[string]*PaperOrder
    balance   float64
    mu        sync.RWMutex
    slippage  float64 // basis points
    dataProvider data.MarketDataProvider
}

func (p *PaperBroker) SubmitOrder(ctx context.Context, req OrderRequest) (*OrderResult, error) {
    // Get current market price
    price, err := p.dataProvider.GetLatestPrice(ctx, req.Ticker)
    if err != nil {
        return nil, err
    }

    // Apply slippage
    fillPrice := price * (1 + p.slippage/10000)
    if req.Side == "sell" {
        fillPrice = price * (1 - p.slippage/10000)
    }

    // Simulate fill
    // ...
}
```

Paper trading adds configurable slippage (default: 5 basis points) and simulates partial fills for large orders.

## Order Lifecycle

```
Signal (buy/sell/hold)
    │
    ▼
Risk Engine Check ──► REJECTED (if limits breached)
    │
    ▼
Position Sizing ──► Calculate quantity from config
    │
    ▼
Order Creation ──► Save to `orders` table (status: pending)
    │
    ▼
Broker Submission ──► Update status to submitted
    │
    ├──► Partial Fill ──► Update filled_quantity
    │
    ├──► Full Fill ──► Update position, calculate P&L
    │
    └──► Rejected/Cancelled ──► Log reason, alert
```

## Position Tracking

```go
// internal/execution/position.go
type PositionTracker struct {
    repo repository.PositionRepository
}

func (pt *PositionTracker) OnFill(ctx context.Context, fill FillEvent) error {
    // Update or create position
    pos, err := pt.repo.GetOpenPosition(ctx, fill.StrategyID, fill.Ticker)
    if err != nil && !errors.Is(err, ErrNotFound) {
        return err
    }

    if pos == nil {
        // New position
        return pt.repo.Create(ctx, &domain.Position{
            StrategyID: fill.StrategyID,
            Ticker:     fill.Ticker,
            Side:       fill.Side,
            Quantity:   fill.Quantity,
            AvgEntry:   fill.Price,
        })
    }

    // Update existing — recalculate average entry
    newQty := pos.Quantity + fill.Quantity
    pos.AvgEntry = (pos.AvgEntry*pos.Quantity + fill.Price*fill.Quantity) / newQty
    pos.Quantity = newQty
    return pt.repo.Update(ctx, pos)
}
```

## Execution Safeguards

- **Pre-submission**: Check kill switch, circuit breakers, position limits
- **Order validation**: Verify ticker exists, quantity > 0, price within reasonable range
- **Duplicate prevention**: Cooldown period between identical orders (configurable, default 60s)
- **No auto-retry**: Failed orders are logged and alerted, never retried automatically
- **Audit trail**: Every order state change is logged to `audit_log`

---

**Related:** [[execution-overview]] · [[risk-management-engine]] · [[paper-trading]] · [[agent-orchestration-engine]]
