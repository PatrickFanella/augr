# Contributing

Thanks for contributing to this repository.

## Agent workflow

Autonomous agents should follow the execution conventions in:

- [`docs/agent-execution-guide.md`](docs/agent-execution-guide.md)

At a minimum:

- Pick up only issues in **Ready**
- Move issues through **Ready -> In Progress -> In Review -> Done**
- Use branches named `agent/{issue-number}-{slug}`
- Open PRs that reference the issue number (for example, `Closes #123`)
- Clearly report blocked, failed, or partial completion states

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

## Dependency tracking

Cross-issue dependencies must be made explicit so contributors and agents can determine safe execution order.

### Marking dependencies in issue bodies

Use plain text references in the issue body to declare relationships:

- `Blocked by #X` — this issue cannot start until issue #X is resolved.
- `Blocks #X` — this issue must land before issue #X can begin.

Place these references near the top of the issue body, before the main description, so they are immediately visible. Multiple dependencies are listed one per line:

```
Blocked by #12
Blocked by #15
```

Both sides of a dependency should be cross-referenced. When you mark an issue as `Blocked by #X`, also add a `Blocks #Y` reference to issue #X so the relationship is discoverable from either issue.

### Applying the `workflow:blocked` label

Apply the `workflow:blocked` label to any issue that **cannot be started** because a dependency is unresolved. Remove the label once all blockers are merged or closed.

Do **not** apply `workflow:blocked` to an issue that is merely waiting for review — reserve it for hard dependencies where starting work would be premature.

### Representing dependencies in GitHub Project

In the GitHub Project board:

- Issues carrying `workflow:blocked` should remain in **Ready** (not **In Progress**) until their blockers are resolved.
- Use the issue body references (`Blocked by #X`) as the authoritative source of truth for the dependency relationship.
- Optionally group or filter the project board by the `workflow:blocked` label to produce a blockers view.

### Surfacing blockers in project views

To make blocked work visible at a glance:

1. Add a **Group by: Label** view and pin the `workflow:blocked` group at the top.
2. Add a filtered view named **Blockers** using the filter `label:workflow:blocked` so the queue of blocked issues is always one click away.
3. When triaging, review the **Blockers** view first: close or merge blocking issues before assigning downstream work to agents.

## Pull request expectations

- Link the issue being addressed
- Use labels that match the work (`type:*`, `priority:*`, `phase:*`, etc.)
- Confirm changes are scoped to the issue
- Ensure documentation is updated when behavior or process changes

## Pre-commit hooks

This repository uses [pre-commit](https://pre-commit.com/) to run formatting and lint checks automatically on `git commit`.

### One-time setup

1. Install `pre-commit`:
   - `pip install pre-commit`
2. Install hook tools used by this repository:
   - `gofumpt` is installed automatically by `pre-commit`
   - Install `golangci-lint` (see: https://golangci-lint.run/welcome/install/)
3. Install hooks in your local clone:
   - `pre-commit install`

### Hook behavior

- **Go files (`*.go`)**
  - `gofumpt` checks formatting and blocks commits for unformatted files (run `gofumpt -w .` or `gofumpt -w <file>` to fix)
  - `golangci-lint` runs lint checks and blocks commits on lint errors
  - If `go.mod` is not present yet, `golangci-lint` is skipped
- **TypeScript/JavaScript files (`*.ts`, `*.tsx`, `*.js`, `*.jsx`, `*.mjs`, `*.cjs`)**
  - ESLint runs when a frontend `package.json` is present
- **Formatting files (`*.ts`, `*.tsx`, `*.js`, `*.jsx`, `*.mjs`, `*.cjs`, `*.json`, `*.yaml`, `*.yml`, `*.md`)**
  - Prettier `--check` runs when a frontend `package.json` is present

### Useful commands

- Run hooks for staged files (same behavior as commit): `pre-commit run`
- Run hooks for all files: `pre-commit run --all-files`
