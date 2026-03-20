---
title: State Management
description: AgentState, InvestDebateState, RiskDebateState TypedDicts and data flow
type: architecture
source_file: TradingAgents/tradingagents/agents/utils/agent_states.py
created: 2026-03-20
---

# State Management

All agents communicate through a shared state object that flows through the [[langgraph-orchestration|LangGraph workflow]]. State is defined using LangChain's `TypedDict` pattern.

## AgentState (Main State)

Extends `MessagesState` (which provides a `messages` list):

```python
class AgentState(MessagesState):
    company_name: str           # Ticker symbol (e.g. "AAPL")
    trade_date: str             # Analysis date (YYYY-MM-DD)
    market_report: str          # Market analyst output
    sentiment_report: str       # Social media analyst output
    news_report: str            # News analyst output
    fundamentals_report: str    # Fundamentals analyst output
    investment_plan: str        # Research manager's plan
    trading_plan: str           # Trader's plan
    risk_report: str            # Risk manager's final decision
    invest_debate: InvestDebateState
    risk_debate: RiskDebateState
```

Each analyst writes to its specific report field. Downstream agents read from multiple fields.

## InvestDebateState

Tracks the [[research-team|bull/bear research debate]]:

```python
class InvestDebateState(TypedDict):
    invest_debate_messages: list      # Full debate conversation
    invest_judge_response: str        # Research manager's judgment
    invest_debate_count: int          # Current round number
```

## RiskDebateState

Tracks the [[risk-management-team|risk management debate]]:

```python
class RiskDebateState(TypedDict):
    risk_debate_messages: list        # Full risk debate conversation
    risk_judge_response: str          # Risk manager's final decision
    risk_debate_count: int            # Current round number
```

## State Flow

```
propagation.py initializes state
       ↓
Analysts write to *_report fields
       ↓
Researchers read reports, write to invest_debate
       ↓
Research Manager writes invest_judge_response
       ↓
Trader reads investment_plan, writes trading_plan
       ↓
Risk debators read trading_plan, write to risk_debate
       ↓
Risk Manager writes risk_judge_response (final decision)
       ↓
signal_processing.py extracts BUY/SELL/HOLD
```

## State Initialization

`propagation.py` (70 lines) creates the initial state with:

- Company name and trade date
- Empty report fields
- Initialized debate substates with count = 0

## Related

- [[langgraph-orchestration]] - How state flows through the graph
- [[conditional-routing]] - How debate counts control routing
- [[signal-processing]] - How the final state is interpreted
