# Frontend vertical slice security and QA review

Date: 2026-06-20

Scope: implemented authentication, shared API client, app shell, shared WebSocket client, read-only cockpit, tests, mocks, and relevant backend auth/WebSocket handlers. This review did not expand product scope or add feature screens.

## Findings

### Critical

- **Public backend registration is enabled.** Relevant backend handlers expose public registration that mints tokens immediately. The current frontend does not link to registration, but the backend route is still a production blocker if exposed beyond a trusted development environment. This review did not change backend product policy; registration must be disabled, invite-gated, or admin-gated before production.

### High

- **In-flight refresh could restore tokens after logout.** `refreshAccessToken()` wrote refreshed tokens without checking whether logout/session cleanup happened during the refresh.
- **Refresh-token-only session restore failed after reload.** The token store preserved the refresh token in session storage, but bootstrap treated missing in-memory access token as anonymous instead of refreshing once.
- **WebSocket connect/reconnect had stale-connection races.** A pending refresh could finish after logout and still open a socket; stale socket callbacks could also alter current connection state.
- **Non-GET requests could be replayed by the generic API client.** `api.post()` disabled retry, but direct `apiRequest()` with `PUT`, `PATCH`, or `DELETE` would retry after a 401.

### Medium

- **Login bootstrap failure left tokens installed.** Successful token issuance followed by `/me` failure could leave token storage populated while the UI reported login failure.
- **WebSocket subscriptions were command-log based.** Duplicate commands could accumulate and be replayed on reconnect. The current slice only uses `subscribe_all`, so the bounded fix dedupes commands; future feature-level subscribe/unsubscribe should move to desired-state sets.
- **Mock code was included in production output as a lazy chunk.** Production did not execute mocks, but the dynamic import path was not dev-gated.
- **`/login?next=/login` could redirect an authenticated user back to login.** External redirects were blocked, but self-loop login redirects were not.
- **Cockpit ARIA IDs were generated from visible titles with spaces.** This was fragile for assistive technologies.
- **Global paragraph color reduced contrast inside light panels.** The global `p` color came from the old dark scaffold.

### Low

- **Raw backend error strings are rendered in cockpit widget alerts.** React escapes content, so this is not XSS, but production UX should eventually map server errors to safer operator-facing summaries.
- **Realtime events do not yet invalidate TanStack Query caches.** Polling keeps the read-only cockpit current enough for this slice; mutation work should add bounded event-to-query invalidation.
- **Backend WebSocket still uses query-token auth.** Frontend avoids logging token-bearing URLs, but proxy/access-log redaction and a future WS-ticket or cookie-backed auth flow remain needed.

### Informational

- No `dangerouslySetInnerHTML`, `innerHTML`, `eval`, or JSON/event payload HTML rendering was found in the frontend slice.
- Unknown enum/status strings are preserved by schemas and rendered as text.
- The cockpit shows independent widget failures instead of collapsing the page.
- Accessibility automation such as axe or Playwright is not configured in this package.

## Issues fixed in this pass

- Added auth epoch checks so an in-flight refresh cannot reinstall credentials after logout/session cleanup.
- Added refresh-token-only bootstrap: page reload with a session refresh token refreshes once, then calls `/me`.
- Restricted automatic 401 refresh/retry to `GET` requests only.
- Cleared tokens if login token issuance succeeds but `/me` bootstrap fails.
- Added WebSocket connection epoch guards, stale-handler checks, existing-socket cleanup before reconnect, and handler removal on disconnect.
- Deduped repeated WebSocket subscription commands for reconnect replay.
- Dev-gated browser MSW imports so production builds do not include mock chunks; production explicit mocks still fail closed.
- Rejected `/login` as a post-auth `next` redirect target.
- Switched cockpit panel labelling to generated IDs.
- Removed low-contrast global paragraph color.

## Tests added or corrected

- Concurrent expired access-token requests refresh once and queued requests resume.
- Logout during pending refresh does not reinstall tokens.
- Non-GET request is not replayed after a 401.
- Refresh-token-only session restore after reload.
- Invalid credential messaging remains generic.
- `/login?next=/login` falls back to cockpit.
- Refresh failure logs the user out.
- WebSocket reconnect triggers token refresh and restores subscriptions.
- REST cockpit data remains visible while WebSocket is down/reconnecting.
- One cockpit service returning 501 renders feature unavailable.
- One cockpit service returning 500 leaves other widgets visible.
- Unsafe-looking HTML in event type/data is rendered as escaped text and does not create DOM nodes.
- More than 250 events are capped at 250.
- Unknown risk status values render without crashing.

## Remaining risks

- Public backend registration remains a production blocker.
- Refresh tokens remain JavaScript-readable session-storage tokens until the backend supports HttpOnly cookie or server-tracked rotating refresh sessions.
- WebSocket auth still uses token-bearing query strings.
- Backend WebSocket authorization is all-or-nothing; `subscribe_all` is unsuitable for multi-user/RBAC scenarios.
- Query invalidation from realtime events is deferred until mutation/read-write flows need stronger freshness guarantees.
- Accessibility coverage is manual/test-library only; axe or Playwright accessibility checks are not installed.

## Recommendation for mutation readiness

The foundation is improved and acceptable for continued read-only P0 cockpit work. It is **not ready for operational mutations** until the remaining auth/session and backend authorization risks are resolved or explicitly risk-accepted, especially public registration, JS-readable refresh tokens, query-token WebSocket auth, and mutation-specific idempotency/verification behavior.
