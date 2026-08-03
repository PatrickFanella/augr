# Recovery: Timescale, Source Control, and JWT Secret Implementation Plan

> **For agentic workers:** Execute this plan step-by-step. Use checkbox (`- [ ]`) syntax exactly as written. Do not stage or overwrite `docker-compose.prod.yml` (user-owned). Do not perform any destructive restore, force-push, or secret disclosure.

**Goal:** Complete three recovery actions safely: (1) fix Timescale backup/restore fidelity, (2) reconcile and safely push the 61-commit local-vs-origin history, and (3) rotate `JWT_SECRET` with explicit session invalidation and rollback coverage.

**Architecture:** This plan is split into three independently gated tracks. Timescale recovery begins with catalog/table-of-contents inspection, then a same-version clean-target logical rehearsal, then a fallback physical backup/volume snapshot evaluation. Git recovery begins with fetch + ref preservation, complete 61-commit inventory/classification, then a safe linearization plan that may use cherry-pick or rebase only after review; no force push. JWT rotation is a secret-only deployment change with validation on login/refresh/me/WebSocket flows, and a documented rollback path. Production restore/push/secret cutover only happen after all gates pass and the operator explicitly approves. Git reconciliation and the approved push must finish before JWT cutover begins.

**Tech Stack:** PostgreSQL + TimescaleDB, Docker Compose, existing backup/restore runbooks, Git, deployment environment secrets, and existing auth/session services.

---

## Hard safety gates

- [ ] Do **not** restore production data until the same-version clean-target rehearsal passes row/chunk/schema parity gates.
- [ ] Do **not** overwrite, stage, or revert `docker-compose.prod.yml`.
- [ ] Do **not** force-push; use only safe fetch/rebase/cherry-pick/merge flows.
- [ ] Do **not** print, log, or commit `JWT_SECRET` or any derived secret material.
- [ ] Do **not** invalidate production sessions until the operator approves the JWT rotation window.
- [ ] Approval checkpoint A: operator confirms the Timescale rehearsal target, verified physical rollback artifact, external repair audit, exact source-index repair/reindex scripts, and restore command set before any mutation is executed. Item 1 passes only after completed repair plus fresh logical restore parity.
- [ ] Approval checkpoint B: operator confirms the Git commit inventory and push strategy before any remote update is attempted.
- [ ] Approval checkpoint C: operator confirms the JWT rotation window and expected forced logout behavior before secret deployment.

---

## Task 1: Fix Timescale backup/restore fidelity

**Purpose:** Rehearse restore fidelity on a same-version clean target, prove the current duplicate hypertable-key issue in a non-production target, and decide whether a physical fallback is needed.

**Files:**
- Create: `docs/runbooks/2026-07-20-timescale-backup-restore-fidelity.md`
- Create: `docs/reports/2026-07-20-timescale-backup-restore-fidelity.md`
- Create: `docs/reports/2026-07-20-timescale-backup-restore-fidelity.sql`
- Create: `docs/reports/2026-07-20-timescale-backup-restore-fidelity.test.sh`

- [ ] **Step 1: Inspect the backup/restore table of contents before any restore.**

Run a TOC inspection on the source backup artifact and preserve the raw output. Use the exact backup path supplied by the operator at runtime.

```bash
pg_restore -l "$SOURCE_BACKUP_PATH" > docs/reports/2026-07-20-timescale-backup-restore-fidelity.toc.txt
```

- [ ] **Step 2: Record the current failure signature and source-side duplicate state.**

Capture the current restore blocker as part of the report: restore is **BLOCKED** by duplicate hypertable keys on the target side. Preserve evidence from the failed restore attempt and any catalog/query output that demonstrates the exact source duplicate groups, counts, and conflict classifications.

- [ ] **Step 3: Build a same-version clean-target logical rehearsal.**

Create an empty rehearsal database/container using the **same PostgreSQL/Timescale version** as production. Restore only into that clean target.

```bash
createdb "$REHEARSAL_DB_NAME"
pg_restore \
  --verbose \
  --clean \
  --if-exists \
  --no-owner \
  --no-privileges \
  -d "$REHEARSAL_DB_NAME" \
  "$SOURCE_BACKUP_PATH" \
  > docs/reports/2026-07-20-timescale-backup-restore-fidelity.restore.log 2>&1
```

- [ ] **Step 4: Capture concrete source/target parity gates.**

