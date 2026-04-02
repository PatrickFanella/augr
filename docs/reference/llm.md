# LLM Configuration Reference

This document describes the LLM abstractions in `internal/llm/*`, the environment-backed application config in `internal/config/config.go`, and strategy/global overrides in `internal/agent/strategy_config.go` plus `internal/agent/resolve_config.go`.

## Core abstraction

`internal/llm/provider.go` defines one interface:

```go
type Provider interface {
    Complete(ctx context.Context, request CompletionRequest) (*CompletionResponse, error)
}
```

Shared request/response types live in `internal/llm/types.go`.

Important types:

- `ModelTierDeepThink` = `deep_think`
- `ModelTierQuickThink` = `quick_think`
- `CompletionRequest` supports `Model`, `Messages`, `Temperature`, `MaxTokens`, and optional structured `ResponseFormat`
- `CompletionResponse` carries `Content`, `Usage`, `Model`, `LatencyMS`, and `CostUSD`

## Registry package

`internal/llm/registry.go` provides a generic provider registry keyed by normalized provider name.

A registration contains:

- the `Provider`
- a `map[ModelTier]string`

Supported registry operations:

- `Register(name, provider, models)`
- `Get(name)`
- `Resolve(name, tier)`

`Resolve` returns:

- `ErrProviderNotFound` when no provider is registered
- `ErrModelTierNotConfigured` when the provider exists but has no model for the requested tier

Current code status:

- the registry abstraction is implemented and tested
- current runtime wiring in `cmd/tradingagent/runtime.go` does not build or use `llm.Registry`; it switches directly on `LLM_DEFAULT_PROVIDER`

## Provider implementations present in code

### Available provider packages

| Provider | Package | Constructor requirements | Base URL override |
| --- | --- | --- | --- |
| OpenAI | `internal/llm/openai` | API key required | Yes |
| Anthropic | `internal/llm/anthropic` | API key required | Yes |
| Google | `internal/llm/google` | API key required | Yes |
| Ollama | `internal/llm/ollama` | no API key required | Yes |

### Config-only providers

`internal/config/config.go`, `internal/config/validate.go`, `internal/api/settings.go`, and strategy validation all know about two additional provider names:

- `openrouter`
- `xai`

Current code status:

- they are valid config/provider names
- they have environment variables and settings API fields
- there is no `internal/llm/openrouter` package
- there is no `internal/llm/xai` package
- `cmd/tradingagent/runtime.go` does not construct them in `newLLMProviderFromConfig`

That means config can name `openrouter` or `xai`, but the shipped runtime provider factory only instantiates `openai`, `anthropic`, `google`, or `ollama`.

## Model tiers

### Tier meaning

`internal/llm/types.go` defines two tiers:

- `deep_think` — higher-quality, slower, usually more expensive tasks
- `quick_think` — faster, cheaper tasks

### Package-level default models by tier

Provider packages expose `DefaultModelsByTier()` helpers for registry-style use.

| Provider package | Deep think | Quick think |
| --- | --- | --- |
| `internal/llm/openai` | `gpt-5.2` | `gpt-5-mini` |
| `internal/llm/anthropic` | `claude-sonnet-4-6` | `claude-haiku-4-5` |
| `internal/llm/google` | `gemini-3.1-pro` | `gemini-3.1-flash` |
| `internal/llm/ollama` | `llama3.2` | `llama3.2` |

These helpers are package defaults, not application startup defaults.

## Application config

`internal/config/config.go` loads the environment into `config.LLMConfig`.

### Top-level LLM settings

| Environment variable | Default | Field |
| --- | --- | --- |
| `LLM_DEFAULT_PROVIDER` | `openai` | `LLM.DefaultProvider` |
| `LLM_DEEP_THINK_MODEL` | `gpt-5.2` | `LLM.DeepThinkModel` |
| `LLM_QUICK_THINK_MODEL` | `gpt-5-mini` | `LLM.QuickThinkModel` |
| `LLM_TIMEOUT` | `30s` | `LLM.Timeout` |

### Provider-specific settings

| Provider | Env vars | Defaults from config loader |
| --- | --- | --- |
| OpenAI | `OPENAI_API_KEY`, `OPENAI_BASE_URL`, `OPENAI_MODEL` | model `gpt-5-mini` |
| Anthropic | `ANTHROPIC_API_KEY`, `ANTHROPIC_MODEL` | model `claude-3-7-sonnet-latest` |
| Google | `GOOGLE_API_KEY`, `GOOGLE_MODEL` | model `gemini-2.5-flash` |
| OpenRouter | `OPENROUTER_API_KEY`, `OPENROUTER_BASE_URL`, `OPENROUTER_MODEL` | model `openai/gpt-4.1-mini` |
| xAI | `XAI_API_KEY`, `XAI_BASE_URL`, `XAI_MODEL` | model `grok-3-mini` |
| Ollama | `OLLAMA_BASE_URL`, `OLLAMA_MODEL` | base URL `http://localhost:11434`, model `llama3.2` |

Note: `config.go` stores `OLLAMA_BASE_URL` without appending `/v1`. The Ollama provider package itself defaults to `http://localhost:11434/v1` only when its own config `BaseURL` is empty.

## Config validation

`internal/config/validate.go` enforces:

