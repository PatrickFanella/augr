package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/config"
	_ "github.com/PatrickFanella/get-rich-quick/internal/data/alphavantage"
	_ "github.com/PatrickFanella/get-rich-quick/internal/data/binance"
	_ "github.com/PatrickFanella/get-rich-quick/internal/data/polygon"
	_ "github.com/PatrickFanella/get-rich-quick/internal/data/yahoo"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	level := os.Getenv("LOG_LEVEL")
	if level == "" {
		level = "info"
	}

	logger := config.SetDefaultLogger(cfg.Environment, level)
	logger.Info("starting trading agent",
		slog.String("env", cfg.Environment),
		slog.String("log_level", level),
	)

	addr := net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port))
	fmt.Printf("Trading Agent configured for %s on %s\n", cfg.Environment, addr)

	server := &http.Server{
		Addr:              addr,
		Handler:           newHTTPHandler(logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, server); err != nil {
		log.Fatalf("serve http: %v", err)
	}

	logger.Info("trading agent stopped")
}

func run(ctx context.Context, server *http.Server) error {
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	err := <-serverErr
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func newHTTPHandler(logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	return config.HTTPRequestLogger(logger)(mux)
}
