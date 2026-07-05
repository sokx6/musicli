package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
	app.pos = 2500
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

	rendered := app.renderCurrentLyricLine(line, 80)
	plain := stripANSI(rendered)
	if plain != line.Text {
		t.Fatalf("rendered text shifted or dropped glyphs: %q", plain)
	}
	if strings.Contains(plain, "patched time") {
		t.Fatalf("current line should not include translation: %q", plain)
	}
	if strings.Contains(rendered, "\x1b[1;") || strings.Contains(rendered, "\x1b[1m") {
		t.Fatalf("word highlight should not use bold SGR because it can shift wide glyphs: %q", rendered)
	}
	if got := strings.Count(rendered, "\x1b["); got > 6 {
		t.Fatalf("word highlight should render at most three styled runs, got %d ANSI sequences: %q", got/2, rendered)
	}
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;:]*[A-Za-z]`)

func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

func trimRightLines(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n")
}
