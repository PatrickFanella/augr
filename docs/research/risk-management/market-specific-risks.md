---
title: Market-Specific Risks
description: Regulatory, structural, and counterparty risks across stocks, crypto, and prediction markets
type: reference
tags: [regulation, PDT, wash-sale, FINRA, MiCA, CFTC, counterparty-risk]
created: 2026-03-20
---

# Market-Specific Risks

## Stock Markets

### Regulatory Framework

- SEC/FINRA maintain technology-neutral stance: existing rules apply regardless of AI or human
- FINRA Rule 3110 (supervision), Rules 2010/2020 (anti-manipulation), Rule 5270 (front-running)
- FINRA 2025 Annual Regulatory Oversight Report identified AI risk management as key focus
- Trump admin Jan 2025 executive order signals more permissive environment but doesn't change existing rules

### Pattern Day Trader Rule

- 4+ day trades in 5 business days requires $25K equity in margin accounts
- FINRA filed amendments Dec 2025 to replace with flexible intraday margin requirements
- Pending SEC approval, expected late 2026

### Wash Sale Rule

Particularly dangerous for bots: frequent trading of same ticker inflates taxable gains. **Section 475(f) mark-to-market election** (filed by April 15) bypasses wash sales entirely. Recommended for active trading bots.

### Short Selling

- Reg SHO: locate/borrow requirement
- Short Sale Restriction (SSR): triggers when stock falls 10%+ from prior close; shorts must execute at ask price
- Bots must check SSR status before any short order

## Cryptocurrency

- **Exchange counterparty risk**: FTX collapse demonstrated catastrophic loss potential. Never keep all funds on single exchange.
- **Smart contract risk**: DeFi protocol exploits
- **Funding rate erosion**: On perpetual futures positions
- **EU MiCA**: Declares MEV as illegal market abuse (see [[crypto-execution]])
- **US DOJ**: Has arrested individuals for MEV-related theft
- **24/7 operation**: Requires cloud deployment with failover and weekend monitoring

## Prediction Markets (Polymarket)

- **Binary outcome risk**: Positions resolve to exactly $0 or $1
- **Resolution risk**: UMA oracle disputes, ambiguous outcomes
- **Liquidity risk**: Thin order books on many markets
- **Regulatory evolution**: CFTC DCM status as of Nov 2025 (see [[polymarket-execution]])
- Limit single-market exposure to <5% of portfolio; size assuming worst-case total loss

## Related

- [[portfolio-risk-controls]] - Technical safety controls
- [[stock-market-execution]] - Equity platform details
- [[crypto-execution]] - Crypto platform details
- [[polymarket-execution]] - Prediction market details
