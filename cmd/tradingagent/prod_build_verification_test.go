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
		`docker compose --project-name "$PROJECT_NAME" -f "$COMPOSE_FILE" "$@"`,
		`compose build`,
		`compose up -d`,
		`wait_for_postgres`,
		`wait_for_app_health`,
		`find "${ROOT_DIR}/migrations" -maxdepth 1 -type f -name '*.up.sql' -printf '%f\n' | sort`,
		`compose exec -T postgres`,
		`wget -qO- http://127.0.0.1:8080/healthz`,
		`"status") == "all-ok"`,
		`Authorization: Bearer ${AUTH_TOKEN}`,
		`http://127.0.0.1:8080/api/v1/strategies`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("verify-prod-build.sh missing required content %q", want)
		}
	}

	for _, unwanted := range []string{
		`smoke-jwt-secret`,
		`docker compose -f "$COMPOSE_FILE" down -v`,
	} {
		if strings.Contains(script, unwanted) {
			t.Fatalf("verify-prod-build.sh unexpectedly contains %q", unwanted)
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
