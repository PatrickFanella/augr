---
title: Trader Agent
description: Composes research insights into a concrete trading plan with entry/exit and sizing
type: agent
source_file: TradingAgents/tradingagents/agents/trader/trader.py
created: 2026-03-20
---

# Trader Agent

The third phase of the [[architecture|pipeline]]. Takes the [[research-team|Research Manager's]] investment plan and translates it into a concrete, actionable trading plan.

## Inputs

- Investment plan from the Research Manager
- Current market data (price, volume, key levels)
- Analyst reports from the [[analyst-team]]

## Output

A detailed trading plan including:

- **Action**: Buy, sell, or hold
- **Entry point**: Specific price level or condition
- **Position size**: How much to allocate
- **Stop-loss**: Downside protection level
- **Take-profit**: Target exit price
- **Time horizon**: Expected holding period
- **Risk parameters**: Maximum acceptable loss
- **Rationale**: Natural language explanation

## Role in Pipeline

The trader bridges the gap between the research team's qualitative investment thesis and the [[risk-management-team]]'s final risk-adjusted decision:

```
Research Manager → Investment Plan
                       ↓
                  Trader Agent → Trading Plan
                       ↓
              Risk Management Team → Final Decision
```

The trader's plan is then stress-tested by the three risk debators before the risk manager issues the final signal.

## Memory

The trader has its own [[bm25-memory-system|memory store]], separate from the researchers. Past trading plans and their outcomes inform future decisions.

## Related

- [[research-team]] - Provides the investment plan
- [[risk-management-team]] - Evaluates the trading plan
- [[bm25-memory-system]] - Trader-specific learning
