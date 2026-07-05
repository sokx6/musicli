package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/locxl/musicli/internal/audio"
	"github.com/locxl/musicli/internal/library"
	"github.com/locxl/musicli/internal/log"
	"github.com/locxl/musicli/internal/lyrics"
	"github.com/locxl/musicli/internal/theme"
)

// trackItem adapts a library.Track to a list.Item.
type trackItem struct {
	track *library.Track
}

func (i trackItem) Title() string {
	t := i.track.Title
	if t == "" {
		t = "(unknown)"
	}
	return t
}
func (i trackItem) Description() string {
	parts := []string{}
	if i.track.Artist != "" {
		parts = append(parts, i.track.Artist)
	}
	if i.track.Album != "" {
		parts = append(parts, i.track.Album)
	}
	d := time.Duration(i.track.Duration) * time.Millisecond
	if d > 0 {
		parts = append(parts, fmtDuration(d))
	}
	return strings.Join(parts, " - ")
}
func (i trackItem) FilterValue() string { return i.track.Title + " " + i.track.Artist }

// Options configures UI layout behavior.
type Options struct {
	// TrackListMaxWidth caps the content-fit track list width. Zero means no cap.
	TrackListMaxWidth int
}

// App is the top-level bubbletea model.
type App struct {
	log     *log.Logger
	theme   *theme.Theme
	styles  *Styles
	keys    keyMap
	options Options
	engine  *audio.Engine
	scanner *library.Scanner

	width, height int
	leftW         int

	trackList list.Model
	delegate  list.DefaultDelegate
	progress  progress.Model

	tracks    []*library.Track
	current   int // index into tracks, -1 if none
	loading   bool
	lyric     *lyrics.Lyric
	lyricPath string

	// playback state mirror (polled from engine)
	pos       int
	dur       int
	state     audio.State
	lastState audio.State
	volume    int
	speed     float64
	errMsg    string
}

// New creates the App model. Engine and scanner must be initialised.
func New(eng *audio.Engine, sc *library.Scanner, t *theme.Theme, lg *log.Logger) *App {
	return NewWithOptions(eng, sc, t, lg, Options{})
}

// NewWithOptions creates the App model with explicit UI options.
func NewWithOptions(eng *audio.Engine, sc *library.Scanner, t *theme.Theme, lg *log.Logger, opts Options) *App {
	fl := lg.WithModule("ui").WithFunc("New")
	keys := defaultKeyMap()
	styles := NewStyles(t)
	delegate := newListDelegate(t)

	trackList := list.New([]list.Item{}, delegate, 40, 20)
	trackList.Title = "Tracks"
	trackList.Styles = newListComponentStyles(t)
	trackList.SetShowHelp(false)
	trackList.SetShowTitle(true)
	trackList.SetShowStatusBar(false)
	trackList.SetFilteringEnabled(true)
	trackList.DisableQuitKeybindings()

	pbar := newProgressBar(t)

	fl.Debug("app created",
		"engine", eng != nil,
		"scanner", sc != nil,
		"theme_mode", t.Mode,
		"keybindings", 12,
	)

	return &App{
		log:       lg.WithModule("ui"),
		theme:     t,
		styles:    styles,
		keys:      keys,
		options:   opts,
		engine:    eng,
		scanner:   sc,
		trackList: trackList,
		delegate:  delegate,
		progress:  pbar,
		current:   -1,
		volume:    80,
		speed:     1.0,
		lastState: audio.StateStopped,
	}
}

// newListDelegate builds a default delegate with themed styles.
func newListDelegate(t *theme.Theme) list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.Styles = newListStyles(t)
	return d
}

// --- messages ---

// TracksLoadedMsg is delivered when a scan completes. Exported so the
// entry point can deliver scan results via p.Send.
type TracksLoadedMsg struct{ Tracks []*library.Track }

// ScanErrMsg is delivered when a scan fails.
type ScanErrMsg struct{ Err error }

// ScanStartMsg signals that a scan has begun.
type ScanStartMsg struct{ Path string }

type tickMsg struct{}
type errMsg struct{ err error }

