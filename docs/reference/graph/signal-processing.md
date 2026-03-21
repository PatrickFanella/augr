---
title: Signal Processing
description: Extracting the final BUY/SELL/HOLD decision from verbose agent output
type: architecture
source_file: TradingAgents/tradingagents/graph/signal_processing.py
created: 2026-03-20
---

# Signal Processing

`signal_processing.py` (32 lines) extracts a clean trading signal from the [[risk-management-team|Risk Manager's]] verbose reasoning output.

## Process

The Risk Manager produces a detailed response with full justification. `process_signal()` parses this to extract one of three values:

- **`"buy"`** - Enter a long position
- **`"sell"`** - Exit or enter a short position
- **`"hold"`** - Take no action

## Implementation

Searches the full signal text for keywords indicating the decision. The extraction is straightforward string matching against the Risk Manager's structured output format.

## Usage

Called automatically by `TradingAgentsGraph.propagate()`:

```python
state, signal = ta.propagate("AAPL", "2025-01-06")
# state = full AgentState with all reports and debate transcripts
# signal = "buy", "sell", or "hold"
```

The full state is also returned, allowing inspection of every agent's reasoning at any point in the pipeline.

## Related

- [[risk-management-team]] - Produces the raw signal
- [[state-management]] - Full state returned alongside the signal
- [[architecture]] - Final phase of the pipeline
