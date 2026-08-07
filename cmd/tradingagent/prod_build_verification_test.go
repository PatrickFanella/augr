package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProductionBuildVerificationScriptContainsExpectedSteps(t *testing.T) {
	contents, err := os.ReadFile(productionBuildVerificationScriptPath(t))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	script := string(contents)
	for _, want := range []string{
		`docker compose --project-name "$PROJECT_NAME" "${compose_files[@]}" "$@"`,
		`augr-prod-verify-`,
		`refusing to reuse existing Compose project`,
		`VERIFY_PUBLIC_SUBNET`,
		`VERIFY_BACKEND_SUBNET`,
		`subnet: ${VERIFY_PUBLIC_SUBNET}`,
		`subnet: ${VERIFY_BACKEND_SUBNET}`,
		`VERIFY_APP_PORT`,
		`APP_BIND="127.0.0.1"`,
		`ENABLE_SCHEDULER=false`,
		`ENABLE_LIVE_TRADING=false`,
		`ALPACA_PAPER_MODE=true`,
		`BINANCE_PAPER_MODE=true`,
		`KALSHI_DRY_RUN=true`,
		`ENABLE_POLYMARKET_AUTOMATION=false`,
		`OLLAMA_API_KEY=smoke-key`,
		`compose build app`,
		`BUILT_APP_IMAGE_ID=$(docker image inspect --format '{{.Id}}' "${PROJECT_NAME}-app:latest"`,
		`org.opencontainers.image.revision`,
		`org.opencontainers.image.version`,
		`org.opencontainers.image.created`,
		`built app revision label mismatch`,
		`VERIFY_WEB_IMAGE="${PROJECT_NAME}-web:latest"`,
		`docker buildx build --load`,
		`BUILT_WEB_REVISION=`,
		`built web revision label mismatch`,
		`docker image rm "$VERIFY_WEB_IMAGE"`,
		`compose up -d postgres redis`,
		`wait_for_postgres`,
		`pg_isready -h postgres`,
		`migrate/migrate:v4.18.3`,
		`VERIFY_ROLLBACK_SCHEMA_VERSION="${VERIFY_ROLLBACK_SCHEMA_VERSION:-60}"`,
		`VERIFY_ROLLBACK_IMAGE="${VERIFY_ROLLBACK_IMAGE:-}"`,
		`VERIFY_ROLLBACK_IMAGE contains unsupported characters`,
		`VERIFY_ROLLBACK_SCHEMA_VERSION must be a non-negative integer`,
		`ROLLBACK_STEPS=$((EXPECTED_VERSION - VERIFY_ROLLBACK_SCHEMA_VERSION))`,
		`-path=/migrations`,
		`SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`,
		`schema version mismatch after migrations`,
		`compose up -d app`,
		`wait_for_app_health`,
		`body.get("status") == "ok" and body.get("db") == "ok" and body.get("redis") == "ok"`,
		`"token_type": "access"`,
		`Authorization: Bearer ${AUTH_TOKEN}`,
		`/api/v1/strategies`,
		`Verifying schema-${VERIFY_ROLLBACK_SCHEMA_VERSION} predeployment backup and restore`,
		`--format=custom`,
		`--no-owner >"$BACKUP_FILE"`,
		`createdb -U "$POSTGRES_USER" "$RESTORE_DB"`,
		`pg_restore`,
		`--single-transaction`,
		`--exit-on-error`,
		`restored backup schema mismatch`,
		`dropdb -U "$POSTGRES_USER" "$RESTORE_DB"`,
		`compose stop app`,
		`SELECT count(*) FROM automation_job_controls`,
		`SELECT count(*) FROM trades WHERE exit_reason IS NOT NULL`,
		`refusing rollback rehearsal with writes in schema 61/62 structures`,
		`down "$ROLLBACK_STEPS"`,
		`schema rollback mismatch`,
		`Verifying exact rollback image with scheduler disabled`,
		`rollback image mismatch`,
		`rollback scheduler check returned HTTP`,
		`schema reapply mismatch`,
		`compose down --volumes --remove-orphans --rmi local`,
		`trap cleanup EXIT HUP INT TERM`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("verify-prod-build.sh missing required content %q", want)
		}
	}

	for _, unwanted := range []string{
		`AUTH_TOKEN="${AUTH_TOKEN:?`,
		`compose up -d` + "\n" + `wait_for_postgres`,
		`http://127.0.0.1:8080`,
		`psql -U augr -d augr`,
		`POLYMARKET_AUTOMATION_ENABLED=false`,
	} {
		if strings.Contains(script, unwanted) {
			t.Fatalf("verify-prod-build.sh unexpectedly contains %q", unwanted)
		}
	}

	appBuildIdx := strings.Index(script, `compose build app`)
	webBuildIdx := strings.Index(script, `docker buildx build --load`)
	dependenciesIdx := strings.Index(script, `compose up -d postgres redis`)
	migrationsIdx := strings.Index(script, `-path=/migrations`)
	schemaAssertIdx := strings.Index(script, `SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`)
	appStartIdx := strings.Index(script, `compose up -d app`)
	backupIdx := strings.Index(script, `Verifying schema-${VERIFY_ROLLBACK_SCHEMA_VERSION} predeployment backup and restore`)
	rollbackGuardIdx := strings.Index(script, `NEW_STRUCTURE_WRITES=`)
	downIdx := strings.Index(script, `down "$ROLLBACK_STEPS"`)
	reapplyIdx := strings.Index(script, `REAPPLIED_SCHEMA_VERSION=`)
	healthWaitIdx := strings.LastIndex(script, "\nwait_for_app_health\n")
	if appBuildIdx == -1 || webBuildIdx == -1 || dependenciesIdx == -1 || migrationsIdx == -1 || schemaAssertIdx == -1 || appStartIdx == -1 || backupIdx == -1 || rollbackGuardIdx == -1 || downIdx == -1 || reapplyIdx == -1 || healthWaitIdx == -1 {
		t.Fatal("verify-prod-build.sh missing ordering anchors")
	}
	if appBuildIdx >= webBuildIdx || webBuildIdx >= dependenciesIdx || dependenciesIdx >= migrationsIdx || migrationsIdx >= schemaAssertIdx || schemaAssertIdx >= appStartIdx || appStartIdx >= rollbackGuardIdx || rollbackGuardIdx >= downIdx || downIdx >= backupIdx || backupIdx >= reapplyIdx || reapplyIdx >= healthWaitIdx {
		t.Fatalf("verify-prod-build.sh expected app build -> web build -> dependencies -> migrations -> schema -> app -> rollback guard -> down -> backup -> reapply -> health ordering, got %d %d %d %d %d %d %d %d %d %d %d", appBuildIdx, webBuildIdx, dependenciesIdx, migrationsIdx, schemaAssertIdx, appStartIdx, rollbackGuardIdx, downIdx, backupIdx, reapplyIdx, healthWaitIdx)
	}
}

