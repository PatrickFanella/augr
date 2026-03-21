---
title: "Analyst Agents"
date: 2026-03-20
tags: [agents, analysts, market, fundamentals, news, social]
---

# Analyst Agents

Four specialized analyst agents form Phase 1 of the pipeline. They run in parallel, each producing an independent report from its domain.

## Market Analyst

**Focus:** Technical analysis of price action and indicators.

**Data Sources:**

- OHLCV price data via [[data-ingestion-pipeline]]
- 14 technical indicators via [[technical-indicators]]

**System Prompt Core:**

> You are a senior technical analyst. Analyze the provided price data and technical indicators for {TICKER}. Evaluate:
>
> - Price trends (short, medium, long-term)
> - Volume patterns and anomalies
> - Support and resistance levels
> - Technical indicator signals (MACD crossovers, RSI extremes, Bollinger Band positioning)
> - Key chart patterns
>
> Produce a concise technical analysis report with a directional bias (bullish, bearish, or neutral) and confidence level.

**Output:** Technical analysis report written to `state.MarketReport`

**Indicators Analyzed:**

| Category   | Indicators                                  |
| ---------- | ------------------------------------------- |
| Trend      | SMA (20, 50, 200), EMA (12, 26), MACD       |
| Momentum   | RSI, MFI, Stochastic, Williams %R, CCI, ROC |
| Volatility | Bollinger Bands, ATR                        |
| Volume     | VWMA, OBV, ADL                              |

## Fundamentals Analyst

**Focus:** Financial statement analysis and valuation.

**Data Sources:**

- Income statement, balance sheet, cash flow via [[data-ingestion-pipeline]]
- Company overview (market cap, P/E, P/B, dividend yield)

**System Prompt Core:**

> You are a senior fundamental analyst. Analyze the financial statements and metrics for {TICKER}. Evaluate:
>
> - Revenue trends and growth trajectory
> - Profitability margins (gross, operating, net)
> - Balance sheet strength (debt-to-equity, current ratio)
> - Cash flow quality (free cash flow, operating cash flow vs. net income)
> - Valuation metrics relative to sector peers
> - Insider trading patterns (if available)
>
> Produce a fundamental analysis report with intrinsic value assessment.

**Output:** Fundamental analysis report written to `state.FundamentalsReport`

**Note:** For crypto and prediction markets, this analyst is typically skipped (configured via `SelectedAnalysts`).

## News Analyst

**Focus:** News sentiment and macroeconomic context.

**Data Sources:**

- Company-specific news via NewsAPI, Polygon.io
- Global/macro news via [[data-ingestion-pipeline]]

**System Prompt Core:**

> You are a senior news analyst. Analyze recent news for {TICKER} and the broader market. Evaluate:
>
> - Company-specific developments (earnings, product launches, management changes)
> - Sector and industry trends
> - Macroeconomic factors (Fed policy, inflation data, employment)
> - Regulatory developments
> - Overall sentiment (positive, negative, mixed)
> - Upcoming catalysts or risk events
>
> Produce a news impact assessment with sentiment score and key catalysts.

**Output:** News impact assessment written to `state.NewsReport`

## Social Media Analyst

**Focus:** Social sentiment and retail investor discussion.

**Data Sources:**

- Social-focused news and sentiment via [[data-ingestion-pipeline]]
- Future: Reddit, Twitter/X API integration

**System Prompt Core:**

> You are a social media sentiment analyst. Analyze public discussion and sentiment around {TICKER}. Evaluate:
>
> - Overall sentiment direction and intensity
> - Viral narratives or trending topics
> - Retail investor sentiment vs. institutional positioning
> - Social volume trends (increasing or decreasing attention)
> - Divergence between social sentiment and fundamental reality
>
> Produce a social sentiment report highlighting key narratives and sentiment signals.

**Output:** Social sentiment report written to `state.SocialReport`

## Parallel Execution

All selected analysts run concurrently:

```go
func (o *Orchestrator) runAnalysts(ctx context.Context, state *PipelineState) error {
    analysts := o.buildAnalysts(state.Config.SelectedAnalysts)

    g, ctx := errgroup.WithContext(ctx)
    for _, analyst := range analysts {
        a := analyst
        g.Go(func() error {
            return a.Execute(ctx, state)
        })
    }
    return g.Wait()
}
```

Each analyst writes to its own field in `PipelineState` — there are no data races since each field is written by exactly one goroutine.

## Data Requirements Summary

| Analyst      | Required Data        | Typical Latency          |
| ------------ | -------------------- | ------------------------ |
| Market       | OHLCV + indicators   | 2-3s (data) + 2-4s (LLM) |
| Fundamentals | Financial statements | 1-2s (data) + 2-4s (LLM) |
| News         | Recent articles      | 1-2s (data) + 2-4s (LLM) |
| Social       | Social sentiment     | 1-2s (data) + 2-4s (LLM) |

With parallel execution, total Phase 1 latency ≈ 4-7 seconds (limited by the slowest analyst).

---

**Related:** [[agent-system-overview]] · [[research-debate-system]] · [[data-ingestion-pipeline]] · [[technical-indicators]]
