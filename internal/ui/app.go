package ui

import (
	"context"
	"errors"
	"fmt"
	"image"
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
	"github.com/locxl/musicli/internal/cover"
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
	DisableCover      bool
	CoverScale        string
	CoverProtocol     string
}

type leftContentMode int

const (
	leftContentBoth leftContentMode = iota
	leftContentCover
	leftContentLyrics
)

type coverScaleMode int

const (
	coverScaleFit coverScaleMode = iota
	coverScaleStretch
)

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

	tracks     []*library.Track
	current    int // index into tracks, -1 if none
	loading    bool
	lyric      *lyrics.Lyric
	lyricPath  string
	coverImage image.Image

	leftContent   leftContentMode
	coverScale    coverScaleMode
	coverProtocol string

	// playback state mirror (polled from engine)
	pos       int
	dur       int
	state     audio.State
	lastState audio.State
	volume    int
	speed     float64
	errMsg    string

	// lastLyricRender tracks the previously active lyric line and word so we
	// can force a full screen redraw when either changes, bypassing bubbletea's
	// cell diff engine which mis-handles SGR transitions across CJK wide chars.
	lastLyricRender lyricRenderState
}

type lyricRenderState struct {
	line int
	word int
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
	coverScale := coverScaleFromString(opts.CoverScale)
	coverProtocol := cover.SelectProtocol(opts.CoverProtocol, os.Getenv)

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
		"keybindings", 14,
	)

	return &App{
		log:             lg.WithModule("ui"),
		theme:           t,
		styles:          styles,
		keys:            keys,
		options:         opts,
		engine:          eng,
		scanner:         sc,
		trackList:       trackList,
		delegate:        delegate,
		progress:        pbar,
		current:         -1,
		volume:          80,
		speed:           1.0,
		leftContent:     leftContentBoth,
		coverScale:      coverScale,
		coverProtocol:   coverProtocol,
		lastState:       audio.StateStopped,
		lastLyricRender: lyricRenderState{line: -1, word: -1},
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
		prevLyric := a.lastLyricRender
		a.pollEngine()
		newLyric := a.currentLyricRenderState()
		a.lastLyricRender = newLyric
		coverCmd := a.kittyCoverCmd()
		// When the active lyric cell range changes, force a full screen redraw
		// to bypass the diff engine's mishandling of SGR transitions on CJK wide
		// chars.
		if newLyric != prevLyric {
			return a, tea.Batch(tickCmd(), tea.Sequence(func() tea.Msg { return tea.ClearScreen() }, coverCmd))
		}
		return a, tea.Batch(tickCmd(), coverCmd)

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

	case key.Matches(msg, a.keys.ToggleView):
		fl.Debug("key matched", "key", keyStr, "action", "toggleLeftContent")
		a.toggleLeftContent()
		return a, tea.Sequence(func() tea.Msg { return tea.ClearScreen() }, a.kittyCoverCmd())

	case key.Matches(msg, a.keys.ToggleScale):
		fl.Debug("key matched", "key", keyStr, "action", "toggleCoverScale")
		a.toggleCoverScale()
		return a, tea.Sequence(func() tea.Msg { return tea.ClearScreen() }, a.kittyCoverCmd())

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
	a.loadCurrentCover()
	return a.kittyCoverCmd()
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
	a.loadCurrentCover()
	return a.kittyCoverCmd()
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
	a.loadCurrentCover()
	return a.kittyCoverCmd()
}

func (a *App) toggleLeftContent() {
	switch a.leftContent {
	case leftContentBoth:
		a.leftContent = leftContentCover
	case leftContentCover:
		a.leftContent = leftContentLyrics
	default:
		a.leftContent = leftContentBoth
	}
}

func (a *App) toggleCoverScale() {
	switch a.coverScale {
	case coverScaleFit:
		a.coverScale = coverScaleStretch
	default:
		a.coverScale = coverScaleFit
	}
}

func coverScaleFromString(s string) coverScaleMode {
	if s == "stretch" {
		return coverScaleStretch
	}
	return coverScaleFit
}

func (a *App) coverScaleMode() cover.ScaleMode {
	if a.coverScale == coverScaleStretch {
		return cover.ScaleStretch
	}
	return cover.ScaleFit
}

