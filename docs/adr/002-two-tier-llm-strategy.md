# ADR-002: Two-tier LLM model strategy (DeepThink vs QuickThink)

- **Status:** accepted
- **Date:** 2026-03-21
- **Deciders:** Engineering
- **Technical Story:** Issue: "ADR-002: Two-tier LLM model strategy (DeepThink vs QuickThink)"

## Context

Each pipeline run invokes an LLM for every agent decision. Using a single high-capability model for all
calls maximises quality but produces unnecessarily high cost and latency for simple extraction tasks.
Using a single low-cost model reduces spend but degrades the nuanced reasoning required in debate and
judgment phases.

The pipeline currently defines six agent roles across four phases:

| Phase            | Agent roles invoked                                    |
|------------------|--------------------------------------------------------|
| Analysis         | MarketAnalyst                                          |
| ResearchDebate   | BullResearcher, BearResearcher (N rounds), InvestJudge |
| Trading          | Trader                                                 |
| RiskDebate       | BullResearcher, BearResearcher (M rounds), RiskManager |

With the default configuration of two research-debate rounds and two risk-debate rounds, a single
pipeline run produces **10 LLM invocations**:

1. MarketAnalyst (analysis phase)
2. BullResearcher — research debate round 1
3. BearResearcher — research debate round 1
4. BullResearcher — research debate round 2
5. BearResearcher — research debate round 2
6. InvestJudge (research debate conclusion)
7. Trader (trading phase)
8. BullResearcher — risk debate round 1
9. BearResearcher — risk debate round 1
10. RiskManager (risk debate conclusion)

The cost and latency profile of each invocation differs significantly by task type:

- **Complex reasoning tasks** — debate argumentation, investment synthesis, trading-plan generation, and
  risk judgment — require a model with strong multi-step reasoning capability.
- **Extraction and summarisation tasks** — parsing market data and producing structured summaries — are
  simpler and do not benefit from a larger model.

## Decision

We will use a **two-tier model strategy** codified as the `ModelTier` type in `internal/llm/types.go`:

- **DeepThink** (`deep_think`) — higher-quality, higher-cost models for complex reasoning.
- **QuickThink** (`quick_think`) — faster, lower-cost models for extraction and summarisation.

### Default tier assignments

| # | Agent role       | Phase            | Tier       | Rationale                                              |
|---|------------------|------------------|------------|--------------------------------------------------------|
| 1 | MarketAnalyst    | Analysis         | QuickThink | Extracts and summarises structured market data         |
| 2 | BullResearcher   | ResearchDebate   | DeepThink  | Constructs nuanced multi-step bullish arguments        |
| 3 | BearResearcher   | ResearchDebate   | DeepThink  | Constructs nuanced multi-step bearish arguments        |
| 4 | BullResearcher   | ResearchDebate   | DeepThink  | Repeat per debate round                                |
| 5 | BearResearcher   | ResearchDebate   | DeepThink  | Repeat per debate round                                |
| 6 | InvestJudge      | ResearchDebate   | DeepThink  | Synthesises debate into investment plan                |
| 7 | Trader           | Trading          | DeepThink  | Generates structured trading plan with risk parameters |
| 8 | BullResearcher   | RiskDebate       | DeepThink  | Re-argues position under risk constraints              |
| 9 | BearResearcher   | RiskDebate       | DeepThink  | Re-argues position under risk constraints              |
|10 | RiskManager      | RiskDebate       | DeepThink  | Evaluates risk-reward tradeoffs and emits final signal |

Summary: **1 QuickThink call** and **9 DeepThink calls** per default pipeline run.

### Default model mappings per provider

| Provider  | DeepThink model         | QuickThink model         |
|-----------|-------------------------|--------------------------|
| OpenAI    | `gpt-5.2`               | `gpt-5-mini`             |
| Anthropic | `claude-sonnet-4-6`     | `claude-haiku-4-5`       |
| Google    | `gemini-3.1-pro`        | `gemini-3.1-flash`       |

