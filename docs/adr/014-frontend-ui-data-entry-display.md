---
title: "ADR-014: Frontend UI Data Entry and Display Infrastructure"
description: "Decision record for tables, forms, runtime validation, charts, flags, env config, formatting, logging, and error boundaries."
status: "proposed"
updated: "2026-06-19"
tags: [adr, frontend, ui, validation, logging]
---

# ADR-014: Frontend UI Data Entry and Display Infrastructure

- **Status:** proposed
- **Date:** 2026-06-19
- **Deciders:** Frontend architecture
- **Technical Story:** Augr trading UI foundation

## Context

The UI is an operational trading dashboard with dense tables, JSON inspectors, charts, settings/forms, secrets, risk confirmations, and accessibility requirements. Recharts is installed; table/form/runtime-validation dependencies are not installed.

## Decision 1: Tables

- **Problem:** Many P0/P1 screens need dense filterable tables with sorting, column visibility, pagination/load-more, row focus, keyboard access, stale states, and custom cells.
- **Options:** Hand-built tables; TanStack Table; AG Grid; browser-native tables only.
- **Chosen:** Propose `@tanstack/react-table` for table state/model; render with shared accessible components and design-system conventions.
- **Why it fits:** Headless model supports dense operational UI without AG Grid weight/licensing concerns.
- **Consequences/tradeoffs:** Must build accessible rendering and keyboard affordances ourselves.
- **Migration concerns:** Start with shared `DataTable` API before feature tables.
- **Security concerns:** Do not put secrets in column persistence or URLs.
- **Unresolved dependencies:** Dependency approval/install.

## Decision 2: Forms

- **Problem:** Login, settings, provider key replacement, strategies/backtests, confirmations, and admin-key dialogs need validation and controlled sensitive fields.
- **Options:** Controlled React state; React Hook Form; Formik; native forms only.
- **Chosen:** Propose `react-hook-form` plus shared field primitives; use native form semantics and accessible error messaging.
- **Why it fits:** Handles complex settings/JSON forms with low rerender overhead.
- **Consequences/tradeoffs:** Adds dependency and form conventions.
- **Migration concerns:** Existing app has no forms.
- **Security concerns:** Passwords, provider keys, API keys, and admin keys must clear on submit/cancel/unmount and never enter logs/query strings.
- **Unresolved dependencies:** Dependency approval/install.

## Decision 3: Runtime validation

- **Problem:** Backend returns many domain structs and `unknown` WS payloads; TypeScript alone cannot protect runtime boundaries.
- **Options:** No runtime validation; Zod; Valibot; generated validators from OpenAPI/JSON Schema.
- **Chosen:** Propose Zod for env/form/API-boundary validation until generated schemas exist; keep unknown payloads unknown when no schema exists.
- **Why it fits:** Widely used, integrates with React Hook Form, and can parse Vite env at startup.
- **Consequences/tradeoffs:** Manual schemas may drift; generated schemas should supersede them later.
- **Migration concerns:** Keep schemas in endpoint/domain modules so generated replacements are localized.
- **Security concerns:** Reject invalid env and suspicious payloads safely; avoid logging raw validation input if it may contain secrets.
- **Unresolved dependencies:** Backend schema generation decision.

## Decision 4: Charts

- **Problem:** Cockpit, portfolio, market, backtest, and research views need accessible charts with stale/missing-data conventions.
- **Options:** Existing Recharts; ECharts; Visx; custom SVG/canvas.
- **Chosen:** Use existing Recharts for the first chart wrapper (`DataChart`), behind a shared component contract.
- **Why it fits:** Already installed and sufficient for first-line/bar/area/sparkline charts.
- **Consequences/tradeoffs:** Recharts accessibility/performance must be verified; wrapper prevents lock-in.
- **Migration concerns:** If dense/interactive charts outgrow Recharts, replace internals behind `DataChart`.
- **Security concerns:** Tooltips and labels must not expose secrets/raw prompts; chart data from unknown payloads must be validated or clearly raw.
- **Unresolved dependencies:** Chart accessibility tests and final library assessment after first cockpit/market slice.

## Decision 5: Feature flags

