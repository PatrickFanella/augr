# Frontend Architecture Overview

Status: proposed  
Date: 2026-06-19  
Scope: Augr trading UI foundation only. This document does not implement feature screens or require dependency installation.

## 1. Verified repository baseline

- Frontend app root: `web/`.
- Current source: `web/src/App.tsx`, `web/src/main.tsx`, `web/src/index.css`; the app is a reset scaffold, not a feature UI.
- Package manager: npm, confirmed by `web/package-lock.json` lockfile version 3.
- Baseline stack: React 19.2, React DOM 19.2, Vite 7.2, TypeScript 5.9.
- Already available runtime dependencies: React Router DOM 7.13, TanStack Query 5.95, Recharts 3.8, Tailwind CSS 4.2, `@tailwindcss/vite`, Radix Slot, `class-variance-authority`, `clsx`, `tailwind-merge`, `lucide-react`, `react-markdown`, `tw-animate-css`.
- Already available test dependencies: Vitest 4.1, Testing Library React/Jest DOM/User Event, jsdom.
- Current Vite dev proxy: `/api` to `http://localhost:8081`, `/ws` to `ws://localhost:8081`.
- Current TypeScript posture: strict app config with `@/* -> src/*`, `verbatimModuleSyntax`, `erasableSyntaxOnly`, `noUnusedLocals`, `noUnusedParameters`, `noUncheckedSideEffectImports`.
- Current lint posture: ESLint 9 flat config with TS recommended, React Hooks, React Refresh.

Backend constraints verified from `internal/api`:

- REST base is `/api/v1`; ops routes include `/health`, `/healthz`, `/metrics`; WebSocket route is `/ws`.
- Standard API errors use `{ error, code }` from `internal/api/responses.go`.
- Collection endpoints often use `{ data, total?, limit, offset }`, with default `limit=50`, max `100`, offset default `0`.
- Login/register/refresh return `{ access_token, refresh_token, expires_at }`.
- Access JWT default TTL is 1 hour; refresh JWT default TTL is 24 hours and stateless in reviewed source.
- Browser WebSocket auth must use `?token=` unless a future ticket endpoint exists.
- WebSocket commands support subscribe/unsubscribe by strategy/run, subscribe all, and Polymarket subscription.
- WebSocket event envelope is `{ type, strategy_id?, run_id?, data?, timestamp }`; `data` is untyped.
- No backend role model is confirmed; breaker reset uses one-shot `X-Admin-Key`.
- CORS defaults may omit `PATCH`, `X-API-Key`, and `X-Admin-Key`; deployment must confirm browser preflight.

## 2. Architecture goals

1. Keep the UI operationally calm, dense, accessible, and conservative around trading risk.
2. Centralize network, auth, realtime, errors, logging, and formatting so feature screens cannot bypass safety behavior.
3. Organize source primarily by product domain and workflow, not by HTTP verb.
4. Treat backend DTO gaps honestly: `unknown` at boundaries until generated or source-derived schemas exist.
5. Use already-installed libraries where they fit; propose new dependencies only when current repository cannot cover the requirement safely.

## 3. ADR map

- ADR-010: Frontend routing, app shell, and repository organization.
- ADR-011: Server state, API client, errors, query keys, pagination, and request cancellation.
- ADR-012: Authentication, token storage, refresh, concurrent 401 handling, and authorization boundaries.
- ADR-013: WebSocket ownership, reconnect/resubscribe, event dispatch, and query-cache synchronization.
- ADR-014: UI data-entry/data-display infrastructure: tables, forms, runtime validation, charts, feature flags, env config, formatting, logging, and error boundaries.
- ADR-015: Frontend testing, mocks, accessibility, E2E, observability, and foundation rollout.

## 4. Proposed source tree