// scanCmd scans the given path async.
func scanCmd(sc *library.Scanner, path string) tea.Cmd {
	return func() tea.Msg {
		tracks, err := sc.ScanPath(path)
		if err != nil {
			return ScanErrMsg{Err: fmt.Errorf("scan path %q: %w", path, err)}
		}
		return TracksLoadedMsg{Tracks: tracks}
	}
}

// tickCmd emits a tick every 100ms for progress/state polling.
func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

// --- bubbletea Model ---

// Init starts the app.
func (a *App) Init() tea.Cmd {
	a.log.WithFunc("Init").Debug("init started")
	return tea.Batch(tickCmd())
}

// LoadPath triggers an async scan of the given file/dir path.
func (a *App) LoadPath(path string) {
	fl := a.log.WithFunc("LoadPath")
	a.loading = true
	a.trackList.ResetSelected()
	a.trackList.Title = "Scanning..."
	a.trackList.SetItems([]list.Item{})
	a.tracks = nil
	fl.Debug("loading path", "path", path)
}

// LoadPathCmd returns a command that scans path.
func (a *App) LoadPathCmd(path string) tea.Cmd {
	fl := a.log.WithFunc("LoadPathCmd")
	a.loading = true
	a.trackList.Title = "Scanning..."
	fl.Debug("loading path", "path", path)
	return scanCmd(a.scanner, path)
}

// Update handles messages.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		fl := a.log.WithFunc("Update")
		fl.Debug("received msg", "type", "WindowSizeMsg", "width", msg.Width, "height", msg.Height)
		a.width, a.height = msg.Width, msg.Height
		a.resizeComponents()
		return a, nil

	case tea.KeyMsg:
		fl := a.log.WithFunc("Update")
		fl.Debug("received msg", "type", "KeyMsg", "key", msg.String())
		return a.handleKey(msg)

	case tea.MouseClickMsg:
		fl := a.log.WithFunc("Update")
		fl.Debug("received msg", "type", "MouseClickMsg", "x", msg.X, "y", msg.Y, "button", mouseButtonStr(msg.Button))
		return a.handleMouse(msg)

	case tea.MouseWheelMsg:
		fl := a.log.WithFunc("Update")
		fl.Debug("received msg", "type", "MouseWheelMsg")
		// forward wheel to list
		var cmd tea.Cmd
		a.trackList, cmd = a.trackList.Update(msg)
		return a, cmd

	case TracksLoadedMsg:
		fl := a.log.WithFunc("Update")
		fl.Debug("received msg", "type", "TracksLoadedMsg", "count", len(msg.Tracks))
		a.loading = false
		a.tracks = msg.Tracks
		items := make([]list.Item, len(msg.Tracks))
		for i, t := range msg.Tracks {
			items[i] = trackItem{track: t}
		}
		a.trackList.SetItems(items)
		a.trackList.Title = fmt.Sprintf("Tracks (%d)", len(msg.Tracks))
		a.resizeComponents()
		a.log.Info("library loaded", "count", len(msg.Tracks))
		return a, nil

	case ScanErrMsg:
		fl := a.log.WithFunc("Update")
		fl.Debug("received msg", "type", "ScanErrMsg", "err", msg.Err)
		a.loading = false
		a.trackList.Title = "Tracks"
		a.errMsg = fmt.Sprintf("scan failed: %v", msg.Err)
		a.log.Error("scan failed", "err", msg.Err)
		return a, nil

	case tickMsg:
		a.pollEngine()
		return a, tickCmd()

	case errMsg:
		fl := a.log.WithFunc("Update")
		fl.Debug("received msg", "type", "errMsg", "err", msg.err)
		a.errMsg = msg.err.Error()
		return a, nil
	}

	// forward to list
	var cmd tea.Cmd
	a.trackList, cmd = a.trackList.Update(msg)
	return a, cmd
}