- **Problem:** Backend routes exist for registration, guest observer, breaker reset, and live mutations, but product/security approval is unresolved.
- **Options:** Compile-time flags; runtime server config; Vite env flags; hard-code hidden.
- **Chosen:** Typed Vite env flags defaulting to conservative false: `VITE_ENABLE_LIVE_MUTATIONS=false`, `VITE_ENABLE_REGISTER=false`, `VITE_ENABLE_GUEST_OBSERVER=false`, `VITE_ENABLE_BREAKER_RESET=false`.
- **Why it fits:** Client flags control exposure without pretending to enforce authorization.
- **Consequences/tradeoffs:** Requires rebuild to change Vite flags unless a later server config endpoint is added.
- **Migration concerns:** Can move to `/settings`/server-provided capability config later.
- **Security concerns:** Flags are UX only; backend must enforce all permissions.
- **Unresolved dependencies:** Product decisions for registration/guest/live/admin exposure.

## Decision 6: Environment configuration

- **Problem:** Frontend needs API base, WS base, feature flags, app environment, and optional observability config.
- **Options:** Read `import.meta.env` ad hoc; typed parser; server-injected config JSON.
- **Chosen:** `app/config/env.ts` parses `VITE_*` with runtime validation at startup; fail fast for invalid required config.
- **Why it fits:** Prevents silent bad deployments and keeps client-exposed config explicit.
- **Consequences/tradeoffs:** Requires config schema maintenance.
- **Migration concerns:** Server-injected config can be added later for runtime deploy flexibility.
- **Security concerns:** Never expose secrets via `VITE_*`.
- **Unresolved dependencies:** Deployment environment strategy.

## Decision 7: Date, time-zone, currency, and number formatting

- **Problem:** Trading UI must display timestamps, stale ages, P&L, exposure, percentages, and quantities consistently.
- **Options:** Ad hoc `Intl`; date-fns/Luxon; shared formatter wrappers.
- **Chosen:** Shared wrappers over built-in `Intl` initially; UTC/source timezone in titles; local timezone in readable labels where appropriate; tabular numeric formatting.
- **Why it fits:** Avoids new dependency until advanced date math is needed.
- **Consequences/tradeoffs:** Built-in APIs are verbose; wrappers must be carefully tested.
- **Migration concerns:** Add date library only if complex market calendar/timezone logic requires it.
- **Security concerns:** Do not localize away audit precision; full ISO timestamp remains accessible.
- **Unresolved dependencies:** Product timezone preference for market-specific views.

## Decision 8: Logging and sensitive-data redaction

- **Problem:** Debug logs can leak tokens, API keys, provider secrets, admin keys, passwords, prompts, request bodies, or WS query strings.
- **Options:** Use `console` freely; ban logs; central logger with redaction.
- **Chosen:** Central `shared/logging/logger` with recursive redaction; direct `console.*` restricted outside bootstrap/tests.
- **Why it fits:** Supports frontend observability later without leaking secrets.
- **Consequences/tradeoffs:** Requires discipline and lint enforcement.
- **Migration concerns:** Existing app has minimal logging.
- **Security concerns:** Redact Authorization, access/refresh tokens, API/admin keys, passwords, provider keys, WS query strings, and secret request bodies.
- **Unresolved dependencies:** Observability vendor choice.

## Decision 9: Error boundaries

- **Problem:** Chart/JSON/markdown rendering and route-level failures should not crash the whole operational app.
- **Options:** Root boundary only; route boundaries; component-specific boundaries; no boundaries.
- **Chosen:** Root boundary for unrecoverable app failures, route-level boundaries for pages, component boundaries around charts, JSON viewers, markdown/prompt/report rendering.
- **Why it fits:** Keeps cockpit shell and risk state visible when a subview fails.
- **Consequences/tradeoffs:** More fallback UI paths to test.
- **Migration concerns:** Add boundaries with router setup.
- **Security concerns:** Error UI must show redacted messages and avoid raw secret payloads.
- **Unresolved dependencies:** None.

## Consequences

### Positive

- Provides a coherent UI infrastructure for dense, safe trading screens.
- Uses existing chart library first and proposes only missing dependencies.
- Centralizes formatting and redaction.

### Negative

- Requires dependency approval for tables/forms/validation/testing mocks.
- Manual validation schemas can drift until generated schemas exist.
