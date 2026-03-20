---
title: "Executive Summary"
date: 2026-03-20
tags: [overview, vision, goals]
---

# Executive Summary

## Vision

Build an autonomous multi-agent LLM trading system that decomposes trading decisions into specialized roles — mirroring a real trading firm — with full observability, rigorous risk controls, and support for stocks, crypto, and prediction markets.

## Problem Statement

The reference TradingAgents framework (v0.2.1) demonstrates the multi-agent approach but is a Python research prototype with significant production gaps:

- No persistent storage — memory resets between runs
- No execution layer — produces signals but does not trade
- No frontend — CLI-only interaction
- Single-threaded Python — poor concurrency for real-time data
- No backtesting pipeline — manual evaluation only
- Tight coupling to LangGraph and Python LLM libraries

## Our Solution

A production-grade system that retains the proven multi-agent architecture while solving every gap above.

### Key Differentiators

| Aspect        | TradingAgents (Reference) | Our System                                  |
| ------------- | ------------------------- | ------------------------------------------- |
| Language      | Python                    | **Go** (backend), **TypeScript** (frontend) |
| Orchestration | LangGraph StateGraph      | Custom DAG engine in Go                     |
| Database      | None (in-memory)          | **PostgreSQL**                              |
| Memory        | In-memory BM25            | PostgreSQL full-text search (`tsvector`)    |
| Execution     | None (signal only)        | Alpaca, CCXT, Polymarket APIs               |
| Frontend      | CLI (Typer/Rich)          | React dashboard with real-time WebSocket    |
| Concurrency   | Single-threaded           | Goroutines + channels                       |
| Backtesting   | Manual                    | Integrated backtesting engine               |
| Paper Trading | Not supported             | Built-in paper trading mode                 |
| Multi-market  | Stocks only               | Stocks, crypto, prediction markets          |

## Target Markets

1. **US Equities** — via Alpaca (commission-free, paper trading built-in)
2. **Crypto** — via CCXT-equivalent direct exchange APIs (Binance, Coinbase, Kraken)
3. **Prediction Markets** — via Polymarket CLOB API on Polygon

## Core Design Principles

1. **Role Specialization** — Each agent has a single clear responsibility
2. **Adversarial Debate** — Bull/bear and risk perspectives force rigorous analysis
3. **Memory and Learning** — Agents learn from past outcomes via persistent memory
4. **Defense in Depth** — Multiple layers of risk controls with hard circuit breakers
5. **Observability** — Every agent decision is logged, stored, and visualizable
6. **Plugin Architecture** — Strategies, data providers, and exchanges are pluggable

## Success Criteria

- Paper trading Sharpe ratio > 1.0 over 60-day validation period
- Agent pipeline latency < 30 seconds end-to-end
- System handles 24/7 operation without manual intervention
- All agent decisions auditable with full reasoning chain
- Frontend provides real-time visibility into agent state

---

**Related:** [[system-architecture]] · [[technology-stack]] · [[implementation-roadmap]]