// pollEngine reads engine state for the UI (oracle Sim-C: polling, no callback).
func (a *App) pollEngine() {
	fl := a.log.WithFunc("pollEngine")
	a.pos = a.engine.Position()
	a.dur = a.engine.Duration()
	prevState := a.state
	a.state = a.engine.State()
	a.volume = a.engine.Volume()
	a.speed = a.engine.Speed()
	if err := a.engine.Err(); err != nil {
		a.errMsg = err.Error()
	}
	if a.state != prevState {
		fl.Debug("state changed", "from", prevState.String(), "to", a.state.String())
	}
	// update progress bar
	if a.dur > 0 {
		_ = a.progress.SetPercent(float64(a.pos) / float64(a.dur))
	} else {
		_ = a.progress.SetPercent(0)
	}
}

// handleKey processes key messages.
func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	fl := a.log.WithFunc("handleKey")
	keyStr := msg.String()

	// If the list is filtering, let it handle keys.
	if a.trackList.FilterState() == list.Filtering {
		fl.Debug("forwarding to list (filtering)", "key", keyStr)
		var cmd tea.Cmd
		a.trackList, cmd = a.trackList.Update(msg)
		return a, cmd
	}

	switch {
	case key.Matches(msg, a.keys.Quit):
		fl.Debug("key matched", "key", keyStr, "action", "quit")
		return a, tea.Quit

	case key.Matches(msg, a.keys.Enter):
		fl.Debug("key matched", "key", keyStr, "action", "playSelected")
		return a, a.playSelected()

	case key.Matches(msg, a.keys.PlayPause):
		fl.Debug("key matched", "key", keyStr, "action", "togglePlayPause")
		return a, a.togglePlayPause()

	case key.Matches(msg, a.keys.Next):
		fl.Debug("key matched", "key", keyStr, "action", "nextTrack")
		return a, a.nextTrack()

	case key.Matches(msg, a.keys.Prev):
		fl.Debug("key matched", "key", keyStr, "action", "prevTrack")
		return a, a.prevTrack()

	case key.Matches(msg, a.keys.SeekFwd):
		fl.Debug("key matched", "key", keyStr, "action", "seekRelative+5000")
		return a, a.seekRelative(5000)

	case key.Matches(msg, a.keys.SeekBack):
		fl.Debug("key matched", "key", keyStr, "action", "seekRelative-5000")
		return a, a.seekRelative(-5000)

	case key.Matches(msg, a.keys.VolUp):
		fl.Debug("key matched", "key", keyStr, "action", "volUp")
		a.engine.SetVolume(a.volume + 5)
		return a, nil

	case key.Matches(msg, a.keys.VolDown):
		fl.Debug("key matched", "key", keyStr, "action", "volDown")
		a.engine.SetVolume(a.volume - 5)
		return a, nil

	case key.Matches(msg, a.keys.SpeedUp):
		fl.Debug("key matched", "key", keyStr, "action", "speedUp")
		a.engine.SetSpeed(a.speed + 0.1)
		return a, nil

	case key.Matches(msg, a.keys.SpeedDown):
		fl.Debug("key matched", "key", keyStr, "action", "speedDown")
		a.engine.SetSpeed(a.speed - 0.1)
		return a, nil
	}

	fl.Debug("key unmatched, forwarding to list", "key", keyStr)
	// navigation falls through to list
	var cmd tea.Cmd
	a.trackList, cmd = a.trackList.Update(msg)
	return a, cmd
}

// handleMouse processes mouse click messages.
func (a *App) handleMouse(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	fl := a.log.WithFunc("handleMouse")
	leftW := a.leftPaneWidth()
	const topBarTotalH = 2    // 1 content + bottom border
	const playerBarTotalH = 4 // 3 content + top border

	fl.Debug("mouse click", "x", msg.X, "y", msg.Y, "button", mouseButtonStr(msg.Button))

	// Click on the player bar (bottom 3 lines + border) → seek.
	if msg.Y >= a.height-playerBarTotalH {
		fl.Debug("hit player bar", "action", "seek")
		if a.dur > 0 && a.width > 0 {
			target := a.dur * msg.X / a.width
			return a, a.seekTo(target)
		}
		return a, nil
	}

	// Click in the right pane list area → select; play only if selecting a
	// different track than the current one.
	listStartX := leftW
	inListArea := (leftW == 0 || msg.X >= listStartX) &&
		msg.Y >= topBarTotalH && msg.Y < a.height-playerBarTotalH
	if inListArea {
		fl.Debug("hit list area", "action", "select")
		// Adjust coordinates to the list's local space.
		localMsg := tea.MouseClickMsg{
			X:      msg.X - listStartX,
			Y:      msg.Y - topBarTotalH,
			Button: msg.Button,
			Mod:    msg.Mod,
		}
		prevIdx := a.trackList.Index()
		var cmd tea.Cmd
		a.trackList, cmd = a.trackList.Update(localMsg)
		newIdx := a.trackList.Index()
		if msg.Button == tea.MouseLeft && newIdx >= 0 && newIdx != prevIdx {
			return a, a.playSelected()
		}
		return a, cmd
	}

	return a, nil
}

