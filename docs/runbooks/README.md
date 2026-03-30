---
title: "Runbooks"
date: 2026-03-30
tags: [runbook, operations, incidents]
type: runbook
---

# Runbooks

Use these runbooks for the operational scenarios that recur during incidents, deploys, and investigations.

## Before you start

- Export `TRADINGAGENT_API_URL` if the API is not on `http://127.0.0.1:8080`.
- Export either `TRADINGAGENT_API_KEY` or `TRADINGAGENT_TOKEN` before using authenticated CLI or `curl` examples.
- Run commands from the repository root unless a step says otherwise.
- Save the current state before making changes during an incident so rollback is mechanical.

## Available runbooks

- [Emergency kill switch activation](emergency-kill-switch.md)
- [Circuit breaker investigation and reset](circuit-breaker.md)
- [Database backup and restore](database-backup-restore.md)
- [LLM provider outage handling](llm-provider-outage.md)
- [Broker API outage handling](broker-api-outage.md)
- [Rolling restart procedure](rolling-restart.md)
- [Adding a new strategy](add-strategy.md)
- [Investigating a bad trade](bad-trade.md)
- [Reviewing agent decisions for a run](review-agent-decisions.md)
