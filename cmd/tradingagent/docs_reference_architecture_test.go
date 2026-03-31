package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocsReferenceArchitectureExists(t *testing.T) {
	path := filepath.Join(docsReferencePath(t), "architecture.md")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if info.IsDir() {
		t.Fatalf("%s is a directory, expected a file", path)
	}
}

func TestDocsReferenceArchitectureContent(t *testing.T) {
	path := filepath.Join(docsReferencePath(t), "architecture.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}

	body := string(content)

	// Project structure section covers cmd/ and internal/ packages.
	for _, pkg := range []string{
		"cmd/tradingagent",
		"internal/agent",
		"internal/api",
		"internal/backtest",
		"internal/cli",
		"internal/config",
		"internal/data",
		"internal/domain",
		"internal/execution",
		"internal/llm",
		"internal/memory",
		"internal/notification",
		"internal/registry",
		"internal/repository",
		"internal/risk",
		"internal/scheduler",
	} {
		if !strings.Contains(body, pkg) {
			t.Errorf("architecture.md missing package reference %q", pkg)
		}
	}

	// Pipeline execution flow describes all 4 phases.
	for _, phase := range []string{
		"analysis",
		"research_debate",
		"trading",
		"risk_debate",
	} {
		if !strings.Contains(body, phase) {
			t.Errorf("architecture.md missing pipeline phase %q", phase)
		}
	}

	// Key interfaces section documents required interfaces.
	for _, iface := range []string{
		"Node",
		"Broker",
		"DataProvider",
		"RiskEngine",
	} {
		if !strings.Contains(body, iface) {
			t.Errorf("architecture.md missing interface %q", iface)
		}
	}

	// Data flow section present.
	if !strings.Contains(body, "Data Flow") {
		t.Error("architecture.md missing data flow section")
	}

	// Verify referenced Go package paths actually exist on disk.
	repoRoot := filepath.Join(docsReferencePath(t), "..", "..")
	for _, rel := range []string{
		"cmd/tradingagent",
		"internal/agent",
		"internal/agent/analysts",
		"internal/agent/debate",
		"internal/agent/risk",
		"internal/agent/trader",
		"internal/api",
		"internal/backtest",
		"internal/cli",
		"internal/cli/tui",
		"internal/config",
		"internal/data",
		"internal/data/alphavantage",
		"internal/data/binance",
		"internal/data/newsapi",
		"internal/data/polygon",
		"internal/data/yahoo",
		"internal/domain",
		"internal/execution",
		"internal/execution/alpaca",
		"internal/execution/binance",
		"internal/execution/paper",
		"internal/execution/polymarket",
		"internal/llm",
		"internal/llm/anthropic",
		"internal/llm/google",
		"internal/llm/ollama",
		"internal/llm/openai",
		"internal/llm/parse",
		"internal/memory",
		"internal/notification",
		"internal/papervalidation",
		"internal/registry",
		"internal/repository",
		"internal/repository/postgres",
		"internal/risk",
		"internal/scheduler",
	} {
		dir := filepath.Join(repoRoot, rel)
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("referenced package path %s does not exist: %v", rel, err)
		} else if !info.IsDir() {
			t.Errorf("referenced package path %s is not a directory", rel)
		}
	}

	// Verify key source files referenced exist.
	for _, rel := range []string{
		"internal/agent/node.go",
		"internal/data/provider.go",
		"internal/execution/broker.go",
		"internal/risk/engine.go",
		"internal/data/chain.go",
		"internal/risk/engine_impl.go",
	} {
		fp := filepath.Join(repoRoot, rel)
		if _, err := os.Stat(fp); err != nil {
			t.Errorf("referenced source file %s does not exist: %v", rel, err)
		}
	}
}
