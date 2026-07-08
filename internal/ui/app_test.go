package ui

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/locxl/musicli/internal/audio"
	"github.com/locxl/musicli/internal/cover"
	"github.com/locxl/musicli/internal/library"
	"github.com/locxl/musicli/internal/log"
	"github.com/locxl/musicli/internal/lyrics"
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
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})

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

func TestConfiguredTrackListMaxWidthControlsSingleColumnThreshold(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		TrackListMaxWidth: 50,
	})

	m, _ := app.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	app = m.(*App)
	m, _ = app.Update(TracksLoadedMsg{Tracks: []*library.Track{
		{
			Title:  "this title is intentionally wider than fifty cells",
			Artist: "artist",
		},
	}})
	app = m.(*App)

	if got := app.trackList.Width(); got != 50 {
		t.Fatalf("track list width = %d, want configured max 50", got)
	}
	if got := app.leftPaneWidth(); got != 10 {
		t.Fatalf("left pane width = %d, want remaining width 10", got)
	}
}

func TestWidthBelowConfiguredTrackListMaxWidthUsesSingleColumn(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		TrackListMaxWidth: 50,
	})

	m, _ := app.Update(tea.WindowSizeMsg{Width: 49, Height: 24})
	app = m.(*App)
	m, _ = app.Update(TracksLoadedMsg{Tracks: []*library.Track{
		{
			Title:  "this title is intentionally wider than fifty cells",
			Artist: "artist",
		},
	}})
	app = m.(*App)

	if got := app.leftPaneWidth(); got != 0 {
		t.Fatalf("left pane width = %d, want 0 in single-column mode", got)
	}
	if got := app.trackList.Width(); got != 49 {
		t.Fatalf("track list width = %d, want full terminal width 49", got)
	}
}

func TestTracksLoadedAppliesConfiguredSort(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		LibrarySortField: "year",
		LibrarySortOrder: "desc",
	})

	m, _ := app.Update(TracksLoadedMsg{Tracks: []*library.Track{
		{Path: "old.mp3", Title: "Old", Year: 1999},
		{Path: "new.mp3", Title: "New", Year: 2024},
		{Path: "mid.mp3", Title: "Mid", Year: 2010},
	}})
	app = m.(*App)

	if got := trackTitles(app.tracks); strings.Join(got, ",") != "New,Mid,Old" {
		t.Fatalf("sorted track titles = %v, want [New Mid Old]", got)
	}
	if got := app.trackList.Title; got != "Tracks (3)" {
		t.Fatalf("track list title = %q, want Tracks (3)", got)
	}
}

func TestPlaybackOptionsAreStored(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		PlaybackRepeat:  "one",
		PlaybackShuffle: true,
	})

	if app.options.PlaybackRepeat != "one" {
		t.Fatalf("playback repeat = %q, want one", app.options.PlaybackRepeat)
	}
	if !app.options.PlaybackShuffle {
		t.Fatal("playback shuffle = false, want true")
	}
}

func TestPlayTrackAtRejectsInvalidIndex(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	app.tracks = []*library.Track{{Path: "one.mp3", Title: "One"}}
	app.current = 0

	cmd := app.playTrackAt(-1)
	if cmd != nil {
		t.Fatalf("playTrackAt(-1) returned command %#v, want nil", cmd)
	}
	if app.current != 0 {
		t.Fatalf("current = %d, want unchanged 0", app.current)
	}

	cmd = app.playTrackAt(1)
	if cmd != nil {
		t.Fatalf("playTrackAt(1) returned command %#v, want nil", cmd)
	}
	if app.current != 0 {
		t.Fatalf("current = %d, want unchanged 0", app.current)
	}
}

func TestNextTrackIndexListOrderWraps(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		PlaybackRepeat: "list",
	})
	app.tracks = []*library.Track{
		{Title: "One"},
		{Title: "Two"},
		{Title: "Three"},
	}
	app.current = 1
	if got := app.nextTrackIndex(false); got != 2 {
		t.Fatalf("next from 1 = %d, want 2", got)
	}
	app.current = 2
	if got := app.nextTrackIndex(false); got != 0 {
		t.Fatalf("next from 2 = %d, want wrap to 0", got)
	}
}

func TestNextTrackIndexRepeatNoneStopsAtEnd(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		PlaybackRepeat: "none",
	})
	app.tracks = []*library.Track{
		{Title: "One"},
		{Title: "Two"},
	}
	app.current = 0
	if got := app.nextTrackIndex(true); got != 1 {
		t.Fatalf("auto next from 0 = %d, want 1", got)
	}
	app.current = 1
	if got := app.nextTrackIndex(true); got != -1 {
		t.Fatalf("auto next from final track = %d, want -1", got)
	}
	if got := app.nextTrackIndex(false); got != 0 {
		t.Fatalf("manual next from final track = %d, want wrap to 0", got)
	}
}

