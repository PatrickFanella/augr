# Agent System Reference

This document describes the agent pipeline implemented under `internal/domain/agent.go` and `internal/agent/`.

## Canonical types

### `AgentRole`

`internal/domain/agent.go` defines 15 role constants.

| Role constant | String value | Phase | Current runtime use |
| --- | --- | --- | --- |
| `AgentRoleMarketAnalyst` | `market_analyst` | `analysis` | Yes |
| `AgentRoleFundamentalsAnalyst` | `fundamentals_analyst` | `analysis` | Yes |
| `AgentRoleNewsAnalyst` | `news_analyst` | `analysis` | Yes |
| `AgentRoleSocialMediaAnalyst` | `social_media_analyst` | `analysis` | Yes |
| `AgentRoleBullResearcher` | `bull_researcher` | `research_debate` | Yes |
| `AgentRoleBearResearcher` | `bear_researcher` | `research_debate` | Yes |
| `AgentRoleInvestJudge` | `invest_judge` | `research_debate` | Yes |
| `AgentRoleTrader` | `trader` | `trading` | Yes |
| `AgentRoleAggressiveAnalyst` | `aggressive_analyst` | `risk_debate` | Yes |
| `AgentRoleConservativeAnalyst` | `conservative_analyst` | `risk_debate` | Yes |
| `AgentRoleNeutralAnalyst` | `neutral_analyst` | `risk_debate` | Yes |
| `AgentRoleRiskManager` | `risk_manager` | `risk_debate` | Yes |
| `AgentRoleAggressiveRisk` | `aggressive_risk` | `risk_debate` | Defined, but not used by `PipelineBuilder` or the shipped risk nodes |
| `AgentRoleConservativeRisk` | `conservative_risk` | `risk_debate` | Defined, but not used by `PipelineBuilder` or the shipped risk nodes |
| `AgentRoleNeutralRisk` | `neutral_risk` | `risk_debate` | Defined, but not used by `PipelineBuilder` or the shipped risk nodes |

The risk-debate implementations in `internal/agent/risk/*.go` return the `*_analyst` roles, and `internal/agent/builder.go` requires those same `*_analyst` roles for a valid pipeline.

### `Phase`

`internal/domain/agent.go` defines four pipeline phases, executed in this order by `Pipeline.Execute`:

1. `analysis`
2. `research_debate`
3. `trading`
4. `risk_debate`

## Pipeline contract

### Core interfaces

`internal/agent/node.go` defines the base `Node` interface:

- `Name() string`
- `Role() AgentRole`
- `Phase() Phase`
- `Execute(ctx, state) error`

Optional typed interfaces sit on top of `Node`:

- `AnalystNode` → `Analyze(ctx, AnalysisInput) (AnalysisOutput, error)`
- `DebaterNode` → `Debate(ctx, DebateInput) (DebateOutput, error)`
- `TraderNode` → `Trade(ctx, TradingInput) (TradingOutput, error)`
- `RiskJudgeNode` → `JudgeRisk(ctx, RiskJudgeInput) (RiskJudgeOutput, error)`

`Pipeline` will use the typed method when a node implements it; otherwise it falls back to `Execute`.

### `PipelineState`

`internal/agent/state.go` is the shared mutable state passed through the run.

Important fields:

- `Market`, `News`, `Fundamentals`, `Social` — analysis inputs
- `AnalystReports` — per-analyst text outputs
- `ResearchDebate.Rounds` and `ResearchDebate.InvestmentPlan`
- `TradingPlan`
- `RiskDebate.Rounds` and `RiskDebate.FinalSignal`
- `FinalSignal`
- `LLMCacheStats`

`PipelineState` also keeps an internal `decisions` map keyed by role + phase + optional round number. That map is the handoff point between node execution and persistence.

## Phase behavior

### 1. Analysis

`Pipeline.executeAnalysisPhase` runs every registered analysis node concurrently via `errgroup`.

Current analysis nodes:

- `market_analyst`
- `fundamentals_analyst`
- `news_analyst`
- `social_media_analyst`

Shared behavior lives in `internal/agent/analysts/base.go`:

- builds system + user messages
- calls the configured `llm.Provider`
- records provider/prompt/usage metadata when an LLM call happens
- supports skip paths (`shouldCall=false`) that still store a report and decision

Current skip cases:

- fundamentals analyst skips when `state.Fundamentals == nil`
- news analyst skips when `len(state.News) == 0`

Phase failure semantics:

- individual analyst failures are logged and tolerated
- the phase still returns success unless a structural/persistence error occurs

After analysis completes, `persistAnalysisSnapshots` stores JSON snapshots for:

- `market`
- `news`
- `fundamentals`
- `social`

### 2. Research debate

`Pipeline.executeResearchDebatePhase` delegates to `DebateExecutor` with:

- debaters: `bull_researcher`, `bear_researcher`
- judge: `invest_judge`
- rounds: `PipelineConfig.ResearchDebateRounds` (default 3)

Per round, `DebateExecutor`:

