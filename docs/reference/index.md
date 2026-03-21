---
title: TradingAgents - Map of Content
description: Index of all notes covering the TradingAgents multi-agent LLM trading framework
type: MOC
created: 2026-03-20
---

# TradingAgents

Multi-agent LLM financial trading framework (v0.2.1) that mirrors real-world trading firms by deploying specialized LLM-powered agents to evaluate market conditions and make informed trading decisions. Built on LangGraph with multi-provider LLM support.

**Source**: [TradingAgents on GitHub](https://github.com/TradingAgents) (UCLA/MIT, arXiv:2412.20138)

## Project Overview

- [[overview]] - Purpose, architecture summary, and key concepts
- [[architecture]] - End-to-end execution flow and system design
- [[configuration]] - All configurable options and defaults
- [[getting-started]] - Installation, setup, and basic usage

## Agent Teams

- [[analyst-team]] - Overview of the four analyst specializations
  - [[market-analyst]] - Technical indicators and price action
  - [[fundamentals-analyst]] - Financial statements and company health
  - [[news-analyst]] - News sentiment and macroeconomic context
  - [[social-media-analyst]] - Social sentiment and public discussion
- [[research-team]] - Bull/bear debate and research manager
- [[trader-agent]] - Trading plan composition
- [[risk-management-team]] - Three-perspective risk debate and final decision

## Workflow Orchestration

- [[langgraph-orchestration]] - StateGraph setup and node wiring
- [[state-management]] - AgentState, debate substates, and data flow
- [[conditional-routing]] - Tool calls, debate loops, and analyst sequencing
- [[signal-processing]] - Extracting BUY/SELL/HOLD from verbose output

## Data Layer

- [[vendor-system]] - Vendor routing, fallback, and tool registration
- [[yahoo-finance]] - yfinance implementation and capabilities
- [[alpha-vantage]] - Alpha Vantage implementation and rate limit handling
- [[technical-indicators]] - All supported indicators and calculations

## LLM Integration

- [[multi-provider-system]] - Factory pattern and provider abstraction
- [[supported-models]] - Models, providers, and provider-specific quirks

## Learning & Memory

- [[bm25-memory-system]] - BM25-based retrieval, reflection, and learning from outcomes

## CLI

- [[cli-interface]] - Interactive command-line interface
