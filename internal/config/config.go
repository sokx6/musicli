package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

//go:embed config.example.toml
var defaultConfigTOML string

// Config is the full musicli configuration loaded from TOML.
type Config struct {
	Audio       Audio       `toml:"audio"`
	Playback    Playback    `toml:"playback"`
	Library     Library     `toml:"library"`
	Lyrics      Lyrics      `toml:"lyrics"`
	Cover       Cover       `toml:"cover"`
	Theme       Theme       `toml:"theme"`
	UI          UI          `toml:"ui"`
	Keybindings Keybindings `toml:"keybindings"`
	Log         Log         `toml:"log"`

	// Dirs is not in TOML; it is resolved at load time.
	Dirs Dirs `toml:"-"`
}

type Audio struct {
	Volume int     `toml:"volume"`
	Speed  float64 `toml:"speed"`
}

type Playback struct {
	Repeat  string `toml:"repeat"`
	Shuffle bool   `toml:"shuffle"`
}

type Library struct {
	SortField    string `toml:"sort_field"`
	SortOrder    string `toml:"sort_order"`
	GroupByAlbum bool   `toml:"group_by_album"`
}

type Lyrics struct {
	AutoFetch bool     `toml:"auto_fetch"`
	Sources   []string `toml:"sources"`
	SaveDir   string   `toml:"save_dir"`
	Align     string   `toml:"align"`
}

type Cover struct {
	Show     bool   `toml:"show"`
	Protocol string `toml:"protocol"`
	Scale    string `toml:"scale"`
}

type Theme struct {
	Mode string `toml:"mode"`
	Name string `toml:"name"`
}

type UI struct {
	TrackListMaxWidth int `toml:"track_list_max_width"`
}

type Keybindings struct {
	PlayPause []string `toml:"play_pause"`
	Next      []string `toml:"next"`
	Prev      []string `toml:"prev"`
	SeekFwd   []string `toml:"seek_fwd"`
	SeekBack  []string `toml:"seek_back"`
	VolUp     []string `toml:"vol_up"`
	VolDown   []string `toml:"vol_down"`
}

type Log struct {
	Level string `toml:"level"`
	File  string `toml:"file"`
}

// Load reads config from path. If the file does not exist, it writes the
// embedded default config there first. Missing or invalid fields fall back
// to defaults. Returns the resolved config plus any warnings (non-fatal).
func Load(path string) (*Config, []string, error) {
	warnings := []string{}

	// Ensure config dir exists.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, nil, fmt.Errorf("create config dir: %w", err)
	}

	// First run: write default config.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if werr := os.WriteFile(path, []byte(defaultConfigTOML), 0o644); werr != nil {
			return nil, nil, fmt.Errorf("write default config: %w", werr)
		}
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read config %q: %w", path, err)
	}

	cfg := Defaults()
	cfg.Dirs = DefaultDirs()

	if err := toml.Unmarshal(raw, cfg); err != nil {
		return nil, nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.applyDefaults(&warnings)
	cfg.expandPaths()

	return cfg, warnings, nil
}

// Defaults returns a Config populated with built-in defaults.
func Defaults() *Config {
	c := &Config{}
	if err := toml.Unmarshal([]byte(defaultConfigTOML), c); err != nil {
		// embedded default is compile-time fixed; panic is acceptable
		panic(fmt.Sprintf("invalid embedded default config: %v", err))
	}
	return c
}

// applyDefaults clamps/validates fields and records warnings for invalid values.
func (c *Config) applyDefaults(warnings *[]string) {
	if c.Audio.Volume < 0 || c.Audio.Volume > 100 {
		*warnings = append(*warnings, fmt.Sprintf("audio.volume %d out of range, using 80", c.Audio.Volume))
		c.Audio.Volume = 80
	}
	if c.Audio.Speed < 0.5 || c.Audio.Speed > 2.0 {
		*warnings = append(*warnings, fmt.Sprintf("audio.speed %.2f out of range, using 1.0", c.Audio.Speed))
		c.Audio.Speed = 1.0
	}
	switch c.Playback.Repeat {
	case "none", "one", "list":
	default:
		*warnings = append(*warnings, fmt.Sprintf("playback.repeat %q invalid, using list", c.Playback.Repeat))
		c.Playback.Repeat = "list"
	}
	switch c.Lyrics.Align {
	case "", "left":
		c.Lyrics.Align = "left"
	case "center", "right":
	default:
		*warnings = append(*warnings, fmt.Sprintf("lyrics.align %q invalid, using left", c.Lyrics.Align))
		c.Lyrics.Align = "left"
	}
	switch c.Library.SortField {
	case "title", "artist", "album", "size", "year":
	default:
		*warnings = append(*warnings, fmt.Sprintf("library.sort_field %q invalid, using title", c.Library.SortField))
		c.Library.SortField = "title"
	}
	switch c.Library.SortOrder {
	case "asc", "desc":
	default:
		*warnings = append(*warnings, fmt.Sprintf("library.sort_order %q invalid, using asc", c.Library.SortOrder))
		c.Library.SortOrder = "asc"
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	switch c.Log.Level {
	case "debug", "info", "warning", "error":
	default:
		*warnings = append(*warnings, fmt.Sprintf("log.level %q invalid, using info", c.Log.Level))
		c.Log.Level = "info"
	}
	if c.UI.TrackListMaxWidth < 0 {
		*warnings = append(*warnings, fmt.Sprintf("ui.track_list_max_width %d out of range, using 0", c.UI.TrackListMaxWidth))
		c.UI.TrackListMaxWidth = 0
	}
	switch c.Cover.Scale {
	case "", "fit":
		c.Cover.Scale = "fit"
	case "stretch":
	default:
		*warnings = append(*warnings, fmt.Sprintf("cover.scale %q invalid, using fit", c.Cover.Scale))
		c.Cover.Scale = "fit"
	}
	switch c.Cover.Protocol {
	case "", "auto":
		c.Cover.Protocol = "auto"
	case "kitty", "halfblock", "sixel", "iterm":
	default:
		*warnings = append(*warnings, fmt.Sprintf("cover.protocol %q invalid, using auto", c.Cover.Protocol))
		c.Cover.Protocol = "auto"
	}
}

// expandPaths resolves ~ and empty path placeholders against XDG dirs.
func (c *Config) expandPaths() {
	if c.Log.File == "" {
		c.Log.File = c.Dirs.LogPath()
	} else {
		c.Log.File = expandHome(c.Log.File)
	}
	if c.Lyrics.SaveDir == "" {
		c.Lyrics.SaveDir = c.Dirs.LyricsDir()
	} else {
		c.Lyrics.SaveDir = expandHome(c.Lyrics.SaveDir)
	}
}

func expandHome(p string) string {
	if p == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}