func TestNextTrackIndexRepeatOneReplaysOnAutoAdvance(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		PlaybackRepeat: "one",
	})
	app.tracks = []*library.Track{
		{Title: "One"},
		{Title: "Two"},
	}
	app.current = 1
	if got := app.nextTrackIndex(true); got != 1 {
		t.Fatalf("auto next in repeat one = %d, want current 1", got)
	}
	if got := app.nextTrackIndex(false); got != 0 {
		t.Fatalf("manual next in repeat one = %d, want normal wrap 0", got)
	}
}

func TestNextTrackIndexShuffleAvoidsCurrentWhenPossible(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		PlaybackRepeat:  "list",
		PlaybackShuffle: true,
	})
	app.tracks = []*library.Track{
		{Title: "One"},
		{Title: "Two"},
		{Title: "Three"},
	}
	app.current = 1
	for i := 0; i < 30; i++ {
		got := app.nextTrackIndex(true)
		if got < 0 || got >= len(app.tracks) {
			t.Fatalf("shuffle index = %d, want valid track index", got)
		}
		if got == app.current {
			t.Fatalf("shuffle index = current %d, want different track", got)
		}
	}
}

func TestTrackEndedNaturallyRequiresStoppedAtDurationAfterPlaying(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	app.wasPlaying = true
	app.state = audio.StateStopped
	app.pos = 1000
	app.dur = 1000
	if !app.trackEndedNaturally() {
		t.Fatal("trackEndedNaturally = false, want true")
	}
}

func TestTrackEndedNaturallyIgnoresStartupStoppedState(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	app.wasPlaying = false
	app.state = audio.StateStopped
	app.pos = 1000
	app.dur = 1000
	if app.trackEndedNaturally() {
		t.Fatal("trackEndedNaturally = true, want false")
	}
}

func TestTrackEndedNaturallyAllowsUnknownDuration(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	app.wasPlaying = true
	app.state = audio.StateStopped
	app.pos = 0
	app.dur = 0
	if !app.trackEndedNaturally() {
		t.Fatal("trackEndedNaturally = false, want true for clean stop with unknown duration")
	}
}

func TestTrackEndedNaturallyIgnoresEngineError(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	app.wasPlaying = true
	app.state = audio.StateStopped
	app.pos = 0
	app.dur = 0
	app.engineErr = fmt.Errorf("ffmpeg failed")
	if app.trackEndedNaturally() {
		t.Fatal("trackEndedNaturally = true, want false when engine has an error")
	}
}

func TestPlayerBarShowsPlaybackMode(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		PlaybackRepeat:  "one",
		PlaybackShuffle: true,
	})
	app.width = 100
	app.volume = 80
	app.speed = 1.0

	plain := ansi.Strip(app.renderPlayerBar())
	if !strings.Contains(plain, "repeat one") {
		t.Fatalf("player bar missing repeat mode:\n%s", plain)
	}
	if !strings.Contains(plain, "shuffle on") {
		t.Fatalf("player bar missing shuffle mode:\n%s", plain)
	}
}

func TestToggleRepeatCyclesModes(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		PlaybackRepeat: "list",
	})

	app.toggleRepeat()
	if app.options.PlaybackRepeat != "one" {
		t.Fatalf("after first toggle repeat = %q, want one", app.options.PlaybackRepeat)
	}
	app.toggleRepeat()
	if app.options.PlaybackRepeat != "none" {
		t.Fatalf("after second toggle repeat = %q, want none", app.options.PlaybackRepeat)
	}
	app.toggleRepeat()
	if app.options.PlaybackRepeat != "list" {
		t.Fatalf("after third toggle repeat = %q, want list", app.options.PlaybackRepeat)
	}
}

func TestToggleShuffleFlipsMode(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})

	app.toggleShuffle()
	if !app.options.PlaybackShuffle {
		t.Fatal("after first toggle shuffle = false, want true")
	}
	app.toggleShuffle()
	if app.options.PlaybackShuffle {
		t.Fatal("after second toggle shuffle = true, want false")
	}
}

func TestPlaybackModeKeysToggleModes(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		PlaybackRepeat: "list",
	})

	_, _ = app.handleKey(tea.KeyPressMsg(tea.Key{Text: "r"}))
	if app.options.PlaybackRepeat != "one" {
		t.Fatalf("repeat after r = %q, want one", app.options.PlaybackRepeat)
	}

	_, _ = app.handleKey(tea.KeyPressMsg(tea.Key{Text: "s"}))
	if !app.options.PlaybackShuffle {
		t.Fatal("shuffle after s = false, want true")
	}
}

