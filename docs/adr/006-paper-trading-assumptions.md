---
title: "ADR-006: Paper trading slippage and fee assumptions"
description: "Architecture decision record."
status: "superseded"
updated: "2026-08-14"
tags: [adr]
---

# ADR-006: Paper trading slippage and fee assumptions

- **Status:** superseded by [ADR-018](018-scored-and-stress-paper-modes.md)
- **Date:** 2026-03-27
- **Deciders:** Engineering
- **Technical Story:** [#96](https://github.com/PatrickFanella/get-rich-quick/issues/96)

## Context

The paper broker (`internal/execution/paper/broker.go`) accepts `slippageBps` and `feePct` as constructor parameters. The backtest fill engine (`internal/backtest/fill_engine.go`) provides three slippage models (fixed, proportional, volatility-scaled) and a `TransactionCosts` struct with per-order commission, per-unit commission, and exchange fee percentage.

We need to decide default values per market type so paper trading results are realistic without requiring manual configuration.

## Decision

### Default slippage per market type

| Market              | Slippage (bps) | Model             | Rationale                            |
| ------------------- | :------------: | ----------------- | ------------------------------------ |
| US Large-cap Equity |       5        | Proportional      | Tight spreads for >$1B market cap    |
| US Small-cap Equity |       25       | Proportional      | Wider spreads, lower liquidity       |
| Crypto (BTC, ETH)   |       10       | Proportional      | Tier-1 pairs on major exchanges      |
| Crypto (altcoins)   |       30       | Volatility-scaled | Thin order books, regime-sensitive   |
| Polymarket          |       50       | Fixed             | CLOB with low and variable liquidity |

### Default fees per broker

| Broker           | Commission   | Exchange Fee                      | Notes                                                  |
| ---------------- | ------------ | --------------------------------- | ------------------------------------------------------ |
| Alpaca (stocks)  | $0 per order | 0.01% of notional                 | Zero-commission; SEC/FINRA fees negligible but modeled |
| Binance (crypto) | $0 per order | 0.10% taker fee                   | Standard tier; maker fee lower but we assume taker     |
| Polymarket       | $0 per order | 0% commission, 2% on net winnings | Fee on resolution, not on trade entry                  |

### Slippage model selection

- **Proportional model for live paper trading** — simpler, more predictable, sufficient for monitoring purposes.
- **Volatility-scaled model for backtests** — captures regime shifts (e.g., flash crash days where slippage spikes).

### Validation cadence

After 30 days of paper trading, compare simulated fill prices against actual market prices at the same timestamps. If simulated P&L exceeds what real fills would have produced by more than 10%, tighten slippage assumptions. This prevents false confidence from over-optimistic simulation.

## Consequences

- Paper trading P&L will be slightly worse than a zero-cost simulation, which is intentional. We prefer conservative estimates that survive the transition to live trading.
- The 5 bps default for large-cap stocks is at the higher end of realistic (1-3 bps is achievable on highly liquid names) but provides a safety margin.
- Polymarket's 50 bps fixed slippage may underestimate cost on very illiquid contracts. The risk debate agents are prompted to flag illiquid Polymarket positions as a qualitative check.
