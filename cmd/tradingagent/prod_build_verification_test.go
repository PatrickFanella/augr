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
		`Verifying custom-format database backup and restore`,
		`--format=custom`,
		`--no-owner >"$BACKUP_FILE"`,
		`createdb -U "$POSTGRES_USER" "$RESTORE_DB"`,
		`pg_restore`,
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
		`compose down --volumes --remove-orphans`,
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

	dependenciesIdx := strings.Index(script, `compose up -d postgres redis`)
	migrationsIdx := strings.Index(script, `-path=/migrations`)
	schemaAssertIdx := strings.Index(script, `SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`)
	appStartIdx := strings.Index(script, `compose up -d app`)
	backupIdx := strings.Index(script, `Verifying custom-format database backup and restore`)
	rollbackGuardIdx := strings.Index(script, `NEW_STRUCTURE_WRITES=`)
	downIdx := strings.Index(script, `down "$ROLLBACK_STEPS"`)
	reapplyIdx := strings.Index(script, `REAPPLIED_SCHEMA_VERSION=`)
	healthWaitIdx := strings.LastIndex(script, "\nwait_for_app_health\n")
	if dependenciesIdx == -1 || migrationsIdx == -1 || schemaAssertIdx == -1 || appStartIdx == -1 || backupIdx == -1 || rollbackGuardIdx == -1 || downIdx == -1 || reapplyIdx == -1 || healthWaitIdx == -1 {
		t.Fatal("verify-prod-build.sh missing ordering anchors")
	}
	if dependenciesIdx >= migrationsIdx || migrationsIdx >= schemaAssertIdx || schemaAssertIdx >= appStartIdx || appStartIdx >= backupIdx || backupIdx >= rollbackGuardIdx || rollbackGuardIdx >= downIdx || downIdx >= reapplyIdx || reapplyIdx >= healthWaitIdx {
		t.Fatalf("verify-prod-build.sh expected dependencies -> migrations -> schema -> app -> backup -> rollback guard -> down -> reapply -> health ordering, got %d %d %d %d %d %d %d %d %d", dependenciesIdx, migrationsIdx, schemaAssertIdx, appStartIdx, backupIdx, rollbackGuardIdx, downIdx, reapplyIdx, healthWaitIdx)
	}
}

func TestReleaseGateIncludesProductionVerificationAndPinnedPromtool(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join(filepath.Dir(productionBuildVerificationScriptPath(t)), "release-gate.sh"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	script := string(contents)
	for _, want := range []string{
		`docker compose -f docker-compose.nuc.yml config --quiet`,
		`docker compose -f docker-compose.nuc.yml -f deploy/docker-compose.nuc.rollback.yml config --quiet`,
		`./scripts/verify-prod-build.sh`,
		`prom/prometheus@sha256:`,
		`"$promtool_image" check rules`,
		`./scripts/verify-secret-history.sh`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("release-gate.sh missing required content %q", want)
		}
	}
	if strings.Contains(script, `prom/prometheus:v3.3.0`) {
		t.Fatal("release-gate.sh uses a mutable Prometheus tag")
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