func TestGroupByAlbumOptionDefaultsToAlbumView(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		GroupByAlbum: true,
	})

	m, _ := app.Update(TracksLoadedMsg{Tracks: []*library.Track{
		{Path: "a1.mp3", Title: "One", Album: "Alpha", AlbumArtist: "Artist A"},
		{Path: "a2.mp3", Title: "Two", Album: "Alpha", AlbumArtist: "Artist A"},
		{Path: "b1.mp3", Title: "Three", Album: "Beta", AlbumArtist: "Artist B"},
	}})
	app = m.(*App)

	if app.libraryView != libraryViewAlbums {
		t.Fatalf("library view = %v, want albums", app.libraryView)
	}
	if got := app.trackList.Title; got != "Albums (2)" {
		t.Fatalf("list title = %q, want Albums (2)", got)
	}
	if item, ok := app.trackList.SelectedItem().(albumItem); !ok || item.album.Name != "Alpha" {
		t.Fatalf("selected album item = %#v, want Alpha album", app.trackList.SelectedItem())
	}
}

func TestAlbumViewWidthUsesAlbumItems(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		GroupByAlbum: true,
	})

	m, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	app = m.(*App)
	m, _ = app.Update(TracksLoadedMsg{Tracks: []*library.Track{
		{Path: "a1.mp3", Title: "Short", Album: "A very very long album title", AlbumArtist: "Artist"},
	}})
	app = m.(*App)

	if got := app.trackList.Width(); got < ansi.StringWidth("A very very long album title") {
		t.Fatalf("album list width = %d, want enough for album title", got)
	}
}

func TestTabTogglesTracksAndAlbums(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	m, _ := app.Update(TracksLoadedMsg{Tracks: []*library.Track{
		{Path: "a1.mp3", Title: "One", Album: "Alpha"},
		{Path: "b1.mp3", Title: "Two", Album: "Beta"},
	}})
	app = m.(*App)

	if app.libraryView != libraryViewTracks {
		t.Fatalf("initial library view = %v, want tracks", app.libraryView)
	}
	_, _ = app.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	if app.libraryView != libraryViewAlbums {
		t.Fatalf("after tab library view = %v, want albums", app.libraryView)
	}
	_, _ = app.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	if app.libraryView != libraryViewTracks {
		t.Fatalf("second tab library view = %v, want tracks", app.libraryView)
	}
}

