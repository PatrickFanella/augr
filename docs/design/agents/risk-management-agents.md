---
title: "Risk Management Agents"
date: 2026-03-20
tags: [agents, risk, debate, aggressive, conservative, neutral]
---

# Risk Management Agents

Phase 4 of the pipeline. Three risk perspectives debate the Trader Agent's plan, and a Risk Manager renders the final BUY/SELL/HOLD decision.

## Architecture

```
Trading Plan (from Trader Agent)
         │
         ▼
┌─────────────────────────────────────────┐
│  Risk Debate (N rounds)                  │
│                                          │
│  Round 1:                                │
│    Aggressive → Conservative → Neutral   │
│  Round 2:                                │
│    Aggressive → Conservative → Neutral   │
│  ...                                     │
│                                          │
│  Risk Manager (Judge)                    │
│    → FINAL DECISION: BUY / SELL / HOLD   │
└─────────────────────────────────────────┘
```

## The Three Perspectives

### Aggressive Analyst

**Bias:** Maximize returns; accept higher risk.

> You are an aggressive risk analyst. Evaluate the trading plan with a focus on MAXIMIZING RETURNS. Consider:
>
> - Is the position size large enough to capture the opportunity?
> - Should the stop-loss be wider to avoid being shaken out?
> - Could the take-profit be more ambitious?
> - Is there an opportunity to use leverage?

### Conservative Analyst

**Bias:** Preserve capital; minimize risk.

> You are a conservative risk analyst. Evaluate the trading plan with a focus on CAPITAL PRESERVATION. Consider:
>
> - Is the position size too large relative to account?
> - Is the stop-loss tight enough?
> - What's the worst-case scenario?
> - Are there hidden risks (liquidity, gap risk, correlation)?
> - Should we reduce position size or pass entirely?

### Neutral Analyst

**Bias:** Balanced risk/reward optimization.

> You are a neutral risk analyst. Balance the aggressive and conservative perspectives. Consider:
>
> - Is the risk/reward ratio justified by the thesis conviction?
> - Does the position size match the confidence level?
> - Are the stop-loss and take-profit levels technically sound?
> - What's the portfolio-level impact (correlation with existing positions)?

## Risk Manager (Final Judge)

The Risk Manager reviews the complete debate and issues the definitive trading signal.

**Inputs:**

- Trading plan from [[trader-agent]]
- Complete risk debate transcript
- Portfolio snapshot (existing positions, P&L, exposure)
- Risk limits from [[risk-management-engine]]

**System Prompt Core:**

> You are the Risk Manager. You have final authority over all trading decisions. Review:
>
> Trading Plan: {trading_plan}
> Risk Debate: {debate_transcript}
> Portfolio: {portfolio_snapshot}
> Risk Limits: {risk_limits}
>
> Make your FINAL DECISION:
>
> - **BUY**: Approve the trade (specify any modifications to size/stops)
> - **SELL**: Close an existing position
> - **HOLD**: Reject the trade (explain why)
>
> Your decision is final and will be executed.

## Implementation

```go
// internal/agent/risk/manager.go
type RiskManager struct {
    agent.BaseAgent
    portfolioRepo repository.PositionRepository
    riskEngine    risk.Engine
}

func (rm *RiskManager) Execute(ctx context.Context, state *agent.PipelineState) error {
    // Get portfolio snapshot for context
    positions, _ := rm.portfolioRepo.GetOpenPositions(ctx, state.Config.StrategyID)
    riskStatus, _ := rm.riskEngine.GetStatus(ctx)

    req := rm.buildPrompt(riskManagerSystemPrompt, state)
    req.Messages = append(req.Messages, llm.Message{
        Role: "user",
        Content: buildRiskManagerPrompt(
            state.TradingPlan,
            state.RiskDebate.Messages,
            positions,
            riskStatus,
        ),
    })

    resp, err := rm.llm.Complete(ctx, req)
    if err != nil {
        return fmt.Errorf("risk manager LLM call: %w", err)
    }

    state.RiskDebate.JudgeResponse = resp.Content
    return nil
}
```

## Signal Extraction

After the Risk Manager responds, the system extracts a clean signal:

```go
// internal/agent/signal/extractor.go
func ExtractSignal(riskManagerResponse string) (string, float64) {
    response := strings.ToLower(riskManagerResponse)

    // Look for explicit decision keywords
    switch {
    case strings.Contains(response, "final decision: buy"),
         strings.Contains(response, "decision: buy"),
         strings.Contains(response, "approve the trade"):
        return "buy", extractConfidence(response)

    case strings.Contains(response, "final decision: sell"),
         strings.Contains(response, "decision: sell"):
        return "sell", extractConfidence(response)

    default:
        return "hold", extractConfidence(response)
    }
}
```

The signal is conservative — if the response is ambiguous, it defaults to "hold".

## Debate Configuration

| Parameter                  | Default | Description                                    |
| -------------------------- | ------- | ---------------------------------------------- |
| `max_risk_debate_rounds`   | 3       | Number of round-robin debate rounds            |
| `risk_debate_temperature`  | 0.4     | LLM temperature for risk debaters              |
| `risk_manager_temperature` | 0.1     | Low temperature for consistent final decisions |

## Two Layers of Risk

The system has two independent risk layers:

1. **Agent-level risk** (this document) — LLM-based risk debate that evaluates thesis quality
2. **System-level risk** ([[risk-management-engine]]) — Hard-coded circuit breakers, position limits, kill switches

The system-level risk check runs AFTER the agent-level decision. Even if the Risk Manager says "BUY", the system-level engine can reject the order if portfolio limits are breached.

```
Risk Manager → "BUY" → System Risk Engine → ✓ PASS → Order Submitted
Risk Manager → "BUY" → System Risk Engine → ✗ FAIL → Order Blocked (limit breached)
```

---

**Related:** [[agent-system-overview]] · [[trader-agent]] · [[risk-management-engine]] · [[execution-engine]]