// --- playback commands ---

func (a *App) playSelected() tea.Cmd {
	fl := a.log.WithFunc("playSelected")
	idx := a.trackList.Index()
	if idx < 0 || idx >= len(a.tracks) {
		fl.Debug("invalid index", "idx", idx, "tracks", len(a.tracks))
		return nil
	}
	// Don't restart if the same track is already playing (avoids stutter from
	// rapid Enter/click re-triggering ffmpeg spawn).
	if idx == a.current && a.state == audio.StatePlaying {
		fl.Debug("skipped, same track playing", "idx", idx, "title", a.tracks[idx].Title)
		return nil
	}
	a.current = idx
	t := a.tracks[idx]
	fl.Debug("playing track", "idx", idx, "title", t.Title, "path", t.Path)
	if err := a.engine.Play(t.Path); err != nil {
		fl.Error("Play failed", "err", err)
		return func() tea.Msg { return errMsg{err: err} }
	}
	a.loadCurrentLyrics()
	return nil
}

func (a *App) togglePlayPause() tea.Cmd {
	fl := a.log.WithFunc("togglePlayPause")
	switch a.state {
	case audio.StatePlaying:
		fl.Debug("toggling", "from", "playing", "action", "pause")
		a.engine.Pause()
	case audio.StatePaused:
		fl.Debug("toggling", "from", "paused", "action", "resume")
		a.engine.Resume()
	case audio.StateStopped:
		fl.Debug("toggling", "from", "stopped", "action", "playSelected")
		return a.playSelected()
	}
	return nil
}

func (a *App) nextTrack() tea.Cmd {
	fl := a.log.WithFunc("nextTrack")
	if len(a.tracks) == 0 {
		return nil
	}
	prevIdx := a.current
	a.current = (a.current + 1) % len(a.tracks)
	a.trackList.Select(a.current)
	t := a.tracks[a.current]
	fl.Debug("next track", "prevIdx", prevIdx, "newIdx", a.current, "title", t.Title)
	if err := a.engine.Play(t.Path); err != nil {
		fl.Error("Play failed", "err", err)
		return func() tea.Msg { return errMsg{err: err} }
	}
	a.loadCurrentLyrics()
	return nil
}

func (a *App) prevTrack() tea.Cmd {
	fl := a.log.WithFunc("prevTrack")
	if len(a.tracks) == 0 {
		return nil
	}
	prevIdx := a.current
	if a.current < 0 {
		a.current = 0
	} else {
		a.current = (a.current - 1 + len(a.tracks)) % len(a.tracks)
	}
	a.trackList.Select(a.current)
	t := a.tracks[a.current]
	fl.Debug("prev track", "prevIdx", prevIdx, "newIdx", a.current, "title", t.Title)
	if err := a.engine.Play(t.Path); err != nil {
		fl.Error("Play failed", "err", err)
		return func() tea.Msg { return errMsg{err: err} }
	}
	a.loadCurrentLyrics()
	return nil
}

func (a *App) seekRelative(deltaMs int) tea.Cmd {
	fl := a.log.WithFunc("seekRelative")
	if a.dur <= 0 {
		fl.Debug("seek skipped, no duration")
		return nil
	}
	target := a.pos + deltaMs
	fl.Debug("seeking relative", "delta", deltaMs, "pos", a.pos, "target", target)
	return a.seekTo(target)
}

