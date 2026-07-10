// Package config resolves XDG base directories for musicli.
package config

import (
	"os"
	"path/filepath"
)

// Dirs holds the resolved XDG paths for musicli. All fields are directory
// paths (strings); use the helper methods to get specific file paths.
type Dirs struct {
	ConfigDir string // ~/.config/musicli/ (or $XDG_CONFIG_HOME/musicli)
	StateDir  string // ~/.local/state/musicli/ (or $XDG_STATE_HOME/musicli)
	CacheDir  string // ~/.cache/musicli/ (or $XDG_CACHE_HOME/musicli)
}

// DefaultDirs resolves XDG paths using stdlib (respects XDG_*_HOME env vars).
func DefaultDirs() Dirs {
	return Dirs{
		ConfigDir: filepath.Join(userConfigDir(), "musicli"),
		StateDir:  filepath.Join(userStateDir(), "musicli"),
		CacheDir:  filepath.Join(userCacheDir(), "musicli"),
	}
}

// ConfigPath returns the config.toml path.
func (d Dirs) ConfigPath() string { return filepath.Join(d.ConfigDir, "config.toml") }

// LogPath returns the log file path.
func (d Dirs) LogPath() string { return filepath.Join(d.StateDir, "musicli.log") }

// PlaylistPath returns the playlists.json path.
func (d Dirs) PlaylistPath() string { return filepath.Join(d.StateDir, "playlists.json") }

// LibraryIndexPath returns the persistent library metadata index path.
func (d Dirs) LibraryIndexPath() string { return filepath.Join(d.StateDir, "library-index.json") }

// LyricsDir returns the fetched-lyrics cache dir.
func (d Dirs) LyricsDir() string { return filepath.Join(d.CacheDir, "lyrics") }

func userConfigDir() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	d, err := os.UserConfigDir() // ~/.config on Linux
	if err != nil || d == "" {
		return filepath.Join(os.Getenv("HOME"), ".config")
	}
	return d
}

func userStateDir() string {
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return v
	}
	return filepath.Join(os.Getenv("HOME"), ".local", "state")
}

func userCacheDir() string {
	d, err := os.UserCacheDir() // ~/.cache on Linux, respects XDG_CACHE_HOME
	if err != nil || d == "" {
		return filepath.Join(os.Getenv("HOME"), ".cache")
	}
	return d
}
