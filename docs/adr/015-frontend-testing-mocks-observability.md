---
title: "ADR-015: Frontend Testing, Mocks, Accessibility, E2E, and Observability"
description: "Decision record for frontend verification strategy and rollout plan."
status: "proposed"
updated: "2026-06-19"
tags: [adr, frontend, testing, mocks, accessibility, observability]
---

# ADR-015: Frontend Testing, Mocks, Accessibility, E2E, and Observability

- **Status:** proposed
- **Date:** 2026-06-19
- **Deciders:** Frontend architecture
- **Technical Story:** Augr trading UI foundation

## Context

Vitest, Testing Library, user-event, jest-dom, and jsdom are installed. No frontend tests, fixtures, or mocks exist yet. The UI must verify auth refresh, realtime degradation, high-risk confirmations, accessibility, and backend envelope handling.

## Decision 1: Testing

- **Problem:** Foundation behavior is safety-critical and should be validated before feature screens grow.
- **Options:** Unit tests only; component tests; integration tests with mocked network; manual QA.
- **Chosen:** Vitest + Testing Library + jsdom for unit/component/integration tests, using real providers where possible.
- **Why it fits:** Tooling is already installed.
- **Consequences/tradeoffs:** Browser-only behaviors such as real WebSocket and navigation still need E2E.
- **Migration concerns:** Add `web/src/test` harness before feature tests.
- **Security concerns:** Test fixtures must not contain real secrets.
- **Unresolved dependencies:** Mock server dependency approval.

## Decision 2: Mock API behavior

- **Problem:** UI tests need realistic backend responses without bypassing API client behavior.
- **Options:** Mock endpoint functions; mock `fetch` ad hoc; MSW REST handlers; generated mocks from OpenAPI.
- **Chosen:** Propose MSW handlers for REST and WebSocket-like scenarios; do not mock `apiClient` in most UI tests.
- **Why it fits:** Exercises API client normalization, refresh, query caching, and error states.
- **Consequences/tradeoffs:** Adds dependency and handler maintenance.
- **Migration concerns:** Generated schemas can later generate fixtures/handlers.
- **Security concerns:** Mock auth tokens must be obviously fake.
- **Unresolved dependencies:** `msw` install approval and backend schema generation.

## Decision 3: Accessibility testing

- **Problem:** Tables, dialogs, status indicators, charts, and keyboard navigation are accessibility-sensitive.
- **Options:** Manual review only; Testing Library role assertions; axe-based automated checks; external audit.
- **Chosen:** Role/name/focus assertions in component tests plus proposed `jest-axe` or `@axe-core/react` smoke tests for shared components and critical dialogs.
- **Why it fits:** Approved docs require accessible operational clarity and non-color-only statuses.
- **Consequences/tradeoffs:** Automated a11y does not prove usability; manual review remains required.
- **Migration concerns:** Start with shared components before feature pages.
- **Security concerns:** Accessible labels must not include hidden secrets.
- **Unresolved dependencies:** Choose axe package during implementation.

## Decision 4: End-to-end testing

- **Problem:** Auth refresh, route guards, realtime degradation, and high-risk confirmations need full browser validation.
- **Options:** No E2E; Cypress; Playwright; hosted synthetic tests only.
- **Chosen:** Propose Playwright for P0 E2E once the foundation and mocks exist.
- **Why it fits:** Strong network control, multi-tab potential, and CI-friendly browser coverage.
- **Consequences/tradeoffs:** Adds dependency and runtime cost.
- **Migration concerns:** Start with smoke tests: login, protected redirect, token refresh, cockpit stale/offline, risk confirmation, admin-key clearing, WebSocket reconnect.
- **Security concerns:** E2E secrets must use local fake credentials only.
- **Unresolved dependencies:** CI/browser install strategy.

## Decision 5: Frontend observability

- **Problem:** Production UI needs client-side errors, degraded realtime, API failures, and user-impact signals without leaking data.
- **Options:** Console only; Sentry; OpenTelemetry browser; custom endpoint; no frontend observability.
- **Chosen:** Define an observability interface now; defer vendor/dependency until deployment target is known. Capture sanitized route, endpoint name, error kind, query key family, realtime state, and request/correlation ID if backend provides one.
- **Why it fits:** Keeps vendor optional while shaping safe events early.
- **Consequences/tradeoffs:** Initial implementation may only log locally.
- **Migration concerns:** Add transport adapter later behind the same interface.
- **Security concerns:** Redaction is mandatory before event emission; no tokens, API keys, admin keys, passwords, prompts, provider secrets, or raw bodies.
- **Unresolved dependencies:** Observability vendor, backend correlation/request IDs, deployment privacy requirements.

## Foundation implementation plan

1. Approve ADRs and dependency list.
2. Add source tree scaffolding and boundary lint plan.
3. Add env/feature flag parser.
4. Add errors/logger/redaction utilities.
5. Add auth session and token-store abstraction.
6. Add API client with refresh-once, normalized errors, pagination, and cancellation.
7. Add QueryClient and query-key factories.
8. Add router, providers, protected layouts, and error boundaries.
9. Add realtime service and event bus.
10. Add shared components needed for first vertical slice.
11. Add test harness, fixtures, and mocks after dependency approval.
12. Add E2E and accessibility checks after first vertical slice is runnable.

## Consequences

### Positive

- Verifies the risky foundation before feature scale-up.
- Keeps mocks at the network boundary.
- Provides a path to production observability without premature vendor lock-in.

### Negative

- Requires additional dependencies for MSW, axe checks, and Playwright.
- More upfront foundation work before visible product screens.
