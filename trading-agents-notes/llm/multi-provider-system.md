---
title: Multi-Provider LLM System
description: Factory pattern, base client abstraction, and provider-specific implementations
type: architecture
source_files:
  - TradingAgents/tradingagents/llm_clients/factory.py
  - TradingAgents/tradingagents/llm_clients/base_client.py
  - TradingAgents/tradingagents/llm_clients/openai_client.py
  - TradingAgents/tradingagents/llm_clients/anthropic_client.py
  - TradingAgents/tradingagents/llm_clients/google_client.py
created: 2026-03-20
---

# Multi-Provider LLM System

TradingAgents supports 6 LLM providers through a factory pattern that abstracts away provider differences.

## Architecture

### Factory (`factory.py`)

```python
def create_llm_client(provider: str, model: str, **kwargs) -> BaseLLMClient
```

Routes to the correct client based on the `llm_provider` string in [[configuration]]:

| Provider string | Client class      | API used                                |
| --------------- | ----------------- | --------------------------------------- |
| `"openai"`      | `OpenAIClient`    | OpenAI API                              |
| `"google"`      | `GoogleClient`    | Google Generative AI                    |
| `"anthropic"`   | `AnthropicClient` | Anthropic API                           |
| `"xai"`         | `OpenAIClient`    | OpenAI-compatible (xAI endpoint)        |
| `"openrouter"`  | `OpenAIClient`    | OpenAI-compatible (OpenRouter endpoint) |
| `"ollama"`      | `OpenAIClient`    | OpenAI-compatible (local Ollama)        |

### Base Client (`base_client.py`)

Abstract base class defining the interface all providers must implement. Ensures consistent behavior across providers.

### Provider Clients

**OpenAI Client** (`openai_client.py`):

- Handles OpenAI, xAI, OpenRouter, and Ollama (all OpenAI-compatible APIs)
- Special handling for GPT-5 family: strips `temperature` and `top_p` parameters (model-specific quirk)
- Configures base URL per provider

**Anthropic Client** (`anthropic_client.py`):

- Uses `langchain_anthropic` for Claude model integration
- Handles Anthropic-specific API parameters

**Google Client** (`google_client.py`):

- Uses `langchain_google_genai` for Gemini models
- Handles Google-specific API configuration

### Validators (`validators.py`)

Model name validation to catch configuration errors early.

## Two-Tier Usage

The framework creates **two** LLM clients (see [[configuration]]):

1. **Deep think**: Complex reasoning tasks (debate judgments, final decisions)
2. **Quick think**: Faster, simpler tasks (initial analysis, tool interpretation)

Both are created via the same factory with different model names.

## Related

- [[supported-models]] - Full model list per provider
- [[configuration]] - How to set provider and model
- [[langgraph-orchestration]] - Where LLM clients are used
