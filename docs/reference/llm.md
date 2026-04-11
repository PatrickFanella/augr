---
title: "LLM Providers"
description: "LLM provider support, model routing, defaults, and runtime behavior."
status: "canonical"
updated: "2026-04-03"
tags: [llm, models, reference]
---

# LLM Providers

The application supports multiple LLM providers, both in global environment configuration and in per-strategy overrides.

## Supported providers

- `openai`
- `anthropic`
- `google`
- `openrouter`
- `xai`
- `ollama`

## Global defaults

The config layer exposes:

- `LLM_DEFAULT_PROVIDER`
- `LLM_DEEP_THINK_MODEL`
- `LLM_QUICK_THINK_MODEL`
- `LLM_TIMEOUT`

Current defaults:

| Setting | Default |
| --- | --- |
| default provider | `openai` |
| deep think model | `gpt-5.2` |
| quick think model | `gpt-5-mini` |
| timeout | `30s` |

## Provider-specific configuration

### OpenAI

- `OPENAI_API_KEY`
- `OPENAI_BASE_URL`
- `OPENAI_MODEL`

### Anthropic

- `ANTHROPIC_API_KEY`
- `ANTHROPIC_MODEL`

### Google

- `GOOGLE_API_KEY`
- `GOOGLE_MODEL`

### OpenRouter

- `OPENROUTER_API_KEY`
- `OPENROUTER_BASE_URL`
- `OPENROUTER_MODEL`

### xAI

- `XAI_API_KEY`
- `XAI_BASE_URL`
- `XAI_MODEL`

### Ollama

- `OLLAMA_BASE_URL`
- `OLLAMA_MODEL`

## Runtime selection rules

Provider selection can come from:

1. strategy config
2. global settings
3. config defaults

The production runner builds the concrete provider at runtime based on the resolved selection.

## Important implementation details

- `openrouter` and `xai` are routed through OpenAI-compatible transport wiring in the production runner.
- `ollama` is the local/self-hosted option.
- per-strategy model overrides are validated by `ValidateStrategyConfig`.

## Model validation

Known model allowlist entries currently include:

- `gpt-5-mini`
- `gpt-5.2`
- `gpt-5.4`
- `gpt-4.1-mini`
- `openai/gpt-4.1-mini`
- `claude-3-7-sonnet-latest`
- `gemini-2.5-flash`
- `grok-3-mini`
- `llama3.2`

Some providers enforce explicit provider/model pairings more strictly than others.

## Practical guidance

### Use hosted models when you want

- simpler bring-up
- higher-quality reasoning
- less local infra work

### Use Ollama when you want

- local experimentation
- no hosted API dependency
- offline-ish development

Trade-off:

- local models are often slower or lower quality for the deeper debate/trader phases unless carefully chosen

## UI/settings caveat

The settings page lets you edit provider settings. In the API server those edits persist via the `app_settings` table when the settings persister is configured; local/dev workflows without a writable database may still behave ephemerally.