Verify all three parity layers by comparing source and rehearsal target directly:

  - per-table row counts for every application table on source vs target
  - Timescale hypertable and chunk inventories on source vs target
  - schema parity for extensions, hypertables, chunks, public indexes, public constraints, and public function signatures

Use explicit SQL generation for both sides and keep the result artifacted. Generate stable TSVs by emitting one `SELECT '<schema.table>', count(*) FROM schema.table` per public app table via `psql \gexec`, then diff the source and target outputs. Do **not** count `pg_class` rows. Use consistent artifact base names and `.tsv` extensions for generation and diff commands. Example gate queries:

```sql
-- Source and target table row counts, run once on each database and diff the outputs.
SELECT format('SELECT %L AS table_name, count(*) AS row_count FROM %I.%I;',
              n.nspname || '.' || c.relname,
              n.nspname,
              c.relname)
FROM pg_namespace n
JOIN pg_class c ON c.relnamespace = n.oid
WHERE c.relkind = 'r'
  AND n.nspname = 'public'
  AND c.relname IN (SELECT tablename FROM pg_tables WHERE schemaname = 'public')
ORDER BY 1;

-- Timescale hypertable and chunk inventories, run once on each database and diff the outputs.
SELECT hypertable_schema,
       hypertable_name,
       count(*) AS chunk_count
FROM timescaledb_information.chunks
GROUP BY 1, 2
ORDER BY 1, 2;

SELECT hypertable_schema,
       hypertable_name,
       chunk_schema,
       chunk_name
FROM timescaledb_information.chunks
ORDER BY 1, 2, 3, 4;

-- Schema object inventory parity for extensions, public indexes, public constraints, and public function signatures.
SELECT object_type, object_schema, object_name, object_signature
FROM (
  SELECT 'extension' AS object_type, extnamespace::regnamespace::text AS object_schema, extname AS object_name, NULL::text AS object_signature
  FROM pg_extension
  UNION ALL
  SELECT 'index', schemaname, indexname, NULL::text FROM pg_indexes WHERE schemaname = 'public'
  UNION ALL
  SELECT 'constraint', n.nspname, c.conname, NULL::text
  FROM pg_constraint c
  JOIN pg_class rel ON rel.oid = c.conrelid
  JOIN pg_namespace n ON n.oid = rel.relnamespace
  WHERE n.nspname = 'public'
  UNION ALL
  SELECT 'function', n.nspname, p.proname, pg_get_function_identity_arguments(p.oid)
  FROM pg_proc p
  JOIN pg_namespace n ON n.oid = p.pronamespace
  WHERE n.nspname = 'public'
) objects
ORDER BY 1, 2, 3, 4;
```

Run each side with a `psql \gexec` flow that writes stable TSVs named `docs/reports/2026-07-20-timescale-backup-restore-fidelity.source.row-counts.tsv` and `docs/reports/2026-07-20-timescale-backup-restore-fidelity.target.row-counts.tsv`, plus matching artifacts for `timescale-hypertables.tsv`, `timescale-chunks.tsv`, `public-indexes.tsv`, `public-constraints.tsv`, and `public-function-signatures.tsv`, then diff those TSVs directly using the same base names and `.tsv` extension on both source and target.

- [ ] **Step 5: Validate the duplicate hypertable-key blocker on rehearsal only.**

If the logical restore still fails on the clean target with duplicate hypertable keys, document the exact object/key pair and stop. Do **not** attempt a production restore.

Use concrete diff commands for the artifacts, for example:

```bash
diff -u docs/reports/2026-07-20-timescale-backup-restore-fidelity.source.row-counts.tsv docs/reports/2026-07-20-timescale-backup-restore-fidelity.target.row-counts.tsv
diff -u docs/reports/2026-07-20-timescale-backup-restore-fidelity.source.timescale-hypertables.tsv docs/reports/2026-07-20-timescale-backup-restore-fidelity.target.timescale-hypertables.tsv
diff -u docs/reports/2026-07-20-timescale-backup-restore-fidelity.source.timescale-chunks.tsv docs/reports/2026-07-20-timescale-backup-restore-fidelity.target.timescale-chunks.tsv
diff -u docs/reports/2026-07-20-timescale-backup-restore-fidelity.source.public-indexes.tsv docs/reports/2026-07-20-timescale-backup-restore-fidelity.target.public-indexes.tsv
diff -u docs/reports/2026-07-20-timescale-backup-restore-fidelity.source.public-constraints.tsv docs/reports/2026-07-20-timescale-backup-restore-fidelity.target.public-constraints.tsv
diff -u docs/reports/2026-07-20-timescale-backup-restore-fidelity.source.public-function-signatures.tsv docs/reports/2026-07-20-timescale-backup-restore-fidelity.target.public-function-signatures.tsv
```

