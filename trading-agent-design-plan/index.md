---
title: "Trading Agent Design Plan — Map of Content"
date: 2026-03-20
tags: [moc, index, navigation]
---

# Trading Agent Design Plan

A multi-agent LLM trading system built with **Go**, **TypeScript/React**, and **PostgreSQL**. Inspired by the TradingAgents v0.2.1 framework and grounded in systematic trading research.

---

## Core Design

- [[executive-summary]] — Vision, goals, and key differentiators
- [[system-architecture]] — High-level architecture and data flow
- [[technology-stack]] — Stack choices and rationale
- [[database-schema]] — PostgreSQL schema design
- [[api-design]] — REST and WebSocket API specification
- [[implementation-roadmap]] — Phased build plan

---

## Backend (Go)

- [[go-project-structure]] — Repository layout and module organization
- [[agent-orchestration-engine]] — DAG-based agent pipeline (replaces LangGraph)
- [[llm-provider-system]] — Multi-provider LLM abstraction layer
- [[data-ingestion-pipeline]] — Market data fetching and caching
- [[execution-engine]] — Order routing and fill management
- [[risk-management-engine]] — Portfolio-level risk controls and circuit breakers
- [[memory-and-learning]] — Agent memory with PostgreSQL full-text search
- [[websocket-server]] — Real-time event streaming to frontend
- [[cli-interface]] — Full TUI dashboard with Bubble Tea + Lipgloss

---

## Frontend (TypeScript / React)

- [[frontend-overview]] — App structure, routing, and state management
- [[dashboard-design]] — Main monitoring dashboard
- [[agent-visualization]] — Agent pipeline and debate visualization
- [[portfolio-and-strategy-ui]] — Portfolio views and strategy configuration

---

## Agent System

- [[agent-system-overview]] — Agent roles, interfaces, and lifecycle
- [[analyst-agents]] — Market, fundamentals, news, and social media analysts
- [[research-debate-system]] — Bull/bear adversarial debate mechanism
- [[trader-agent]] — Trading plan generation
- [[risk-management-agents]] — Risk debate and final signal decision

---

## Data Layer

- [[data-architecture]] — Data flow, caching, and provider abstraction
- [[market-data-providers]] — Polygon.io, Alpha Vantage, Yahoo Finance, CCXT
- [[technical-indicators]] — Indicator computation in Go

---

## Execution Layer

- [[execution-overview]] — Multi-market execution architecture
- [[paper-trading]] — Simulated trading for validation

---

## Infrastructure

- [[deployment-and-operations]] — Docker, observability, security, and CI/CD
