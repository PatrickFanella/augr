# Operational Mutation Pattern

Status: initial pattern, validated by the paper-mode strategy pause workflow.

## Scope

This pattern applies to frontend operational mutations such as strategy pause/resume, skip-next, manual run, and risk controls. The first implementation is intentionally narrow: paper-mode strategy pause only.

## Required sequence

1. **Read current server state.** Render mutation controls only from a successful detail query.
2. **Gate availability.** Disable the action when required preconditions are absent. For the first workflow, `strategy.is_paper === true` and `strategy.status === "active"` are required.
3. **Confirm intent.** Show an accessible confirmation dialog with the entity name, ticker, current status, mode label, and operational consequence copy.
4. **Single-flight pending state.** Disable duplicate submits while the request is in flight or while server-state verification is running. Disable dialog dismissal during this confusing point.
5. **No optimistic updates.** Do not update status from local assumptions or from the POST response alone.
6. **No automatic mutation retry.** Mutations use `retry: false`; the API client only auto-refreshes/retries GET requests. If a mutation needs a fresh token, refresh before sending it.
7. **Submit mutation.** Send one POST after confirmation.
8. **Invalidate and verify.** Invalidate list/detail/run query families, then refetch detail through the query client. Display the confirmed server state from the refetch.
9. **Handle uncertain outcomes.** Network failures, timeouts, or successful POST followed by failed verification are unknown-completion states. Instruct the operator not to retry until server state is verified.

## Error mapping

- `401 unauthorized`: session no longer authorized; require sign-in before retry.
- `409 conflict`: server state changed; preserve the dialog, refetch detail, and ask operator to review.
- `400/422 validation`: preserve the dialog when operator-correctable; show the server-safe message or a generic correction prompt.
- `429 rate_limited`: preserve the dialog and tell the operator to wait before retrying.
- `500 server`: do not assume completion; verify server state before retry.
- `network`: unknown completion; do not retry until detail can be refetched.
- Runtime contract failure: treat as unsafe; do not apply local state.

## Query invalidation

For strategy mutations, invalidate at minimum:

- strategy list query family
- strategy detail query key for the entity
- running runs query
- strategy-specific run query family when present

Verification must refetch the strategy detail and render the confirmed status.

## Confirmation-dialog behavior

- Initial focus lands on the cancel action.
- Tab focus is trapped inside the dialog.
- Escape/backdrop dismiss only before submission starts.
- While submitting/verifying, disable dismissal and duplicate confirmation.
- Conflict/validation/rate-limit messages keep the dialog open when the operator can review and retry.

## Audit and toast behavior

The frontend does not fabricate audit records. It may display a local confirmation message after server-state verification. Backend audit events remain authoritative. Toasts are optional; if added later, they must not imply success until verification completes.

## Reuse readiness

This pattern is reusable for paper-mode, low-blast-radius operational mutations after endpoint-specific backend preconditions are confirmed. It is not sufficient for live/risk/admin mutations without backend RBAC, server-side precondition enforcement, idempotency/atomicity guarantees, and explicit rollback/unknown-completion runbooks.
