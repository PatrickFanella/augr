---
title: CLI Interface
description: Interactive command-line interface for running TradingAgents analyses
type: reference
source_files:
  - TradingAgents/cli/main.py
  - TradingAgents/cli/config.py
  - TradingAgents/cli/models.py
  - TradingAgents/cli/utils.py
  - TradingAgents/cli/stats_handler.py
  - TradingAgents/cli/announcements.py
created: 2026-03-20
---

# CLI Interface

Built with **Typer** and **Rich** for an interactive terminal experience. Entry point: `tradingagents` command (registered via `pyproject.toml`).

## Usage

```bash
tradingagents
```

Launches an interactive session that prompts for:

1. **Ticker symbol**: Stock to analyze (e.g. `AAPL`)
2. **Date**: Analysis date in `YYYY-MM-DD` format
3. **LLM provider**: Select from supported providers (see [[supported-models]])
4. **Model selection**: Choose deep think and quick think models
5. **Analysts**: Select which [[analyst-team|analysts]] to enable

## Components

### `main.py`

Entry point using Typer framework. Orchestrates the interactive flow, creates the `TradingAgentsGraph`, and runs `propagate()`.

### `config.py`

CLI-specific configuration defaults and validation.

### `models.py`

Data models including `AnalystType` enum mapping analyst names to their config keys:

- `MARKET` → `"market"`
- `FUNDAMENTALS` → `"fundamentals"`
- `NEWS` → `"news"`
- `SOCIAL` → `"social"`

### `utils.py`

Helper utilities for terminal output formatting, input validation, and date parsing.

### `stats_handler.py`

Callback handler that tracks and displays:

- Number of LLM calls made
- Number of tool invocations
- Token usage per agent
- Total cost estimate

### `announcements.py`

Fetches and displays project announcements (version updates, news).

### `static/`

Static assets for the terminal UI (welcome screen, branding).

## Output

The CLI displays:

- Progress through each pipeline phase (analysis → debate → trading → risk)
- Statistics on LLM and tool usage
- Final **BUY/SELL/HOLD** decision with reasoning summary

## Related

- [[getting-started]] - Setup instructions
- [[configuration]] - Configurable options
- [[overview]] - What the system does
