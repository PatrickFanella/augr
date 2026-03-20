# Contributing

## Branch strategy

Use short-lived branches and open a pull request (PR) early.

### Branch naming convention

Use lowercase branch prefixes with a concise kebab-case description:

- `feat/<description>` for new functionality
- `fix/<description>` for bug fixes
- `infra/<description>` for infrastructure/tooling/workflow changes
- `docs/<description>` for documentation-only changes
- `test/<description>` for test-only changes
- `chore/<description>` for maintenance tasks that do not fit the categories above

Examples:

- `feat/trading-agent-order-routing`
- `fix/portfolio-risk-threshold`
- `infra/label-sync-workflow`
- `docs/branch-strategy`

## Merge strategy

`main` uses **Squash and merge** as the default strategy.

Why:

- Keeps commit history clean and readable
- Ensures each merged PR maps to one logical change on `main`
- Avoids noisy merge commits from short-lived branches

Guidance:

- Keep PRs focused and reasonably small
- Write a clear PR title and summary (the squash commit message should describe the change)
- Rebase or merge `main` into your branch as needed while the PR is open to resolve conflicts before merge

## Recommended protection rules for `main`

Define the following branch protection settings for `main`:

1. **Require a pull request before merging**
   - Require at least 1 approving review
   - Dismiss stale approvals when new commits are pushed
   - Require review from code owners when CODEOWNERS is present
2. **Require status checks to pass before merging**
   - Require branches to be up to date before merging
   - Add required checks as CI is introduced
3. **Require conversation resolution before merging**
4. **Restrict direct pushes to `main`**
5. **Prevent force pushes**
6. **Prevent branch deletion**

## Pull request expectations

- Link the issue being addressed
- Use labels that match the work (`type:*`, `priority:*`, `phase:*`, etc.)
- Confirm changes are scoped to the issue
- Ensure documentation is updated when behavior or process changes
