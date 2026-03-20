---
title: Configuration
description: All configurable options, defaults, and environment variables for TradingAgents
type: reference
source_file: TradingAgents/tradingagents/default_config.py
created: 2026-03-20
---

# Configuration

## Default Config

Defined in `tradingagents/default_config.py`, passed as a dict to `TradingAgentsGraph(config=...)`:

```python
config = {
    # LLM settings
    "llm_provider": "openai",         # openai | google | anthropic | xai | ollama | openrouter
    "deep_think_llm": "gpt-5.2",     # Complex reasoning tasks
    "quick_think_llm": "gpt-5-mini", # Faster, simpler tasks

    # Debate control
    "max_debate_rounds": 1,           # Bull/bear debate iterations
    "max_risk_discuss_rounds": 1,     # Risk debate iterations

    # Data vendors (per-category overrides)
    "data_vendors": {
        "core_stock_apis": "yfinance",       # OHLCV price data
        "technical_indicators": "yfinance",  # Technical analysis
        "fundamental_data": "yfinance",      # Financial statements
        "news_data": "yfinance",             # News and sentiment
    },
}
```

## LLM Provider Options

See [[supported-models]] for full model list per provider.

| Provider   | Config value   | Requires             |
| ---------- | -------------- | -------------------- |
| OpenAI     | `"openai"`     | `OPENAI_API_KEY`     |
| Google     | `"google"`     | `GOOGLE_API_KEY`     |
| Anthropic  | `"anthropic"`  | `ANTHROPIC_API_KEY`  |
| xAI        | `"xai"`        | `XAI_API_KEY`        |
| OpenRouter | `"openrouter"` | `OPENROUTER_API_KEY` |
| Ollama     | `"ollama"`     | Local Ollama server  |

## Two-Tier LLM Strategy

The framework uses two model tiers:

- **`deep_think_llm`**: Used for complex reasoning (research debate, risk judgment, final decisions)
- **`quick_think_llm`**: Used for faster tasks (initial analysis, tool calls)

This allows cost optimization by routing simpler work to cheaper/faster models.

## Analyst Selection

Pass to constructor:

```python
TradingAgentsGraph(
    selected_analysts=["market", "social", "news", "fundamentals"],
    # Any subset of the four
)
```

See [[analyst-team]] for what each provides.

## Environment Variables

From `.env.example`:

```
OPENAI_API_KEY=
GOOGLE_API_KEY=
ANTHROPIC_API_KEY=
XAI_API_KEY=
OPENROUTER_API_KEY=
```

## Related

- [[getting-started]] - How to use these options
- [[multi-provider-system]] - How providers are loaded
- [[vendor-system]] - Data vendor configuration details
