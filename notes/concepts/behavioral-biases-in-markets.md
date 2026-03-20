---
title: Behavioral Biases in Markets
description: Cognitive biases that create persistent market anomalies exploited by systematic strategies
type: concept
tags: [behavioral-finance, biases, anomalies, market-psychology]
created: 2026-03-20
---

# Behavioral Biases in Markets

Behavioral biases explain why many [[strategy-diversification|systematic strategies]] persist despite being well-documented. They are unlikely to be arbitraged away because they are rooted in human psychology.

## Key Biases

### Underreaction Biases (Drive [[momentum]])

- **Conservatism bias**: Investors underweight new evidence (Barberis, Shleifer & Vishny, 1998)
- **Slow information diffusion**: Information spreads gradually across "newswatchers" (Hong & Stein, 1999)
- **Disposition effect**: Selling winners too early, holding losers too long

### Overreaction Biases (Drive [[mean-reversion]])

- **Extrapolation bias**: Projecting recent trends too far into the future
- **Overreaction to news**: Panic selling or euphoric buying creates temporary mispricings (De Bondt & Thaler, 1985)
- **Growth rate extrapolation**: Overpaying for "glamour" stocks, underpaying for [[value-investing|value]] (Lakonishok et al., 1994)

### Risk Perception Biases

- **Lottery preference**: Overpaying for high-beta, high-skew assets; underpaying for boring low-vol stocks (see [[low-volatility-anomaly]])
- **Insurance demand**: Persistent overpayment for downside protection drives [[volatility-selling|volatility risk premium]]
- **Anchoring**: Creates gradual price trends exploited by [[trend-following]]
- **Herding**: Amplifies trends as investors follow each other

## Structural Constraints (Non-Behavioral)

Some anomalies persist due to structural market features:

- **Leverage constraints**: Investors can't lever low-beta assets, creating [[low-volatility-anomaly|BAB]] premium (Frazzini & Pedersen, 2014)
- **Benchmark constraints**: Institutional investors penalized for tracking error, preventing arbitrage
- **Hedger-speculator transfer**: In futures markets, hedgers pay speculators for risk transfer ([[trend-following]])

## Implication for Strategy Persistence

Strategies rooted in deep behavioral biases or structural constraints are unlikely to be arbitraged away by informed capital alone. This explains why [[momentum]], [[trend-following]], and [[low-volatility-anomaly]] remain robust post-publication, while more technical strategies like [[statistical-arbitrage]] have weakened.

## Related

- [[risk-vs-mispricing-debate]] - The central academic question
- [[strategy-diversification]] - Behavioral origins explain why combining strategies works
