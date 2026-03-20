---
title: Cryptocurrency Execution
description: CCXT unified API, DEX trading, and MEV protection for crypto trading bots
type: reference
tags: [crypto, CCXT, DEX, MEV, Uniswap, Solana, Ethereum]
created: 2026-03-20
---

# Cryptocurrency Execution

## CCXT (Centralized Exchanges)

`pip install ccxt` -- unified Python API across **110+ exchanges**:

- Single `exchange.create_order()` call works across Binance, Coinbase, Kraken, Bybit, etc.
- Consistent parameters for market, limit, stop-loss, OCO orders
- Install `orjson` and `coincurve` for performance (ECDSA signing drops from ~45ms to <0.05ms)

### Fee Comparison

| Exchange          | Maker/Taker    |
| ----------------- | -------------- |
| Binance           | ~0.1%          |
| Coinbase Advanced | 0.4-0.6% taker |
| Kraken            | 0.16-0.26%     |

## DEX Trading

### Uniswap (Ethereum)

- Trades against AMM liquidity pools
- Slippage is function of trade size vs pool depth

### Jupiter (Solana)

- Aggregates 22+ DEXs
- Dynamic slippage settings
- Near-zero gas fees (~$0.001 per transaction)
- Ideal for high-frequency DEX strategies

### Gas Fees

- **Ethereum L1**: ~$0.44 average (Dencun upgrade 2024 cut L2 fees by 90%), but can spike
- **Solana**: ~$0.00025 per transaction

## MEV Protection

**Critical for all crypto bots.** One MEV bot made ~$34M in 3 months through sandwich attacks. Another bot (0xbad) earned 800 ETH then lost all profits + 300 ETH to a hacker exploiting its code.

- **Flashbots Protect**: Private relay to prevent sandwich attacks on Ethereum
- **Jito bundles**: MEV protection on Solana
- **CoWSwap**: Batch auctions eliminating sandwich attacks by design
- Always set tight slippage tolerance
- Prefer limit orders over market orders on DEXs

### Regulatory Warning

EU MiCA declares MEV as **illegal market abuse**. US DOJ has arrested individuals for MEV-related theft. MEV protection is both security necessity and compliance requirement.

## Operational Considerations

- 24/7 markets require cloud deployment with uptime guarantees
- Automated failover and weekend monitoring (liquidity thins, volatility spikes)
- Exchange counterparty risk (FTX collapse): never keep all funds on single exchange
- Separate cold storage for reserves, limited "hot" wallets for bot operations
- Funding rate erosion on perpetual futures

## Related

- [[stock-market-execution]] - Equity equivalent
- [[polymarket-execution]] - Prediction market execution
- [[market-specific-risks]] - Regulatory landscape
- [[portfolio-risk-controls]] - Kill switches and circuit breakers
