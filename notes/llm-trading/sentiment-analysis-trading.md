---
title: Sentiment Analysis for Trading
description: Using LLMs to extract financial sentiment from news, filings, and social media as trading signals
type: reference
tags: [sentiment, NLP, FinBERT, FinGPT, news-driven]
created: 2026-03-20
---

# Sentiment Analysis for Trading

Financial sentiment analysis is where LLMs have proven **most effective** as trading signals.

## Performance Evidence

- **965,375 US financial news articles (2010-2023)**: GPT-3-based OPT model achieved 74.4% accuracy (vs BERT 72.5%, FinBERT 72.2%, Loughran-McDonald 50.1%)
- Long-short strategy based on OPT sentiment: **Sharpe ratio of 3.05**, 355% returns (Aug 2021 - Jul 2023)
- University of Florida: ChatGPT scores "significantly predict subsequent daily stock returns"; self-financing strategy yielded 400%+ cumulative return
- MarketSenseAI 2.0: 77-78% win rate with alpha of 8.0-18.9% on S&P 500 in 2024

## Why LLMs Excel Here

- Detect hedging in forward guidance
- Identify tone shifts in earnings calls
- Distinguish genuine optimism from PR speak
- Process nuanced language hours before human analysts
- Example: LLM flagged Amazon's "cautious optimization" language 2 hours before analysts, generating **4.2% alpha**

## Implementation Architecture

### Two-Tier Approach

1. **FinBERT** (ProsusAI/finbert): Fast, lightweight first-pass filter for high-volume headline classification
2. **GPT-4 or Claude**: Nuanced analysis of complex texts (earnings transcripts, filings)

### FinGPT

Open-source financial LLM via LoRA fine-tuning on Llama base models:

- Sentiment F1 scores up to **87.6%** on single RTX 3090
- Comparable to GPT-4 at fraction of cost (~$416 training vs BloombergGPT's $5M)
- 13K+ GitHub stars

### Pipeline

News APIs (NewsAPI, Benzinga, CryptoCompare) -> Sentiment model -> Numerical score -> Combine with traditional signals -> Trading decision

## Event-Driven Trading

LLMs excel because they interpret meaning of events in context. Multi-agent frameworks construct specialized agents (announcement, event, price momentum) that synthesize signals. See [[multi-agent-trading-systems]].

## Technical Analysis Augmentation

- 2025 framework: LLMs generate "formulaic alphas" (math expressions) from OHLCV + indicators + sentiment -> Transformer price prediction
- Vision-capable models (Gemini Flash) can analyze chart images directly

## Caveats

See [[llm-strategy-limitations]] -- short-period backtests on narrow universes are unreliable. FINSABER (2025) shows advantages deteriorate over broader testing.

## Related

- [[llm-bot-architecture]] - Where sentiment fits in the system
- [[multi-agent-trading-systems]] - Sentiment as analyst agent input
- [[llm-strategy-limitations]] - Realistic performance expectations
