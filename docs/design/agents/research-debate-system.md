---
title: "Research Debate System"
date: 2026-03-20
tags: [agents, debate, research, bull, bear, adversarial]
---

# Research Debate System

Phase 2 of the pipeline. An adversarial debate between Bull and Bear researchers, judged by a Research Manager, produces a balanced investment plan.

## Why Adversarial Debate

Research on multi-agent systems shows that adversarial debate:

- Reduces confirmation bias (a major weakness of single-agent systems)
- Forces consideration of downside risks
- Produces more calibrated confidence estimates
- Mirrors real trading firm analyst meetings

The TradingAgents framework demonstrated 26.6% returns with this structure on AAPL analysis.

## Agents

### Bull Researcher

**Role:** Advocate for a long/bullish position.

**Inputs:**

- All analyst reports from Phase 1
- Previous debate rounds (if any)
- Relevant bull memories from [[memory-and-learning]]

**System Prompt Core:**

> You are a senior bull researcher at a trading firm. Your role is to build the strongest possible case for BUYING {TICKER}.
>
> Analyst Reports:
> {market_report}
> {fundamentals_report}
> {news_report}
> {social_report}
>
> {previous_debate_messages if round > 1}
>
> Build a compelling bullish thesis. Address the bear's counterarguments directly. Support claims with specific data from the analyst reports.

### Bear Researcher

**Role:** Advocate against the position / for a short position.

**Inputs:** Same as Bull, plus Bull's latest argument.

**System Prompt Core:**

> You are a senior bear researcher at a trading firm. Your role is to identify every risk and reason NOT to buy {TICKER}.
>
> Challenge the bull's thesis point by point. Highlight risks the bull is ignoring. Identify potential catalysts for downside.

### Research Manager (Judge)

**Role:** Evaluate both sides and produce a balanced investment plan.

**Inputs:** Complete debate transcript and all analyst reports.

**System Prompt Core:**

> You are the Research Manager. You have heard the bull and bear debate. Review:
>
> 1. The strength of each argument
> 2. Which claims are supported by data vs. speculation
> 3. The balance of risks and opportunities
>
> Produce an INVESTMENT PLAN that includes:
>
> - Overall assessment (bullish, bearish, or neutral)
> - Confidence level (1-10)
> - Key thesis points that survived the debate
> - Primary risks that must be managed
> - Recommended position direction and conviction

## Debate Flow

```
Round 1:
  Bull: "AAPL showing breakout pattern above 200-day SMA with RSI at 62..."
  Bear: "Revenue growth decelerating; iPhone cycle peaking; China risk..."

Round 2:
  Bull: "Services revenue +18% offsets hardware cycle; AI integration catalyst..."
  Bear: "P/E at 32x vs. 5-year avg of 28x; margin compression in Services..."

Round 3:
  Bull: "Cash flow generation supports buyback at any valuation; regulatory moat..."
  Bear: "EU DMA compliance costs unknown; antitrust risk to App Store revenue..."

Research Manager:
  "Assessment: Moderately Bullish (7/10 confidence)
   Thesis: Strong services growth + AI integration outweigh cyclical headwinds
   Key risk: Valuation stretched — entry should be limit order below $180
   Recommendation: Buy with tight risk management"
```

## Implementation

```go
// internal/agent/research/bull.go
type BullResearcher struct {
    agent.BaseAgent
}

func (b *BullResearcher) Execute(ctx context.Context, state *agent.PipelineState) error {
    req := b.buildPrompt(bullSystemPrompt, state)

    // Add analyst reports as context
    userMsg := fmt.Sprintf("Analyst Reports:\n\nMarket: %s\n\nFundamentals: %s\n\nNews: %s\n\nSocial: %s",
        state.MarketReport, state.FundamentalsReport, state.NewsReport, state.SocialReport)

    // Add previous debate messages if not round 1
    if len(state.InvestDebate.Messages) > 0 {
        userMsg += "\n\nPrevious Debate:\n"
        for _, msg := range state.InvestDebate.Messages {
            userMsg += fmt.Sprintf("[%s]: %s\n\n", msg.Role, msg.Content)
        }
    }

    req.Messages = append(req.Messages, llm.Message{Role: "user", Content: userMsg})
    resp, err := b.llm.Complete(ctx, req)
    if err != nil {
        return fmt.Errorf("bull researcher: %w", err)
    }

    state.InvestDebate.Messages = append(state.InvestDebate.Messages, agent.DebateMessage{
        Role:    "bull",
        Content: resp.Content,
        Round:   state.InvestDebate.RoundCount,
    })
    return nil
}
```

## Configurable Parameters

| Parameter            | Default | Description                                                    |
| -------------------- | ------- | -------------------------------------------------------------- |
| `max_debate_rounds`  | 3       | Number of bull-bear exchange rounds                            |
| `debate_temperature` | 0.5     | LLM temperature for debate (higher = more creative arguments)  |
| `judge_temperature`  | 0.2     | LLM temperature for manager (lower = more consistent judgment) |

## State Management

```go
type InvestDebateState struct {
    Messages      []DebateMessage // chronological debate transcript
    JudgeResponse string          // Research Manager's investment plan
    RoundCount    int             // current round number
}
```

The debate state is persisted to `agent_decisions` after each message, enabling:

- Real-time debate visualization in the [[agent-visualization]] component
- Post-hoc analysis of debate quality
- Memory generation from debate outcomes

---

**Related:** [[agent-system-overview]] · [[analyst-agents]] · [[trader-agent]] · [[memory-and-learning]]
