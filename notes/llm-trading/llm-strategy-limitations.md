---
title: LLM Strategy Performance & Limitations
description: Benchmarks revealing that LLM trading advantages often vanish under rigorous testing
type: reference
tags: [benchmarks, FINSABER, StockBench, limitations, overfitting]
created: 2026-03-20
---

# LLM Strategy Performance & Limitations

Three realities temper enthusiasm for LLM trading systems.

## 1. Comprehensive Benchmarks Show Deterioration

### FINSABER (2025)

The most comprehensive benchmarking study to date. Tested across **20+ years and 100+ symbols**:

- Previously reported LLM advantages **deteriorate significantly** under broader evaluation
- LLM strategies tend to be overly conservative in bull markets (underperforming passive benchmarks) and overly aggressive in bear markets (heavy losses)
- Example: FinMem's reported MSFT return of +23.3% dropped to **-22.0%** when tested more broadly

### StockBench (2025)

Confirmed that most LLM agents **struggle to outperform simple buy-and-hold**.

### Eurekahedge AI Hedge Fund Index

**9.8% annualized return** vs S&P 500's 13.7% over 15 years. Sophistication in AI does not automatically translate to market outperformance.

## 2. Non-Determinism Problem

See [[llm-backtesting-challenges]] for details. Even at temperature zero, LLMs produce varying outputs. **Prompt sensitivity alone can swing results from -15% to +17%.**

## 3. Inference Costs

- Each comprehensive backtest: **$10-40** in inference fees
- Total development costs can exceed $150 in API calls
- Continuous live costs make LLMs unsuitable for high-frequency strategies
- Best deployed for lower-frequency, higher-conviction decisions

## What Still Works

- [[sentiment-analysis-trading]] with 74%+ accuracy on financial news
- [[multi-agent-trading-systems]] where specialized agents cross-check each other
- LLMs as meta-strategists for regime detection and strategy selection
- Combining LLM signals with traditional quant factors

## Practical Guidance

- Start simple ([[paper-trading|Alpaca paper trading]])
- Validate rigorously (multi-year backtests across diverse conditions)
- Deploy cautiously (fractional capital with layered [[portfolio-risk-controls|risk controls]])
- Maintain intellectual honesty about capabilities

## Related

- [[llm-backtesting-challenges]] - Technical barriers to reliable testing
- [[sentiment-analysis-trading]] - The strongest use case
- [[backtesting-methodology]] - General pitfalls that amplify LLM issues
