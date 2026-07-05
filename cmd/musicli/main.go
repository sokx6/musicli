// Command musicli is a TUI music player.
//
// Usage: musicli [path]
//   path - a file or directory to scan for music (default: current dir)
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"github.com/locxl/musicli/internal/audio"
	"github.com/locxl/musicli/internal/config"
	"github.com/locxl/musicli/internal/library"
	"github.com/locxl/musicli/internal/log"
	"github.com/locxl/musicli/internal/theme"
	"github.com/locxl/musicli/internal/ui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "musicli: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	xdg := config.DefaultDirs()

	cfg, warnings, err := config.Load(xdg.ConfigPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger, err := log.New(cfg.Log.Level, cfg.Log.File)
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer logger.Close()

	for _, w := range warnings {
		logger.Warn("config warning", "msg", w)
	}

	scanPath := "."
	if len(os.Args) > 1 {
		scanPath = os.Args[1]
	}

	logger.Info("musicli starting",
		"config", xdg.ConfigPath(),
		"scan_path", scanPath,
	)

	// Root context: cancelled on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Audio engine (oto context created once, reused).
	eng, err := audio.New(ctx, logger)
	if err != nil {
		return fmt.Errorf("init audio: %w", err)
	}
	eng.SetVolume(cfg.Audio.Volume)
	eng.SetSpeed(cfg.Audio.Speed)

	// Library scanner.
	sc := library.NewScanner(logger)

	// Theme + UI.
	t := theme.Default()
	app := ui.New(eng, sc, t, logger)

	// Start bubbletea. v2: alt-screen is implicit via View; mouse mode is
	// set on the View returned by the model's View().
	p := tea.NewProgram(app, tea.WithFPS(30))

	// Kick off the library scan in a goroutine; deliver results via Send.
	go func() {
		tracks, err := sc.ScanPath(scanPath)
		if err != nil {
			p.Send(ui.ScanErrMsg{Err: err})
			return
		}
		p.Send(ui.TracksLoadedMsg{Tracks: tracks})
	}()

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("run tui: %w", err)
	}

	eng.Stop()
	logger.Info("musicli exiting")
	return nil
}
