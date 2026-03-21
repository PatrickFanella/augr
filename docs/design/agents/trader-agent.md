---
title: "Trader Agent"
date: 2026-03-20
tags: [agents, trader, trading-plan, execution]
---

# Trader Agent

Phase 3 of the pipeline. Translates the Research Manager's investment plan into a concrete, executable trading plan with specific parameters.

## Role

The Trader Agent bridges research insights and execution. It receives the qualitative investment plan and produces quantitative trading parameters.

## Inputs

- Research Manager's investment plan (from [[research-debate-system]])
- All analyst reports (from [[analyst-agents]])
- Current market data (latest price, ATR, volume)
- Account information (balance, existing positions)
- Strategy configuration (risk parameters, position sizing method)

## Output — Trading Plan

```go
type TradingPlan struct {
    Action       string  // "buy", "sell", "hold"
    Ticker       string
    EntryType    string  // "market", "limit"
    EntryPrice   float64 // target entry (for limit orders)
    PositionSize float64 // dollar amount or share count
    StopLoss     float64 // stop-loss price
    TakeProfit   float64 // take-profit price
    TimeHorizon  string  // "intraday", "swing", "position"
    Confidence   float64 // 0.0 to 1.0
    Rationale    string  // human-readable justification
    RiskReward   float64 // calculated risk/reward ratio
}
```

## System Prompt Core

> You are a senior trader at a quantitative trading firm. You have received an investment plan from the research team. Translate it into a concrete trading plan.
>
> Investment Plan:
> {research_manager_judgment}
>
> Current Market Data:
>
> - Last Price: {current_price}
> - ATR(14): {atr_14}
> - Daily Volume: {avg_volume}
>
> Account:
>
> - Balance: {account_balance}
> - Existing Positions: {positions}
>
> Risk Parameters:
>
> - Max position size: {max_position_pct}%
> - Stop-loss ATR multiplier: {stop_loss_atr_mult}
> - Take-profit ATR multiplier: {take_profit_atr_mult}
>
> Produce a trading plan with:
>
> 1. **Action**: BUY, SELL, or HOLD
> 2. **Entry**: Market or limit order with specific price
> 3. **Position Size**: Dollar amount based on risk parameters
> 4. **Stop-Loss**: Specific price level
> 5. **Take-Profit**: Specific price level
> 6. **Time Horizon**: How long to hold
> 7. **Risk/Reward Ratio**: Must be at least 1:2
> 8. **Rationale**: Why this specific plan

## Implementation

```go
// internal/agent/trader/trader.go
type TraderAgent struct {
    agent.BaseAgent
    dataProvider data.MarketDataProvider
    riskEngine   risk.Engine
}

func (t *TraderAgent) Execute(ctx context.Context, state *agent.PipelineState) error {
    // Gather current market context
    latestPrice, err := t.dataProvider.GetLatestPrice(ctx, state.Ticker)
    if err != nil {
        return fmt.Errorf("get latest price: %w", err)
    }

    indicators, err := t.dataProvider.GetIndicators(ctx, data.IndicatorRequest{
        Ticker: state.Ticker,
        Window: 14,
    })
    if err != nil {
        return fmt.Errorf("get indicators: %w", err)
    }

    // Build prompt with full context
    req := t.buildPrompt(traderSystemPrompt, state)
    req.Messages = append(req.Messages, llm.Message{
        Role: "user",
        Content: buildTraderUserPrompt(
            state.InvestDebate.JudgeResponse,
            latestPrice,
            indicators.ATR,
            state.Config,
        ),
    })

    resp, err := t.llm.Complete(ctx, req)
    if err != nil {
        return fmt.Errorf("trader LLM call: %w", err)
    }

    state.TradingPlan = resp.Content
    return nil
}
```

## Trading Plan Validation

Before passing the plan to the risk debate, the system validates:

1. **Action is valid** — must be "buy", "sell", or "hold"
2. **Stop-loss exists** — no plan without a stop-loss
3. **Risk/reward ratio** — minimum 1:2 (configurable)
4. **Position size** — within account limits
5. **Entry price** — within 5% of current market price (sanity check)

If validation fails, the plan is rejected and the pipeline continues to the risk debate with a "hold" recommendation and the validation failure reason.

## Relationship to Execution

The Trader Agent produces a **plan**, not an order. The plan flows to the [[risk-management-agents]] for final approval. Only after the Risk Manager issues a BUY or SELL signal does the [[execution-engine]] create and submit an actual order.

```
Trader Agent → Trading Plan → Risk Debate → Risk Manager → Signal → Execution
```

This separation ensures the Trader Agent's LLM output never directly triggers a trade — there is always a risk review layer between analysis and execution.

---

**Related:** [[agent-system-overview]] · [[research-debate-system]] · [[risk-management-agents]] · [[execution-engine]]
