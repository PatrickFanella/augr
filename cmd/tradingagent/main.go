package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"

	"github.com/PatrickFanella/get-rich-quick/internal/cli"
	"github.com/PatrickFanella/get-rich-quick/internal/config"
	_ "github.com/PatrickFanella/get-rich-quick/internal/data/alphavantage"
	_ "github.com/PatrickFanella/get-rich-quick/internal/data/binance"
	_ "github.com/PatrickFanella/get-rich-quick/internal/data/polygon"
	_ "github.com/PatrickFanella/get-rich-quick/internal/data/yahoo"
)

var version = "dev"

func main() {
	if err := cli.Execute(context.Background(), cli.Dependencies{
		Version:      version,
		NewAPIServer: newAPIServer,
	}); err != nil {
		log.Fatalf("tradingagent: %v", err)
	}
}

func newHTTPHandler(logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("all-ok"))
	})

	return config.HTTPRequestLogger(logger)(mux)
}
