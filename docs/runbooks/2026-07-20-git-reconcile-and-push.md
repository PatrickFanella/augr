---
title: "Git reconciliation and exact-commit publication"
status: "canonical"
updated: "2026-08-07"
---

# Git reconciliation and exact-commit publication

Use this procedure only after the release evidence and external prerequisites
are complete. It preserves local work, reconciles the configured upstream
without rewriting history, runs the release gate on the reconciled commit, and
publishes exactly that verified object. A pre-fetch ahead/behind count uses
local tracking refs and is not authoritative.

## Guardrails

- Preserve every unrelated tracked, staged, and untracked path. Stop if the
  intended release-tree allowlist does not explain the complete status output.
- Do not reset, discard, amend, rebase, force-push, or delete branches/tags.
- Do not fetch or push until the release procedure authorizes synchronization.
- Do not use a passing gate from before fetch, merge, or conflict resolution.
- Do not deploy a commit that differs from the object verified by the final
  gate and confirmed at the remote ref.

## 1. Capture the local baseline

From the repository root, record these outputs in the release evidence:

```sh
git status --short --branch
git remote -v
git branch --show-current
git rev-parse HEAD
git rev-parse --abbrev-ref '@{upstream}'
git rev-list --left-right --count 'HEAD...@{upstream}'
```

The last count is orientation only until fetch completes. Commit all intended
release changes and leave only explicitly approved untracked files before
continuing.

## 2. Fetch and inspect authoritative divergence

Resolve the configured upstream name and remote explicitly; do not assume a
different repository or branch:

```sh
upstream_ref=$(git rev-parse --abbrev-ref '@{upstream}')
remote_name=${upstream_ref%%/*}
remote_branch=${upstream_ref#*/}
git fetch --prune "$remote_name"
git rev-list --left-right --count "HEAD...$upstream_ref"
git log --left-right --graph --decorate --oneline "HEAD...$upstream_ref"
```

Review every upstream-only commit and its affected paths before integrating
it. If the remote or upstream is unexpected, stop rather than retargeting it.

## 3. Reconcile without rewriting

- If upstream has zero new commits, retain local `HEAD`.
- If local has zero new commits, a clean-tree fast-forward is acceptable:

  ```sh
  git merge --ff-only "$upstream_ref"
  ```

- If both sides have commits, merge the fetched upstream into the current
  branch without autocommitting, inspect the complete result, then commit the
  reviewed merge:

  ```sh
  git merge --no-ff --no-commit "$upstream_ref"
  git status --short
  git diff --check
  git diff --cached --stat
  git commit --no-edit
  ```

Resolve conflicts file by file while preserving both the audit changes and
unrelated upstream work. If correct resolution is uncertain, abort only the
in-progress merge with `git merge --abort`, return to the captured baseline,
and request operator review. Never substitute a destructive reset.

## 4. Verify the reconciled candidate

Record the candidate object, run the complete gate with only the exact approved
untracked allowlist, and confirm the gate reports the same object:

```sh
candidate_commit=$(git rev-parse HEAD)
RELEASE_ALLOWED_UNTRACKED='docs/audits/2026-08-05-automation-audit-fresh.md' \
  ./scripts/release-gate.sh
test "$(git rev-parse HEAD)" = "$candidate_commit"
```

Any edit, merge, amend, commit, or conflict resolution after this point
invalidates the gate. Commit the change and rerun the entire gate.

## 5. Push the unchanged verified object

Push normally to the configured upstream—never with force—and then compare the
remote object ID with the verified candidate:

```sh
git push "$remote_name" "HEAD:$remote_branch"
remote_commit=$(git ls-remote --heads "$remote_name" "refs/heads/$remote_branch" | awk 'NR == 1 {print $1}')
test -n "$remote_commit"
test "$remote_commit" = "$candidate_commit"
```

If the push is rejected because upstream advanced, do not force it. Fetch,
reconcile the new commits, and rerun the complete gate on the new object. If
branch protection requires a review branch or pull request, stop and obtain the
operator-approved publication path; do not silently change the release target.

## 6. Deployment handoff

Record the local candidate, remote object, branch, and immutable image tags in
the release report. Build images only from `candidate_commit`, verify their
labels/digests identify that object, and deploy those exact images through the
approved production procedure.
