# Prediction-market runtime contract

Kalshi and Polymarket share one paper execution contract. LLM-backed discovery
may propose research metadata, but it cannot submit an order. At run time the
native executor reloads provider market data and independently validates the
outcome side, supported template, market status and close time, executable
book, liquidity, confidence threshold, configured price ceiling, and positive
spread-adjusted probability edge.

Discovery conviction is retained as a probability proxy and is deliberately
labelled `discovery_conviction_proxy_uncalibrated`; it is not presented as a
calibrated forecast. Phase 7 owns measured calibration. A rejected gate always
produces a hold and no order.

For an approved paper order, the trade-decision journal stores the executable
snapshot, fair-probability proxy, executable price, spread, depth, gross and net
edge, evidence references, deterministic gate results, and risk decision.
Replay events then record decision creation, risk review, paper order linkage,
fill, position update, and outcome resolution. Orders and positions retain the
canonical `YES` or `NO` contract identity; event positions use
`instrument:OUTCOME` keys.

Resolved contracts cash-settle at 1 for the winning outcome and 0 otherwise.
The shared settler closes the position, records realized P&L and a payout trade,
marks the decision closed, and appends `outcome_resolved`. Polymarket resolution
processing and the hourly Kalshi settlement job call this same idempotent path.
Configured live brokers have read-only reconciliation jobs, but live execution
still requires the global, broker, strategy, and runtime gates and remains off
by default.
