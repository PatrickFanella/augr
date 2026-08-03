# Git reconcile and push runbook

Scope: read-only inventory and non-destructive push planning only.

## Guardrails
- Do not modify `docker-compose.prod.yml`.
- Do not stage, commit, or rewrite branches.
- Preserve untracked 2026-07-20 plan docs.
- Do not force-push.

## Verified state
- `HEAD` = `87aba28`
- `origin/main` = `f6d59bf`
- Commit gap = `61`

## Recommended topology
1. Keep current history unchanged.
2. If remote branch policy allows, update `main` by fast-forward only.
3. Otherwise, publish the same `HEAD` to a new branch name without rewriting history.

## Validation
- Confirm the inventory report has 61 rows.
- Confirm no row references `docker-compose.prod.yml`.
- Confirm the report records the protected-file and untracked-docs guardrails.
