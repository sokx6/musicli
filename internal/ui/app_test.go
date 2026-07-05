package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/locxl/musicli/internal/library"
	"github.com/locxl/musicli/internal/log"
	"github.com/locxl/musicli/internal/theme"
)

func TestTrackListWidthFitsContentAndStaysRightAligned(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		TrackListMaxWidth: 80,
	})

	m, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	app = m.(*App)
	m, _ = app.Update(TracksLoadedMsg{Tracks: []*library.Track{
		{Title: "Short", Artist: "A", Album: "B", Duration: 60000},
	}})
	app = m.(*App)

	const wantListW = 15 // len("Tracks (1)") + title padding + status gap.
	if got := app.trackList.Width(); got != wantListW {
		t.Fatalf("track list width = %d, want %d", got, wantListW)
	}
	if got := app.leftPaneWidth(); got != 120-wantListW {
		t.Fatalf("left pane width = %d, want %d", got, 120-wantListW)
	}
}

func TestTrackListWidthShrinksToKeepLeftPaneWhenContentIsTooWide(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		TrackListMaxWidth: 200,
	})

	m, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	app = m.(*App)
	m, _ = app.Update(TracksLoadedMsg{Tracks: []*library.Track{
		{
			Title:  "this title is intentionally much wider than the available list area",
			Artist: "artist",
			Album:  "album",
		},
	}})
	app = m.(*App)

	const wantLeftW = 40
	const wantListW = 60
	if got := app.leftPaneWidth(); got != wantLeftW {
		t.Fatalf("left pane width = %d, want %d", got, wantLeftW)
	}
	if got := app.trackList.Width(); got != wantListW {
		t.Fatalf("track list width = %d, want %d", got, wantListW)
	}
}

func TestTrackListMaxWidthCapsContentWidth(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		TrackListMaxWidth: 32,
	})

	m, _ := app.Update(tea.WindowSizeMsg{Width: 160, Height: 24})
	app = m.(*App)
	m, _ = app.Update(TracksLoadedMsg{Tracks: []*library.Track{
		{
			Title:  "this title is intentionally wider than the configured maximum",
			Artist: "artist",
			Album:  "album",
		},
	}})
	app = m.(*App)

	if got := app.trackList.Width(); got != 32 {
		t.Fatalf("track list width = %d, want 32", got)
	}
	if got := app.leftPaneWidth(); got != 128 {
		t.Fatalf("left pane width = %d, want 128", got)
	}
}

func TestLoadCurrentLyricsShowsCurrentLine(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(audio, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "song.lrc"), []byte("[00:01.00]First\n[00:03.00]Second"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	m, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	app = m.(*App)
	m, _ = app.Update(TracksLoadedMsg{Tracks: []*library.Track{{Path: audio, Title: "Song"}}})
	app = m.(*App)

	app.current = 0
	app.pos = 3500
	app.loadCurrentLyrics()

	view := app.renderLeftPane()
	if !strings.Contains(view, "Second") {
		t.Fatalf("left pane missing current lyric line:\n%s", view)
	}
}

func TestRenderLyricsPaneIncludesWordTimedCurrentLine(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(audio, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "song.spl"), []byte("[00:01.00]Hello [00:02.00]world[00:03.00]"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	m, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	app = m.(*App)
	m, _ = app.Update(TracksLoadedMsg{Tracks: []*library.Track{{Path: audio, Title: "Song"}}})
	app = m.(*App)

	app.current = 0
	app.pos = 2500
	app.loadCurrentLyrics()

	view := app.renderLeftPane()
	if !strings.Contains(view, "Hello ") || !strings.Contains(view, "world") {
		t.Fatalf("left pane missing word-timed lyric text:\n%s", view)
	}
}
