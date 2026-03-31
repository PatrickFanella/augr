package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestDocsReferenceContainsNoStalePythonReferences(t *testing.T) {
	docsPath := docsReferencePath(t)

	info, err := os.Stat(docsPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", docsPath)
	}

	forbidden := []string{
		"tradingagents",
		"langgraph",
		"typer",
		"rich.console",
		"rich.table",
		"rich.panel",
		"pyproject.toml",
		"pip install",
		"from tradingagents",
		"import tradingagents",
	}
	pythonSourcePathPattern := regexp.MustCompile(`\.py\b`)

	if err := filepath.WalkDir(docsPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		ext := strings.ToLower(filepath.Ext(path))
		if d.IsDir() || (ext != ".md" && ext != ".markdown") {
			return nil
		}

		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		lowerContents := strings.ToLower(string(contents))
		relativePath, err := filepath.Rel(docsPath, path)
		if err != nil {
			return err
		}

		for _, unwanted := range forbidden {
			if strings.Contains(lowerContents, unwanted) {
				t.Fatalf("%s unexpectedly contains stale Python-era reference %q", relativePath, unwanted)
			}
		}
		if pythonSourcePathPattern.MatchString(lowerContents) {
			t.Fatalf("%s unexpectedly contains stale Python-era reference matching %q", relativePath, pythonSourcePathPattern.String())
		}

		return nil
	}); err != nil {
		t.Fatalf("WalkDir() error = %v", err)
	}
}

func docsReferencePath(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine test file path")
	}

	return filepath.Join(filepath.Dir(filename), "..", "..", "docs", "reference")
}