func TestAlbumEnterBackAndTrackMapping(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{GroupByAlbum: true})
	m, _ := app.Update(TracksLoadedMsg{Tracks: []*library.Track{
		{Path: "b2.mp3", Title: "Beta 2", Album: "Beta", TrackNo: 2},
		{Path: "a2.mp3", Title: "Alpha 2", Album: "Alpha", TrackNo: 2},
		{Path: "a1.mp3", Title: "Alpha 1", Album: "Alpha", TrackNo: 1},
	}})
	app = m.(*App)

	_, _ = app.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if app.libraryView != libraryViewAlbumTracks {
		t.Fatalf("after enter library view = %v, want album tracks", app.libraryView)
	}
	if got := app.trackList.Title; got != "Alpha (2)" {
		t.Fatalf("album track title = %q, want Alpha (2)", got)
	}
	if idx := app.selectedTrackIndex(); idx != 0 {
		t.Fatalf("selected album track index = %d, want 0 for Alpha 1 in sorted global tracks", idx)
	}

	_, _ = app.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	if app.libraryView != libraryViewTracks {
		t.Fatalf("tab from album tracks library view = %v, want tracks", app.libraryView)
	}
	_, _ = app.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	if app.libraryView != libraryViewAlbums {
		t.Fatalf("tab back to library view = %v, want albums", app.libraryView)
	}
	_, _ = app.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if app.libraryView != libraryViewAlbumTracks {
		t.Fatalf("after re-enter library view = %v, want album tracks", app.libraryView)
	}
	_, _ = app.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	if app.libraryView != libraryViewAlbums {
		t.Fatalf("after backspace library view = %v, want albums", app.libraryView)
	}
	_, _ = app.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if app.libraryView != libraryViewAlbumTracks {
		t.Fatalf("after third enter library view = %v, want album tracks", app.libraryView)
	}
	_, _ = app.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if app.libraryView != libraryViewAlbums {
		t.Fatalf("after escape library view = %v, want albums", app.libraryView)
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

func TestRenderLyricsPaneSeparatesTranslationPairsWithBlankLine(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(audio, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "song.lrc"), []byte("[00:01.00]Original one\nTranslation one\n[00:03.00]Original two\nTranslation two"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	m, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	app = m.(*App)
	m, _ = app.Update(TracksLoadedMsg{Tracks: []*library.Track{{Path: audio, Title: "Song"}}})
	app = m.(*App)

	app.current = 0
	app.pos = 1500
	app.loadCurrentLyrics()

	view := trimRightLines(stripANSI(app.renderLeftPane()))
	if !strings.Contains(view, " Original one\n Translation one\n\n Original two\n Translation two") {
		t.Fatalf("lyric pairs are not separated as expected:\n%s", view)
	}
	currentLine := app.renderCurrentLyricLine(app.lyric.Lines[0], 80)
	if strings.Contains(currentLine, "Translation one") {
		t.Fatalf("current highlighted line includes translation: %q", currentLine)
	}
	rawLines := strings.Split(app.renderLyricsPane(80, 8), "\n")
	translationRow := ""
	for _, line := range rawLines {
		if strings.Contains(line, "Translation one") {
			translationRow = line
			break
		}
	}
	if translationRow == "" {
		t.Fatal("translation row not rendered")
	}
	if strings.Contains(translationRow, "\x1b[1;") || strings.Contains(translationRow, "\x1b[1m") {
		t.Fatalf("translation row uses highlighted style: %q", translationRow)
	}
}

func TestRenderLyricsPaneDoesNotHighlightTranslationAtPairBoundary(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(audio, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "song.lrc"), []byte("[00:01.00]Original one\nTranslation one\n[00:03.00]Original two\nTranslation two"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	m, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	app = m.(*App)
	m, _ = app.Update(TracksLoadedMsg{Tracks: []*library.Track{{Path: audio, Title: "Song"}}})
	app = m.(*App)

	app.current = 0
	app.pos = 3000
	app.loadCurrentLyrics()

	rawLines := strings.Split(app.renderLyricsPane(80, 8), "\n")
	for _, line := range rawLines {
		if !strings.Contains(line, "Translation one") && !strings.Contains(line, "Translation two") {
			continue
		}
		if strings.Contains(line, "\x1b[1;") || strings.Contains(line, "\x1b[1m") {
			t.Fatalf("translation row uses highlighted style at pair boundary: %q", line)
		}
	}
}

func TestRenderCurrentLyricLineKeepsWideTextStable(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	const lineWidth = 40
	line := lyrics.Line{
		Text: "ツギハギだらけの君との時間も",
		Words: []lyrics.Word{
			{Text: "ツ", StartMs: 1000, EndMs: 1100},
			{Text: "ギ", StartMs: 1100, EndMs: 1200},
			{Text: "ハ", StartMs: 1200, EndMs: 1300},
			{Text: "ギ", StartMs: 1300, EndMs: 1400},
			{Text: "だ", StartMs: 1400, EndMs: 1500},
			{Text: "ら", StartMs: 1500, EndMs: 1600},
			{Text: "け", StartMs: 1600, EndMs: 1700},
			{Text: "の", StartMs: 1700, EndMs: 1800},
			{Text: "君", StartMs: 1800, EndMs: 2400},
			{Text: "と", StartMs: 2400, EndMs: 3000},
			{Text: "の", StartMs: 3000, EndMs: 3100},
			{Text: "時", StartMs: 3100, EndMs: 3200},
			{Text: "間", StartMs: 3200, EndMs: 3300},
			{Text: "も", StartMs: 3300, EndMs: 4000},
		},
		Translation: "patched time",
	}
	wantWidth := ansi.StringWidth(line.Text)

	for _, word := range line.Words {
		app.pos = word.StartMs
		rendered := app.renderCurrentLyricLine(line, lineWidth)
		plain := ansi.Strip(rendered)
		if strings.TrimRight(plain, " ") != line.Text {
			t.Fatalf("rendered text shifted or dropped glyphs at %q: %q", word.Text, plain)
		}
		if strings.Contains(plain, "patched time") {
			t.Fatalf("current line should not include translation: %q", plain)
		}
		if got := ansi.StringWidth(strings.TrimRight(plain, " ")); got != wantWidth {
			t.Fatalf("text width changed at %q: got %d, want %d: %q", word.Text, got, wantWidth, rendered)
		}
		if got := ansi.StringWidth(rendered); got != lineWidth {
			t.Fatalf("rendered line should cover full row at %q: got %d, want %d: %q", word.Text, got, lineWidth, rendered)
		}
		if strings.Contains(rendered, "\x1b[1;") || strings.Contains(rendered, "\x1b[1m") {
			t.Fatalf("word highlight should not use bold SGR because it can shift wide glyphs: %q", rendered)
		}
		if got := strings.Count(rendered, "\x1b["); got > 4 {
			t.Fatalf("word highlight should render at most three styled runs, got %d ANSI sequences: %q", got/2, rendered)
		}
	}
}

func TestRenderCurrentLyricLineDoesNotRevealClippedActiveWord(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	line := lyrics.Line{
		Text: "ツギハギだらけの君との時間も",
		Words: []lyrics.Word{
			{Text: "ツ", StartMs: 1000, EndMs: 1100},
			{Text: "ギ", StartMs: 1100, EndMs: 1200},
			{Text: "ハ", StartMs: 1200, EndMs: 1300},
			{Text: "ギ", StartMs: 1300, EndMs: 1400},
			{Text: "だ", StartMs: 1400, EndMs: 1500},
			{Text: "ら", StartMs: 1500, EndMs: 1600},
			{Text: "け", StartMs: 1600, EndMs: 1700},
			{Text: "の", StartMs: 1700, EndMs: 1800},
			{Text: "君", StartMs: 1800, EndMs: 2400},
			{Text: "と", StartMs: 2400, EndMs: 3000},
			{Text: "の", StartMs: 3000, EndMs: 3100},
		},
	}

	app.pos = 1200 // Active word "ハ" has only one visible cell left.
	rendered := app.renderCurrentLyricLine(line, 5)
	plain := ansi.Strip(rendered)
	expected := app.styles.muted.Render(truncateCellText(line.Text, 5))

	if got := ansi.StringWidth(rendered); got != 5 {
		t.Fatalf("rendered width = %d, want 5: %q", got, rendered)
	}
	if strings.Contains(plain, "ハ") {
		t.Fatalf("clipped active word became visible: %q", plain)
	}
	if !strings.Contains(plain, "…") {
		t.Fatalf("clipped line should still show truncation marker: %q", plain)
	}
	if rendered != expected {
		t.Fatalf("clipped active word should not render its own highlighted segment:\ngot  %q\nwant %q", rendered, expected)
	}
}

func TestRenderLeftPaneDoesNotWrapNarrowHighlightedLine(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	app.lyric = &lyrics.Lyric{Lines: []lyrics.Line{
		{
			StartMs: 1000,
			EndMs:   4000,
			Text:    "ツギハギだらけの君との時間も",
			Words: []lyrics.Word{
				{Text: "ツ", StartMs: 1000, EndMs: 1100},
				{Text: "ギ", StartMs: 1100, EndMs: 1200},
				{Text: "ハ", StartMs: 1200, EndMs: 1300},
				{Text: "ギ", StartMs: 1300, EndMs: 1400},
				{Text: "だ", StartMs: 1400, EndMs: 1500},
				{Text: "ら", StartMs: 1500, EndMs: 1600},
				{Text: "け", StartMs: 1600, EndMs: 1700},
				{Text: "の", StartMs: 1700, EndMs: 1800},
				{Text: "君", StartMs: 1800, EndMs: 2400},
				{Text: "と", StartMs: 2400, EndMs: 3000},
				{Text: "の", StartMs: 3000, EndMs: 3100},
				{Text: "時", StartMs: 3100, EndMs: 3200},
				{Text: "間", StartMs: 3200, EndMs: 3300},
				{Text: "も", StartMs: 3300, EndMs: 4000},
			},
		},
	}}
	app.pos = 2400
	app.leftW = 8
	app.height = 9

	const paneW = 8
	const paneH = 3 // bodyHeight: 9 - 2 top bar - 4 player bar.
	rendered := app.styles.leftPane.
		Width(paneW).
		Height(paneH).
		Render(app.renderLeftPane())
	lines := strings.Split(rendered, "\n")
	if len(lines) != paneH {
		t.Fatalf("left pane wrapped into %d lines, want %d:\n%q", len(lines), paneH, rendered)
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got > paneW {
			t.Fatalf("line %d width = %d, want <= %d: %q", i, got, paneW, line)
		}
	}
}

func TestRenderLeftPaneKeepsCoverAndLyricsSeparated(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{CoverProtocol: "halfblock"})
	app.lyric = &lyrics.Lyric{Lines: []lyrics.Line{
		{
			StartMs: 1000,
			EndMs:   4000,
			Text:    "ツギハギだらけの君との時間も",
			Words: []lyrics.Word{
				{Text: "ツギハギだらけの", StartMs: 1000, EndMs: 2000},
				{Text: "君との時間も", StartMs: 2000, EndMs: 4000},
			},
		},
	}}
	app.coverImage = testCoverImage(12, 12)
	app.leftContent = leftContentBoth
	app.pos = 2000
	app.leftW = 44
	app.height = 12

	const paneW = 44
	const paneH = 6 // bodyHeight: 12 - 2 top bar - 4 player bar.
	rendered := app.styles.leftPane.
		Width(paneW).
		Height(paneH).
		Render(app.renderLeftPane())
	lines := strings.Split(rendered, "\n")
	if len(lines) != paneH {
		t.Fatalf("left pane height = %d, want %d:\n%q", len(lines), paneH, rendered)
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got > paneW {
			t.Fatalf("line %d width = %d, want <= %d: %q", i, got, paneW, line)
		}
		plain := ansi.Strip(line)
		if strings.Contains(plain, "▀ツ") || strings.Contains(plain, "▀君") {
			t.Fatalf("cover and lyric text overlapped on line %d: %q", i, plain)
		}
	}
	plain := ansi.Strip(rendered)
	if !strings.Contains(plain, "▀") {
		t.Fatalf("left pane missing cover blocks:\n%s", plain)
	}
	if !strings.Contains(plain, "君") {
		t.Fatalf("left pane missing lyric text:\n%s", plain)
	}
}

func TestToggleLeftContentModeCyclesCoverLyricsBoth(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	if app.leftContent != leftContentBoth {
		t.Fatalf("initial left content mode = %v, want both", app.leftContent)
	}

	_, _ = app.handleKey(tea.KeyPressMsg(tea.Key{Text: "v", Code: 'v'}))
	if app.leftContent != leftContentCover {
		t.Fatalf("after first toggle = %v, want cover", app.leftContent)
	}
	_, _ = app.handleKey(tea.KeyPressMsg(tea.Key{Text: "v", Code: 'v'}))
	if app.leftContent != leftContentLyrics {
		t.Fatalf("after second toggle = %v, want lyrics", app.leftContent)
	}
	_, _ = app.handleKey(tea.KeyPressMsg(tea.Key{Text: "v", Code: 'v'}))
	if app.leftContent != leftContentBoth {
		t.Fatalf("after third toggle = %v, want both", app.leftContent)
	}
}

func TestToggleCoverScaleCyclesFitStretch(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	if app.coverScale != coverScaleFit {
		t.Fatalf("initial cover scale = %v, want fit", app.coverScale)
	}

	_, _ = app.handleKey(tea.KeyPressMsg(tea.Key{Text: "c", Code: 'c'}))
	if app.coverScale != coverScaleStretch {
		t.Fatalf("after first toggle = %v, want stretch", app.coverScale)
	}
	_, _ = app.handleKey(tea.KeyPressMsg(tea.Key{Text: "c", Code: 'c'}))
	if app.coverScale != coverScaleFit {
		t.Fatalf("after second toggle = %v, want fit", app.coverScale)
	}
	if app.leftContent != leftContentBoth {
		t.Fatalf("cover scale toggle should not change left content mode: %v", app.leftContent)
	}
}

func TestConfiguredCoverScaleSetsInitialMode(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{CoverScale: "stretch"})
	if app.coverScale != coverScaleStretch {
		t.Fatalf("initial cover scale = %v, want stretch", app.coverScale)
	}
}

func TestDisableCoverFallsBackToLyrics(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{DisableCover: true})
	app.lyric = &lyrics.Lyric{Lines: []lyrics.Line{{StartMs: 0, EndMs: 1000, Text: "lyrics only"}}}
	app.coverImage = testCoverImage(4, 4)
	app.leftContent = leftContentBoth
	app.leftW = 30
	app.height = 10

	plain := ansi.Strip(app.renderLeftPane())
	if strings.Contains(plain, "▀") {
		t.Fatalf("disabled cover should not render cover blocks:\n%s", plain)
	}
	if !strings.Contains(plain, "lyrics only") {
		t.Fatalf("disabled cover should leave lyrics visible:\n%s", plain)
	}
}