func TestReleaseGateIncludesProductionVerificationAndPinnedPromtool(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join(filepath.Dir(productionBuildVerificationScriptPath(t)), "release-gate.sh"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	script := string(contents)
	for _, want := range []string{
		`candidate_commit=$(git rev-parse HEAD)`,
		`./scripts/verify-release-tree.sh`,
		`go test -count=1 ./...`,
		`go vet ./...`,
		`golangci-lint run ./...`,
		`npm --prefix web test`,
		`npm --prefix web run lint`,
		`npm --prefix web run build`,
		`docker compose -f docker-compose.nuc.yml config --quiet`,
		`docker compose -f docker-compose.nuc.yml -f deploy/docker-compose.nuc.rollback.yml config --quiet`,
		`MIGRATION_DOWN_STEPS=2 docker compose -f docker-compose.nuc.yml -f deploy/docker-compose.nuc.migrate-down.yml config --quiet`,
		`docker buildx build --check -f Dockerfile .`,
		`docker buildx build --check -f Dockerfile.web .`,
		`./scripts/verify-prod-build.sh`,
		`prom/prometheus@sha256:`,
		`"$promtool_image" check rules`,
		`./scripts/verify-secret-history.sh`,
		`[ "$verified_commit" = "$candidate_commit" ]`,
		`Paper release gate passed for commit $verified_commit`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("release-gate.sh missing required content %q", want)
		}
	}
	if strings.Contains(script, `prom/prometheus:v3.3.0`) {
		t.Fatal("release-gate.sh uses a mutable Prometheus tag")
	}

	candidateIdx := strings.Index(script, `candidate_commit=$(git rev-parse HEAD)`)
	firstTreeIdx := strings.Index(script, `./scripts/verify-release-tree.sh`)
	goTestIdx := strings.Index(script, `go test -count=1 ./...`)
	secretIdx := strings.Index(script, `./scripts/verify-secret-history.sh`)
	lastTreeIdx := strings.LastIndex(script, `./scripts/verify-release-tree.sh`)
	verifiedIdx := strings.Index(script, `verified_commit=$(git rev-parse HEAD)`)
	identityIdx := strings.Index(script, `[ "$verified_commit" = "$candidate_commit" ]`)
	if candidateIdx >= firstTreeIdx || firstTreeIdx >= goTestIdx || goTestIdx >= secretIdx || secretIdx >= lastTreeIdx || lastTreeIdx >= verifiedIdx || verifiedIdx >= identityIdx {
		t.Fatalf("release-gate.sh expected candidate -> initial tree -> tests -> secrets -> final tree -> verified commit -> identity ordering, got %d %d %d %d %d %d %d", candidateIdx, firstTreeIdx, goTestIdx, secretIdx, lastTreeIdx, verifiedIdx, identityIdx)
	}
}

