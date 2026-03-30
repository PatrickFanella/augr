package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProductionDockerComposeContainsRequiredConfiguration(t *testing.T) {
	contents, err := os.ReadFile(productionDockerComposePath(t))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	compose := string(contents)
	for _, want := range []string{
		"services:",
		"image: postgres:17",
		"postgres_data:/var/lib/postgresql/data",
		"pg_isready -U ${POSTGRES_USER:-postgres} -d ${POSTGRES_DB:-tradingagent}",
		"image: redis:7-alpine",
		"redis_data:/data",
		"[\"CMD\", \"redis-cli\", \"ping\"]",
		"dockerfile: Dockerfile",
		"target: production",
		"APP_ENV: production",
		"env_file:",
		"- .env",
		"postgres:\n        condition: service_healthy",
		"redis:\n        condition: service_healthy",
		"restart: unless-stopped",
		"backend:\n    internal: true",
		"public:",
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("docker-compose.prod.yml missing required content %q", want)
		}
	}

	for _, unwanted := range []string{
		"- .:/app",
		"go_cache",
		`target: dev`,
	} {
		if strings.Contains(compose, unwanted) {
			t.Fatalf("docker-compose.prod.yml unexpectedly contains %q", unwanted)
		}
	}
}

func productionDockerComposePath(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine test file path")
	}

	return filepath.Join(filepath.Dir(filename), "..", "..", "docker-compose.prod.yml")
}
