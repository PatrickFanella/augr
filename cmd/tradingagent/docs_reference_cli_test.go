package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocsReferenceCLIExists(t *testing.T) {
	path := filepath.Join(docsReferencePath(t), "cli.md")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if info.IsDir() {
		t.Fatalf("%s is a directory, expected a file", path)
	}
}

func TestDocsReferenceCLIContent(t *testing.T) {
	path := filepath.Join(docsReferencePath(t), "cli.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}

	body := string(content)

	for _, snippet := range []string{
		"# CLI Reference",
		"## Command hierarchy",
		"tradingagent",
		"├── serve",
		"├── run TICKER",
		"├── strategies",
		"│   ├── list",
		"│   └── create",
		"├── dashboard",
		"├── portfolio",
		"├── risk",
		"│   ├── status",
		"│   └── kill",
		"└── memories",
		"    └── search QUERY",
		"### `tradingagent serve`",
		"### `tradingagent run TICKER`",
		"### `tradingagent strategies`",
		"#### `tradingagent strategies list`",
		"#### `tradingagent strategies create`",
		"### `tradingagent dashboard`",
		"### `tradingagent portfolio`",
		"### `tradingagent risk`",
		"#### `tradingagent risk status`",
		"#### `tradingagent risk kill`",
		"### `tradingagent memories`",
		"#### `tradingagent memories search QUERY`",
		"| `--api-url string` | `http://127.0.0.1:8080` | Base URL for the local trading agent API |",
		"| `--token string` | from `TRADINGAGENT_TOKEN` / empty | Bearer token for authenticated API requests (or set `TRADINGAGENT_TOKEN`) |",
		"| `--api-key string` | from `TRADINGAGENT_API_KEY` / empty | API key for authenticated API requests (or set `TRADINGAGENT_API_KEY`) |",
		"| `--format string` | `table` | Output format: `table` or `json` |",
		"--name string",
		"--ticker string",
		"--market-type string",
		"--active",
		"--paper",
		"--once",
		"--width int",
		"--height int",
		"--reason string",
		"activated from CLI",
		"TRADINGAGENT_API_URL",
		"TRADINGAGENT_TOKEN",
		"TRADINGAGENT_API_KEY",
		"APP_ENV",
		"LOG_LEVEL",
		"APP_PORT",
		"JWT_SECRET",
		"DATABASE_URL",
		"REDIS_URL",
		"LLM_DEFAULT_PROVIDER",
		"OPENAI_API_KEY",
		"POLYGON_API_KEY",
		"ALPACA_API_KEY",
		"RISK_MAX_POSITION_SIZE_PCT",
		"TRADING_AGENT_KILL",
		"ENABLE_AGENT_MEMORY",
	} {
		if !strings.Contains(body, snippet) {
			t.Errorf("cli.md missing required snippet %q", snippet)
		}
	}
}
