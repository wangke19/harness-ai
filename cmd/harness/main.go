package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/wangke19/harness-system/config"
)

func main() {
	cfgPath := flag.String("config", "config/config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	slog.Info("harness starting", "workers", cfg.Server.WorkerCount, "poll_interval", cfg.Server.PollInterval)
	// components wired in later tasks
	select {} // block until signal
}
