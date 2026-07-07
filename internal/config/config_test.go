package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsRoundtrip(t *testing.T) {
	c := Defaults()
	if c.Audio.Volume != 80 {
		t.Errorf("default volume = %d, want 80", c.Audio.Volume)
	}
	if c.Audio.Speed != 1.0 {
		t.Errorf("default speed = %v, want 1.0", c.Audio.Speed)
	}
	if c.Playback.Repeat != "list" {
		t.Errorf("default repeat = %q, want list", c.Playback.Repeat)
	}
	if c.Log.Level != "debug" {
		t.Errorf("default log level = %q, want debug", c.Log.Level)
	}
	if c.UI.TrackListMaxWidth != 80 {
		t.Errorf("default track_list_max_width = %d, want 80", c.UI.TrackListMaxWidth)
	}
	if c.Cover.Scale != "fit" {
		t.Errorf("default cover scale = %q, want fit", c.Cover.Scale)
	}
}

func TestLoadCreatesDefaultOnFirstRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "musicli", "config.toml")
	c, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings on fresh default, got %v", warnings)
	}
	if c.Audio.Volume != 80 {
		t.Errorf("volume = %d, want 80", c.Audio.Volume)
	}
	// file should now exist
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config file not created: %v", err)
	}
}

func TestLoadClampsInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	invalid := `
[audio]
volume = 200
speed = 5.0
[playback]
repeat = "bogus"
[library]
sort_field = "nonsense"
[ui]
track_list_max_width = -1
[cover]
scale = "warped"
protocol = "bad"
[log]
level = "wat"
`
	if err := os.WriteFile(path, []byte(invalid), 0o644); err != nil {
		t.Fatal(err)
	}
	c, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Audio.Volume != 80 {
		t.Errorf("clamped volume = %d, want 80", c.Audio.Volume)
	}
	if c.Audio.Speed != 1.0 {
		t.Errorf("clamped speed = %v, want 1.0", c.Audio.Speed)
	}
	if c.Playback.Repeat != "list" {
		t.Errorf("clamped repeat = %q, want list", c.Playback.Repeat)
	}
	if c.Library.SortField != "title" {
		t.Errorf("clamped sort_field = %q, want title", c.Library.SortField)
	}
	if c.Log.Level != "info" {
		t.Errorf("clamped log level = %q, want info", c.Log.Level)
	}
	if c.UI.TrackListMaxWidth != 0 {
		t.Errorf("clamped track_list_max_width = %d, want 0", c.UI.TrackListMaxWidth)
	}
	if c.Cover.Scale != "fit" {
		t.Errorf("clamped cover scale = %q, want fit", c.Cover.Scale)
	}
	if c.Cover.Protocol != "auto" {
		t.Errorf("clamped cover protocol = %q, want auto", c.Cover.Protocol)
	}
	if len(warnings) < 8 {
		t.Errorf("expected >=8 warnings, got %d: %v", len(warnings), warnings)
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := []struct{ in, want string }{
		{"~", home},
		{"~/foo/bar", filepath.Join(home, "foo", "bar")},
		{"/abs/path", "/abs/path"},
		{"relative", "relative"},
	}
	for _, c := range cases {
		got := expandHome(c.in)
		if got != c.want {
			t.Errorf("expandHome(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDirsResolved(t *testing.T) {
	d := DefaultDirs()
	if d.ConfigDir == "" || d.StateDir == "" || d.CacheDir == "" {
		t.Errorf("dirs not resolved: %+v", d)
	}
	if d.LogPath() == "" {
		t.Error("LogPath empty")
	}
}
