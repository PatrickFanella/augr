---
title: TradingAgents Overview
description: Purpose, design philosophy, and high-level architecture of the TradingAgents framework
type: overview
source_file: TradingAgents/README.md
created: 2026-03-20
---

# TradingAgents Overview

## Purpose

TradingAgents decomposes complex trading decisions into specialized roles, mirroring a real trading firm. Instead of one monolithic LLM making all decisions, a team of agents with distinct expertise collaborates through structured debate and deliberation.

## Core Design Principles

1. **Role specialization**: Each agent has a narrow focus (technical analysis, fundamentals, news, sentiment, risk)
2. **Adversarial debate**: Bull and bear researchers argue opposing cases; risk debators argue from aggressive/conservative/neutral perspectives
3. **Hierarchical judgment**: Managers judge debates and make decisions; the system doesn't average opinions
4. **Memory and reflection**: Agents learn from past trading outcomes via [[bm25-memory-system]]
5. **Tool-augmented reasoning**: Agents call real data tools rather than relying on LLM knowledge

## Agent Team Structure

```
┌─────────────────────────────────────────────────┐
│                 Analyst Team                     │
│  [[market-analyst]] [[fundamentals-analyst]]     │
│  [[news-analyst]]   [[social-media-analyst]]     │
└──────────────────────┬──────────────────────────┘
                       ▼
┌─────────────────────────────────────────────────┐
│              Research Debate                      │
│  Bull Researcher ◄──► Bear Researcher            │
│         └──► Research Manager (judge)            │
└──────────────────────┬──────────────────────────┘
                       ▼
┌─────────────────────────────────────────────────┐
│              [[trader-agent]]                    │
│         Creates trading plan                     │
└──────────────────────┬──────────────────────────┘
                       ▼
┌─────────────────────────────────────────────────┐
│          Risk Management Debate                  │
│  Aggressive ◄──► Conservative ◄──► Neutral       │
│         └──► Risk Manager (final decision)       │
└─────────────────────────────────────────────────┘
```

## Version and Status

- **Version**: 0.2.1 (March 2026)
- **License**: Apache 2.0
- **Python**: Package via `pyproject.toml` with Typer CLI
- **Key dependencies**: LangChain, LangGraph, yfinance, pandas, rank-bm25

## Output

The system produces a final **BUY / SELL / HOLD** signal with full reasoning traces from every agent in the chain. See [[signal-processing]].

## Related

- [[architecture]] - Detailed execution flow
- [[configuration]] - All configurable options
- [[getting-started]] - How to run it