The active provider is selected via `LLM_DEFAULT_PROVIDER` (default: `openai`). Model overrides are
registered per-provider in `llm.Registry` using `DefaultModelsByTier()` from each provider package.

### Cost estimates per pipeline run

Estimates assume a typical prompt of ~2 000 input tokens and ~800 output tokens per DeepThink call and
~800 input tokens and ~300 output tokens per QuickThink call, with default two-round debate
configuration (10 total invocations).

**OpenAI (default provider)**

| Model       | Calls | Input tokens | Output tokens | Est. cost  |
|-------------|-------|-------------|---------------|------------|
| gpt-5.2     |   9   |   18 000    |    7 200      | ~$0.14     |
| gpt-5-mini  |   1   |     800     |      300      | ~$0.0003   |
| **Total**   |  10   |   18 800    |    7 500      | **~$0.14** |

**Anthropic (alternative provider)**

| Model               | Calls | Input tokens | Output tokens | Est. cost  |
|---------------------|-------|-------------|---------------|------------|
| claude-sonnet-4-6   |   9   |   18 000    |    7 200      | ~$0.16     |
| claude-haiku-4-5    |   1   |     800     |      300      | ~$0.0006   |
| **Total**           |  10   |   18 800    |    7 500      | **~$0.16** |

> Pricing is approximate based on publicly listed rates at time of writing and will change over time.
> Actual token counts vary with ticker, market volatility, and the number of configured debate rounds.

### Configuration

Tier-to-model mappings are configured at provider registration time via `llm.Registry.Register`. Each
provider package exposes a `DefaultModelsByTier()` helper that returns the canonical
`map[llm.ModelTier]string` mapping.

The active provider and fallback model names are configurable via environment variables:

```
LLM_DEFAULT_PROVIDER=openai      # which registered provider to use
LLM_DEEP_THINK_MODEL=gpt-5.2     # override DeepThink model name
LLM_QUICK_THINK_MODEL=gpt-5-mini # override QuickThink model name
```

Per-strategy overrides are not yet implemented. When they are, they should follow the same
`map[llm.ModelTier]string` convention and be stored in the strategy's `ConfigSnapshot`.

### Fallback behaviour

When the configured provider is unavailable (missing API key, network error, or rate-limit) the
system currently surfaces an error to the caller and marks the pipeline run as `failed`. No automatic
cross-provider fallback is implemented. Operators should:

1. Configure a second provider (e.g. set both `OPENAI_API_KEY` and `ANTHROPIC_API_KEY`).
2. Change `LLM_DEFAULT_PROVIDER` to switch the active provider.
3. Future work: add an ordered `ProviderChain` (modelled on `internal/data.ProviderChain`) that
   retries DeepThink and QuickThink calls against a ranked list of providers before failing.

## Consequences

### Positive

- Significant cost reduction: the single QuickThink call costs roughly 50–100× less than a DeepThink
  call, saving spend without sacrificing quality where it matters.
- Faster end-to-end latency for extraction steps: QuickThink models respond in a fraction of the time
  of DeepThink models.
- Provider-agnostic: `llm.Registry` maps tiers to concrete models per provider, so swapping providers
  or upgrading individual tier models requires only a configuration change.
- Debate and judgment quality is preserved by always using the capable DeepThink tier for the nine
  invocations where multi-step reasoning is critical.

### Negative

- Additional configuration surface: operators must understand which tier maps to which model for each
  provider, and misconfigured mappings cause silent quality degradation rather than hard errors.
- No automatic failover: if the DeepThink tier is unavailable, the pipeline fails rather than
  gracefully degrading to a QuickThink model.
- Cost estimates will drift as provider pricing changes; they must be kept up to date manually.

### Neutral

- The number of DeepThink calls scales linearly with the number of configured debate rounds; operators
  can reduce cost by lowering round counts at the expense of debate thoroughness.
- Providers not listed in the default mappings (OpenRouter, xAI, Ollama) are supported but require
  manual `DefaultModelsByTier` configuration when registering with the registry.