- [ ] **Step 6: Evaluate fallback physical recovery options.**

If logical restore fidelity cannot be achieved safely, evaluate a fallback path and document tradeoffs:

  - `pg_basebackup` from a consistent physical source
  - filesystem/volume snapshot restore from the live data directory
  - operational RPO/RTO, storage, and maintenance-window implications

Use the following evaluation commands only for rehearsal/planning unless the operator later approves the chosen path:

```bash
pg_basebackup -h "$PRIMARY_HOST" -p "$PRIMARY_PORT" -D "$PG_BASEBACKUP_TARGET" -Fp -Xs -P -R
```

- [ ] **Step 7: Produce the fidelity runbook and decision record.**

The runbook must state:

  - source duplicate groups are explicitly enumerated and classified
  - restore is currently blocked by duplicate hypertable keys on the target path
  - no production restore before a passing rehearsal
  - physical fallback is only an evaluated contingency, not a default
  - parity gates must pass before any production action

- [ ] **Step 8: Approval checkpoint A.**

Operator reviews the rehearsal logs, TOC output, parity results, and fallback evaluation before any production restore is considered.

- [ ] **Step 9: Commit the documentation artifacts.**

```bash
git add docs/runbooks/2026-07-20-timescale-backup-restore-fidelity.md docs/reports/2026-07-20-timescale-backup-restore-fidelity.md docs/reports/2026-07-20-timescale-backup-restore-fidelity.sql docs/reports/2026-07-20-timescale-backup-restore-fidelity.test.sh docs/reports/2026-07-20-timescale-backup-restore-fidelity.toc.txt
git commit -m "docs(ops): add timescale backup restore fidelity plan"
```

---

## Task 2: Reconcile and safely push 61 local commits

**Purpose:** Preserve current refs, inventory every local commit ahead of `origin/main`, classify what belongs in the final push, and update the remote safely without force-pushing.

**Files:**
- Create: `docs/runbooks/2026-07-20-git-reconcile-and-push.md`
- Create: `docs/reports/2026-07-20-git-history-inventory.md`
- Create: `docs/reports/2026-07-20-git-history-inventory.txt`
- Create: `docs/reports/2026-07-20-git-history-inventory.test.sh`

- [ ] **Step 1: Fetch and preserve refs before any rewrite.**

```bash
git fetch origin
git show-ref --heads --tags --dereference > docs/reports/2026-07-20-git-show-ref.txt
git branch "backup/recovery-$(date +%Y%m%d-%H%M%S)" HEAD
git rev-parse HEAD origin/main
git status --short -- docker-compose.prod.yml
```

- [ ] **Step 2: Record the verified current state.**

Capture the current state in the report:

  - `HEAD` = `87aba28`
  - `origin/main` = `f6d59bf`
  - local branch is 61 commits ahead of origin
  - preserve `docker-compose.prod.yml` as user-owned and untouched
  - confirm the current refs before any rewrite by comparing fetched `origin/main` with local `HEAD`

- [ ] **Step 3: Inventory all 61 commits.**

Generate a complete ordered commit list from `origin/main..HEAD` and preserve it raw.

```bash
git log --oneline --decorate --reverse origin/main..HEAD > docs/reports/2026-07-20-git-history-inventory.txt
```

- [ ] **Step 4: Classify commits safely.**

For each of the 61 commits, classify into one of these buckets:

  - safe-to-push as-is
  - needs reorder/rebase to keep history linear
  - should be squashed into an earlier logical change
  - should be dropped because it is redundant/experiments-only
  - touches user-owned `docker-compose.prod.yml` and must not be staged or overwritten

Record the classification table in the markdown report with commit hash, subject, risk note, and final disposition.

- [ ] **Step 5: Decide the safe topology.**

Choose the safest of:

  - fast-forward if and only if local history is already cleanly based on `origin/main`
  - interactive rebase onto `origin/main` if commits need linear cleanup
  - cherry-pick onto a fresh branch if history is too tangled or contains risky side branches

Avoid any rewrite that risks losing work; keep a backup branch reference regardless.

- [ ] **Step 6: Reconcile the branch without force-pushing.**