1. appends a new `DebateRound`
2. runs each debater sequentially
3. persists each round decision with its round number
4. persists and emits `debate_round_completed`

After the rounds, the judge runs once and writes `ResearchDebate.InvestmentPlan`.

Research debate node behavior:

- bull and bear researchers store raw text contributions in the current round
- `ResearchManager` asks for JSON, parses it with `internal/llm/parse`, and stores normalized JSON when parsing succeeds
- if parsing fails, `ResearchManager` stores the raw LLM content so the pipeline can continue

### 3. Trading

`Pipeline.executeTradingPhase` requires exactly one `trader` node.

The trader reads:

- ticker
- normalized investment plan string from research debate
- analyst reports

`internal/agent/trader/trader.go` asks the LLM for JSON, parses it into `TradingPlan`, and stores:

- normalized JSON when parsing succeeds
- a default hold plan when parsing fails

Safety behavior:

- if the LLM returns a mismatched ticker, the trader overwrites it with the pipeline ticker before storing the plan

### 4. Risk debate

`Pipeline.executeRiskDebatePhase` delegates to `DebateExecutor` with:

- debaters: `aggressive_analyst`, `conservative_analyst`, `neutral_analyst`
- judge: `risk_manager`
- rounds: `PipelineConfig.RiskDebateRounds` (default 3)

Each debater receives the current `TradingPlan` as JSON context under the `trader` role.

`RiskManager` asks for JSON, parses it into a final signal, and then:

- sets `FinalSignal.Signal` to buy/sell/hold
- converts integer confidence `1..10` to a float `0.1..1.0`
- updates `TradingPlan.PositionSize` and `TradingPlan.StopLoss` for BUY/SELL outputs when adjusted values are positive
- leaves the trading plan unchanged for HOLD
- stores normalized JSON when parsing succeeds, raw content otherwise

## Builder rules

`internal/agent/builder.go` validates pipelines before construction.

A valid pipeline must have:

- at least one analysis node
- `bull_researcher`
- `bear_researcher`
- `invest_judge`
- `trader`
- `aggressive_analyst`
- `conservative_analyst`
- `neutral_analyst`
- `risk_manager`

The builder does not treat `aggressive_risk`, `conservative_risk`, or `neutral_risk` as required runtime roles.

## Timeouts and failure boundaries

`PipelineConfig` has:

- `PipelineTimeout` — whole run deadline
- `PhaseTimeout` — per-phase deadline
- `ResearchDebateRounds`
- `RiskDebateRounds`

Behavior:

- analysis failures inside a node do not abort the run
- research debate, trading, and risk debate errors abort the run immediately
- `DebateExecutor` clamps configured rounds below 1 up to 1 and logs a warning
- `RecordRunComplete` writes status updates with a fresh background context and a 10 second timeout so cancellation does not strand runs in `running`

## Persistence model

### Pipeline runs

`DecisionPersister.RecordRunStart` stores a `PipelineRun` before phase execution.

`DecisionPersister.RecordRunComplete` updates the run with:

- final status
- completion time
- error message, if any
- per-phase timings JSON

When `ExecuteStrategy` is used, the resolved strategy config is marshaled into `PipelineRun.ConfigSnapshot` for auditability.

### Agent decisions

`DecisionPersister.PersistDecision` writes `domain.AgentDecision` records with:

- `pipeline_run_id`
- `agent_role`
- `phase`
- `round_number` for debate contributions
- `output_text`
- optional LLM metadata:
  - `llm_provider`
  - `llm_model`
  - `prompt_text`
  - `prompt_tokens`
  - `completion_tokens`
  - `latency_ms`
  - `cost_usd`

If a node stored a `DecisionLLMResponse`, persistence copies those fields. Static or skipped outputs still persist as decisions with nil LLM metadata.

### Structured events

`DecisionPersister.PersistEvent` writes `domain.AgentEvent` records.

Persisted event kinds:

- `phase_started`
- `phase_completed`
- `agent_started`
- `agent_completed`
- `debate_round_completed`
- `signal_produced`
- `pipeline_started`
- `pipeline_completed`
- `pipeline_failed`

These are distinct from the in-memory/user-visible `PipelineEvent` channel.

### User-visible pipeline events

`internal/agent/event.go` defines the emitted runtime event types:

- `pipeline_started`
- `agent_decision_made`
- `debate_round_completed`
- `signal_generated`
- `llm_cache_stats_reported`
- `pipeline_completed`
- `pipeline_error`

Phase and agent events are sent non-blocking. Terminal events use `emitEvent` without a request context so they are not dropped just because the pipeline context was canceled.

## Current wiring notes

- `cmd/tradingagent/runtime.go` builds a deterministic smoke pipeline only when `APP_ENV=smoke`.
- That smoke pipeline uses the same phase ordering and persistence APIs, but all nodes are local deterministic stubs with no live LLM calls.
- The reusable node implementations under `internal/agent/analysts`, `internal/agent/debate`, `internal/agent/trader`, and `internal/agent/risk` are the current source of truth for agent behavior, prompts, parsing, and decision recording.
