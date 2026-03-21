---
title: Social Media Analyst
description: Social sentiment and public discussion analysis agent
type: agent
source_file: TradingAgents/tradingagents/agents/analysts/social_media_analyst.py
created: 2026-03-20
---

# Social Media Analyst

Analyzes social media discussions and public sentiment around a company to gauge retail investor sentiment and emerging narratives.

## Tools

| Tool       | Data Returned                                       |
| ---------- | --------------------------------------------------- |
| `get_news` | News articles analyzed for social sentiment signals |

Uses the same news tools as [[news-analyst]] but with a sentiment-focused system prompt that emphasizes public perception and social dynamics.

## System Prompt Behavior

The social media analyst evaluates:

- Overall sentiment direction in public discussions
- Viral narratives or memes affecting perception
- Retail investor enthusiasm or fear
- Social media volume and momentum
- Divergence between social sentiment and fundamental reality

## Output

A sentiment report assessing public perception, social momentum, and any disconnect between sentiment and fundamentals. Consumed by the [[research-team]].

## Related

- [[analyst-team]] - Overview of all analysts
- [[news-analyst]] - Complementary news analysis
