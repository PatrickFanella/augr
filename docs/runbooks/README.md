---
title: "Runbooks"
description: "Operational procedures for common incidents, controls, and interventions in get-rich-quick."
status: "canonical"
updated: "2026-08-20"
tags: [runbooks, operations]
---

# Runbooks

These runbooks are for operators and contributors handling a running system, an incident, or a safety intervention.

## Before you start

- Export `TRADINGAGENT_API_URL` if the API is not on `http://127.0.0.1:8080`.
- Export either `TRADINGAGENT_TOKEN` or `TRADINGAGENT_API_KEY` for authenticated CLI/API calls.
- Assume the system may already be in a degraded state; gather evidence before changing it.
- Read [Known Issues](../known-issues.md) if behavior seems inconsistent with older docs.

## Safety-first runbooks

- [Emergency kill switch activation](emergency-kill-switch.md)
- [Circuit breaker investigation and reset](circuit-breaker.md)
- [Investigating a bad trade](bad-trade.md)
- [Reviewing agent decisions for a run](review-agent-decisions.md)

## Platform and dependency runbooks

- [Broker API outage handling](broker-api-outage.md)
- [LLM provider outage handling](llm-provider-outage.md)
- [OpenCode OAuth provider](opencode-oauth-fallback.md)
- [Rolling restart procedure](rolling-restart.md)
- [Database backup and restore](database-backup-restore.md)
- [Release readiness and recovery drills](release-readiness.md)

## Routine operator tasks

- [Adding a new strategy](add-strategy.md)
- [Capital-tier and margin-policy evidence](capital-margin-policy.md)
- [Point-in-time dataset evidence](dataset-evidence.md)
- [Reproducible experiment runner](reproducible-experiment-runner.md)
- [Venue reconciliation evidence](venue-reconciliation.md)
- [Polymarket live activation (retired historical path)](polymarket-live-activation.md)

## Notes on scope

These runbooks assume the current implementation reality:

- non-secret settings persist through the backend settings store, but secrets entered through the UI do not survive restart
- WebSocket access is authenticated and tested, but public exposure still requires the same reverse-proxy and origin controls as the API
- release readiness is capability-scoped; an unavailable optional provider must remain visible without being misreported as a healthy capability
- rollout order for schema-affecting changes is migrate first, then restart app processes, then verify schema and health
