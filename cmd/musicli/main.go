// Command musicli is a TUI music player.
//
// Usage: musicli [path]
//
//	path - a file or directory to scan for music (default: current dir)
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

	fl := logger.WithFunc("run")

	for _, w := range warnings {
		logger.Warn("config warning", "msg", w)
	}

	fl.Info("config loaded",
		"audio.volume", cfg.Audio.Volume,
		"audio.speed", cfg.Audio.Speed,
		"playback.repeat", cfg.Playback.Repeat,
		"playback.shuffle", cfg.Playback.Shuffle,
		"library.sort_field", cfg.Library.SortField,
		"library.sort_order", cfg.Library.SortOrder,
		"library.group_by_album", cfg.Library.GroupByAlbum,
		"lyrics.auto_fetch", cfg.Lyrics.AutoFetch,
		"lyrics.sources", cfg.Lyrics.Sources,
		"lyrics.save_dir", cfg.Lyrics.SaveDir,
		"cover.show", cfg.Cover.Show,
		"cover.protocol", cfg.Cover.Protocol,
		"theme.mode", cfg.Theme.Mode,
		"theme.name", cfg.Theme.Name,
		"ui.track_list_max_width", cfg.UI.TrackListMaxWidth,
		"log.level", cfg.Log.Level,
		"log.file", cfg.Log.File,
		"config_path", xdg.ConfigPath(),
		"data_dir", xdg.StateDir,
		"cache_dir", xdg.CacheDir,
	)

	scanPath := "."
	if len(os.Args) > 1 {
		scanPath = os.Args[1]
	}
	fl.Info("scan path resolved", "path", scanPath)

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
	fl.Info("audio engine created", "volume", cfg.Audio.Volume, "speed", cfg.Audio.Speed)

	// Library scanner.
	sc := library.NewScanner(logger)
	fl.Info("scanner created")

	// Theme + UI.
	t := theme.Default()
	modeStr := "dark"
	if t.Mode == theme.ModeLight {
		modeStr = "light"
	}
	fl.Info("theme loaded", "mode", modeStr, "name", cfg.Theme.Name)

	app := ui.NewWithOptions(eng, sc, t, logger, ui.Options{
		TrackListMaxWidth: cfg.UI.TrackListMaxWidth,
		DisableCover:      !cfg.Cover.Show,
	})
	fl.Info("ui app created")

	// Start bubbletea. v2: alt-screen is implicit via View; mouse mode is
	// set on the View returned by the model's View().
	p := tea.NewProgram(app, tea.WithFPS(30))
	fl.Info("bubbletea program created", "fps", 30)

	// Kick off the library scan in a goroutine; deliver results via Send.
	go func() {
		fl.Info("scan goroutine launched", "path", scanPath)
		tracks, err := sc.ScanPath(scanPath)
		if err != nil {
			p.Send(ui.ScanErrMsg{Err: err})
			return
		}
		p.Send(ui.TracksLoadedMsg{Tracks: tracks})
	}()

	fl.Info("program Run starting")
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("run tui: %w", err)
	}
	fl.Info("program Run returned")

	eng.Stop()
	fl.Info("engine Stop called")

	fl.Info("musicli exiting")
	return nil
}