```text
web/src/
  app/
    App.tsx
    providers/
      AppProviders.tsx
      AuthProvider.tsx
      QueryProvider.tsx
      RealtimeProvider.tsx
      ErrorBoundaryProvider.tsx
    router/
      router.tsx
      routes.tsx
      guards.tsx
      routePaths.ts
    config/
      env.ts
      featureFlags.ts
    layout/
      AppShell.tsx
      PublicShell.tsx
  shared/
    api/
      client.ts
      endpoints/
        auth.ts
        risk.ts
        strategies.ts
        runs.ts
        portfolio.ts
        orders.ts
        trades.ts
        settings.ts
      pagination.ts
      request.ts
    auth/
      session.ts
      tokenStore.ts
      refresh.ts
      authTypes.ts
    query/
      client.ts
      keys.ts
      invalidation.ts
      freshness.ts
    websocket/
      realtimeClient.ts
      subscriptions.ts
      events.ts
      eventBus.ts
    errors/
      ApiError.ts
      normalize.ts
      errorBoundary.tsx
    components/
      data-table/
      data-chart/
      forms/
      feedback/
      layout/
      status/
    logging/
      logger.ts
      redact.ts
    formatting/
      dateTime.ts
      numbers.ts
      currency.ts
    types/
      api.ts
      domain.ts
      ids.ts
    utils/
  features/
    auth-login/
    cockpit/
    strategies/
    runs/
    portfolio/
    execution/
    risk/
    markets/
    research/
    backtests/
    automation/
    settings/
    account/
    audit/
  pages/
    cockpit/
    strategies/
    runs/
    portfolio/
    orders/
    trades/
    risk/
    markets/
    research/
    backtests/
    automation/
    settings/
    account/
    audit-log/
  test/
    fixtures/
    factories/
    mock-server/
      handlers/
      browser.ts
      node.ts
    render.tsx
    a11y.ts
```

## 5. Dependency rules

1. Feature modules may depend on shared modules.
2. Shared modules may not depend on feature modules.
3. Pages compose feature modules and shared components; pages should not own transport logic.
4. Components may not read auth tokens directly.
5. Feature components may not create independent WebSocket connections.
6. Feature components may not make unwrapped `fetch` calls.
7. Feature modules may use endpoint wrappers from `shared/api/endpoints/*`, query-key factories from `shared/query/keys`, and subscription APIs from `shared/websocket/subscriptions`.
8. `shared/auth` is the only token/session owner.
9. `shared/websocket` is the only `WebSocket` owner.
10. `shared/api/client` is the only production `fetch` owner.
11. Direct browser storage access for tokens is forbidden outside `shared/auth/tokenStore`.
12. `X-Admin-Key` may exist only in the breaker-reset dialog submit scope and must be cleared on success, error, cancel, unmount, and logout.

## 6. Boundary expectations

### `shared/api`

Owns base URL resolution, request construction, JSON parsing, error normalization, abort signals, timeout handling, auth header injection through `shared/auth`, refresh-once integration, and endpoint wrappers. It must not import feature UI.

### `shared/auth`

Owns session state, access-token memory storage, refresh-token strategy abstraction, refresh lifecycle, logout cleanup, current-user bootstrap, and auth events. It exposes token access only to `shared/api` and `shared/websocket` through narrow functions.

### `shared/query`

Owns the `QueryClient`, default retry/freshness policies, query-key factories, invalidation helpers, cache-reset on logout, and cache updates from WebSocket events.

### `shared/websocket`

Owns the single authenticated connection, reconnection/backoff, resubscription, event envelope parsing, event buffer, event bus, realtime status, and bridge to query invalidation. It never authorizes high-risk actions by itself.

### `shared/errors`

Owns `ApiError`, error-kind mapping, feature-unavailable mapping for `501`/not-configured cases, error boundary primitives, and user-safe error messages.

### `shared/components`

Owns design-system primitives and shared operational components from `docs/frontend/08-shared-component-inventory.md`: tables, charts, status badges, dialogs, JSON viewer, alerts, skeletons, empty/error states, activity drawer, and shell primitives.

### Feature modules

Own domain-specific query hooks, mutations, forms, local UI state, feature components, and route-specific composition. Feature modules use shared API/query/websocket abstractions only.

### App providers

Own provider ordering: env/flags validation, auth, query client, router, realtime, toast/error systems, and global shell context.

### Router

Owns route definitions, lazy page boundaries, protected-route guards, safe `next` redirects, URL-state parsing, and route-level error boundaries. It does not own normal REST server state.

### Test fixtures

Own canonical sample DTOs, factories, auth sessions, WebSocket events, and degraded/error states. Fixtures should be schema-derived when backend DTOs are generated.

### Mock handlers

Own MSW-style REST and WebSocket mocks once MSW is added. Mocks should represent real backend envelopes, including inconsistent envelopes where they exist.

## 7. Dependency and data-flow diagram

```text
Browser route / user action
  └─ pages/*
     └─ features/<domain>
        ├─ shared/components
        ├─ shared/query hooks + keys
        │  └─ shared/api/endpoints
        │     └─ shared/api/client
        │        ├─ shared/auth/session + refresh
        │        ├─ shared/errors/normalize
        │        └─ shared/logging/redact
        └─ shared/websocket subscriptions
           └─ single RealtimeClient
              ├─ eventBus/activity buffer
              └─ shared/query invalidation/patching

Logout/session expiry
  ├─ shared/auth clears tokens
  ├─ shared/query clears sensitive cache
  ├─ shared/websocket closes socket
  └─ router redirects to /login?next=...
```

