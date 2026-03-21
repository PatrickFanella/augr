---
title: Alpha Vantage Data
description: Alpha Vantage vendor implementation with rate limit handling and yfinance fallback
type: data-source
source_files:
  - TradingAgents/tradingagents/dataflows/alpha_vantage_stock.py
  - TradingAgents/tradingagents/dataflows/alpha_vantage_indicators.py
  - TradingAgents/tradingagents/dataflows/alpha_vantage_fundamentals.py
  - TradingAgents/tradingagents/dataflows/alpha_vantage_news.py
  - TradingAgents/tradingagents/dataflows/alpha_vantage_common.py
created: 2026-03-20
---

# Alpha Vantage Data

Alternative data vendor providing stock data, technical indicators, fundamentals, and news via the Alpha Vantage API.

## Implementation

Split across multiple files by endpoint type:

| File                            | Coverage                                              |
| ------------------------------- | ----------------------------------------------------- |
| `alpha_vantage_stock.py`        | OHLCV price data                                      |
| `alpha_vantage_indicators.py`   | Technical indicators                                  |
| `alpha_vantage_fundamentals.py` | Financial statements                                  |
| `alpha_vantage_news.py`         | News and sentiment                                    |
| `alpha_vantage_common.py`       | Shared utilities (API key management, error handling) |

## Rate Limit Handling

Alpha Vantage has strict rate limits (5 requests/minute on free tier). When rate limits are hit:

1. The implementation detects the rate limit error response
2. Automatically falls back to [[yahoo-finance]] for that specific request
3. Logs the fallback for debugging

This makes Alpha Vantage safe to configure even with a free-tier API key.

## Configuration

```python
config = {
    "data_vendors": {
        "technical_indicators": "alpha_vantage",  # Use AV for indicators
        "fundamental_data": "yfinance",            # Keep yfinance for fundamentals
    }
}
```

Requires `ALPHA_VANTAGE_API_KEY` in environment.

## Related

- [[vendor-system]] - Routing and fallback architecture
- [[yahoo-finance]] - Fallback vendor
- [[configuration]] - How to set vendor preferences
