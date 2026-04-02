# Strategy Config Reference

This page documents the JSON stored in `domain.Strategy.Config` (`strategy.config` in API payloads). The schema is defined by `internal/agent/strategy_config.go`, validated by `agent.ValidateStrategyConfig`, resolved by `agent.ResolveConfig`, and consumed at run start by `(*Pipeline).ExecuteStrategy`.

## Where this config is used

- `POST`/`PUT` strategy requests validate `config` by unmarshalling it into `agent.StrategyConfig` and calling `agent.ValidateStrategyConfig`.
- `ExecuteStrategy` resolves each field in this order: strategy value -> global setting -> hardcoded default.
- The fully resolved config is stored in `PipelineRun.ConfigSnapshot` for auditability.

## Resolution rules

| Case | Behavior |
| --- | --- |
| Missing section (`llm_config`, `pipeline_config`, `risk_config`) | Falls through to global settings, then hardcoded defaults |
| Missing field inside a present section | Falls through per field, not per section |
| `analyst_selection: null` or field omitted | Means all analysts enabled |
| `prompt_overrides: null` or field omitted | Means no overrides |
| Strategy and global both set a field | Strategy wins |

## Current runtime behavior

`ExecuteStrategy` currently applies only two resolved fields to the live pipeline:

- `pipeline_config.debate_rounds` -> sets both research and risk debate round counts.
- `pipeline_config.analysis_timeout_seconds` -> sets `Pipeline.PhaseTimeout`, which is then used by the analysis phase, both debate phases, and the trading phase.

The following fields are accepted, validated, resolved, and included in `ConfigSnapshot`, but are not currently applied by `ExecuteStrategy`:

- `llm_config.provider`
- `llm_config.deep_think_model`
- `llm_config.quick_think_model`
- `pipeline_config.debate_timeout_seconds`
- `risk_config.*`
- `analyst_selection`
- `prompt_overrides`

## Annotated example

The example below is JSONC for readability. The stored payload must be plain JSON.

```jsonc
{
  "llm_config": {
    "provider": "anthropic",                  // validated + resolved; not currently applied at run time
    "deep_think_model": "claude-3-7-sonnet-latest", // validated + resolved; not currently applied at run time
    "quick_think_model": "gpt-5-mini"        // validated + resolved; not currently applied at run time
  },
  "pipeline_config": {
    "debate_rounds": 4,                        // applied to both research and risk debates
    "analysis_timeout_seconds": 45,            // currently becomes the shared per-phase timeout
    "debate_timeout_seconds": 90               // validated + resolved; not currently applied at run time
  },
  "risk_config": {
    "position_size_pct": 2.5,                  // validated + resolved; not currently applied at run time
    "stop_loss_multiplier": 1.25,              // validated + resolved; not currently applied at run time
    "take_profit_multiplier": 2.5,             // validated + resolved; not currently applied at run time
    "min_confidence": 0.7                      // validated + resolved; not currently applied at run time
  },
  "analyst_selection": [
    "market_analyst",
    "news_analyst"
  ],                                            // validated + resolved; nil means all analysts enabled; not currently applied at run time
  "prompt_overrides": {
    "trader": "You are a conservative trader."
  }                                             // validated + resolved; nil means no overrides; not currently applied at run time
}
```

## LLM config

### Allowed providers

`provider` is trimmed and lowercased before validation. Allowed values:

- `openai`
- `anthropic`
- `google`
- `openrouter`
- `xai`
- `ollama`

### Allowed models

Both model fields are trimmed before validation. Accepted model IDs:

- `gpt-5-mini`
- `gpt-5.2`
- `gpt-5.4`
- `gpt-4.1-mini`
- `openai/gpt-4.1-mini`
- `claude-3-7-sonnet-latest`
- `gemini-2.5-flash`
- `grok-3-mini`
- `llama3.2`

Provider/model compatibility checks are stricter for `openai`, `anthropic`, and `google`:

