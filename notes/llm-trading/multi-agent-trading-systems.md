---
title: Multi-Agent Trading Systems
description: Specialized LLM agents debating and cross-checking each other for improved trading decisions
type: reference
tags: [multi-agent, LangGraph, CrewAI, TradingAgents, debate]
created: 2026-03-20
---

# Multi-Agent Trading Systems

The dominant architectural pattern in 2024-2025 for LLM trading. Mirrors real trading firms with specialized roles.

## TradingAgents Framework (UCLA/MIT)

arXiv:2412.20138, built on LangGraph:

1. **Four analyst agents**: Fundamental, sentiment, news, technical -- each analyzes from their domain
2. **Bull and bear researchers**: Debate the analyst findings from opposing perspectives
3. **Trader agent**: Makes final decision with position sizing
4. **Risk management team**: Holds veto power over trades

Results: **26.6% returns** on Apple stock in a three-month backtest. Supports OpenAI, Anthropic, Google, xAI, and local models via Ollama.

## Other Multi-Agent Patterns

- **FS-ReasoningAgent**: Ran "factual" and "subjective" LLMs in parallel for crypto news analysis; 7-10% higher profits than simpler pipelines
- **Multi-agent Bitcoin framework**: GPT-4o with announcement, event, and price momentum agents; 21.75% total return (29.30% annualized), Sharpe 1.08
- **Debate-driven (AutoGen)**: Multiple LLMs argue over a decision in conversational format

## Orchestration Frameworks

| Framework               | Strengths                                                               | Stars |
| ----------------------- | ----------------------------------------------------------------------- | ----- |
| **LangGraph**           | Production standard; stateful workflows, ReAct, LangSmith observability | --    |
| **CrewAI**              | Quick multi-agent setup, role-based collaboration                       | 44K+  |
| **AutoGen** (Microsoft) | Conversational debate patterns                                          | --    |
| **LlamaIndex**          | RAG for financial document ingestion                                    | --    |

## Implementation Path

This is Step 5 in the [[llm-bot-architecture|prototype-to-production roadmap]] (months 3-4):

- Separate concerns into specialized agents
- Add memory and reflection (store past trade outcomes)
- Let agents learn from mistakes
- Use TradingAgents as a template

## Related

- [[llm-bot-architecture]] - Overall system design
- [[sentiment-analysis-trading]] - Key input signal for analyst agents
- [[llm-trading-tools]] - Specific tools and frameworks
