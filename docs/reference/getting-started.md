---
title: Getting Started
description: Installation, setup, and basic usage of TradingAgents
type: guide
source_files:
  - TradingAgents/main.py
  - TradingAgents/pyproject.toml
  - TradingAgents/README.md
created: 2026-03-20
---

# Getting Started

## Installation

```bash
# Clone and install
cd TradingAgents
pip install -e .

# Or install dependencies directly
pip install -r requirements.txt
```

## Environment Setup

Create a `.env` file with at least one LLM provider key:

```
OPENAI_API_KEY=sk-...
# Optional alternatives:
GOOGLE_API_KEY=...
ANTHROPIC_API_KEY=...
```

## Basic Python Usage

```python
from tradingagents.graph.trading_graph import TradingAgentsGraph

# Initialize with defaults (OpenAI, all analysts)
ta = TradingAgentsGraph()

# Or customize
ta = TradingAgentsGraph(
    selected_analysts=["market", "news", "fundamentals"],
    config={
        "llm_provider": "anthropic",
        "deep_think_llm": "claude-opus-4-6",
        "quick_think_llm": "claude-haiku-4-5-20251001",
        "max_debate_rounds": 2,
    },
    debug=True,
)

# Run analysis for a ticker on a date
state, signal = ta.propagate("AAPL", "2025-01-06")

# signal is "buy", "sell", or "hold"
print(f"Decision: {signal}")
```

## CLI Usage

```bash
# Interactive mode
tradingagents

# The CLI prompts for:
# - Ticker symbol
# - Date (YYYY-MM-DD)
# - LLM provider and models
# - Which analysts to enable
```

See [[cli-interface]] for details.

## Learning from Outcomes

After observing the trade result:

```python
# Tell the system how the trade performed
ta.reflect_and_remember(position_returns=0.05)  # 5% gain
```

This updates the [[bm25-memory-system]] so agents improve over time.

## Key Classes

| Class                | Location                               | Purpose               |
| -------------------- | -------------------------------------- | --------------------- |
| `TradingAgentsGraph` | `tradingagents/graph/trading_graph.py` | Main orchestrator     |
| `create_llm_client`  | `tradingagents/llm_clients/factory.py` | LLM provider factory  |
| `Memory`             | `tradingagents/agents/utils/memory.py` | BM25 retrieval memory |

## Related

- [[configuration]] - All configurable options
- [[architecture]] - How the system works end-to-end
- [[overview]] - Design philosophy