func (a *App) seekTo(target int) tea.Cmd {
	fl := a.log.WithFunc("seekTo")
	if target < 0 {
		fl.Debug("clamped to 0", "target", target)
		target = 0
	}
	if a.dur > 0 && target > a.dur {
		fl.Debug("clamped to duration", "target", target, "dur", a.dur)
		target = a.dur
	}
	fl.Debug("seeking to", "target", target)
	if err := a.engine.Seek(target); err != nil {
		fl.Error("Seek failed", "err", err)
		return func() tea.Msg { return errMsg{err: err} }
	}
	return nil
}

func (a *App) loadCurrentLyrics() {
	fl := a.log.WithFunc("loadCurrentLyrics")
	a.lyric = nil
	a.lyricPath = ""
	if a.current < 0 || a.current >= len(a.tracks) {
		return
	}
	path := a.tracks[a.current].Path
	if path == "" {
		return
	}
	ly, lyricPath, err := lyrics.LoadLocal(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			fl.Warn("local lyric load failed", "path", path, "err", err)
		}
		return
	}
	a.lyric = ly
	a.lyricPath = lyricPath
	fl.Info("local lyric loaded", "path", lyricPath, "lines", len(ly.Lines))
}

// --- layout ---

func (a *App) leftPaneWidth() int {
	if a.leftW > 0 {
		return a.leftW
	}
	return a.baseLeftPaneWidth()
}

func (a *App) baseLeftPaneWidth() int {
	if a.width < 80 {
		return 0
	}
	w := int(float64(a.width) * 0.4)
	if w < 1 {
		w = 1
	}
	return w
}

func (a *App) trackListContentWidth() int {
	const minListWidth = 1

	width := ansi.StringWidth(a.trackList.Title) + 5 // title left pad + status gap.
	for _, tr := range a.tracks {
		item := trackItem{track: tr}
		titleW := ansi.StringWidth(item.Title()) + 1
		if titleW > width {
			width = titleW
		}
		if desc := item.Description(); desc != "" {
			descW := ansi.StringWidth(desc) + 1
			if descW > width {
				width = descW
			}
		}
	}
	if width < minListWidth {
		return minListWidth
	}
	return width
}

func (a *App) layoutWidths() (leftW, listW int) {
	if a.width < 1 {
		return 0, 1
	}
	baseLeftW := a.baseLeftPaneWidth()
	availableListW := a.width - baseLeftW
	if availableListW < 1 {
		availableListW = 1
	}

	listW = a.trackListContentWidth()
	if maxW := a.options.TrackListMaxWidth; maxW > 0 && listW > maxW {
		listW = maxW
	}
	if listW > availableListW {
		listW = availableListW
	}
	if listW < 1 {
		listW = 1
	}

	leftW = a.width - listW
	if a.width < 80 {
		leftW = 0
		listW = a.width
		if listW < 1 {
			listW = 1
		}
	}
	return leftW, listW
}

func (a *App) bodyHeight() int {
	const topBarH = 2    // 1 content + bottom border
	const playerBarH = 4 // 3 content + top border
	h := a.height - topBarH - playerBarH
	if h < 1 {
		h = 1
	}
	return h
}

func (a *App) resizeComponents() {
	fl := a.log.WithFunc("resizeComponents")
	if a.width == 0 || a.height == 0 {
		return
	}
	leftW, listW := a.layoutWidths()
	a.leftW = leftW
	bodyH := a.bodyHeight()

	a.trackList.SetWidth(listW)
	a.trackList.SetHeight(bodyH)
	fl.Debug("layout sizes",
		"term_w", a.width, "term_h", a.height,
		"leftW", leftW, "listW", listW, "bodyH", bodyH)

	// Force item styles to fill the full list width so there's no empty
	// space on the right of each row.
	s := newListStyles(a.theme)
	s.NormalTitle = s.NormalTitle.Width(listW)
	s.NormalDesc = s.NormalDesc.Width(listW)
	s.SelectedTitle = s.SelectedTitle.Width(listW)
	s.SelectedDesc = s.SelectedDesc.Width(listW)
	s.DimmedTitle = s.DimmedTitle.Width(listW)
	s.DimmedDesc = s.DimmedDesc.Width(listW)
	a.delegate.Styles = s
	// The list stores its own copy of the delegate; updating a.delegate alone
	// leaves the rendered list using the old narrow styles.
	a.trackList.SetDelegate(a.delegate)

	progressW := a.width - 2
	if progressW < 1 {
		progressW = 1
	}
	a.progress.SetWidth(progressW)
}

