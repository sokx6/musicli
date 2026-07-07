package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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
	app.pos = 2000
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