func TestKittyCoverUsesBlankPlaceholderAndRawImage(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{CoverProtocol: "kitty"})
	app.coverImage = testCoverImage(4, 4)
	app.leftContent = leftContentCover
	app.leftW = 12
	app.height = 10

	plain := ansi.Strip(app.renderLeftPane())
	if strings.Contains(plain, "▀") {
		t.Fatalf("kitty cover pane should reserve blank cells, not halfblock text:\n%s", plain)
	}

	seq := app.renderKittyCoverOverlay()
	if !strings.Contains(seq, "\x1b_Ga") {
		t.Fatalf("kitty overlay missing graphics escape: %q", seq)
	}
	if !strings.Contains(seq, "\x1b[3;1H") {
		t.Fatalf("kitty overlay should target top-left of body: %q", seq)
	}
}

func TestLyricsOnlyClearsKittyCover(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{CoverProtocol: "kitty"})
	app.coverImage = testCoverImage(4, 4)
	app.leftContent = leftContentLyrics
	app.leftW = 20
	app.height = 10

	seq := app.renderKittyCoverOverlay()
	if seq != "\x1b_Ga=d,d=I,i=1\x1b\\" {
		t.Fatalf("lyrics-only should clear kitty image only, got %q", seq)
	}
}

func TestKittyCoverCommandOnlyEmitsWhenOverlayChanges(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{CoverProtocol: "kitty"})
	app.coverImage = testCoverImage(4, 4)
	app.leftContent = leftContentCover
	app.leftW = 12
	app.height = 10

	if cmd := app.kittyCoverCmd(); cmd == nil {
		t.Fatal("first kitty cover command should draw image")
	} else if _, ok := cmd().(tea.RawMsg); !ok {
		t.Fatalf("first kitty cover command returned %T, want tea.RawMsg", cmd())
	}
	if cmd := app.kittyCoverCmd(); cmd != nil {
		t.Fatalf("unchanged kitty cover should not redraw, got command %#v", cmd())
	}
}

