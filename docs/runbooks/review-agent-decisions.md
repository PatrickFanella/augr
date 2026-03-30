---
title: "Reviewing agent decisions for a run"
date: 2026-03-30
tags: [runbook, operations, agents]
type: runbook
---

# Reviewing agent decisions for a run

## Context

Use this runbook when you need to reconstruct why a specific run produced its final signal. This is narrower than the bad-trade runbook and focuses on the agent reasoning trail rather than execution reconciliation.

## Steps

1. Start with the run ID. If you only know the ticker and time window, list recent runs first:

   ```bash
   curl -sS \
     -H "X-API-Key: $TRADINGAGENT_API_KEY" \
     "$TRADINGAGENT_API_URL/api/v1/runs?ticker=$TICKER&limit=20&offset=0"
   ```

2. Fetch the run summary so you know the status, trade date, strategy ID, and final signal:

   ```bash
   curl -sS \
     -H "X-API-Key: $TRADINGAGENT_API_KEY" \
     "$TRADINGAGENT_API_URL/api/v1/runs/$RUN_ID"
   ```

3. Fetch the full decision list for that run:

   ```bash
   curl -sS \
     -H "X-API-Key: $TRADINGAGENT_API_KEY" \
     "$TRADINGAGENT_API_URL/api/v1/runs/$RUN_ID/decisions?limit=200&offset=0" | tee /tmp/run-decisions.json
   ```

4. Review the decisions in phase order:
   - analysis outputs
   - research debate
   - trader output
   - risk debate and final signal
5. Search memory context if you need to understand whether prior observations affected the run:

   ```bash
   tradingagent --api-url "$TRADINGAGENT_API_URL" --api-key "$TRADINGAGENT_API_KEY" \
     memories search "$TICKER"
   ```

6. If you need additional timing detail, inspect the app logs using the run ID as the correlation handle:

   ```bash
   docker compose logs --since=2h app | grep "$RUN_ID"
   ```

7. Summarize the answer in the incident or review ticket: what the analysts concluded, what the debate managers decided, what the trader proposed, and why the risk layer allowed or blocked execution.

## Verification

- The run summary and decision list line up on strategy ID, ticker, and timestamps.
- You can explain the final signal without guessing at missing steps.
- Any ambiguity is clearly narrowed to a specific decision or missing artifact instead of a vague “LLM issue.”

## Rollback

This runbook is read-only by default. If you created temporary files, delete them after the review. If you changed settings or trading state while reproducing the issue, restore the saved pre-review configuration before closing the task.