func TestReleaseTreeVerifierRequiresExactCleanInput(t *testing.T) {
	repoRoot := filepath.Join(filepath.Dir(productionBuildVerificationScriptPath(t)), "..")
	contents, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "verify-release-tree.sh"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	script := string(contents)
	for _, want := range []string{
		`git diff --quiet --ignore-submodules`,
		`git diff --cached --quiet --ignore-submodules`,
		`RELEASE_ALLOWED_UNTRACKED`,
		`git ls-files --others --exclude-standard`,
		`[ "$path" = "$allowed_path" ]`,
		`release tree has unexpected untracked files`,
		`git rev-parse HEAD`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("verify-release-tree.sh missing required content %q", want)
		}
	}
}

func TestGitReconciliationRunbookPreservesAndPublishesVerifiedCommit(t *testing.T) {
	repoRoot := filepath.Join(filepath.Dir(productionBuildVerificationScriptPath(t)), "..")
	contents, err := os.ReadFile(filepath.Join(repoRoot, "docs", "runbooks", "2026-07-20-git-reconcile-and-push.md"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	runbook := string(contents)
	for _, want := range []string{
		`git fetch --prune "$remote_name"`,
		`git merge --ff-only "$upstream_ref"`,
		`git merge --no-ff --no-commit "$upstream_ref"`,
		`git commit --no-edit`,
		`./scripts/release-gate.sh`,
		`git push "$remote_name" "HEAD:$remote_branch"`,
		`git ls-remote --heads`,
		`test "$remote_commit" = "$candidate_commit"`,
	} {
		if !strings.Contains(runbook, want) {
			t.Fatalf("git reconciliation runbook missing required content %q", want)
		}
	}
	for _, forbidden := range []string{`git reset`, `git rebase`, `git push --force`, `git push -f`, `git checkout --`} {
		if strings.Contains(runbook, forbidden) {
			t.Fatalf("git reconciliation runbook contains destructive command %q", forbidden)
		}
	}

	fetchIdx := strings.Index(runbook, `git fetch --prune "$remote_name"`)
	gateIdx := strings.Index(runbook, `./scripts/release-gate.sh`)
	pushIdx := strings.Index(runbook, `git push "$remote_name" "HEAD:$remote_branch"`)
	remoteIdx := strings.Index(runbook, `git ls-remote --heads`)
	identityIdx := strings.Index(runbook, `test "$remote_commit" = "$candidate_commit"`)
	if fetchIdx >= gateIdx || gateIdx >= pushIdx || pushIdx >= remoteIdx || remoteIdx >= identityIdx {
		t.Fatalf("git reconciliation runbook expected fetch -> gate -> push -> remote lookup -> identity ordering, got %d %d %d %d %d", fetchIdx, gateIdx, pushIdx, remoteIdx, identityIdx)
	}
}

func TestNUCDeploymentRunbooksRequireImmutableSingleReplacementAndRestoreProof(t *testing.T) {
	repoRoot := filepath.Join(filepath.Dir(productionBuildVerificationScriptPath(t)), "..")
	rollingContents, err := os.ReadFile(filepath.Join(repoRoot, "docs", "runbooks", "rolling-restart.md"))
	if err != nil {
		t.Fatalf("ReadFile(rolling-restart.md) error = %v", err)
	}
	backupContents, err := os.ReadFile(filepath.Join(repoRoot, "docs", "runbooks", "database-backup-restore.md"))
	if err != nil {
		t.Fatalf("ReadFile(database-backup-restore.md) error = %v", err)
	}

	rolling := string(rollingContents)
	for _, want := range []string{
		`previous_app_image=$(docker inspect`,
		`previous_web_image=$(docker inspect`,
		`candidate_app_image="augr-app:$release_tag"`,
		`candidate_web_image="augr-web:$release_tag"`,
		`docker compose -f docker-compose.nuc.yml build app web`,
		`docker compose -f docker-compose.nuc.yml --profile tools run --rm migrate`,
		`up -d --no-build --no-deps app web`,
		`Replace app and web together exactly once`,
		`Require clean schema 62`,
		`AUGR_APP_IMAGE="$previous_app_image"`,
		`AUGR_WEB_IMAGE="$previous_web_image"`,
		`It does not select image names`,
		`org.opencontainers.image.revision`,
	} {
		if !strings.Contains(rolling, want) {
			t.Fatalf("rolling restart runbook missing required content %q", want)
		}
	}
	if strings.Contains(rolling, `up -d --build`) {
		t.Fatal("rolling restart runbook permits mutable build-and-replace deployment")
	}

	backup := string(backupContents)
	for _, want := range []string{
		`baseline_schema=$(docker compose -f docker-compose.nuc.yml`,
		`baseline_counts=$(docker compose -f docker-compose.nuc.yml`,
		`sh -ec 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB"`,
		`backup_sha256=$(sha256sum "$BACKUP_FILE"`,
		`restore_db="augr_restore_check_$release_short"`,
		`sh -ec 'createdb -U "$POSTGRES_USER" "$RESTORE_DB"'`,
		`--clean --if-exists --single-transaction --exit-on-error --no-owner`,
		`SELECT version, dirty FROM schema_migrations`,
		`test "$restored_schema" = "$baseline_schema"`,
		`test "$restored_counts" = "$baseline_counts"`,
		`sh -ec 'dropdb -U "$POSTGRES_USER" "$RESTORE_DB"'`,
		`pre-backup critical-table counts`,
		`EXPECTED_BACKUP_SHA256`,
		`com.docker.compose.project`,
		`com.docker.compose.service`,
		`test "$restored_production_schema" = "60|f"`,
	} {
		if !strings.Contains(backup, want) {
			t.Fatalf("database backup runbook missing required content %q", want)
		}
	}
	dumpIdx := strings.Index(backup, `sh -ec 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB"`)
	createIdx := strings.Index(backup, `sh -ec 'createdb -U "$POSTGRES_USER" "$RESTORE_DB"'`)
	restoreIdx := strings.Index(backup, `sh -ec 'pg_restore -U "$POSTGRES_USER" -d "$RESTORE_DB" --clean --if-exists --single-transaction --exit-on-error --no-owner'`)
	validateIdx := strings.Index(backup, `SELECT version, dirty FROM schema_migrations`)
	dropIdx := strings.Index(backup, `sh -ec 'dropdb -U "$POSTGRES_USER" "$RESTORE_DB"'`)
	if dumpIdx >= createIdx || createIdx >= restoreIdx || restoreIdx >= validateIdx || validateIdx >= dropIdx {
		t.Fatalf("database backup runbook expected dump -> create -> restore -> validate -> drop ordering, got %d %d %d %d %d", dumpIdx, createIdx, restoreIdx, validateIdx, dropIdx)
	}
}

func TestNUCSchemaRollbackRequiresZeroWritesAndExactDownOverride(t *testing.T) {
	repoRoot := filepath.Join(filepath.Dir(productionBuildVerificationScriptPath(t)), "..")
	overrideContents, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "docker-compose.nuc.migrate-down.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.nuc.migrate-down.yml) error = %v", err)
	}
	override := string(overrideContents)
	for _, want := range []string{
		`-path`,
		`/migrations`,
		`-database`,
		`down`,
		`${MIGRATION_DOWN_STEPS:?MIGRATION_DOWN_STEPS is required}`,
	} {
		if !strings.Contains(override, want) {
			t.Fatalf("NUC migrate-down override missing required content %q", want)
		}
	}

	runbookContents, err := os.ReadFile(filepath.Join(repoRoot, "docs", "runbooks", "rolling-restart.md"))
	if err != nil {
		t.Fatalf("ReadFile(rolling-restart.md) error = %v", err)
	}
	runbook := string(runbookContents)
	for _, want := range []string{
		`SELECT count(*) FROM automation_job_controls`,
		`SELECT count(*) FROM trades WHERE exit_reason IS NOT NULL`,
		`test "$new_structure_writes" = "0|0"`,
		`MIGRATION_DOWN_STEPS=2`,
		`deploy/docker-compose.nuc.migrate-down.yml`,
		`test "$rollback_schema" = "60|f"`,
	} {
		if !strings.Contains(runbook, want) {
			t.Fatalf("rolling restart rollback missing required content %q", want)
		}
	}
	guardIdx := strings.Index(runbook, `test "$new_structure_writes" = "0|0"`)
	downIdx := strings.Index(runbook, `MIGRATION_DOWN_STEPS=2`)
	schemaIdx := strings.Index(runbook, `test "$rollback_schema" = "60|f"`)
	if guardIdx >= downIdx || downIdx >= schemaIdx {
		t.Fatalf("rolling restart rollback expected zero-write guard -> down migration -> schema check ordering, got %d %d %d", guardIdx, downIdx, schemaIdx)
	}
}

