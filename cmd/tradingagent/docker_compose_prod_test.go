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
		"POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}",
		"pg_isready -U ${POSTGRES_USER:-postgres} -d ${POSTGRES_DB:-tradingagent}",
		"image: redis:7-alpine",
		"redis_data:/data",
		"[\"CMD\", \"redis-cli\", \"ping\"]",
		"dockerfile: Dockerfile",
		"target: production",
		"APP_ENV: production",
		"env_file:",
		"- .env",
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
		`POSTGRES_PASSWORD:?`,
	} {
		if strings.Contains(compose, unwanted) {
			t.Fatalf("docker-compose.prod.yml unexpectedly contains %q", unwanted)
		}
	}

	dependsOnBlock := sectionBetween(t, compose, "depends_on:\n", "\n    restart: unless-stopped")
	for _, want := range []string{"postgres:", "redis:"} {
		if !strings.Contains(dependsOnBlock, want) {
			t.Fatalf("depends_on block missing %q", want)
		}
	}
	if got := strings.Count(dependsOnBlock, "condition: service_healthy"); got != 2 {
		t.Fatalf("depends_on healthy conditions = %d, want 2", got)
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

func sectionBetween(t *testing.T, contents, start, end string) string {
	t.Helper()

	startIndex := strings.Index(contents, start)
	if startIndex == -1 {
		t.Fatalf("section start %q not found", start)
	}
	startIndex += len(start)

	endIndex := strings.Index(contents[startIndex:], end)
	if endIndex == -1 {
		t.Fatalf("section end %q not found", end)
	}

	return contents[startIndex : startIndex+endIndex]
}
