// Command musicli is a TUI music player.
//
// This is the entry point. Phase 0 wires up: config load, logger, root
// context with signal handling, and deferred cleanup. The TUI and audio
// engine are added in later phases.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/locxl/musicli/internal/config"
	"github.com/locxl/musicli/internal/log"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "musicli: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	xdg := config.DefaultDirs()

	// Load config (writes default on first run).
	cfg, warnings, err := config.Load(xdg.ConfigPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Logger: truncate-on-start per spec.
	logger, err := log.New(cfg.Log.Level, cfg.Log.File)
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer logger.Close()

	for _, w := range warnings {
		logger.Warn("config warning", "msg", w)
	}
	logger.Info("musicli starting",
		"config", xdg.ConfigPath(),
		"state", xdg.StateDir,
		"cache", xdg.CacheDir,
	)

	// Root context: cancelled on SIGINT/SIGTERM. Child packages receive it
	// to shut down goroutines (ffmpeg reader, theme watcher, etc.).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// TODO(phase 1+): init audio engine with ctx, init UI, run bubbletea.
	_ = ctx
	_ = cfg

	// Phase 0 placeholder: exit cleanly.
	logger.Info("musicli exiting (phase 0 placeholder)")
	return nil
}
