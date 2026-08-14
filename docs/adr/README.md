---
title: "Architecture Decision Records"
description: "Index and authoring rules for ADRs in get-rich-quick."
status: "canonical"
updated: "2026-08-14"
tags: [adr, architecture, decisions]
---

# Architecture Decision Records

This directory records material technical decisions that shaped the current system.

## Current ADR set

- [ADR-001: Use Go for backend services](001-go-backend.md)
- [ADR-002: Two-tier LLM strategy](002-two-tier-llm-strategy.md)
- [ADR-003: PostgreSQL full-text search for memory](003-postgres-fts-memory.md)
- [ADR-004: Custom DAG/runner engine](004-custom-dag-engine.md)
- [ADR-005: Position sizing strategy](005-position-sizing-strategy.md)
- [ADR-006: Paper trading assumptions](006-paper-trading-assumptions.md) — superseded by ADR-018
- [ADR-007: Deployment topology](007-deployment-topology.md)
- [ADR-008: Correlated exposure controls](008-correlated-exposure.md)
- [ADR-009: Human review gate](009-human-review-gate.md) — superseded by ADR-019
- [ADR-010: Frontend routing, app shell, and repository organization](010-frontend-routing-app-shell-organization.md)
- [ADR-011: Frontend server state and API client](011-frontend-server-state-api-client.md)
- [ADR-012: Frontend authentication, token storage, and refresh](012-frontend-auth-token-refresh.md)
- [ADR-013: Frontend WebSocket and realtime cache synchronization](013-frontend-websocket-realtime.md)
- [ADR-014: Frontend UI data entry and display infrastructure](014-frontend-ui-data-entry-display.md)
- [ADR-015: Frontend testing, mocks, accessibility, E2E, and observability](015-frontend-testing-mocks-observability.md)
- [ADR-016: Immutable double-entry economic ledger](016-immutable-economic-ledger.md)
- [ADR-017: Common intent and execution lifecycle](017-common-execution-lifecycle.md)
- [ADR-018: Isolate scored and stress paper modes](018-scored-and-stress-paper-modes.md)
- [ADR-019: Deterministic boundary between AI research and orders](019-deterministic-ai-order-boundary.md)

## ADR status lifecycle

- `proposed`: under discussion
- `accepted`: approved and expected to guide implementation
- `superseded`: replaced by a newer ADR
- `deprecated`: no longer recommended, but not directly replaced

## Naming rules

- three-digit numeric prefix
- kebab-case filename
- title format inside the document: `ADR-<number>: <Title>`

Examples:

- `001-go-backend.md`
- `002-two-tier-llm-strategy.md`

## Authoring rules

1. Start from [template.md](template.md) or [../templates/adr.md](../templates/adr.md).
2. Include at least:
   - Context
   - Decision
   - Consequences
3. When superseding an ADR, update both the old ADR and the replacement ADR with explicit links.
4. Keep ADRs about decisions, not implementation diaries.
