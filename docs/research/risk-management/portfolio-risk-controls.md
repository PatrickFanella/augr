---
title: Portfolio Risk Controls
description: Drawdown limits, VaR/CVaR, circuit breakers, kill switches, and correlation monitoring
type: reference
tags: [risk-management, drawdown, VaR, circuit-breaker, kill-switch, safety]
created: 2026-03-20
---

# Portfolio Risk Controls

## Portfolio-Level Safeguards

- **Maximum drawdown limits**: Flatten all positions when drawdown exceeds threshold (commonly 10%)
- **Daily loss limits**: Halt trading after aggregate losses exceed a dollar amount
- **VaR (Value at Risk)**: Statistical risk bound at a confidence level
- **CVaR (Conditional VaR / Expected Shortfall)**: Averages losses beyond VaR threshold; captures tail risk better
- **Correlation monitoring**: Prevent concentrated exposure to correlated positions
- **Diversification**: Spread across sectors, assets, uncorrelated strategies (see [[strategy-diversification]])

## Circuit Breakers & Kill Switches

Production bots implement **layered safety architecture** with multiple independent triggers:

### Circuit Breakers

Auto-pause trading on extreme conditions:

- Daily loss exceeded
- Max drawdown breached
- Sudden market crash detected

### Kill Switches

Emergency halt via multiple channels:

- Environment variable flag
- File flag check
- Telegram command
- Keyboard interrupt

### Trade Cooldowns

Prevent rapid consecutive trades after losses.

### Order Validators

Pre-trade checks on position size, exposure limits, account equity.

## Example: claude-trader Safety Module

Structured as `src/safety/` with dedicated components:

- Kill switch
- Circuit breaker
- Loss limiter
- Cooldown manager
- Order validator

## Example: Polymarket Bot

Configurable limits: `MAX_BET_USD=100`, `MAX_DAILY_LOSS_USD=100`, `MAX_POSITION_PER_MARKET_USD=500`, plus three independent safety gates before any live trade.

## LLM-Specific Caution

LLMs can hallucinate or be misled by adversarial data. Always guard LLM outputs with logic rules. If a recommendation contradicts fundamental limits (e.g. "buy 1000 shares if insufficient funds"), flag it. Consider vetting critical inputs with a secondary model.

## Related

- [[position-sizing]] - Per-trade risk controls
- [[market-specific-risks]] - Regulatory and structural risks
- [[position-management]] - Exit execution
- [[llm-bot-architecture]] - Risk module in system design
