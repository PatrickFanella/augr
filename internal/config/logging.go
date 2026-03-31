package config

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// Standard log field keys used across the application for consistent structured logging.
const (
	KeyRunID      = "run_id"
	KeyTicker     = "ticker"
	KeyAgentRole  = "agent_role"
	KeySignal     = "signal"
	KeyDurationMS = "duration_ms"
)

// NewLogger creates a configured *slog.Logger based on the environment.
// When env is not "development", it uses a JSON handler; otherwise it uses a
// text handler.
// The level string maps to slog levels: "debug", "info", "warn", "error".
// Output is written to the provided writer (use os.Stdout / os.Stderr in production).
func NewLogger(env, level string, w io.Writer) *slog.Logger {
	lvl := ParseLevel(level)
	opts := &slog.HandlerOptions{Level: lvl}

	var handler slog.Handler
	if strings.EqualFold(env, "development") {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}

	return slog.New(handler)
}

// ParseLevel converts a level string to a slog.Level.
// Supported values (case-insensitive): "debug", "info", "warn", "warning", "error".
// Defaults to slog.LevelInfo for unrecognised values.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// SetDefaultLogger is a convenience that creates a logger and sets it as the
// slog default so that callers can use slog.Info / slog.Debug / etc. directly.
func SetDefaultLogger(env, level string) *slog.Logger {
	writer := os.Stderr
	if strings.EqualFold(env, "development") {
		writer = os.Stdout
	}

	logger := NewLogger(env, level, writer)
	slog.SetDefault(logger)
	return logger
}

// WithContext returns a child logger with the supplied key-value pairs attached.
// This is a thin wrapper around slog.Logger.With for discoverability.
func WithContext(logger *slog.Logger, keysAndValues ...any) *slog.Logger {
	return logger.With(keysAndValues...)
}

// HTTPRequestLogger is middleware that logs each incoming HTTP request with
// method, path, status code, and duration.
func HTTPRequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(sw, r)

			logger.Info("http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", sw.status),
				slog.Int64(KeyDurationMS, time.Since(start).Milliseconds()),
			)
		})
	}
}

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (sw *statusWriter) WriteHeader(code int) {
	if !sw.wroteHeader {
		sw.status = code
		sw.wroteHeader = true
		sw.ResponseWriter.WriteHeader(code)
	}
}

func (sw *statusWriter) Write(b []byte) (int, error) {
	if !sw.wroteHeader {
		sw.WriteHeader(http.StatusOK)
	}
	return sw.ResponseWriter.Write(b)
}

// Flush delegates to the underlying ResponseWriter if it implements http.Flusher.
func (sw *statusWriter) Flush() {
	if f, ok := sw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter, allowing callers to recover
// optional interfaces (http.Hijacker, http.Pusher, etc.) via type assertion.
func (sw *statusWriter) Unwrap() http.ResponseWriter {
	return sw.ResponseWriter
}
