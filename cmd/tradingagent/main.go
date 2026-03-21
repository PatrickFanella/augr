package main

import (
	"fmt"
	"log"

	"github.com/PatrickFanella/get-rich-quick/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	fmt.Printf("Trading Agent configured for %s on %s:%d\n", cfg.Environment, cfg.Server.Host, cfg.Server.Port)
}
