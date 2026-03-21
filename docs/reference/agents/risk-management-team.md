---
title: Risk Management Team
description: Three-perspective risk debate (aggressive/conservative/neutral) and risk manager final decision
type: agents
source_files:
  - TradingAgents/tradingagents/agents/risk_mgmt/aggressive_debator.py
  - TradingAgents/tradingagents/agents/risk_mgmt/conservative_debator.py
  - TradingAgents/tradingagents/agents/risk_mgmt/neutral_debator.py
  - TradingAgents/tradingagents/agents/managers/risk_manager.py
created: 2026-03-20
---

# Risk Management Team

The final phase of the [[architecture|pipeline]]. Three risk analysts debate the [[trader-agent]]'s plan from different risk perspectives, and a Risk Manager issues the **final BUY/SELL/HOLD decision**.

## Risk Debators

### Aggressive Analyst

- Focuses on **maximizing returns**
- Willing to accept higher risk for higher potential reward
- Arguments emphasize upside potential, momentum, and growth opportunity
- May advocate for larger position sizes and tighter profit targets

### Conservative Analyst

- Focuses on **capital preservation**
- Prioritizes downside protection and risk minimization
- Arguments emphasize potential losses, volatility, and uncertainty
- May advocate for smaller positions, wider stops, or passing on the trade

### Neutral Analyst

- Focuses on **balanced risk/reward**
- Weighs both upside and downside objectively
- Arguments emphasize risk-adjusted returns and position sizing
- Mediates between aggressive and conservative perspectives

## Debate Mechanism

```
Aggressive → Conservative → Neutral → ... → Risk Manager
                    ↑                             ↑
          max_risk_discuss_rounds          judges, issues
              (default: 1)              final BUY/SELL/HOLD
```

Controlled by `max_risk_discuss_rounds` in [[configuration]].

## Risk Manager (Final Judge)

The Risk Manager:

1. Reads the full risk debate transcript
2. Evaluates the trader's plan against all three risk perspectives
3. Issues the **final trading decision**: BUY, SELL, or HOLD
4. Provides detailed justification
5. May modify position sizing or risk parameters from the trader's original plan

This is the last agent in the pipeline. Its output goes to [[signal-processing]] for clean extraction.

## State Flow

Uses `RiskDebateState` (see [[state-management]]):

- `risk_debate_messages`: Risk debate conversation history
- `risk_judge_response`: Risk manager's final decision
- `risk_debate_count`: Current round number

## Memory

The Risk Manager has its own [[bm25-memory-system|memory store]], learning from past risk assessments and their outcomes.

## Related

- [[trader-agent]] - Provides the trading plan being evaluated
- [[signal-processing]] - Extracts the clean decision
- [[research-team]] - Analogous debate pattern (bull/bear)
- [[conditional-routing]] - How debate rounds are controlled