// View renders the full UI.
func (a *App) View() tea.View {
	if a.width == 0 || a.height == 0 {
		a.log.WithFunc("View").Debug("zero size, early return")
		return tea.NewView("Initializing...")
	}

	bodyH := a.bodyHeight()
	leftW := a.leftPaneWidth()
	rightW := a.trackList.Width()
	if rightW < 1 {
		rightW = a.width - leftW
	}

	topBar := a.renderTopBar()

	rightPaneW := rightW
	if rightPaneW < 1 {
		rightPaneW = 1
	}

	rightPane := a.styles.rightPane.
		Width(rightPaneW).
		Height(bodyH).
		Render(a.trackList.View())

	var body string
	if leftW > 0 {
		leftPane := a.styles.leftPane.
			Width(leftW).
			Height(bodyH).
			Render(a.renderLeftPane())
		body = lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)
	} else {
		body = rightPane
	}

	bar := a.renderPlayerBar()

	full := lipgloss.JoinVertical(lipgloss.Left, topBar, body, bar)

	// Fill the entire terminal frame so stale lines are cleared on resize.
	frame := a.styles.doc.Width(a.width).Height(a.height).Render(full)

	v := tea.NewView(frame)
	v.MouseMode = tea.MouseModeAllMotion
	return v
}

func (a *App) renderTopBar() string {
	var content string
	if a.current >= 0 && a.current < len(a.tracks) {
		t := a.tracks[a.current]
		title := a.styles.title.Render(t.Title)
		parts := []string{}
		if t.Artist != "" {
			parts = append(parts, t.Artist)
		}
		if t.Album != "" {
			parts = append(parts, t.Album)
		}
		if len(parts) > 0 {
			meta := a.styles.muted.Render(strings.Join(parts, " - "))
			content = "▶ " + title + " - " + meta
		} else {
			content = "▶ " + title
		}
	} else {
		content = a.styles.title.Render("musicli")
	}
	return a.styles.topBar.Width(a.width).Render(content)
}

func (a *App) renderLeftPane() string {
	w := a.leftPaneWidth()
	h := a.bodyHeight()
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if a.lyric != nil && len(a.lyric.Lines) > 0 {
		return a.renderLyricsPane(w, h)
	}
	placeholder := a.styles.muted.Render("[ cover + lyrics ]")
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, placeholder)
}

func (a *App) renderLyricsPane(w, h int) string {
	idx := a.currentLyricLineIndex()
	rows, currentRow := a.lyricVisualRows(idx)
	start := currentRow - h/2
	if start < 0 {
		start = 0
	}
	if maxStart := len(rows) - h; maxStart >= 0 && start > maxStart {
		start = maxStart
	}

	rendered := make([]string, 0, h)
	for row := 0; row < h; row++ {
		rowIdx := start + row
		if rowIdx >= len(rows) {
			rendered = append(rendered, "")
			continue
		}
		visual := rows[rowIdx]
		switch visual.kind {
		case lyricRowOriginal:
			if visual.lineIdx == idx {
				rendered = append(rendered, a.renderCurrentLyricLine(a.lyric.Lines[visual.lineIdx], w-1))
			} else {
				text := truncateCellText(a.lyric.Lines[visual.lineIdx].Text, w-1)
				rendered = append(rendered, a.styles.muted.Render(text))
			}
		case lyricRowTranslation:
			text := truncateCellText(visual.text, w-1)
			rendered = append(rendered, a.styles.muted.Render(text))
		default:
			rendered = append(rendered, "")
		}
	}
	return lipgloss.NewStyle().Width(w).Height(h).PaddingLeft(1).Render(strings.Join(rendered, "\n"))
}

