---
title: "LLM provider outage handling"
date: 2026-03-30
tags: [runbook, operations, llm]
type: runbook
---

# LLM provider outage handling

## Context

Use this runbook when OpenAI, Anthropic, Google, OpenRouter, xAI, or Ollama becomes unavailable or starts returning degraded responses. The service exposes the active LLM configuration through `GET /api/v1/settings` and accepts updates through `PUT /api/v1/settings`.

## Steps

1. Determine whether the outage affects all model traffic or only one provider/model. If live trading quality is already degraded, activate the kill switch before making routing changes.
2. Back up the current settings payload:

   ```bash
   curl -sS \
     -H "X-API-Key: $TRADINGAGENT_API_KEY" \
     "$TRADINGAGENT_API_URL/api/v1/settings" | tee /tmp/settings-backup.json
   ```

3. Inspect the current LLM settings and confirm that at least one alternate provider reports `api_key_configured=true`:

   ```bash
   cat /tmp/settings-backup.json
   ```

4. Prepare an update payload that keeps the existing `risk` block intact and only changes the `llm` block. At minimum, set:
   - `llm.default_provider` to the fallback provider
   - `llm.deep_think_model` and `llm.quick_think_model` to models served by that provider
   - any provider-specific `base_url` override if you are routing through a backup endpoint
5. Apply the updated settings:

   ```bash
   curl -sS \
     -X PUT \
     -H "Content-Type: application/json" \
     -H "X-API-Key: $TRADINGAGENT_API_KEY" \
     --data @/tmp/settings-updated.json \
     "$TRADINGAGENT_API_URL/api/v1/settings"
   ```

6. If every remote provider is down and an Ollama host is available, switch to `ollama` and point `llm.providers.ollama.base_url` at the healthy local endpoint from `.env`.
7. Keep the old settings file until the incident is resolved so you can roll back without reconstructing the previous configuration.

## Verification

- `GET /api/v1/settings` returns the new `llm.default_provider`, models, and provider metadata.
- Error rates for new runs stop increasing after the change.
- If the current environment supports manual runs, execute one paper or smoke strategy and confirm it completes with the new provider selection.

## Rollback

1. If the fallback provider is also degraded or the model swap changes behavior unexpectedly, restore the saved payload:

   ```bash
   curl -sS \
     -X PUT \
     -H "Content-Type: application/json" \
     -H "X-API-Key: $TRADINGAGENT_API_KEY" \
     --data @/tmp/settings-backup.json \
     "$TRADINGAGENT_API_URL/api/v1/settings"
   ```

2. Re-check `GET /api/v1/settings` and confirm the prior provider and model values are restored.
3. If confidence in LLM outputs remains low, leave the kill switch active and escalate to human-only operation until provider health returns.