func TestKittyCoverClearOnlyEmitsOnce(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{CoverProtocol: "kitty"})
	app.coverImage = testCoverImage(4, 4)
	app.leftContent = leftContentLyrics
	app.leftW = 20
	app.height = 10

	if cmd := app.kittyCoverCmd(); cmd == nil {
		t.Fatal("first lyrics-only kitty cover command should clear image")
	}
	if cmd := app.kittyCoverCmd(); cmd != nil {
		t.Fatalf("unchanged clear state should not clear repeatedly, got command %#v", cmd())
	}
}

func TestKittyCoverRedrawsAfterLyricsOnlyClear(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{CoverProtocol: "kitty"})
	app.coverImage = testCoverImage(4, 4)
	app.leftContent = leftContentCover
	app.leftW = 20
	app.height = 10

	if cmd := app.kittyCoverCmd(); cmd == nil {
		t.Fatal("initial cover command should draw image")
	}

	app.leftContent = leftContentLyrics
	if cmd := app.kittyCoverCmd(); cmd == nil {
		t.Fatal("lyrics-only command should clear image")
	}

	app.leftContent = leftContentCover
	if cmd := app.kittyCoverCmd(); cmd == nil {
		t.Fatal("cover command should redraw after the image was cleared")
	} else if _, ok := cmd().(tea.RawMsg); !ok {
		t.Fatalf("redraw command returned %T, want tea.RawMsg", cmd())
	}
}