If rebase or cherry-pick is required, perform it on a new branch or the current branch only after the backup ref exists. Example safe patterns:

```bash
git checkout -b "recovery/reconcile-$(date +%Y%m%d-%H%M%S)"
git rebase origin/main
```

or

```bash
git checkout -b "recovery/cherry-pick-$(date +%Y%m%d-%H%M%S)" origin/main
git cherry-pick <commit1> <commit2> <commit3>
```

- [ ] **Step 7: Run full tests before any push request.**

Use the project’s full test suite appropriate to the repository, plus any impacted focused tests discovered during classification. The default gate is:

```bash
go test ./... -count=1
```

If the repository has additional non-Go checks, run them too and record pass/fail in the report.

- [ ] **Step 8: Prepare a push proposal and require approval.**

Document exactly what will be pushed:

  - target branch name
  - commit range/hash list
  - whether the remote will be fast-forwarded or receive a new branch
  - confirmation that no force push will be used
  - confirmation that `docker-compose.prod.yml` was neither staged nor rewritten

Do not push until the operator signs off on the proposal.

- [ ] **Step 9: Commit the reconciliation artifacts.**

```bash
git add docs/runbooks/2026-07-20-git-reconcile-and-push.md docs/reports/2026-07-20-git-history-inventory.md docs/reports/2026-07-20-git-history-inventory.txt docs/reports/2026-07-20-git-history-inventory.test.sh
git commit -m "docs(ops): add git reconcile and push plan"
```

- [ ] **Step 10: Push safely after approval only.**

```bash
git push origin "HEAD:refs/heads/$TARGET_BRANCH"
```

If a different push form is approved, use it only if it remains non-destructive and non-forceful.

---

## Task 3: Rotate JWT secret safely

**Purpose:** Replace the deployment `JWT_SECRET` without printing it, accept that all existing sessions will be invalidated, verify all auth surfaces, and retain a rollback path.

**Files:**
- Create: `docs/runbooks/2026-07-20-jwt-secret-rotation.md`
- Create: `docs/reports/2026-07-20-jwt-secret-rotation.md`
- Create: `docs/reports/2026-07-20-jwt-secret-rotation.test.sh`

- [ ] **Step 0: Prepare rollback, capture an old-session canary, and obtain Approval checkpoint C.**

Before changing the secret, verify the rollback location is protected, capture a valid pre-rotation access token into `OLD_ACCESS_TOKEN` without printing it, confirm that token currently succeeds against `/api/v1/me`, review the forced-logout notice, and obtain the operator's explicit approval for the cutover window. Do not continue to Step 1 without that approval.

```bash
umask 077
install -d -m 700 /tmp/opencode
export OLD_LOGIN_RESPONSE=$(curl -fsS -X POST "$APP_BASE_URL/api/v1/auth/login" -H 'Content-Type: application/json' -d "{\"username\":\"$TEST_LOGIN_USERNAME\",\"password\":\"$TEST_LOGIN_PASSWORD\"}")
export OLD_ACCESS_TOKEN=$(python3 - <<'PY'
import json, os
print(json.loads(os.environ['OLD_LOGIN_RESPONSE'])['access_token'], end='')
PY
)
curl -fsS "$APP_BASE_URL/api/v1/me" -H "Authorization: Bearer $OLD_ACCESS_TOKEN" >/dev/null
```

- [ ] **Step 1: Generate a new secret without printing it.**

Generate exactly one runtime-only secret in the protected temporary file created by the next command block. Never write the secret into the plan, logs, shell history, repo, or a second staging path.

Immediately update `JWT_SECRET` atomically inside the actual `/srv/server/projects/augr/.env` without printing any secret material, preserving all other keys and comments. Back up only the old `JWT_SECRET` value to a mode-600 temp file, not the full `.env`, then recreate the app:

```bash
set -euo pipefail
umask 077
install -d -m 700 /tmp/opencode
old_jwt_secret=$(sudo python3 - <<'PY'
from pathlib import Path
import re
env_path = Path('/srv/server/projects/augr/.env')
env = env_path.read_text()
matches = re.findall(r'^JWT_SECRET=(.*)$', env, re.M)
if len(matches) != 1 or not matches[0].strip():
    raise SystemExit('expected exactly one non-empty JWT_SECRET in production .env')
print(matches[0], end='')
PY
)
test -n "$old_jwt_secret"
printf '%s' "$old_jwt_secret" > /tmp/opencode/jwt-secret-rollback.env
chmod 600 /tmp/opencode/jwt-secret-rollback.env
openssl rand -hex 32 > /tmp/opencode/jwt-secret.new
chmod 600 /tmp/opencode/jwt-secret.new
sudo python3 - <<'PY'
import os
from pathlib import Path
import re
import tempfile
env_path = Path('/srv/server/projects/augr/.env')
new_secret = Path('/tmp/opencode/jwt-secret.new').read_text().strip()
if not new_secret:
    raise SystemExit('generated JWT secret is empty')
lines = env_path.read_text().splitlines()
matches = [line for line in lines if line.startswith('JWT_SECRET=')]
if len(matches) != 1:
    raise SystemExit('expected exactly one JWT_SECRET before cutover')
out = []
replaced = False
for line in lines:
    if line.startswith('JWT_SECRET=') and not replaced:
        out.append(f'JWT_SECRET={new_secret}')
        replaced = True
    else:
        out.append(line)
st = env_path.stat()
fd, tmp_name = tempfile.mkstemp(prefix='.env.jwt-', dir=env_path.parent)
try:
    with os.fdopen(fd, 'w') as tmp:
        tmp.write('\n'.join(out) + '\n')
        tmp.flush()
        os.fsync(tmp.fileno())
    os.chmod(tmp_name, st.st_mode)
    os.chown(tmp_name, st.st_uid, st.st_gid)
    os.replace(tmp_name, env_path)
finally:
    if os.path.exists(tmp_name):
        os.unlink(tmp_name)
PY
sudo docker compose -f /srv/server/projects/augr/docker-compose.nuc.yml up -d --force-recreate --no-deps app
sudo shred -u /tmp/opencode/jwt-secret.new
```

- [ ] **Step 2: Document the cutover expectation.**

The runbook must state clearly that rotating `JWT_SECRET` invalidates all currently signed sessions, refresh tokens, and WebSocket auth state that depends on the old secret. This is expected behavior, not an error.

- [ ] **Step 3: Apply the deployment secret only; do not edit unrelated compose files.**

If the deployment path references environment files or secret mounts, update only the secret source managed by deployment tooling. Do **not** stage or overwrite `docker-compose.prod.yml`.

- [ ] **Step 4: Verify login, refresh, /me, and WebSocket flows.**

Run post-rotation checks against the deployed system:

```bash
export LOGIN_RESPONSE=$(curl -fsS -X POST "$APP_BASE_URL/api/v1/auth/login" -H 'Content-Type: application/json' -d "{\"username\":\"$TEST_LOGIN_USERNAME\",\"password\":\"$TEST_LOGIN_PASSWORD\"}")
export ACCESS_TOKEN=$(python - <<'PY'
import json, os
print(json.loads(os.environ['LOGIN_RESPONSE'])['access_token'], end='')
PY
)
export REFRESH_TOKEN=$(python - <<'PY'
import json, os
print(json.loads(os.environ['LOGIN_RESPONSE'])['refresh_token'], end='')
PY
)
curl -fsS "$APP_BASE_URL/api/v1/me" -H "Authorization: Bearer $ACCESS_TOKEN"
curl -fsS -X POST "$APP_BASE_URL/api/v1/auth/refresh" -H 'Content-Type: application/json' -d "{\"refresh_token\":\"$REFRESH_TOKEN\"}"
test "$(curl -sS -o /dev/null -w '%{http_code}' "$APP_BASE_URL/api/v1/me" -H "Authorization: Bearer $OLD_ACCESS_TOKEN")" = "401"
go test ./... -run 'Test(Auth|Smoke|WebSocket)' -count=1
```

If no existing smoke test covers WebSocket auth, add this exact planned Go test file and use it as the verification path:

```go
package tests

import (
  "net/http"
  "net/url"
  "os"
  "testing"

  "github.com/gorilla/websocket"
)

func TestWebSocketAuthRotation(t *testing.T) {
  t.Parallel()

  baseURL, err := url.Parse(os.Getenv("APP_BASE_URL"))
  if err != nil {
    t.Fatalf("parse app base url: %v", err)
  }
  token := os.Getenv("ACCESS_TOKEN")
  if token == "" {
    t.Fatal("missing ACCESS_TOKEN")
  }

  wsURL := *baseURL
  if wsURL.Scheme == "https" {
    wsURL.Scheme = "wss"
  } else {
    wsURL.Scheme = "ws"
  }
  wsURL.Path = "/ws"

  header := http.Header{}
  header.Set("Authorization", "Bearer "+token)

  c, _, err := websocket.DefaultDialer.Dial(wsURL.String(), header)
  if err != nil {
    t.Fatalf("dial websocket: %v", err)
  }
  defer c.Close()

  if err := c.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping"}`)); err != nil {
    t.Fatalf("write message: %v", err)
  }

  _, msg, err := c.ReadMessage()
  if err != nil {
    t.Fatalf("read message: %v", err)
  }
  if len(msg) == 0 {
    t.Fatal("empty websocket response")
  }
}
```

Acceptance expectations:

  - new login works
  - refresh token flow works for newly issued sessions
  - `/api/v1/me` succeeds for the new token
  - WebSocket auth succeeds for the new token via existing smoke/auth tests or a small Go test at `tests/websocket_auth_test.go` that dials `/ws` with the issued token and asserts an authenticated response
  - old sessions fail as expected after rotation

- [ ] **Step 5: Record rollback steps.**

The rollback runbook must include:

   - restore only the previous `JWT_SECRET` from `/tmp/opencode/jwt-secret-rollback.env` back into `/srv/server/projects/augr/.env` without printing it, using the exact guarded command below
   - recreate the app again with the restored old secret using `sudo docker compose -f /srv/server/projects/augr/docker-compose.nuc.yml up -d --force-recreate --no-deps app`
   - re-run the login/refresh/me/WebSocket verification suite
   - delete `/tmp/opencode/jwt-secret-rollback.env` after acceptance or rollback completion
   - communicate that some sessions may again be invalidated during rollback

Do not reuse the discarded secret in plain text.

```bash
set -euo pipefail
test -s /tmp/opencode/jwt-secret-rollback.env
sudo python3 - <<'PY'
import os
from pathlib import Path
import tempfile
env_path = Path('/srv/server/projects/augr/.env')
rollback_path = Path('/tmp/opencode/jwt-secret-rollback.env')
old_secret = rollback_path.read_text().strip()
if not old_secret:
    raise SystemExit('rollback JWT secret is empty')
lines = env_path.read_text().splitlines()
matches = [line for line in lines if line.startswith('JWT_SECRET=')]
if len(matches) != 1:
    raise SystemExit('expected exactly one JWT_SECRET before rollback')
out = [f'JWT_SECRET={old_secret}' if line.startswith('JWT_SECRET=') else line for line in lines]
st = env_path.stat()
fd, tmp_name = tempfile.mkstemp(prefix='.env.jwt-rollback-', dir=env_path.parent)
try:
    with os.fdopen(fd, 'w') as tmp:
        tmp.write('\n'.join(out) + '\n')
        tmp.flush()
        os.fsync(tmp.fileno())
    os.chmod(tmp_name, st.st_mode)
    os.chown(tmp_name, st.st_uid, st.st_gid)
    os.replace(tmp_name, env_path)
finally:
    if os.path.exists(tmp_name):
        os.unlink(tmp_name)
PY
sudo docker compose -f /srv/server/projects/augr/docker-compose.nuc.yml up -d --force-recreate --no-deps app
```

- [ ] **Step 6: Evaluate optional dual-key design as follow-up only.**

If zero forced logout is required in the future, recommend a separate follow-up to implement a dual-key JWT verification design (`current` + `previous` key window). Do **not** implement that design in this rotation plan.

- [ ] **Step 7: Post-cutover operator sign-off.**

Operator confirms the approved cutover completed, the old-token canary now fails, new auth flows pass, and rollback is no longer required.

- [ ] **Step 8: Commit the rotation artifacts.**

```bash
git add docs/runbooks/2026-07-20-jwt-secret-rotation.md docs/reports/2026-07-20-jwt-secret-rotation.md docs/reports/2026-07-20-jwt-secret-rotation.test.sh
git commit -m "docs(ops): add jwt secret rotation plan"
```

---

## Sequencing rule

- Complete Task 1 rehearsal and parity gates before any production restore.
- Complete Task 2 inventory/classification, commit reconciliation artifacts, and finish the approved push before any JWT cutover.
- Complete Task 3 rollback readiness, old-token canary capture, and Approval checkpoint C before any production secret cutover; complete rotation verification immediately after cutover.
- Never mix the Timescale recovery work with Git history rewrite or JWT cutover in the same unreviewed execution step.