type lyricRowKind int

const (
	lyricRowOriginal lyricRowKind = iota
	lyricRowTranslation
	lyricRowBlank
)

type lyricVisualRow struct {
	lineIdx int
	kind    lyricRowKind
	text    string
}

func (a *App) lyricVisualRows(currentLine int) ([]lyricVisualRow, int) {
	rows := []lyricVisualRow{}
	currentRow := 0
	for i, line := range a.lyric.Lines {
		if i == currentLine {
			currentRow = len(rows)
		}
		rows = append(rows, lyricVisualRow{lineIdx: i, kind: lyricRowOriginal, text: line.Text})
		if line.Translation != "" {
			for _, tr := range strings.Split(line.Translation, "\n") {
				rows = append(rows, lyricVisualRow{lineIdx: i, kind: lyricRowTranslation, text: tr})
			}
		}
		if i != len(a.lyric.Lines)-1 {
			rows = append(rows, lyricVisualRow{lineIdx: i, kind: lyricRowBlank})
		}
	}
	return rows, currentRow
}

func (a *App) renderCurrentLyricLine(line lyrics.Line, width int) string {
	if len(line.Words) == 0 {
		text := truncateCellText(line.Text, width)
		return a.styles.accent.Bold(true).Render(text)
	}

	var b strings.Builder
	usedWidth := 0
	for _, word := range line.Words {
		text := truncateCellText(word.Text, width-usedWidth)
		if text == "" {
			break
		}
		if word.StartMs <= a.pos && a.pos < word.EndMs {
			b.WriteString(a.styles.accent.Bold(true).Render(text))
		} else {
			b.WriteString(a.styles.muted.Render(text))
		}
		usedWidth += lipgloss.Width(text)
		if strings.HasSuffix(text, "…") {
			break
		}
	}
	return b.String()
}

func (a *App) currentLyricLineIndex() int {
	if a.lyric == nil || len(a.lyric.Lines) == 0 {
		return 0
	}
	idx := 0
	for i, line := range a.lyric.Lines {
		if line.StartMs <= a.pos && (line.EndMs == 0 || a.pos < line.EndMs) {
			return i
		}
		if line.StartMs <= a.pos {
			idx = i
		}
	}
	return idx
}

func truncateCellText(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)+"…") > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

func (a *App) renderPlayerBar() string {
	w := a.width

	// state icon
	icon := "▶"
	switch a.state {
	case audio.StatePlaying:
		icon = "▶"
	case audio.StatePaused:
		icon = "⏸"
	case audio.StateStopped:
		icon = "⏹"
	}

	// progress bar
	bar := a.progress.View()

	// time
	timeStr := fmt.Sprintf("%s / %s", fmtDuration(time.Duration(a.pos)*time.Millisecond),
		fmtDuration(time.Duration(a.dur)*time.Millisecond))

	// volume + speed
	info := fmt.Sprintf("vol %d%%  speed %.1fx", a.volume, a.speed)

	line1 := fmt.Sprintf("%s  %s  %s", icon, timeStr, info)
	if a.errMsg != "" {
		line1 = fmt.Sprintf("%s  %s  ⚠ %s", icon, timeStr, a.errMsg)
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		line1,
		bar,
		"  "+a.helpLine(),
	)

	return a.styles.player.
		Width(w).
		Render(content)
}

func (a *App) helpLine() string {
	return "q quit  ⏎ play  ␣ pause  n/b next/prev  ←→ seek  +- vol  [] speed  / filter"
}

// fmtDuration formats ms duration as M:SS or H:MM:SS.
func fmtDuration(d time.Duration) string {
	if d <= 0 {
		return "--:--"
	}
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func mouseButtonStr(b tea.MouseButton) string {
	switch b {
	case tea.MouseLeft:
		return "left"
	case tea.MouseRight:
		return "right"
	}
	return fmt.Sprintf("%d", b)
}

// ensure context import is used (engine takes ctx in future)
var _ = context.Background
