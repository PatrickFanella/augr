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
		`docker ps --filter publish=55432 --filter ancestor=timescale/timescaledb:2.17.2-pg17`,
		`docker exec "$DATABASE_CONTAINER" pg_isready -U tradingagent -d tradingagent_test`,
		`find migrations -maxdepth 1 -type f -name '*.up.sql' -print | sort | while read -r migration; do`,
		`docker exec -i "$DATABASE_CONTAINER" psql -U tradingagent -d tradingagent_test --single-transaction --set ON_ERROR_STOP=1`,
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