func TestKittyToggleResetsDedupForNextTickRedraw(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{CoverProtocol: "kitty"})
	app.coverImage = testCoverImage(4, 4)
	app.leftContent = leftContentCover
	app.leftW = 12
	app.height = 10

	// Prime dedup: first call draws, second is deduped.
	if cmd := app.kittyCoverCmd(); cmd == nil {
		t.Fatal("first kitty cover command should draw image")
	}
	if cmd := app.kittyCoverCmd(); cmd != nil {
		t.Fatalf("unchanged kitty cover should not redraw, got command %#v", cmd())
	}

	// Toggle resets dedup and returns nil (no ClearScreen for kitty).
	// The chunked APC payload prevents base64 leaking even if the drawSeq
	// and view diff land in the same renderer flush.
	if cmd := app.clearScreenAndKittyCoverCmd(); cmd != nil {
		t.Fatalf("kitty toggle should return nil (no ClearScreen), got %v", cmd)
	}

	// Next kittyCoverCmd redraws after dedup reset.
	if cmd := app.kittyCoverCmd(); cmd == nil {
		t.Fatal("should redraw after dedup reset from toggle")
	} else if _, ok := cmd().(tea.RawMsg); !ok {
		t.Fatalf("redraw command returned %T, want tea.RawMsg", cmd())
	}
}

func TestKittyToggleToLyricsClearsCoverImmediately(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{CoverProtocol: "kitty"})
	app.coverImage = testCoverImage(4, 4)
	app.leftContent = leftContentLyrics
	app.leftW = 20
	app.height = 10

	cmd := app.clearScreenAndKittyCoverCmd()
	if cmd == nil {
		t.Fatal("kitty lyrics-only toggle should clear the cover immediately")
	}
	msg := cmd()
	raw, ok := msg.(tea.RawMsg)
	if !ok {
		t.Fatalf("clear command returned %T, want tea.RawMsg", msg)
	}
	if fmt.Sprint(raw.Msg) != cover.ClearKittyImage(1) {
		t.Fatalf("clear command = %q, want kitty image clear", raw.Msg)
	}
}

func TestKittyCoverCmdSkipsRenderWhenFingerprintUnchanged(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{CoverProtocol: "kitty"})
	app.coverImage = testCoverImage(4, 4)
	app.leftContent = leftContentCover
	app.leftW = 12
	app.height = 10

	// First call renders and sets fingerprint.
	if cmd := app.kittyCoverCmd(); cmd == nil {
		t.Fatal("first kitty cover command should draw image")
	}
	fp := app.lastKittyFingerprint
	if fp == "" {
		t.Fatal("fingerprint should be set after first render")
	}

	// Second call: fingerprint unchanged → skip without rendering.
	if cmd := app.kittyCoverCmd(); cmd != nil {
		t.Fatalf("unchanged fingerprint should skip render, got command %v", cmd)
	}

	// Change cover image → fingerprint changes → render again.
	app.coverImage = testCoverImage(8, 8)
	if cmd := app.kittyCoverCmd(); cmd == nil {
		t.Fatal("should redraw after cover image change")
	}
}

