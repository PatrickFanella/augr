---
title: Polymarket Execution
description: Prediction market mechanics, CLOB architecture, and bot competition on Polymarket
type: reference
tags: [Polymarket, prediction-markets, CLOB, Polygon, CFTC, binary-options]
created: 2026-03-20
---

# Polymarket Execution

## Architecture

Hybrid-decentralized **Central Limit Order Book** (CLOB) on Polygon:

- Orders are EIP-712 signed off-chain, matched by operator, settled on-chain
- **Conditional Token Framework** (Gnosis CTF): each market is a "condition" with oracle for resolution
- Users split USDC collateral into YES/NO tokens (ERC-1155), can merge back or redeem after resolution

### Order Types

| Type | Description                     |
| ---- | ------------------------------- |
| GTC  | Good-Til-Cancelled limit orders |
| FOK  | Fill-or-Kill market orders      |
| GTD  | Good-Til-Date with expiration   |

- Python client: `py-clob-client`
- Trading requires USDC on Polygon
- Gas fees: ~$0.007 per transaction
- Rate limits: 60 orders/minute per API key

## Oracle Resolution

Three types:

1. **Polymarket team**: Some markets resolved directly
2. **Chainlink**: Data-driven markets (e.g. 15-minute crypto price markets)
3. **UMA Optimistic Oracle**: Subjective markets (2-hour liveness period, Schelling Point dispute resolution)

## Regulatory Status

As of **November 2025**: CFTC granted Polymarket status as a fully regulated Designated Contract Market (DCM) after acquiring QCEX for $112M. US access restored December 2025 after ~3-year hiatus. Monthly volume exceeds **$3B**. ICE announced plans to invest up to $2B.

## Bot Competition

- **14 of 20** most profitable Polymarket wallets are bots
- One bot turned $313 into $414,000 in a single month via temporal arbitrage on 15-minute BTC markets
- Simple arbitrage now highly competed at sub-100ms execution
- LLM knowledge cutoff creates stale predictions (one bot traded on outdated GPT-3.5 political data and lost)
- Real-time news integration is non-negotiable

## Bot Strategies

- **Last-second directional bets**: Buy when 5-min contract price is still below threshold
- **Oracle checks**: Verify price feed accuracy before trading
- **Market making**: Quote both sides around mid-price to capture spreads
- **Ensemble models**: Multi-model (GPT-4o 40%, Claude 35%, Gemini 25%) for paper trading

## Unique Risks

- Binary outcome risk: positions resolve to exactly $0 or $1
- Resolution risk: UMA oracle disputes, ambiguous outcomes
- Liquidity risk on thin order books
- Limit single-market exposure to <5% of portfolio

## Related

- [[crypto-execution]] - Shares blockchain infrastructure
- [[market-making]] - Market-making strategies apply here
- [[market-specific-risks]] - Prediction market risk factors
- [[llm-strategy-limitations]] - LLM knowledge cutoff challenge