- `openai` accepts only OpenAI-listed models.
- `anthropic` accepts only `claude-3-7-sonnet-latest`.
- `google` accepts only `gemini-2.5-flash`.
- `openrouter`, `xai`, and `ollama` skip the provider-specific compatibility check, but the model must still be one of the globally accepted model IDs above.

| Field | Type | Default after resolution | Valid values | Current effect |
| --- | --- | --- | --- | --- |
| `llm_config.provider` | string | `openai` | provider list above | Resolved into `ConfigSnapshot`; not applied to node construction in `ExecuteStrategy` |
| `llm_config.deep_think_model` | string | `gpt-5.2` | accepted model IDs above; must match provider rules when applicable | Resolved into `ConfigSnapshot`; not applied to node construction in `ExecuteStrategy` |
| `llm_config.quick_think_model` | string | `gpt-5-mini` | accepted model IDs above; must match provider rules when applicable | Resolved into `ConfigSnapshot`; not applied to node construction in `ExecuteStrategy` |

## Pipeline config

| Field | Type | Default after resolution | Valid range | Current effect |
| --- | --- | --- | --- | --- |
| `pipeline_config.debate_rounds` | integer | `3` | `>= 1` when validated through API/`ValidateStrategyConfig` | Applied to both `ResearchDebateRounds` and `RiskDebateRounds` |
| `pipeline_config.analysis_timeout_seconds` | integer | `30` | `>= 1` | Applied as `Pipeline.PhaseTimeout`; currently governs analysis, research debate, trading, and risk debate phase deadlines |
| `pipeline_config.debate_timeout_seconds` | integer | `60` | `>= 1` | Resolved into `ConfigSnapshot`; no current runtime wiring in `ExecuteStrategy` |

## Risk config

| Field | Type | Default after resolution | Valid range | Current effect |
| --- | --- | --- | --- | --- |
| `risk_config.position_size_pct` | number | `5.0` | `0` to `100` inclusive | Resolved into `ConfigSnapshot`; not currently consumed by pipeline execution |
| `risk_config.stop_loss_multiplier` | number | `1.5` | `> 0` | Resolved into `ConfigSnapshot`; not currently consumed by pipeline execution |
| `risk_config.take_profit_multiplier` | number | `2.0` | `> 0` | Resolved into `ConfigSnapshot`; not currently consumed by pipeline execution |
| `risk_config.min_confidence` | number | `0.65` | `0` to `1` inclusive | Resolved into `ConfigSnapshot`; not currently consumed by pipeline execution |

## Analyst selection

`analyst_selection` is validated as a list of `AgentRole` strings. Accepted values are the currently defined role constants:

- `market_analyst`
- `fundamentals_analyst`
- `news_analyst`
- `social_media_analyst`
- `bull_researcher`
- `bear_researcher`
- `trader`
- `invest_judge`
- `risk_manager`
- `aggressive_analyst`
- `conservative_analyst`
- `neutral_analyst`
- `aggressive_risk`
- `conservative_risk`
- `neutral_risk`

| Field | Type | Default after resolution | Valid values | Current effect |
| --- | --- | --- | --- | --- |
| `analyst_selection` | array of strings | `null` (`all analysts enabled`) | Agent role list above | Copied into resolved config and `ConfigSnapshot`; not currently used to register/filter pipeline nodes |

## Prompt overrides

`prompt_overrides` maps an `AgentRole` string to replacement prompt text. Keys must be valid agent roles from the same list shown above.

| Field | Type | Default after resolution | Valid values | Current effect |
| --- | --- | --- | --- | --- |
| `prompt_overrides` | object mapping role -> string | `null` (`no overrides`) | Keys must be valid agent role strings | Copied into resolved config and `ConfigSnapshot`; current node constructors still use built-in prompt constants |

## Notes

- Empty config (`{}`) is valid.
- A partially filled section is valid; omitted fields still inherit from globals/defaults.
- `nil` strategy config and `{}` resolve the same way.
- The required pipeline roles today are fixed by `PipelineBuilder`: both research debaters, the investment judge, the trader, all three risk debaters, the risk manager, and at least one analysis node.
