---
title: Supported Models
description: All LLM models and providers supported by TradingAgents with provider-specific notes
type: reference
source_files:
  - TradingAgents/tradingagents/llm_clients/validators.py
  - TradingAgents/tradingagents/default_config.py
  - TradingAgents/README.md
created: 2026-03-20
---

# Supported Models

## By Provider

### OpenAI (`"openai"`)

| Model        | Tier        | Notes                     |
| ------------ | ----------- | ------------------------- |
| `gpt-5.4`    | Deep think  | Latest, most capable      |
| `gpt-5.2`    | Deep think  | Default `deep_think_llm`  |
| `gpt-5-mini` | Quick think | Default `quick_think_llm` |

**Quirk**: GPT-5 family models have `temperature` and `top_p` parameters stripped by the client -- these models handle sampling differently.

### Google (`"google"`)

| Model        | Tier       |
| ------------ | ---------- |
| `gemini-3.1` | Deep think |

### Anthropic (`"anthropic"`)

| Model                       | Tier             |
| --------------------------- | ---------------- |
| `claude-opus-4-6`           | Deep think       |
| `claude-sonnet-4-6`         | Deep/quick think |
| `claude-haiku-4-5-20251001` | Quick think      |

### xAI (`"xai"`)

Uses OpenAI-compatible API with xAI base URL:

| Model       | Tier    |
| ----------- | ------- |
| Grok models | Various |

### OpenRouter (`"openrouter"`)

Access to many models through a unified API. Any model available on OpenRouter can be specified by its OpenRouter model ID.

### Ollama (`"ollama"`)

Local models. Any model available in your local Ollama installation:

| Model            | Notes                     |
| ---------------- | ------------------------- |
| `llama3`         | Open-source, runs locally |
| `mistral`        | Open-source, runs locally |
| Any Ollama model | No API key needed         |

## Choosing Models

The [[configuration|two-tier strategy]] allows mixing cost and capability:

```python
# Cost-optimized setup
config = {
    "llm_provider": "openai",
    "deep_think_llm": "gpt-5.2",      # Complex reasoning
    "quick_think_llm": "gpt-5-mini",   # Simpler tasks
}

# Anthropic setup
config = {
    "llm_provider": "anthropic",
    "deep_think_llm": "claude-opus-4-6",
    "quick_think_llm": "claude-haiku-4-5-20251001",
}

# Local/free setup
config = {
    "llm_provider": "ollama",
    "deep_think_llm": "llama3",
    "quick_think_llm": "llama3",
}
```

## Environment Variables

| Provider   | Variable             |
| ---------- | -------------------- |
| OpenAI     | `OPENAI_API_KEY`     |
| Google     | `GOOGLE_API_KEY`     |
| Anthropic  | `ANTHROPIC_API_KEY`  |
| xAI        | `XAI_API_KEY`        |
| OpenRouter | `OPENROUTER_API_KEY` |
| Ollama     | None (local)         |

## Related

- [[multi-provider-system]] - How providers are loaded
- [[configuration]] - Setting provider and model names
