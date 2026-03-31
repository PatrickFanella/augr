package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCIWorkflowUsesDynamicMigrationsAndGeneratedSmokeJWTSecret(t *testing.T) {
	contents, err := os.ReadFile(ciWorkflowPath(t))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	workflow := string(contents)
	for _, want := range []string{
		`SMOKE_JWT_SECRET=$(python3 -c 'import secrets; print(secrets.token_hex(32))')`,
		`JWT_SECRET=${SMOKE_JWT_SECRET}`,
		`pg_isready -d "$DATABASE_URL"`,
		`find migrations -maxdepth 1 -type f -name '*.up.sql' -print | sort | while read -r migration; do`,
		`psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$migration"`,
		`curl -fsS http://127.0.0.1:8080/healthz`,
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("ci.yml missing required content %q", want)
		}
	}

	for _, unwanted := range []string{
		"smoke-jwt-secret",
		`migrate -path migrations -database`,
	} {
		if strings.Contains(workflow, unwanted) {
			t.Fatalf("ci.yml unexpectedly contains %q", unwanted)
		}
	}
}

func ciWorkflowPath(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine test file path")
	}

	return filepath.Join(filepath.Dir(filename), "..", "..", ".github", "workflows", "ci.yml")
}
