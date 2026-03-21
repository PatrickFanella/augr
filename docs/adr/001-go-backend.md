# ADR-001: Use Go for backend services

- **Status:** accepted
- **Date:** 2026-03-20
- **Deciders:** Engineering
- **Technical Story:** Issue: "Create ADR template and conventions"

## Context

The project needs a backend language for core services such as data ingestion, orchestration, and execution workflows.

Key requirements include:

- Strong concurrency support for market data and agent workflows
- Predictable performance and low runtime overhead
- Simple deployment as static binaries
- Maintainable codebase with broad tooling and ecosystem support

## Decision

We will use **Go** as the primary language for backend services.

## Consequences

### Positive

- Efficient concurrency model (goroutines/channels) fits event-driven and parallel workloads.
- Compiled static binaries simplify deployment and operations.
- Strong standard library and mature tooling support reliability and developer productivity.

### Negative

- Smaller set of high-level abstractions compared to some other languages can increase boilerplate.
- Team members unfamiliar with Go may require onboarding time.

### Neutral

- Existing non-backend components can continue using other languages where appropriate.
