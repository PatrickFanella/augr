package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/PatrickFanella/get-rich-quick/internal/config"
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

	fmt.Printf("Trading Agent configured for %s on %s:%d\n", cfg.Environment, cfg.Server.Host, cfg.Server.Port)
}
