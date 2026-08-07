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
		`docker compose --project-name "$PROJECT_NAME" -f "$COMPOSE_FILE" -f "$NETWORK_OVERRIDE_FILE" "$@"`,
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
		`POLYMARKET_AUTOMATION_ENABLED=false`,
		`OLLAMA_API_KEY=smoke-key`,
		`compose build app`,
		`compose up -d postgres redis`,
		`wait_for_postgres`,
		`pg_isready -h postgres`,
		`migrate/migrate:v4.18.3`,
		`-path=/migrations`,
		`SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`,
		`schema version mismatch after migrations`,
		`compose up -d app`,
		`wait_for_app_health`,
		`body.get("status") == "ok" and body.get("db") == "ok" and body.get("redis") == "ok"`,
		`"token_type": "access"`,
		`Authorization: Bearer ${AUTH_TOKEN}`,
		`/api/v1/strategies`,
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
	} {
		if strings.Contains(script, unwanted) {
			t.Fatalf("verify-prod-build.sh unexpectedly contains %q", unwanted)
		}
	}

	dependenciesIdx := strings.Index(script, `compose up -d postgres redis`)
	migrationsIdx := strings.Index(script, `-path=/migrations`)
	schemaAssertIdx := strings.Index(script, `SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`)
	appStartIdx := strings.Index(script, `compose up -d app`)
	healthWaitIdx := strings.LastIndex(script, "\nwait_for_app_health\n")
	if dependenciesIdx == -1 || migrationsIdx == -1 || schemaAssertIdx == -1 || appStartIdx == -1 || healthWaitIdx == -1 {
		t.Fatal("verify-prod-build.sh missing ordering anchors")
	}
	if dependenciesIdx >= migrationsIdx || migrationsIdx >= schemaAssertIdx || schemaAssertIdx >= appStartIdx || appStartIdx >= healthWaitIdx {
		t.Fatalf("verify-prod-build.sh expected dependencies -> migrations -> schema -> app -> health ordering, got %d %d %d %d %d", dependenciesIdx, migrationsIdx, schemaAssertIdx, appStartIdx, healthWaitIdx)
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
