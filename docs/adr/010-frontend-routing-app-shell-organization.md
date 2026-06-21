---
title: "ADR-010: Frontend Routing, App Shell, and Repository Organization"
description: "Decision record for Augr frontend routing, app shell ownership, and source organization."
status: "proposed"
updated: "2026-06-19"
tags: [adr, frontend, routing, react, organization]
---

# ADR-010: Frontend Routing, App Shell, and Repository Organization

- **Status:** proposed
- **Date:** 2026-06-19
- **Deciders:** Frontend architecture
- **Technical Story:** Augr trading UI foundation

## Context

The repository baseline is React 19, Vite 7, TypeScript 5.9, and React Router DOM 7. The app source is currently a reset scaffold. Approved IA makes `/cockpit` the authenticated operational landing page and defines persistent chrome with risk, environment, user, and realtime state.

## Decision 1: Routing

- **Problem:** The UI needs protected routes, lazy page boundaries, safe redirects, URL-state filters, route-level errors, and deep links for incident workflows.
- **Options:** React Router DOM 7; TanStack Router; file-based routing; custom router.
- **Chosen:** React Router DOM 7 with `createBrowserRouter`, protected layout routes, lazy route modules, and URL query-state for filters/tabs/pagination.
- **Why it fits:** It is already installed and supports the documented IA without adding dependencies.
- **Consequences/tradeoffs:** Avoids new router dependency; gives less compile-time route typing than TanStack Router.
- **Migration concerns:** If route typing becomes painful, migrate route constants and params first, then consider TanStack Router later.
- **Security concerns:** Validate `next` as same-origin internal paths only; never place tokens, admin keys, or secrets in URLs.
- **Unresolved dependencies:** Product decisions for `/register` and `/observer` exposure.

## Decision 2: Repository organization

- **Problem:** The UI spans many domains; organizing by HTTP verb would scatter workflows and encourage unwrapped transport calls.
- **Options:** Layer-only (`components/api/pages`); product-domain feature slices; endpoint/HTTP-verb folders; atomic design only.
- **Chosen:** Product-domain feature slices under `features/*` and `pages/*`, with hard shared boundaries under `shared/*` and app composition under `app/*`.
- **Why it fits:** Trading workflows are domain/incident driven: strategies, runs, risk, execution, portfolio, markets, research, admin.
- **Consequences/tradeoffs:** Some shared abstractions require discipline and lint rules; feature folders may duplicate small UI patterns before promotion to shared.
- **Migration concerns:** Existing `web/src` is minimal, so migration cost is low.
- **Security concerns:** Enforce that only shared modules own tokens, network, WebSocket, errors, and redaction.
- **Unresolved dependencies:** ESLint restrictions need an implementation pass.

## Dependency rules

- Feature modules may depend on shared modules.
- Shared modules may not depend on feature modules.
- Components may not read auth tokens directly.
- Feature components may not create independent WebSocket connections.
- Feature components may not make unwrapped `fetch` calls.
- Router and providers compose; they do not bypass shared API/auth/query/websocket modules.

## Consequences

### Positive

- Uses installed router dependency.
- Supports approved IA and incident deep links.
- Creates enforceable boundaries before feature screens exist.

### Negative

- Route params are not fully type-safe without extra conventions.
- Boundary enforcement depends on lint/review until tooling is added.

### Neutral

- Guest/public routes remain scaffolded but hidden until product approval.