func TestPaperBoundaryObserverOmitsRawErrorsAndUsesPortableOutputs(t *testing.T) {
	repoRoot := filepath.Join(filepath.Dir(productionBuildVerificationScriptPath(t)), "..")
	contents, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "observe-paper-boundary.sh"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	script := string(contents)
	for _, want := range []string{
		`repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)`,
		`OBSERVATION_REPORT`,
		`mktemp`,
		`--no-log-prefix`,
		`fromjson?`,
		`raw errors and provider bodies omitted`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("observe-paper-boundary.sh missing required content %q", want)
		}
	}
	for _, forbidden := range []string{
		`repo="/home/`,
		`grep -E '"level":"(WARN|ERROR)"`,
		`error: (.error`,
		`msg: (.msg`,
		`.message`,
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("observe-paper-boundary.sh contains unsafe content %q", forbidden)
		}
	}
}

func TestNUCRollbackOverrideDisablesExecution(t *testing.T) {
	repoRoot := filepath.Join(filepath.Dir(productionBuildVerificationScriptPath(t)), "..")
	contents, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "docker-compose.nuc.rollback.yml"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	override := string(contents)
	for _, want := range []string{
		`ENABLE_SCHEDULER: "false"`,
		`ENABLE_LIVE_TRADING: "false"`,
		`ALPACA_PAPER_MODE: "true"`,
		`BINANCE_PAPER_MODE: "true"`,
		`KALSHI_DRY_RUN: "true"`,
		`ENABLE_POLYMARKET_AUTOMATION: "false"`,
	} {
		if !strings.Contains(override, want) {
			t.Fatalf("rollback override missing fail-closed setting %q", want)
		}
	}
}

func productionBuildVerificationScriptPath(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine test file path")
	}

	return filepath.Join(filepath.Dir(filename), "..", "..", "scripts", "verify-prod-build.sh")
}
