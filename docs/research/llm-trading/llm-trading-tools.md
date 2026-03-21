---
title: LLM Trading Tools & Frameworks
description: Models, orchestration libraries, data sources, and execution platforms for LLM trading
type: reference
tags: [tools, frameworks, APIs, FinGPT, FinBERT, DeepSeek, LangGraph]
created: 2026-03-20
---

# LLM Trading Tools & Frameworks

## LLM Layer

### Commercial APIs

- **OpenAI** (GPT-4o, GPT-5): Highest reasoning quality, ongoing inference cost
- **Anthropic** (Claude): Strong reasoning, structured output
- **Google** (Gemini): Vision capabilities for chart analysis

### Specialized Financial Models

- **DeepSeek**: +40% returns in Alpha Arena live crypto competition (GPT-5 and Gemini each lost 25%+). Domain-specific behavior matters more than model size.
- **FinGPT** (13K+ stars): Open-source via LoRA on Llama. Sentiment comparable to GPT-4 on single RTX 3090 (~$416 training cost vs BloombergGPT's $5M)
- **FinBERT** (ProsusAI/finbert): Fast, lightweight financial sentiment classification

### Local Inference

- **Ollama**: Privacy-sensitive or cost-conscious deployments
- **vLLM**: High-throughput local serving

## Agent Orchestration

| Framework               | Use Case                                              |
| ----------------------- | ----------------------------------------------------- |
| **LangGraph**           | Production standard; TradingAgents built on it        |
| **CrewAI** (44K+ stars) | Quick multi-agent setup with role-based collaboration |
| **AutoGen** (Microsoft) | Conversational debate patterns                        |
| **LlamaIndex**          | RAG for financial document ingestion                  |

See [[multi-agent-trading-systems]] for architectural patterns.

## Data Sources

| Source                        | Coverage                                       |
| ----------------------------- | ---------------------------------------------- |
| **Polygon.io**                | Real-time + historical stocks, options, crypto |
| **Yahoo Finance** (yfinance)  | Free but delayed                               |
| **Alpha Vantage**             | Free tier available                            |
| **CryptoCompare / CoinGecko** | Crypto data with free tiers                    |
| **Alchemy**                   | On-chain data via WebSocket, 100+ blockchains  |
| **NewsAPI / Benzinga**        | Financial news feeds                           |

## Execution Platforms

| Platform            | Market                  | Notes                                              |
| ------------------- | ----------------------- | -------------------------------------------------- |
| **Alpaca**          | Stocks/crypto           | Beginner-friendly, see [[stock-market-execution]]  |
| **IBKR** (ib_async) | Multi-asset             | Professional, see [[stock-market-execution]]       |
| **CCXT**            | Crypto (110+ exchanges) | See [[crypto-execution]]                           |
| **py-clob-client**  | Polymarket              | See [[polymarket-execution]]                       |
| **NautilusTrader**  | Institutional           | Bridges backtest to production, Polymarket adapter |

## Backtesting Frameworks

See [[backtesting-methodology]] for details:

- **vectorbt**: Fastest (millions of trades/sec)
- **Backtrader**: Simplest event-driven
- **QuantConnect/LEAN**: Institutional-grade
- **Freqtrade**: Crypto-focused with FreqAI

## Open-Source Trading Projects

- **TradingAgents**: UCLA/MIT multi-agent framework
- **FinRL**: RL-based financial trading
- **CryptoTrade**: Reflective crypto agent (EMNLP 2024)
- **Polymarket/agents**: LangChain-integrated autonomous trading
- **claude-trader**: Safety-focused with dedicated risk module
- **kojott/LLM-trader-test**: DeepSeek single-agent approach

## Related

- [[llm-bot-architecture]] - How these tools fit together
- [[multi-agent-trading-systems]] - Orchestration patterns
