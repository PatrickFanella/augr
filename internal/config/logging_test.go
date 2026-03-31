package config_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/config"
)

func TestNewLoggerJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := config.NewLogger("staging", "info", &buf)

	logger.Info("test message", slog.String("key", "value"))

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("expected valid JSON log output, got error: %v\nraw: %s", err, buf.String())
	}
	if entry["msg"] != "test message" {
		t.Errorf("expected msg=test message, got %v", entry["msg"])
	}
	if entry["level"] != "INFO" {
		t.Errorf("expected level=INFO, got %v", entry["level"])
	}
	if _, ok := entry["time"].(string); !ok {
		t.Errorf("expected time field in JSON output, got %v", entry["time"])
	}
	if entry["key"] != "value" {
		t.Errorf("expected key=value, got %v", entry["key"])
	}
}

func TestNewLoggerText(t *testing.T) {
	var buf bytes.Buffer
	logger := config.NewLogger("development", "debug", &buf)

	logger.Debug("debug msg", slog.String("key", "value"))

	out := buf.String()
	if !strings.Contains(out, "debug msg") {
		t.Errorf("expected text output to contain 'debug msg', got: %s", out)
	}
	if !strings.Contains(out, "level=DEBUG") {
		t.Errorf("expected text output to contain level=DEBUG, got: %s", out)
	}
	if !strings.Contains(out, "key=value") {
		t.Errorf("expected text output to contain key=value, got: %s", out)
	}
	if json.Valid(buf.Bytes()) {
		t.Errorf("expected development output to be human-readable text, got JSON: %s", out)
	}
}

func TestNewLoggerRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := config.NewLogger("development", "warn", &buf)

	logger.Info("should be filtered")

	if buf.Len() != 0 {
		t.Errorf("expected no output for info message at warn level, got: %s", buf.String())
	}
}

func TestParseLevelValues(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"unknown", slog.LevelInfo},
		{"", slog.LevelInfo},
	}

	for _, tc := range tests {
		got := config.ParseLevel(tc.input)
		if got != tc.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestWithContext(t *testing.T) {
	var buf bytes.Buffer
	logger := config.NewLogger("production", "info", &buf)

	child := config.WithContext(logger, slog.String(config.KeyRunID, "abc-123"))
	child.Info("with context")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if entry[config.KeyRunID] != "abc-123" {
		t.Errorf("expected %s=abc-123, got %v", config.KeyRunID, entry[config.KeyRunID])
	}
}

func TestHTTPRequestLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := config.NewLogger("production", "info", &buf)

	handler := config.HTTPRequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if entry["method"] != http.MethodGet {
		t.Errorf("expected method=GET, got %v", entry["method"])
	}
	if entry["path"] != "/healthz" {
		t.Errorf("expected path=/healthz, got %v", entry["path"])
	}
	if int(entry["status"].(float64)) != http.StatusCreated {
		t.Errorf("expected status=201, got %v", entry["status"])
	}
	if _, ok := entry[config.KeyDurationMS]; !ok {
		t.Error("expected duration_ms field in log entry")
	}
}

func TestHTTPRequestLoggerImplicitStatus(t *testing.T) {
	var buf bytes.Buffer
	logger := config.NewLogger("production", "info", &buf)

	handler := config.HTTPRequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Write body without calling WriteHeader; status should be 200.
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if int(entry["status"].(float64)) != http.StatusOK {
		t.Errorf("expected status=200 for implicit write, got %v", entry["status"])
	}
}

func TestFieldConstants(t *testing.T) {
	expected := map[string]string{
		"KeyRunID":      "run_id",
		"KeyTicker":     "ticker",
		"KeyAgentRole":  "agent_role",
		"KeySignal":     "signal",
		"KeyDurationMS": "duration_ms",
	}

	actual := map[string]string{
		"KeyRunID":      config.KeyRunID,
		"KeyTicker":     config.KeyTicker,
		"KeyAgentRole":  config.KeyAgentRole,
		"KeySignal":     config.KeySignal,
		"KeyDurationMS": config.KeyDurationMS,
	}

	for name, want := range expected {
		got := actual[name]
		if got != want {
			t.Errorf("config.%s = %q, want %q", name, got, want)
		}
	}
}

func TestSetDefaultLoggerProductionWritesJSONToStderr(t *testing.T) {
	stdout, stderr := captureStandardOutput(t, func() {
		previous := slog.Default()
		t.Cleanup(func() {
			slog.SetDefault(previous)
		})

		logger := config.SetDefaultLogger("production", "info")
		logger.Info("test message", slog.String("key", "value"))
	})

	if len(stdout) != 0 {
		t.Fatalf("expected production logs to write to stderr, got stdout: %s", stdout)
	}

	var entry map[string]any
	if err := json.Unmarshal(stderr, &entry); err != nil {
		t.Fatalf("expected stderr to contain JSON log output, got error: %v\nraw: %s", err, stderr)
	}
	if entry["msg"] != "test message" {
		t.Errorf("expected msg=test message, got %v", entry["msg"])
	}
	if entry["key"] != "value" {
		t.Errorf("expected key=value, got %v", entry["key"])
	}
}

func TestSetDefaultLoggerDevelopmentWritesTextToStdout(t *testing.T) {
	stdout, stderr := captureStandardOutput(t, func() {
		previous := slog.Default()
		t.Cleanup(func() {
			slog.SetDefault(previous)
		})

		logger := config.SetDefaultLogger("development", "debug")
		logger.Debug("debug msg", slog.String("key", "value"))
	})

	if len(stderr) != 0 {
		t.Fatalf("expected development logs to stay off stderr, got: %s", stderr)
	}
	if !strings.Contains(string(stdout), "debug msg") {
		t.Fatalf("expected stdout to contain debug msg, got: %s", stdout)
	}
	if json.Valid(stdout) {
		t.Fatalf("expected development stdout to be text, got JSON: %s", stdout)
	}
}

func captureStandardOutput(t *testing.T, fn func()) ([]byte, []byte) {
	t.Helper()

	originalStdout := os.Stdout
	originalStderr := os.Stderr

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() stdout error = %v", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() stderr error = %v", err)
	}

	restored := false
	restore := func() {
		if restored {
			return
		}
		os.Stdout = originalStdout
		os.Stderr = originalStderr
		restored = true
	}

	os.Stdout = stdoutWriter
	os.Stderr = stderrWriter
	t.Cleanup(func() {
		restore()
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		_ = stderrReader.Close()
		_ = stderrWriter.Close()
	})

	fn()
	restore()

	if err := stdoutWriter.Close(); err != nil {
		t.Fatalf("stdoutWriter.Close() error = %v", err)
	}
	if err := stderrWriter.Close(); err != nil {
		t.Fatalf("stderrWriter.Close() error = %v", err)
	}

	stdout, err := io.ReadAll(stdoutReader)
	if err != nil {
		t.Fatalf("io.ReadAll(stdoutReader) error = %v", err)
	}
	stderr, err := io.ReadAll(stderrReader)
	if err != nil {
		t.Fatalf("io.ReadAll(stderrReader) error = %v", err)
	}

	return stdout, stderr
}
