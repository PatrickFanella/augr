---
title: "ADR-012: Frontend Authentication, Token Storage, and Refresh"
description: "Decision record for auth state, token storage, refresh-once, concurrent 401 handling, and browser security boundaries."
status: "proposed"
updated: "2026-06-19"
tags: [adr, frontend, auth, security]
---

# ADR-012: Frontend Authentication, Token Storage, and Refresh

- **Status:** proposed
- **Date:** 2026-06-19
- **Deciders:** Frontend architecture
- **Technical Story:** Augr trading UI foundation

## Context

Backend auth returns access and refresh JWTs in JSON. Access TTL defaults to 1 hour; refresh TTL defaults to 24 hours. Refresh tokens are stateless in reviewed source. Browser WebSocket auth currently requires `?token=`. There is no confirmed role model; breaker reset uses `X-Admin-Key`.

## Decision 1: Authentication state

- **Problem:** The app needs session bootstrap, protected route guards, current user, logout, query cache reset, and realtime lifecycle.
- **Options:** Global React context; external store; store inside query cache; route loader only.
- **Chosen:** `shared/auth` owns `AuthSession`, exposed through an app provider and narrow hooks.
- **Why it fits:** Auth state coordinates API, router, query cache, and WebSocket.
- **Consequences/tradeoffs:** Slight custom code; avoids spreading auth decisions across features.
- **Migration concerns:** Initial scaffold has no auth code, so introduce cleanly.
- **Security concerns:** Feature components receive session state only, never raw token access.
- **Unresolved dependencies:** Backend role/session model.

## Decision 2: Access-token and refresh-token storage

- **Problem:** Tokens are powerful credentials; persistence improves UX but increases XSS impact.
- **Options:** Store both in localStorage; store both in sessionStorage; access token in memory with refresh token in JS-readable session storage; access token in memory with refresh token in HttpOnly cookie.
- **Chosen:** Target architecture is access token in memory and refresh token in `HttpOnly; Secure; SameSite` cookie. Current-backend fallback is access token in memory and refresh token isolated behind `shared/auth/tokenStore` using session-only JS-readable storage if persistence is required.
- **Why it fits:** Cookie target is safer; fallback matches current JSON response without pretending it is production-ideal.
- **Consequences/tradeoffs:** Cookie target requires backend change; fallback may force re-login on tab close and carries XSS risk.
- **Migration concerns:** Keep token-store interface swappable so backend cookie migration does not touch features.
- **Security concerns:** Do not use localStorage for production token persistence; strict CSP is required if JS-readable refresh fallback is accepted.
- **Unresolved dependencies:** Backend support for HttpOnly refresh cookie and revocation/rotation.

## Decision 3: Refresh-once behavior

- **Problem:** Expired access tokens should refresh once, not loop indefinitely or retry high-risk actions unpredictably.
- **Options:** Refresh before every request; refresh after each `401`; single shared refresh promise; force logout on first `401`.
- **Chosen:** Module-scoped `refreshPromise` in `shared/auth/refresh`; one refresh attempt per failed request chain; proactive refresh before WebSocket reconnect if token expires within 60 seconds.
- **Why it fits:** Matches approved docs and prevents token-refresh stampedes.
- **Consequences/tradeoffs:** Requires careful retry classification.
- **Migration concerns:** Backend may move refresh to cookie; same refresh function can call cookie-based endpoint.
- **Security concerns:** Refresh request body must never be logged; refresh failure clears tokens, cache, and socket.
- **Unresolved dependencies:** Whether password change should revoke refresh tokens.

## Decision 4: Concurrent 401 handling

- **Problem:** Multiple requests can fail simultaneously when the access token expires.
- **Options:** Each request refreshes independently; global lock/promise; queue all requests and replay everything.
- **Chosen:** All concurrent `401`s await the same `refreshPromise`. Automatically retry only safe idempotent reads; do not automatically replay mutations unless the request was definitely not sent.
- **Why it fits:** Trading actions are high-risk and no idempotency keys exist.
- **Consequences/tradeoffs:** Users may need to manually retry a mutation after session recovery.
- **Migration concerns:** Future idempotency support may permit limited mutation replay.
- **Security concerns:** Failed refresh redirects to `/login?reason=session_expired&next=...` with safe internal next validation.
- **Unresolved dependencies:** Backend idempotency and mutation acknowledgment semantics.

## Decision 5: Authorization boundaries and admin credentials

- **Problem:** Frontend flags and route guards are not security boundaries, but the UI must prevent accidental exposure.
- **Options:** Treat all authenticated users equally; create frontend-only roles; hide actions with feature flags; wait for backend RBAC.
- **Chosen:** Treat backend as authority. Frontend uses route/action guards for UX only. `X-Admin-Key` is one-shot, memory-only, dialog-local, and never persisted.
- **Why it fits:** No backend role model is confirmed.
- **Consequences/tradeoffs:** Some admin actions remain blocked or high-friction until backend RBAC exists.
- **Migration concerns:** Replace admin-key dialog with role/session-based authorization once backend supports it.
- **Security concerns:** API keys are for programmatic access, not browser UI auth; live/risk/admin actions remain disabled by default behind flags until approved.
- **Unresolved dependencies:** RBAC, user-scoped API keys, breaker-reset UX.

## Consequences

### Positive

- Limits token exposure and centralizes session cleanup.
- Avoids automatic replay of unsafe trading mutations.
- Leaves a clear migration path to HttpOnly refresh cookies.

### Negative

- Current backend forces a less-secure fallback if persistent refresh is required before backend changes.
- Users may re-authenticate more often in the safest interim mode.