func TestHalfBlockToggleStillClearsScreen(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{CoverProtocol: "halfblock"})
	app.coverImage = testCoverImage(4, 4)
	app.leftW = 12
	app.height = 10

	cmd := app.clearScreenAndKittyCoverCmd()
	if cmd == nil {
		t.Fatal("halfblock toggle should return ClearScreen sequence")
	}
	msg := cmd()
	if fmt.Sprintf("%T", msg) != "tea.clearScreenMsg" {
		t.Fatalf("halfblock should clear screen, got %T", msg)
	}
}

func TestKittyLyricChangeDoesNotClearScreen(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{CoverProtocol: "kitty"})
	app.coverImage = testCoverImage(4, 4)
	app.leftW = 20
	app.height = 10

	cmd := app.lyricChangeCmd()
	if cmd == nil {
		t.Fatal("lyric change should return a command")
	}
	msg := cmd()
	if fmt.Sprintf("%T", msg) == "tea.clearScreenMsg" {
		t.Fatalf("kitty lyric changes must not clear screen")
	}
}

func TestLyricRenderStateChangesWhenLineChangesWithSameWordIndex(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	app.lyric = &lyrics.Lyric{Lines: []lyrics.Line{
		{
			StartMs: 1000,
			EndMs:   2000,
			Text:    "君",
			Words: []lyrics.Word{
				{Text: "君", StartMs: 1000, EndMs: 2000},
			},
		},
		{
			StartMs: 2000,
			EndMs:   3000,
			Text:    "と",
			Words: []lyrics.Word{
				{Text: "と", StartMs: 2000, EndMs: 3000},
			},
		},
	}}

	app.pos = 1000
	first := app.currentLyricRenderState()
	app.pos = 2000 + lyricFinalWordGraceMs
	second := app.currentLyricRenderState()

	if first == second {
		t.Fatalf("lyric render state should change on line boundary with same word index: %#v", first)
	}
	if first.word != 0 || second.word != 0 {
		t.Fatalf("test setup expected both active word indexes to be 0: first=%#v second=%#v", first, second)
	}
	if first.line != 0 || second.line != 1 {
		t.Fatalf("line indexes = %d, %d; want 0, 1", first.line, second.line)
	}
}

func TestFastLyricKeepsFinalWordVisibleAtLineBoundary(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	app.lyric = &lyrics.Lyric{Lines: []lyrics.Line{
		{
			StartMs: 1000,
			EndMs:   2000,
			Text:    "君と",
			Words: []lyrics.Word{
				{Text: "君", StartMs: 1000, EndMs: 1950},
				{Text: "と", StartMs: 1950, EndMs: 2000},
			},
		},
		{
			StartMs: 2000,
			EndMs:   3000,
			Text:    "次",
			Words: []lyrics.Word{
				{Text: "次", StartMs: 2000, EndMs: 3000},
			},
		},
	}}

	app.pos = 2000

	state := app.currentLyricRenderState()
	if state.line != 0 || state.word != 1 {
		t.Fatalf("final word should remain active at boundary, got %#v", state)
	}
	rendered := app.renderCurrentLyricLine(app.lyric.Lines[state.line], 20)
	muted := ansi.NewStyle().ForegroundColor(app.theme.Muted)
	if rendered == padCellText(muted.Styled(truncateCellText(app.lyric.Lines[state.line].Text, 20)), 20) {
		t.Fatalf("final word should be highlighted at boundary: %q", rendered)
	}
}

func TestCellSizeEventUpdatesDimensionsAndResetsDedup(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{CoverProtocol: "kitty"})
	app.coverImage = testCoverImage(4, 4)
	app.leftContent = leftContentCover
	app.leftW = 12
	app.height = 10

	// Prime dedup.
	if cmd := app.kittyCoverCmd(); cmd == nil {
		t.Fatal("first kitty cover command should draw image")
	}
	if cmd := app.kittyCoverCmd(); cmd != nil {
		t.Fatal("unchanged kitty cover should be deduped")
	}

	// Deliver cell size event.
	m, _ := app.Update(uv.CellSizeEvent{Width: 9, Height: 20})
	app = m.(*App)
	if app.cellPixelW != 9 || app.cellPixelH != 20 {
		t.Fatalf("cell pixel dimensions = %d,%d; want 9,20", app.cellPixelW, app.cellPixelH)
	}

	// Dedup reset → next kittyCoverCmd redraws with new dimensions.
	if cmd := app.kittyCoverCmd(); cmd == nil {
		t.Fatal("should redraw after cell size change")
	} else if _, ok := cmd().(tea.RawMsg); !ok {
		t.Fatalf("redraw returned %T, want tea.RawMsg", cmd())
	}
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;:]*[A-Za-z]`)

func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

func testCoverImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 255 / max(1, w-1)), G: uint8(y * 255 / max(1, h-1)), B: 90, A: 255})
		}
	}
	return img
}

func trackTitles(tracks []*library.Track) []string {
	titles := make([]string, len(tracks))
	for i, track := range tracks {
		titles[i] = track.Title
	}
	return titles
}

func trimRightLines(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n")
}