func (a *App) kittyCoverCmd() tea.Cmd {
	seq := a.renderKittyCoverOverlay()
	if seq == "" {
		return nil
	}
	return tea.Raw(seq)
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
	a.lastLyricRender = lyricRenderState{line: -1, word: -1}
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

func (a *App) loadCurrentCover() {
	fl := a.log.WithFunc("loadCurrentCover")
	a.coverImage = nil
	if a.options.DisableCover {
		return
	}
	if a.current < 0 || a.current >= len(a.tracks) {
		return
	}
	path := a.tracks[a.current].Path
	if path == "" {
		return
	}
	img, err := cover.Extract(path)
	if err != nil {
		fl.Debug("cover load skipped", "path", path, "err", err)
		return
	}
	a.coverImage = img
	fl.Info("cover loaded", "path", path, "bounds", img.Bounds().String())
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
	singleColumnThreshold := 80
	if a.options.TrackListMaxWidth > 0 {
		singleColumnThreshold = a.options.TrackListMaxWidth
	}
	if a.width < singleColumnThreshold {
		listW = a.width
		if listW < 1 {
			listW = 1
		}
		return 0, listW
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
	v.AltScreen = true
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
	contentW := w - a.styles.leftPane.GetHorizontalFrameSize()
	if contentW < 1 {
		contentW = 1
	}
	contentH := h - a.styles.leftPane.GetVerticalFrameSize()
	if contentH < 1 {
		contentH = 1
	}
	switch a.leftContent {
	case leftContentCover:
		return a.renderCoverPane(contentW, contentH)
	case leftContentLyrics:
		return a.renderLyricsOrPlaceholder(contentW, contentH)
	default:
		return a.renderCoverAndLyricsPane(contentW, contentH)
	}
}

func (a *App) renderCoverAndLyricsPane(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	if a.options.DisableCover || a.coverImage == nil {
		return a.renderLyricsOrPlaceholder(w, h)
	}
	if w < 12 {
		return a.renderLyricsOrPlaceholder(w, h)
	}

	gapW := 1
	coverW := w / 2
	lyricsW := w - coverW - gapW
	if coverW < 1 || lyricsW < 1 {
		return a.renderLyricsOrPlaceholder(w, h)
	}

	coverPane := fitBlock(a.renderCoverPane(coverW, h), coverW, h)
	lyricsPane := fitBlock(a.renderLyricsOrPlaceholder(lyricsW, h), lyricsW, h)
	gap := fitBlock("", gapW, h)
	return fitBlock(lipgloss.JoinHorizontal(lipgloss.Top, coverPane, gap, lyricsPane), w, h)
}

func (a *App) renderLyricsOrPlaceholder(w, h int) string {
	if a.lyric != nil && len(a.lyric.Lines) > 0 {
		return fitBlock(a.renderLyricsPane(w, h), w, h)
	}
	return fitBlock(lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, a.styles.muted.Render("[ lyrics ]")), w, h)
}

func (a *App) renderCoverPane(w, h int) string {
	if a.options.DisableCover {
		return a.renderLyricsOrPlaceholder(w, h)
	}
	if a.coverImage == nil {
		return fitBlock(lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, a.styles.muted.Render("[ cover ]")), w, h)
	}
	if a.coverProtocol == cover.ProtocolKitty {
		return fitBlock("", w, h)
	}
	return fitBlock(cover.RenderHalfBlockWithScale(a.coverImage, w, h, a.coverScaleMode()), w, h)
}

