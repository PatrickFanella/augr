package main

import (
	"log/slog"
	"os"
	"fmt"
	"log"

	"github.com/PatrickFanella/get-rich-quick/internal/config"
)

func main() {
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "development"
	}

	level := os.Getenv("LOG_LEVEL")
	if level == "" {
		level = "info"
	}

	logger := config.SetDefaultLogger(env, level)
	logger.Info("starting trading agent",
		slog.String("env", env),
		slog.String("log_level", level),
	)
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	fmt.Printf("Trading Agent configured for %s on %s:%d\n", cfg.Environment, cfg.Server.Host, cfg.Server.Port)
}
