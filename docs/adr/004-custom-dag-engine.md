# ADR-004: Custom DAG engine vs LangGraph

- **Status:** accepted
- **Date:** 2026-03-21
- **Deciders:** Engineering
- **Technical Story:** [ADR-004: Custom DAG engine vs LangGraph](https://github.com/PatrickFanella/get-rich-quick/issues)

## Context

The reference implementation for multi-agent trading pipelines (TradingAgents v0.2.1) uses Python and [LangGraph](https://github.com/langchain-ai/langgraph) for DAG-based agent orchestration.

This project requires an orchestration engine that:

- Executes a directed acyclic graph of heterogeneous agents (analysts, researchers, trader, risk manager).
- Runs independent agents concurrently to reduce pipeline latency.
- Streams progress events to WebSocket clients in real time.
- Enforces strict typed contracts between pipeline phases at compile time.
- Deploys as a single static binary alongside the rest of the Go backend.

LangGraph was evaluated as a ready-made alternative. Key observations:

- LangGraph is Python-only; integrating it with a Go backend would require an out-of-process bridge (e.g. gRPC sidecar, subprocess, or HTTP service), adding operational complexity and a cross-language runtime boundary.
- LangGraph's concurrency model relies on Python's `asyncio`; it cannot use goroutines or Go channels natively.
- LangGraph carries the full LangChain ecosystem as a transitive dependency, which is heavyweight relative to the narrow subset of features needed here.
- A thin custom engine can be implemented in a few hundred lines of Go and composed directly with existing interfaces (`LLMProvider`, `DataProvider`, `RiskEngine`, `Broker`).

## Decision

We will build and maintain a **custom Go-native DAG orchestration engine** rather than adopting LangGraph.

The engine is structured around three primitives:

1. **`Node` interface** (`internal/agent/node.go`) — every agent implements `Name() string`, `Phase() Phase`, and `Execute(ctx, *PipelineState) error`.
2. **`PipelineState`** (`internal/agent/state.go`) — a single shared value passed through all phases, accumulating analyst reports, debate rounds, a trading plan, and a final signal.
3. **`PipelineEvent`** (`internal/agent/event.go`) — typed events emitted after each phase transition for WebSocket streaming.

Phase 1 (analyst execution) uses `golang.org/x/sync/errgroup` to run all selected analyst nodes concurrently as goroutines. Subsequent phases run sequentially, with each debate round spawning goroutines for opposing roles where applicable. Events are published to a channel that the WebSocket layer consumes without blocking the pipeline.

## Consequences

### Positive

- Phase 1 analysts run as concurrent goroutines; a four-analyst pipeline completes in roughly the time of the slowest single LLM call instead of the sum.
- The `Node` interface and `PipelineState` struct provide compile-time type safety; incorrect wiring is caught at build time rather than at runtime.
- Events are delivered to WebSocket clients via Go channels, giving low-latency streaming without additional infrastructure.
- The entire engine is part of the Go binary; no Python runtime, sidecar, or subprocess is required.
- The engine depends only on the standard library and `errgroup`; it adds no third-party dependency footprint.

### Negative

- All orchestration logic must be written and maintained in-house; the LangGraph ecosystem (built-in retries, checkpointing, visualization tooling) is not available.
- Porting future features from the Python reference implementation requires manual translation rather than a direct library upgrade.
- The codebase grows proportionally to the number of phases and agent roles added.

### Neutral

- New agent types must implement the `Node` interface; this is a small, well-defined contract but requires code changes for each addition.
- The decision is consistent with ADR-001 (Go for backend services) and reinforces the single-language backend strategy.
