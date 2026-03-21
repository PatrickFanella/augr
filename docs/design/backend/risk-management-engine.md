---
title: "Risk Management Engine"
date: 2026-03-20
tags: [backend, risk, circuit-breaker, kill-switch, position-sizing]
---

# Risk Management Engine

The risk management engine enforces portfolio-level controls that operate independently of the agent debate system. It provides hard limits that cannot be overridden by agent decisions.

## Architecture

```
Pipeline Signal (BUY/SELL)
         │
         ▼
┌────────────────────────┐
│   Pre-Trade Checks     │
│  ├── Kill Switch       │
│  ├── Circuit Breakers  │
│  ├── Position Limits   │
│  └── Order Validation  │
└────────┬───────────────┘
         │ PASS
         ▼
┌────────────────────────┐
│   Position Sizing      │
│  ├── ATR-Based         │
│  ├── Kelly Criterion   │
│  └── Fixed Fractional  │
└────────┬───────────────┘
         │
         ▼
    Execution Engine
```

## Kill Switch

Three independent kill switch mechanisms — any one halts all trading:

```go
// internal/risk/kill_switch.go
type KillSwitch struct {
    apiActive  atomic.Bool      // toggled via REST API
    fileFlag   string           // path to kill switch file
    envVar     string           // environment variable name
}

func (ks *KillSwitch) IsActive() bool {
    // API-triggered
    if ks.apiActive.Load() {
        return true
    }
    // File-based (touch /tmp/trading-kill-switch)
    if _, err := os.Stat(ks.fileFlag); err == nil {
        return true
    }
    // Environment variable
    if os.Getenv(ks.envVar) == "true" {
        return true
    }
    return false
}
```

## Circuit Breakers

Automatic trading halts based on portfolio metrics:

```go
// internal/risk/circuit_breaker.go
type CircuitBreakerConfig struct {
    MaxDailyLossPct     float64 // e.g., 3.0 = halt at 3% daily loss
    MaxDrawdownPct      float64 // e.g., 10.0 = halt at 10% drawdown from peak
    MaxConsecutiveLosses int    // e.g., 5 = halt after 5 consecutive losing trades
    CooldownMinutes     int    // time before auto-reset (0 = manual reset only)
}

type CircuitBreaker struct {
    config    CircuitBreakerConfig
    state     atomic.Value // "closed" (normal), "open" (halted), "half-open" (testing)
    tripped   atomic.Bool
    trippedAt time.Time
    reason    string
}

func (cb *CircuitBreaker) Check(ctx context.Context, portfolio PortfolioSnapshot) error {
    // Daily loss check
    if portfolio.DailyPnLPct < -cb.config.MaxDailyLossPct {
        return cb.trip(fmt.Sprintf("daily loss %.2f%% exceeds limit %.2f%%",
            portfolio.DailyPnLPct, cb.config.MaxDailyLossPct))
    }

    // Max drawdown check
    if portfolio.DrawdownPct > cb.config.MaxDrawdownPct {
        return cb.trip(fmt.Sprintf("drawdown %.2f%% exceeds limit %.2f%%",
            portfolio.DrawdownPct, cb.config.MaxDrawdownPct))
    }

    // Consecutive losses check
    if portfolio.ConsecutiveLosses >= cb.config.MaxConsecutiveLosses {
        return cb.trip(fmt.Sprintf("%d consecutive losses",
            portfolio.ConsecutiveLosses))
    }

    return nil // all clear
}
```

## Position Sizing

Three sizing strategies, configured per strategy:

### ATR-Based (Default)

```go
func ATRPositionSize(accountValue, riskPct, atr, entryPrice float64, atrMultiplier float64) float64 {
    riskAmount := accountValue * (riskPct / 100)        // e.g., 2% of $100K = $2K
    stopDistance := atr * atrMultiplier                   // e.g., 1.5 * ATR
    sharesAtRisk := riskAmount / stopDistance             // shares that risk $2K
    positionValue := sharesAtRisk * entryPrice
    return math.Min(positionValue, accountValue*0.20)    // cap at 20% of account
}
```

### Kelly Criterion

```go
func KellyPositionSize(winRate, avgWinLossRatio float64, fraction float64) float64 {
    // f* = W - (1-W)/R
    kelly := winRate - (1-winRate)/avgWinLossRatio
    return math.Max(0, kelly * fraction) // fraction = 0.25 for Quarter-Kelly
}
```

### Fixed Fractional

```go
func FixedFractionalSize(accountValue, riskPct float64) float64 {
    return accountValue * (riskPct / 100)
}
```

## Position Limits

| Limit                   | Default         | Description                                     |
| ----------------------- | --------------- | ----------------------------------------------- |
| Max position size       | 20% of account  | No single position exceeds this                 |
| Max total exposure      | 100% of account | Sum of all position values                      |
| Max positions           | 10              | Number of concurrent open positions             |
| Max per-market exposure | 50%             | No more than half the account in one market     |
| Polymarket per-market   | 5%              | Binary outcome risk limit per prediction market |

## Pre-Trade Check Pipeline

```go
// internal/risk/engine.go
type Engine struct {
    killSwitch     *KillSwitch
    circuitBreaker *CircuitBreaker
    limits         PositionLimits
    portfolioRepo  repository.PositionRepository
}

func (e *Engine) ValidateTrade(ctx context.Context, signal TradeSignal) error {
    // 1. Kill switch
    if e.killSwitch.IsActive() {
        return ErrKillSwitchActive
    }

    // 2. Circuit breakers
    portfolio, err := e.getPortfolioSnapshot(ctx)
    if err != nil {
        return fmt.Errorf("portfolio snapshot: %w", err)
    }
    if err := e.circuitBreaker.Check(ctx, portfolio); err != nil {
        return err
    }

    // 3. Position limits
    if err := e.checkPositionLimits(ctx, signal, portfolio); err != nil {
        return err
    }

    // 4. Order validation
    return e.validateOrder(signal)
}
```

## Alerting

When a circuit breaker trips or kill switch activates:

1. Log at ERROR level with full context
2. Emit `circuit_breaker` event via WebSocket
3. Send notification (Telegram/Discord webhook — configurable)
4. Record in `audit_log` table

---

**Related:** [[risk-management-agents]] · [[execution-engine]] · [[agent-orchestration-engine]]
