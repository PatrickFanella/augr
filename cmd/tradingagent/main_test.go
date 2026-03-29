package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/cli"
	"github.com/PatrickFanella/get-rich-quick/internal/config"
)

func TestNewHTTPHandlerHealthz(t *testing.T) {
	logger := config.NewLogger("production", "info", &bytes.Buffer{})
	handler := newHTTPHandler(logger)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ServeHTTP() status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body != "ok" {
		t.Fatalf("ServeHTTP() body = %q, want %q", body, "ok")
	}
}

func TestRun_ReturnsStartupErrorWithoutBlocking(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()

	server := &http.Server{
		Addr:    listener.Addr().String(),
		Handler: http.NewServeMux(),
	}

	done := make(chan error, 1)
	go func() {
		done <- cli.RunServerLifecycle(context.Background(), server.ListenAndServe, server.Shutdown)
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("run() error = nil, want startup error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run() did not return after startup failure")
	}
}