- `LLM_TIMEOUT > 0`
- at least one LLM provider is configured
- `LLM_DEFAULT_PROVIDER` must have matching credentials configured
- `OLLAMA_BASE_URL` counts as configured even without an API key
- `LLM_DEEP_THINK_MODEL` and `LLM_QUICK_THINK_MODEL` cannot be whitespace-only when set

Selected-provider checks:

- `openai` requires `OPENAI_API_KEY`
- `anthropic` requires `ANTHROPIC_API_KEY`
- `google` requires `GOOGLE_API_KEY`
- `openrouter` requires `OPENROUTER_API_KEY`
- `xai` requires `XAI_API_KEY`
- `ollama` requires no API key

## Runtime factory behavior

`cmd/tradingagent/runtime.go:newLLMProviderFromConfig` is the current application-side provider factory.

Behavior:

- reads `LLM_DEFAULT_PROVIDER`
- prefers global `LLM_QUICK_THINK_MODEL` over the provider-specific model field
- returns `nil` and logs a warning for unsupported provider names or missing credentials

Current supported provider names in this factory:

- `openai`
- `anthropic`
- `google`
- `ollama`

Current unsupported names in this factory:

- `openrouter`
- `xai`

This factory is currently used to seed the API conversations/settings surface, not the reusable agent node packages directly.

## Strategy and global overrides

### Strategy JSON schema

`internal/agent/strategy_config.go` defines:

```json
{
  "llm_config": {
    "provider": "openai|anthropic|google|openrouter|xai|ollama",
    "deep_think_model": "...",
    "quick_think_model": "..."
  }
}
```

Other strategy config sections exist too, but only the `llm_config` section is relevant here.

### Validation rules

`ValidateStrategyConfig` enforces:

- provider must be one of `openai`, `anthropic`, `google`, `openrouter`, `xai`, `ollama`
- model strings must exist in the allowlist of known model names
- if a provider is set, provider/model compatibility is checked for providers with explicit allowlists

Provider/model constraints:

- `openai` constrained to known OpenAI models
- `anthropic` constrained to known Anthropic models
- `google` constrained to known Google models
- `openrouter`, `xai`, and `ollama` accept any known model name because `providerModelAllowlist` does not constrain them further

Current known model allowlist includes:

- OpenAI: `gpt-5-mini`, `gpt-5.2`, `gpt-5.4`, `gpt-4.1-mini`, `openai/gpt-4.1-mini`
- Anthropic: `claude-3-7-sonnet-latest`
- Google: `gemini-2.5-flash`
- xAI: `grok-3-mini`
- Ollama: `llama3.2`

## Resolution order

`internal/agent/resolve_config.go` merges values in this order:

1. strategy override
2. global setting
3. hardcoded default

Resolved LLM defaults are:

- provider: `openai`
- deep think model: `gpt-5.2`
- quick think model: `gpt-5-mini`

The resolver also copies analyst-selection and prompt-override slices/maps so callers cannot mutate the original inputs.

## Current limitation: resolved LLM settings are audited, not rebound

`Pipeline.ExecuteStrategy` resolves the strategy/global config and stores the resolved JSON in `PipelineRun.ConfigSnapshot`.

What it applies to the live `Pipeline` today:

- `ResearchDebateRounds`
- `RiskDebateRounds`
- `PhaseTimeout` from `AnalysisTimeoutSeconds`

What it does not apply to the live `Pipeline` today:

- provider rebinding
- deep-think model rebinding
- quick-think model rebinding
- debate-timeout rebinding

So strategy/global `llm_config` values are currently:

- resolved
- validated
- stored in the run config snapshot
- not used to reconstruct already-registered node providers/models

## Wrappers and failure behavior

### Retry

`internal/llm/retry.go` provides `RetryProvider`.

Behavior:

- exponential backoff
- retries on `context.DeadlineExceeded`, HTTP `429`, and HTTP `5xx`
- does not retry `context.Canceled`, auth failures, or other `4xx`
- aggregates token usage across attempts when partial responses include usage

### Fallback

`internal/llm/fallback.go` provides `FallbackProvider`.

Behavior:

- tries primary provider first
- falls back to secondary only on non-context errors
- does not fall back on `context.Canceled` or `context.DeadlineExceeded`
- returns the secondary error if both fail

This is conditional fallback, not a general provider pool.

### Cache

`internal/llm/cache.go` provides `CacheProvider` plus in-memory cache/stat collectors.

Behavior:

- cache key is derived from the full `CompletionRequest` plus cache version
- responses are cloned on read/write
- cache stats are tracked per run through a context-attached collector
- `Pipeline.Execute` publishes `llm_cache_stats_reported` and stores the same stats on `PipelineState.LLMCacheStats`

## Settings API surface

`internal/api/settings.go` exposes editable in-memory LLM settings with fields for:

- default provider
- deep-think model
- quick-think model
- OpenAI / Anthropic / Google / OpenRouter / xAI / Ollama provider settings

Important limitation:

- the settings service edits an in-memory snapshot only
- it does not rebuild live `llm.Provider` instances or hot-swap agent nodes
- it validates model presence, but not provider implementation availability

So the settings API can accept `openrouter` and `xai` values even though `newLLMProviderFromConfig` cannot currently instantiate those providers.
