package notification

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"strings"
	"testing"
	"time"
)

func TestTelegramNotifierNotify(t *testing.T) {
	t.Parallel()

	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottest-token/sendMessage" {
			t.Fatalf("r.URL.Path = %q, want %q", r.URL.Path, "/bottest-token/sendMessage")
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewTelegramNotifier("test-token", "12345")
	notifier.apiBaseURL = server.URL
	notifier.httpClient = server.Client()

	err := notifier.Notify(context.Background(), Alert{
		Title:      "Circuit breaker tripped",
		Body:       "Loss threshold exceeded",
		Severity:   SeverityCritical,
		OccurredAt: time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Notify() error = %v", err)
	}

	if got := requestBody["chat_id"]; got != "12345" {
		t.Fatalf("chat_id = %v, want %q", got, "12345")
	}
	text, _ := requestBody["text"].(string)
	if !strings.Contains(text, "Circuit breaker tripped") {
		t.Fatalf("text = %q, want title", text)
	}
}

func TestEmailNotifierNotify(t *testing.T) {
	t.Parallel()

	var (
		addr string
		from string
		to   []string
		msg  []byte
	)

	notifier := NewEmailNotifier("smtp.example.com", 2525, "user", "pass", "alerts@example.com", []string{"ops@example.com"})
	notifier.sendMail = func(gotAddr string, _ smtp.Auth, gotFrom string, gotTo []string, gotMsg []byte) error {
		addr = gotAddr
		from = gotFrom
		to = append([]string(nil), gotTo...)
		msg = append([]byte(nil), gotMsg...)
		return nil
	}

	err := notifier.Notify(context.Background(), Alert{
		Title:      "Pipeline failure threshold reached",
		Body:       "Pipeline execution failed 3 consecutive times.",
		Severity:   SeverityCritical,
		OccurredAt: time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Notify() error = %v", err)
	}

	if addr != "smtp.example.com:2525" {
		t.Fatalf("addr = %q, want %q", addr, "smtp.example.com:2525")
	}
	if from != "alerts@example.com" {
		t.Fatalf("from = %q, want %q", from, "alerts@example.com")
	}
	if len(to) != 1 || to[0] != "ops@example.com" {
		t.Fatalf("to = %v, want [ops@example.com]", to)
	}
	if !strings.Contains(string(msg), "Subject: Pipeline failure threshold reached") {
		t.Fatalf("message = %q, want subject header", string(msg))
	}
}

func TestWebhookNotifierNotify(t *testing.T) {
	t.Parallel()

	var (
		gotSecret string
		payload   map[string]any
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSecret = r.Header.Get("X-Webhook-Secret")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	notifier := NewWebhookNotifier(server.URL, "super-secret")
	notifier.httpClient = server.Client()

	err := notifier.Notify(context.Background(), Alert{
		Key:        "db_connection_loss",
		Title:      "Database connection lost",
		Body:       "The application could not reach the configured database.",
		Severity:   SeverityCritical,
		OccurredAt: time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC),
		Metadata:   map[string]string{"error": "dial tcp: connection refused"},
	})
	if err != nil {
		t.Fatalf("Notify() error = %v", err)
	}

	if gotSecret != "super-secret" {
		t.Fatalf("X-Webhook-Secret = %q, want %q", gotSecret, "super-secret")
	}
	if payload["key"] != "db_connection_loss" {
		t.Fatalf("payload[key] = %v, want %q", payload["key"], "db_connection_loss")
	}
}