func (a *App) renderKittyCoverOverlay() string {
	const kittyImageID = 1
	if a.coverProtocol != cover.ProtocolKitty || a.options.DisableCover {
		return ""
	}
	if a.leftContent == leftContentLyrics || a.coverImage == nil {
		return cover.ClearKittyImage(kittyImageID)
	}

	w := a.leftPaneWidth() - a.styles.leftPane.GetHorizontalFrameSize()
	h := a.bodyHeight() - a.styles.leftPane.GetVerticalFrameSize()
	if w < 1 || h < 1 {
		return cover.ClearKittyImage(kittyImageID)
	}

	x := 1
	y := 3 // top bar content + bottom border + 1-based terminal row.
	coverW := w
	if a.leftContent == leftContentBoth && w >= 12 {
		coverW = w / 2
	}
	if coverW < 1 {
		return cover.ClearKittyImage(kittyImageID)
	}

	seq, err := cover.RenderKitty(a.coverImage, cover.KittyPlacement{
		ID:     kittyImageID,
		X:      x,
		Y:      y,
		Width:  coverW,
		Height: h,
		Scale:  a.coverScaleMode(),
	})
	if err != nil {
		a.log.WithFunc("renderKittyCoverOverlay").Warn("kitty cover render failed", "err", err)
		return cover.ClearKittyImage(kittyImageID)
	}
	return seq
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
				rendered = append(rendered, padCellText(a.styles.muted.Render(text), w-1))
			}
		case lyricRowTranslation:
			text := truncateCellText(visual.text, w-1)
			rendered = append(rendered, padCellText(a.styles.muted.Render(text), w-1))
		default:
			rendered = append(rendered, strings.Repeat(" ", max(0, w-1)))
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
	muted := ansi.NewStyle().ForegroundColor(a.theme.Muted)
	accent := ansi.NewStyle().ForegroundColor(a.theme.Accent)

	if len(line.Words) == 0 {
		text := truncateCellText(line.Text, width)
		return padCellText(accent.Styled(text), width)
	}

	current := -1
	for i, word := range line.Words {
		if word.StartMs <= a.pos && a.pos < word.EndMs {
			current = i
			break
		}
	}
	if current < 0 {
		return padCellText(muted.Styled(truncateCellText(line.Text, width)), width)
	}

	prefix := wordsText(line.Words[:current])
	active := line.Words[current].Text
	suffix := wordsText(line.Words[current+1:])
	if ansi.StringWidth(prefix)+ansi.StringWidth(active) > width {
		return padCellText(muted.Styled(truncateCellText(line.Text, width)), width)
	}

	var b strings.Builder
	remaining := width
	writeRun := func(style ansi.Style, text string) bool {
		if remaining <= 0 || text == "" {
			return false
		}
		clipped := truncateCellText(text, remaining)
		if clipped == "" {
			return false
		}
		b.WriteString(style.String())
		b.WriteString(clipped)
		remaining -= ansi.StringWidth(clipped)
		return strings.HasSuffix(clipped, "…")
	}
	if writeRun(muted, prefix) {
		b.WriteString(ansi.ResetStyle)
		return padCellText(b.String(), width)
	}
	if writeRun(accent, active) {
		b.WriteString(ansi.ResetStyle)
		return padCellText(b.String(), width)
	}
	writeRun(muted, suffix)
	b.WriteString(ansi.ResetStyle)
	return padCellText(b.String(), width)
}

func wordsText(words []lyrics.Word) string {
	var b strings.Builder
	for _, word := range words {
		b.WriteString(word.Text)
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

// currentLyricWordIndex returns the active word index within the current
// lyric line, or -1 if no word is currently active.
func (a *App) currentLyricWordIndex() int {
	return a.currentLyricRenderState().word
}

// currentLyricRenderState returns the active lyric line and word indexes.
// The word index is -1 if no word is currently active.
func (a *App) currentLyricRenderState() lyricRenderState {
	if a.lyric == nil || len(a.lyric.Lines) == 0 {
		return lyricRenderState{line: -1, word: -1}
	}
	lineIdx := a.currentLyricLineIndex()
	if lineIdx < 0 || lineIdx >= len(a.lyric.Lines) {
		return lyricRenderState{line: -1, word: -1}
	}
	for i, word := range a.lyric.Lines[lineIdx].Words {
		if word.StartMs <= a.pos && a.pos < word.EndMs {
			return lyricRenderState{line: lineIdx, word: i}
		}
	}
	return lyricRenderState{line: lineIdx, word: -1}
}

func truncateCellText(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(s, width, "…")
}

func padCellText(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if pad := width - ansi.StringWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

func fitBlock(s string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	in := strings.Split(s, "\n")
	out := make([]string, height)
	for i := 0; i < height; i++ {
		if i >= len(in) {
			out[i] = strings.Repeat(" ", width)
			continue
		}
		line := in[i]
		if ansi.StringWidth(line) > width {
			line = ansi.Truncate(line, width, "")
		}
		out[i] = padCellText(line, width)
	}
	return strings.Join(out, "\n")
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
	return "q quit  ⏎ play  ␣ pause  n/b next/prev  v view  c scale  ←→ seek  +- vol  [] speed  / filter"
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