## 8. Dependencies already available

- React/React DOM 19 for UI.
- React Router DOM 7 for routing.
- TanStack Query 5 for server state and caching.
- Recharts 3 for first charting pass.
- Tailwind CSS 4 plus `@tailwindcss/vite` for styling.
- `class-variance-authority`, `clsx`, `tailwind-merge` for component variants/classes.
- Radix Slot for composable primitives.
- `lucide-react` for status/action icons.
- `react-markdown` for markdown rendering in prompt/report areas.
- Vitest, Testing Library, jsdom for unit/component tests.

## 9. Proposed new dependencies

Do not install these during architecture planning; add only in the foundation implementation phase after approval.

| Dependency | Purpose | Justification |
| --- | --- | --- |
| `@tanstack/react-table` | Dense accessible table model | The UI requires many filterable/sortable operational tables; current repo lacks a table engine. |
| `react-hook-form` | Form state | Needed for login, settings, strategy/backtest forms, confirmation/reason dialogs with minimal rerenders. |
| `zod` | Runtime validation and typed env parsing | Needed at API/env/form boundaries until generated backend schemas exist. |
| `@hookform/resolvers` | Form/schema bridge | Standard integration between React Hook Form and Zod. |
| `msw` | Mock API and future Storybook/test mocks | Tests should mock network at the protocol boundary, not bypass the API client. |
| `@axe-core/react` or `jest-axe` | Accessibility assertions | Required to test shared components/dialogs/tables beyond manual review. |
| `@playwright/test` | E2E tests | Needed for login, token refresh, stale/offline, risk confirmations, and WebSocket flows. |
| `@sentry/react` or OpenTelemetry browser packages | Frontend observability | Proposed only after deployment target is known; must support redaction. |

Deferred or conditional:

- Chart replacement (`echarts`, `visx`, etc.) is blocked until Recharts cannot satisfy accessibility/performance requirements for dense cockpit/market charts.
- OpenAPI/JSON-schema tooling is blocked on backend schema generation decision.

## 10. Step-by-step foundation implementation plan

1. Freeze ADR approval and update package plan; do not install dependencies until the implementation task starts.
2. Add source directories and lightweight barrel rules without feature screens.
3. Add typed env parser and feature flag defaults.
4. Add shared `ApiError`, response envelopes, pagination helpers, and redaction logger.
5. Add `shared/auth` session abstraction with access token in memory and current backend-compatible refresh-token fallback.
6. Add `apiClient` with refresh-once, concurrent 401 handling, abort signals, normalized errors, and no auto-replay for mutations.
7. Add `QueryClient`, query-key factory skeletons, freshness defaults, and logout cache clear.
8. Add router with public/protected layouts, `/login`, `/cockpit` placeholder route, safe `next` handling, and route-level error boundaries.
9. Add single realtime service with connect/disconnect, backoff, subscription registry, event bus, and query invalidation hooks.
10. Add shared component foundations from docs: shell, status badge, connection status, table skeleton, empty/error states, confirmation/reason/admin-key dialogs, JSON viewer, data chart wrapper.
11. Add test harness, fixtures, and mock handler scaffolding after mock dependency approval.
12. Add ESLint restrictions for direct `fetch`, `WebSocket`, token storage, and unsafe console logging.
13. Build the first vertical slice using the approved foundation: login → protected shell → cockpit data placeholders → realtime status.

## 11. Prerequisites for implementing types and mocks

- Backend-generated OpenAPI/JSON Schema or a source-derived DTO extraction pass for domain structs returned directly by handlers.
- Stable enum confirmation for strategy, run, order, trade, risk, automation, discovery, backtest, and signal statuses.
- Event-specific WebSocket `data` payload schemas and cache-invalidation semantics.
- Confirmation of registration and guest observer product exposure.
- Confirmation of refresh-token cookie support or explicit acceptance of the temporary JS-readable refresh-token fallback.
- Production CORS allowed origins, methods, and headers, including `PATCH`, `X-API-Key`, and `X-Admin-Key` if browser-used.
- Decision on API key scoping and admin authorization model.
- Confirmation of not-configured error vocabulary across `501`, `503`, and current nonstandard service errors.
