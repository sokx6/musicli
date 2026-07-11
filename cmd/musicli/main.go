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
	"github.com/locxl/musicli/internal/mpris"
	"github.com/locxl/musicli/internal/playlist"
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
		"lyrics.align", cfg.Lyrics.Align,
		"cover.show", cfg.Cover.Show,
		"cover.protocol", cfg.Cover.Protocol,
		"cover.scale", cfg.Cover.Scale,
		"dbus.mpris", cfg.DBus.MPRIS,
		"dbus.lyrics", cfg.DBus.Lyrics,
		"theme.mode", cfg.Theme.Mode,
		"theme.dark", cfg.Theme.Dark,
		"theme.light", cfg.Theme.Light,
		"ui.track_list_max_width", cfg.UI.TrackListMaxWidth,
		"ui.progress_style", cfg.UI.ProgressStyle,
		"ui.separator_progress_thickness", cfg.UI.SeparatorProgressThickness,
		"log.level", cfg.Log.Level,
		"log.file", cfg.Log.File,
		"config_path", xdg.ConfigPath(),
		"data_dir", xdg.StateDir,
		"cache_dir", xdg.CacheDir,
	)

	scanPath := "."
	if len(os.Args) > 1 {
		scanPath = os.Args[1]
	} else if cfg.Library.MusicDir != "" {
		scanPath = cfg.Library.MusicDir
	}
	fl.Info("scan path resolved", "path", scanPath)

	// Root context: cancelled on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Audio engine (oto context created once, reused).
	eng, err := audio.NewWithOptions(ctx, logger, audio.Options{SpectrumUpdateHz: cfg.Spectrum.UpdateHz})
	if err != nil {
		return fmt.Errorf("init audio: %w", err)
	}
	eng.SetVolume(cfg.Audio.Volume)
	eng.SetSpeed(cfg.Audio.Speed)
	fl.Info("audio engine created", "volume", cfg.Audio.Volume, "speed", cfg.Audio.Speed, "spectrum.update_hz", cfg.Spectrum.UpdateHz)

	// Library scanner.
	sc := library.NewScanner(logger)
	fl.Info("scanner created")

	playlistStore, err := playlist.Load(xdg.PlaylistPath())
	if err != nil {
		return fmt.Errorf("load playlists: %w", err)
	}
	fl.Info("playlists loaded", "count", len(playlistStore.Playlists), "path", xdg.PlaylistPath())

	// Theme + UI.
	resolvedThemeMode, themeWarnings := theme.ResolveMode(cfg.Theme.Mode)
	for _, warning := range themeWarnings {
		fl.Warn("theme warning", "msg", warning)
	}
	loadTheme := func(mode theme.Mode) *theme.Theme {
		path := cfg.Theme.Dark
		if mode == theme.ModeLight {
			path = cfg.Theme.Light
		}
		loaded, warnings := theme.Load(path, mode)
		for _, warning := range warnings {
			fl.Warn("theme warning", "msg", warning)
		}
		return loaded
	}
	t := loadTheme(resolvedThemeMode)
	modeStr := "dark"
	if t.Mode == theme.ModeLight {
		modeStr = "light"
	}
	fl.Info("theme loaded", "mode", modeStr, "name", t.Name)

	var mprisSvc *mpris.LazyService
	if cfg.DBus.MPRIS || cfg.DBus.Lyrics {
		mprisSvc = mpris.NewLazyService(ctx, logger, cfg.DBus.MPRIS, cfg.DBus.Lyrics, nil)
		defer mprisSvc.Close()
	}

	app := ui.NewWithOptions(eng, sc, t, logger, ui.Options{
		TrackListMaxWidth:          cfg.UI.TrackListMaxWidth,
		ProgressStyle:              cfg.UI.ProgressStyle,
		SeparatorProgressThickness: cfg.UI.SeparatorProgressThickness,
		DisableCover:               !cfg.Cover.Show,
		CoverScale:                 cfg.Cover.Scale,
		CoverProtocol:              cfg.Cover.Protocol,
		LibrarySortField:           cfg.Library.SortField,
		LibrarySortOrder:           cfg.Library.SortOrder,
		GroupByAlbum:               cfg.Library.GroupByAlbum,
		PlaybackRepeat:             cfg.Playback.Repeat,
		PlaybackShuffle:            cfg.Playback.Shuffle,
		LyricsAlign:                cfg.Lyrics.Align,
		LyricsHighlightMode:        cfg.Lyrics.HighlightMode,
		SpectrumEnabled:            cfg.Spectrum.Enabled,
		Keybindings:                cfg.Keybindings,
		PlaylistStore:              playlistStore,
		MPRISSink: func(snapshot mpris.Snapshot) {
			if mprisSvc != nil {
				mprisSvc.Update(snapshot)
			}
		},
	})
	fl.Info("ui app created")

	// Start bubbletea. v2: alt-screen is implicit via View; mouse mode is
	// set on the View returned by the model's View().
	p := tea.NewProgram(app, tea.WithFPS(30))
	fl.Info("bubbletea program created", "fps", 30)

	if cfg.Theme.Mode == "auto" {
		go theme.WatchSystemMode(ctx, func(mode theme.Mode) {
			p.Send(ui.ThemeChangedMsg{Theme: loadTheme(mode)})
		})
	}

	if mprisSvc != nil {
		go func() {
			for command := range mprisSvc.Commands() {
				p.Send(ui.DBusCommandMsg{Command: command})
			}
		}()
	}

	// Kick off the library scan in a goroutine; deliver results via Send.
	go func() {
		fl.Info("scan goroutine launched", "path", scanPath)
		tracks, err := sc.ScanPathCached(scanPath, xdg.LibraryIndexPath(), cfg.Library.IndexCache)
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
