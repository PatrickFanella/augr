---
title: "ADR-013: Frontend WebSocket and Realtime Cache Synchronization"
description: "Decision record for WebSocket ownership, reconnect/resubscribe, event dispatch, and query-cache updates."
status: "proposed"
updated: "2026-06-19"
tags: [adr, frontend, websocket, realtime]
---

# ADR-013: Frontend WebSocket and Realtime Cache Synchronization

- **Status:** proposed
- **Date:** 2026-06-19
- **Deciders:** Frontend architecture
- **Technical Story:** Augr trading UI foundation

## Context

The backend exposes one `/ws` endpoint. Browser clients must use `?token=<access_token>`. Commands support subscribe/unsubscribe by strategy/run, subscribe all, and Polymarket subscription. Server events use a canonical type vocabulary but `data` is `unknown`.

## Decision 1: WebSocket ownership

- **Problem:** Multiple feature-created sockets would duplicate subscriptions, leak tokens, and make degraded state inconsistent.
- **Options:** Socket per page; socket per feature; one shared app-level realtime service; no WebSocket until later.
- **Chosen:** `shared/websocket` owns exactly one connection per authenticated session via `RealtimeProvider`.
- **Why it fits:** Approved brief requires one shared connection and global activity drawer.
- **Consequences/tradeoffs:** Subscription registry is more complex; feature code becomes safer.
- **Migration concerns:** Existing app has no socket code.
- **Security concerns:** Only `shared/websocket` can access tokens for `?token=`; full WS URLs must never be logged.
- **Unresolved dependencies:** Future short-lived WS ticket endpoint would reduce query-token leakage.

## Decision 2: Reconnection and resubscription

- **Problem:** Network loss, server restart, slow-consumer drops, and token expiry must not leave stale UI looking live.
- **Options:** Browser default reconnect none; fixed interval reconnect; exponential backoff with jitter; polling only.
- **Chosen:** Exponential backoff with jitter: 1s, 2s, 4s, 8s up to 30s; mark realtime degraded after 5 failed attempts; refresh token before reconnect if expiry is within 60s; resend active subscriptions after open.
- **Why it fits:** Matches frontend brief and operational safety policy.
- **Consequences/tradeoffs:** Requires visible connection state and subscription bookkeeping.
- **Migration concerns:** Add subscription APIs before feature detail pages.
- **Security concerns:** Stop reconnecting on logout or refresh failure; do not keep stale token in URL.
- **Unresolved dependencies:** Backend close codes/retry hints are not defined.

## Decision 3: WebSocket event dispatch

- **Problem:** Feature modules need realtime updates without parsing raw sockets or trusting unknown payloads.
- **Options:** Dispatch raw messages directly; event bus with typed envelope; per-feature socket callbacks; Redux-style global store.
- **Chosen:** Parse only the envelope into `WsEnvelope`; dispatch through a shared event bus and bounded activity buffer of 250 events.
- **Why it fits:** Event `data` is `unknown`, but `type`, `strategy_id`, `run_id`, and `timestamp` are stable enough for routing/invalidation.
- **Consequences/tradeoffs:** Rich event-specific UI waits for DTOs; raw JSON remains inspectable.
- **Migration concerns:** Event-specific discriminated unions can be added later under the same envelope.
- **Security concerns:** Do not derive high-risk action eligibility from untyped WS payloads alone.
- **Unresolved dependencies:** Event-specific `data` schemas and severity rules.

## Decision 4: WebSocket-to-query-cache updates

- **Problem:** Realtime should keep views fresh without corrupting canonical REST cache.
- **Options:** Patch everything optimistically; invalidate only; ignore cache; hybrid invalidation plus safe patching.
- **Chosen:** Hybrid: targeted invalidation by key family for strategy/run/risk/execution events, optional read-only cache patches only for low-risk display fields, and full REST refetch after reconnect.
- **Why it fits:** REST remains canonical for trading actions; WS gives freshness hints.
- **Consequences/tradeoffs:** More network refetches after reconnect; safer than trusting untyped payloads.
- **Migration concerns:** Add mapping table from event type to query invalidations.
- **Security concerns:** Never enable/disable risk or live actions based solely on WS data.
- **Unresolved dependencies:** Backend event semantics and payload contracts.

## Consequences

### Positive

- Single realtime status in shell.
- Feature modules cannot leak sockets or tokens.
- REST cache remains authoritative for safety-critical state.

### Negative

- Requires provider and event bus infrastructure before realtime feature screens.
- Query invalidation may over-fetch until payload schemas improve.
