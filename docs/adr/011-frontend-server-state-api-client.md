---
title: "ADR-011: Frontend Server State and API Client"
description: "Decision record for TanStack Query, API client, error normalization, query keys, pagination, and cancellation."
status: "proposed"
updated: "2026-06-19"
tags: [adr, frontend, api, query, errors]
---

# ADR-011: Frontend Server State and API Client

- **Status:** proposed
- **Date:** 2026-06-19
- **Deciders:** Frontend architecture
- **Technical Story:** Augr trading UI foundation

## Context

The backend exposes REST under `/api/v1`, standard error envelopes, several list-envelope variants, direct arrays, custom envelopes, and many domain structs without generated DTOs. TanStack Query is already installed.

## Decision 1: Server-state fetching and caching

- **Problem:** Operational views need cached reads, explicit stale state, refresh after mutations, and conservative behavior when realtime is degraded.
- **Options:** TanStack Query; React Router loaders as data owner; hand-rolled hooks; SWR.
- **Chosen:** One app-level TanStack Query `QueryClient`; React Router owns navigation and guards, not normal REST server state.
- **Why it fits:** Installed, mature, handles cancellation, retries, invalidation, error reset, and background refresh.
- **Consequences/tradeoffs:** Requires query-key discipline; cache freshness must be domain-specific rather than generic.
- **Migration concerns:** Start with key factories before feature hooks; avoid ad hoc keys.
- **Security concerns:** Clear sensitive query cache on logout/session expiry; label stale risk data as degraded.
- **Unresolved dependencies:** Exact domain freshness thresholds may evolve with backend SLAs.

## Decision 2: API client structure

- **Problem:** Feature modules need consistent auth, parsing, error handling, timeouts, cancellation, logging, and refresh behavior.
- **Options:** Direct `fetch`; generated client; Axios; single fetch wrapper with endpoint modules.
- **Chosen:** Single `shared/api/client` wrapping `fetch`, with endpoint wrappers in `shared/api/endpoints/*`.
- **Why it fits:** Browser `fetch` is sufficient; generated clients are blocked until schemas exist.
- **Consequences/tradeoffs:** More manual endpoint typing initially; avoids dependency and preserves control over refresh and redaction.
- **Migration concerns:** Endpoint wrappers should be compatible with a future generated client beneath the same interface.
- **Security concerns:** Central bearer injection, redacted logging, no raw request-body logging for secret endpoints.
- **Unresolved dependencies:** Backend OpenAPI/JSON schema generation.

## Decision 3: API error normalization

- **Problem:** UI must consistently handle `401`, `403`, validation, conflict, not configured, network failures, and nonstandard errors.
- **Options:** Pass raw responses; throw native `Error`; normalize into `ApiError` with kind/code/status.
- **Chosen:** Normalize every failure into `ApiError` with `kind`, `status`, `code`, `message`, optional endpoint metadata, and original redacted details.
- **Why it fits:** Approved docs require inline validation/conflict, login redirects, feature-unavailable panels for `501`, and degraded handling.
- **Consequences/tradeoffs:** Some nuance is abstracted; raw payload must remain available only when safe.
- **Migration concerns:** Nonstandard signal errors must map without assuming the standard envelope.
- **Security concerns:** User messages must not expose tokens, headers, admin keys, provider keys, or raw secret bodies.
- **Unresolved dependencies:** Backend consistency for not-configured errors.

## Decision 4: Query-key factories

- **Problem:** Uncoordinated query keys make invalidation and WebSocket cache sync unreliable.
- **Options:** Free-form arrays; string constants; domain query-key factories.
- **Chosen:** `shared/query/keys.ts` exports domain factories such as `keys.strategies.list(filters)` and `keys.runs.detail(id)`.
- **Why it fits:** WebSocket events include `strategy_id`/`run_id`; targeted invalidation needs predictable key families.
- **Consequences/tradeoffs:** Slight boilerplate; much safer invalidation.
- **Migration concerns:** Existing app has no feature queries, so start clean.
- **Security concerns:** Query keys must never include tokens, admin keys, passwords, prompts, or provider secrets.
- **Unresolved dependencies:** DTO/filter confirmation for advanced endpoints.

## Decision 5: Pagination

- **Problem:** Backend uses offset pagination, default `50`, max `100`, and sometimes omits `total` or returns custom envelopes.
- **Options:** Numbered pages always; infinite scroll always; offset helpers that adapt to `total` presence.
- **Chosen:** Offset pagination helpers with numbered pagination only when `total` is present; otherwise use load-more.
- **Why it fits:** Matches `parsePagination` and approved table conventions.
- **Consequences/tradeoffs:** Some screens will use different pagination UI depending on endpoint capability.
- **Migration concerns:** Custom Polymarket/direct-array endpoints need endpoint-specific adapters.
- **Security concerns:** Preserve filters in URL but never secrets.
- **Unresolved dependencies:** Which list endpoints will gain reliable total counts.

## Decision 6: Request cancellation

- **Problem:** Route changes/filter changes must cancel obsolete reads, but mutating requests can have unknown completion after abort.
- **Options:** Ignore cancellation; use AbortSignal from TanStack Query; custom cancellation tokens.
- **Chosen:** Every query function accepts TanStack Query `signal`; API client supports `AbortSignal` and optional timeout signals.
- **Why it fits:** Native with `fetch` and TanStack Query.
- **Consequences/tradeoffs:** Mutation abort semantics require careful UX: timeout/abort may mean completion unknown.
- **Migration concerns:** Endpoint wrappers must include signal parameters from the start.
- **Security concerns:** Do not auto-retry high-risk mutations after abort/timeout.
- **Unresolved dependencies:** Backend idempotency-key support is absent.

## Consequences

### Positive

- Centralized safety behavior and consistent stale/error states.
- Query invalidation can be driven by mutations and WebSocket events.
- Future generated clients can slot under endpoint wrappers.

### Negative

- Requires upfront shared infrastructure before visible feature work.
- Manual DTOs remain provisional until backend schemas exist.
