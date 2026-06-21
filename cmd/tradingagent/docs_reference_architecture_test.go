package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchitectureAuditExists(t *testing.T) {
	path := filepath.Join(repoRootPath(t), "docs", "AUGR_ARCHITECTURE_AUDIT.md")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if info.IsDir() {
		t.Fatalf("%s is a directory, expected a file", path)
	}
}

func TestArchitectureAuditContent(t *testing.T) {
	body := readRepoFile(t, "docs/AUGR_ARCHITECTURE_AUDIT.md")

	for _, snippet := range []string{
		"# Augr Architecture Audit",
		"## Runtime and framework",
		"Backend: Go 1.25",
		"Frontend: TypeScript, React, Vite",
		"Storage: PostgreSQL 17 and Redis 7",
		"## Trading research foundation now implemented",
		"internal/edge",
		"internal/repository/postgres/trade_decision_journal.go",
		"internal/calibration",
		"internal/regime",
		"internal/optionsresearch",
		"internal/polymarketresearch",
		"## Remaining planned services",
		"Full replay workbench",
		"Cross-flow risk cockpit",
		"Live trading requires backend feature flags",
	} {
		if !strings.Contains(body, snippet) {
			t.Errorf("architecture audit missing required snippet %q", snippet)
		}
	}

	for _, rel := range []string{
		"cmd/tradingagent",
		"internal/agent",
		"internal/api",
		"internal/calibration",
		"internal/config",
		"internal/domain",
		"internal/edge",
		"internal/execution",
		"internal/optionsresearch",
		"internal/polymarketresearch",
		"internal/regime",
		"internal/repository/postgres",
		"internal/risk",
		"web/src/features",
	} {
		path := filepath.Join(repoRootPath(t), rel)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("referenced path %s does not exist: %v", rel, err)
		} else if !info.IsDir() {
			t.Errorf("referenced path %s is not a directory", rel)
		}
	}

	for _, rel := range []string{
		"internal/execution/live_gate.go",
		"internal/execution/decision_recorder.go",
		"internal/edge/options_pricing.go",
		"internal/repository/postgres/trade_decision_journal.go",
		"internal/domain/trade_decision.go",
		"web/src/features/risk/RiskPage.tsx",
	} {
		path := filepath.Join(repoRootPath(t), rel)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("referenced source file %s does not exist: %v", rel, err)
		}
	}
}
