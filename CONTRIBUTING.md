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
