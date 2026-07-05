package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/locxl/musicli/internal/audio"
	"github.com/locxl/musicli/internal/library"
	"github.com/locxl/musicli/internal/log"
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

// App is the top-level bubbletea model.
type App struct {
	log     *log.Logger
	theme   *theme.Theme
	styles  *Styles
	keys    keyMap
	engine  *audio.Engine
	scanner *library.Scanner

	width, height int

	trackList list.Model
	progress  progress.Model

	tracks    []*library.Track
	current   int // index into tracks, -1 if none
	loading   bool

	// playback state mirror (polled from engine)
	pos    int
	dur    int
	state  audio.State
	volume int
	speed  float64
	errMsg string
}

// New creates the App model. Engine and scanner must be initialised.
func New(eng *audio.Engine, sc *library.Scanner, t *theme.Theme, lg *log.Logger) *App {
	keys := defaultKeyMap()
	styles := NewStyles(t)

	trackList := list.New([]list.Item{}, newListDelegate(t), 40, 20)
	trackList.Title = "Tracks"
	trackList.Styles.Title = styles.title
	trackList.SetShowHelp(false)
	trackList.SetShowTitle(true)
	trackList.SetShowStatusBar(false)
	trackList.SetFilteringEnabled(true)
	trackList.DisableQuitKeybindings()

	pbar := newProgressBar(t)

	return &App{
		log:       lg.WithModule("ui"),
		theme:     t,
		styles:    styles,
		keys:      keys,
		engine:    eng,
		scanner:   sc,
		trackList: trackList,
		progress:  pbar,
		current:   -1,
		volume:    80,
		speed:     1.0,
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
			return ScanErrMsg{Err: err}
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
	return tea.Batch(tickCmd())
}

// LoadPath triggers an async scan of the given file/dir path.
func (a *App) LoadPath(path string) {
	a.loading = true
	a.trackList.ResetSelected()
	a.trackList.Title = "Scanning..."
	a.trackList.SetItems([]list.Item{})
	a.tracks = nil
}

// LoadPathCmd returns a command that scans path.
func (a *App) LoadPathCmd(path string) tea.Cmd {
	a.loading = true
	a.trackList.Title = "Scanning..."
	return scanCmd(a.scanner, path)
}

// Update handles messages.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		a.resizeComponents()
		return a, nil

	case tea.KeyMsg:
		return a.handleKey(msg)

	case tea.MouseClickMsg:
		return a.handleMouse(msg)

	case tea.MouseWheelMsg:
		// forward wheel to list
		var cmd tea.Cmd
		a.trackList, cmd = a.trackList.Update(msg)
		return a, cmd

	case TracksLoadedMsg:
		a.loading = false
		a.tracks = msg.Tracks
		items := make([]list.Item, len(msg.Tracks))
		for i, t := range msg.Tracks {
			items[i] = trackItem{track: t}
		}
		a.trackList.SetItems(items)
		a.trackList.Title = fmt.Sprintf("Tracks (%d)", len(msg.Tracks))
		a.log.Info("library loaded", "count", len(msg.Tracks))
		return a, nil

	case ScanErrMsg:
		a.loading = false
		a.trackList.Title = "Tracks"
		a.errMsg = fmt.Sprintf("scan failed: %v", msg.Err)
		a.log.Error("scan failed", "err", msg.Err)
		return a, nil

	case tickMsg:
		a.pollEngine()
		return a, tickCmd()

	case errMsg:
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
	a.pos = a.engine.Position()
	a.dur = a.engine.Duration()
	a.state = a.engine.State()
	a.volume = a.engine.Volume()
	a.speed = a.engine.Speed()
	if err := a.engine.Err(); err != nil {
		a.errMsg = err.Error()
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
	// If the list is filtering, let it handle keys.
	if a.trackList.FilterState() == list.Filtering {
		var cmd tea.Cmd
		a.trackList, cmd = a.trackList.Update(msg)
		return a, cmd
	}

	switch {
	case key.Matches(msg, a.keys.Quit):
		return a, tea.Quit

	case key.Matches(msg, a.keys.Enter):
		return a, a.playSelected()

	case key.Matches(msg, a.keys.PlayPause):
		return a, a.togglePlayPause()

	case key.Matches(msg, a.keys.Next):
		return a, a.nextTrack()

	case key.Matches(msg, a.keys.Prev):
		return a, a.prevTrack()

	case key.Matches(msg, a.keys.SeekFwd):
		return a, a.seekRelative(5000)

	case key.Matches(msg, a.keys.SeekBack):
		return a, a.seekRelative(-5000)

	case key.Matches(msg, a.keys.VolUp):
		a.engine.SetVolume(a.volume + 5)
		return a, nil

	case key.Matches(msg, a.keys.VolDown):
		a.engine.SetVolume(a.volume - 5)
		return a, nil

	case key.Matches(msg, a.keys.SpeedUp):
		a.engine.SetSpeed(a.speed + 0.1)
		return a, nil

	case key.Matches(msg, a.keys.SpeedDown):
		a.engine.SetSpeed(a.speed - 0.1)
		return a, nil
	}

	// navigation falls through to list
	var cmd tea.Cmd
	a.trackList, cmd = a.trackList.Update(msg)
	return a, cmd
}

// handleMouse processes mouse click messages.
func (a *App) handleMouse(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	// Click in the sidebar list area → select; play only if selecting a
	// different track than the current one.
	sidebarW := a.sidebarWidth()
	if msg.X < sidebarW {
		prevIdx := a.trackList.Index()
		var cmd tea.Cmd
		a.trackList, cmd = a.trackList.Update(msg)
		newIdx := a.trackList.Index()
		if msg.Button == tea.MouseLeft && newIdx >= 0 && newIdx != prevIdx {
			return a, a.playSelected()
		}
		return a, cmd
	}
	// Click on progress bar → seek
	playerBarH := 4
	if msg.Y >= a.height-playerBarH {
		barX := msg.X
		barW := a.width
		if a.dur > 0 && barW > 0 {
			target := a.dur * barX / barW
			return a, a.seekTo(target)
		}
	}
	return a, nil
}

// --- playback commands ---

func (a *App) playSelected() tea.Cmd {
	idx := a.trackList.Index()
	if idx < 0 || idx >= len(a.tracks) {
		return nil
	}
	// Don't restart if the same track is already playing (avoids stutter from
	// rapid Enter/click re-triggering ffmpeg spawn).
	if idx == a.current && a.state == audio.StatePlaying {
		return nil
	}
	a.current = idx
	t := a.tracks[idx]
	a.log.Info("playing", "path", t.Path, "title", t.Title)
	if err := a.engine.Play(t.Path); err != nil {
		return func() tea.Msg { return errMsg{err: err} }
	}
	return nil
}

func (a *App) togglePlayPause() tea.Cmd {
	switch a.state {
	case audio.StatePlaying:
		a.engine.Pause()
	case audio.StatePaused:
		a.engine.Resume()
	case audio.StateStopped:
		return a.playSelected()
	}
	return nil
}

func (a *App) nextTrack() tea.Cmd {
	if len(a.tracks) == 0 {
		return nil
	}
	a.current = (a.current + 1) % len(a.tracks)
	a.trackList.Select(a.current)
	t := a.tracks[a.current]
	if err := a.engine.Play(t.Path); err != nil {
		return func() tea.Msg { return errMsg{err: err} }
	}
	return nil
}

func (a *App) prevTrack() tea.Cmd {
	if len(a.tracks) == 0 {
		return nil
	}
	if a.current < 0 {
		a.current = 0
	} else {
		a.current = (a.current - 1 + len(a.tracks)) % len(a.tracks)
	}
	a.trackList.Select(a.current)
	t := a.tracks[a.current]
	if err := a.engine.Play(t.Path); err != nil {
		return func() tea.Msg { return errMsg{err: err} }
	}
	return nil
}

func (a *App) seekRelative(deltaMs int) tea.Cmd {
	if a.dur <= 0 {
		return nil
	}
	target := a.pos + deltaMs
	return a.seekTo(target)
}

func (a *App) seekTo(target int) tea.Cmd {
	if err := a.engine.Seek(target); err != nil {
		return func() tea.Msg { return errMsg{err: err} }
	}
	return nil
}

// --- layout ---

func (a *App) sidebarWidth() int {
	w := a.width / 3
	if w < 30 {
		w = 30
	}
	if w > 50 {
		w = 50
	}
	if w > a.width-20 {
		w = a.width / 2
	}
	return w
}

func (a *App) resizeComponents() {
	if a.width == 0 || a.height == 0 {
		return
	}
	sidebarW := a.sidebarWidth()
	playerBarH := 4
	mainH := a.height - playerBarH

	a.trackList.SetWidth(sidebarW - 1)
	a.trackList.SetHeight(mainH)
	a.progress.SetWidth(a.width - 2)
}

// View renders the full UI.
func (a *App) View() tea.View {
	if a.width == 0 {
		return tea.NewView("Initializing...")
	}

	// Sidebar: track list
	sidebar := a.styles.sidebar.
		Width(a.sidebarWidth()).
		Height(a.height-4).
		Render(a.trackList.View())

	// Main area: now playing info
	main := a.renderMain()

	// Player bar
	bar := a.renderPlayerBar()

	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, main)
	full := lipgloss.JoinVertical(lipgloss.Left, body, bar)

	v := tea.NewView(a.styles.doc.Render(full))
	v.MouseMode = tea.MouseModeAllMotion
	return v
}

func (a *App) renderMain() string {
	w := a.width - a.sidebarWidth()
	h := a.height - 4
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}

	var b strings.Builder
	if a.current >= 0 && a.current < len(a.tracks) {
		t := a.tracks[a.current]
		b.WriteString(a.styles.title.Render(t.Title))
		b.WriteString("\n")
		if t.Artist != "" {
			b.WriteString(a.styles.muted.Render(t.Artist))
			b.WriteString("\n")
		}
		if t.Album != "" {
			b.WriteString(a.styles.muted.Render(t.Album))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(a.styles.muted.Render("[ cover + lyrics in later phases ]"))
	} else if a.loading {
		b.WriteString(a.styles.muted.Render("Scanning library..."))
	} else if len(a.tracks) == 0 {
		b.WriteString(a.styles.muted.Render("No tracks loaded.\nPass a file or directory path."))
	} else {
		b.WriteString(a.styles.muted.Render("Select a track to play."))
	}

	return a.styles.main.
		Width(w).
		Height(h).
		Render(b.String())
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

// ensure context import is used (engine takes ctx in future)
var _ = context.Background
