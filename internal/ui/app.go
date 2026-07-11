package ui

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"math/rand/v2"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/locxl/musicli/internal/audio"
	"github.com/locxl/musicli/internal/cover"
	"github.com/locxl/musicli/internal/library"
	"github.com/locxl/musicli/internal/log"
	"github.com/locxl/musicli/internal/lyrics"
	"github.com/locxl/musicli/internal/mpris"
	"github.com/locxl/musicli/internal/playlist"
	"github.com/locxl/musicli/internal/theme"
)

// trackItem adapts a library.Track to a list.Item.
type trackItem struct {
	track    *library.Track
	favorite bool
}

const favoriteMarker = "* "

func (i trackItem) Title() string {
	t := i.track.Title
	if t == "" {
		t = "(unknown)"
	}
	if i.favorite {
		return favoriteMarker + t
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
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " - ")
}
func (i trackItem) FilterValue() string { return i.track.Title + " " + i.track.Artist }

type albumItem struct {
	album *library.Album
}

type playlistItem struct {
	playlist playlist.Playlist
}

func (i playlistItem) Title() string { return i.playlist.Name }
func (i playlistItem) Description() string {
	return fmt.Sprintf("%d tracks", len(i.playlist.Paths))
}
func (i playlistItem) FilterValue() string { return i.playlist.Name }

func (i albumItem) Title() string {
	if i.album == nil || i.album.Name == "" {
		return "Unknown Album"
	}
	return i.album.Name
}

func (i albumItem) Description() string {
	if i.album == nil {
		return "Unknown Artist - 0 tracks"
	}
	artist := i.album.AlbumArtist
	if artist == "" {
		artist = "Unknown Artist"
	}
	return fmt.Sprintf("%s - %d tracks", artist, len(i.album.Tracks))
}

func (i albumItem) FilterValue() string {
	if i.album == nil {
		return ""
	}
	return i.album.Name + " " + i.album.AlbumArtist
}

// Options configures UI layout behavior.
type Options struct {
	// TrackListMaxWidth caps the content-fit track list width. Zero means no cap.
	TrackListMaxWidth int
	// ProgressStyle chooses an independent bar or the player-bar separator.
	ProgressStyle              string
	SeparatorProgressThickness int
	DisableCover               bool
	CoverScale                 string
	CoverProtocol              string
	LibrarySortField           string
	LibrarySortOrder           string
	GroupByAlbum               bool
	PlaybackRepeat             string
	PlaybackShuffle            bool
	LyricsAlign                string
	LyricsHighlightMode        string
	SpectrumEnabled            bool
	PlaylistStore              *playlist.Store
	MPRISSink                  func(mpris.Snapshot)
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

type lyricAlignMode int

const (
	lyricAlignLeft lyricAlignMode = iota
	lyricAlignCenter
	lyricAlignRight
)

type lyricHighlightMode int

const (
	lyricHighlightPlayed lyricHighlightMode = iota
	lyricHighlightCurrent
)

type libraryViewMode int

const (
	libraryViewTracks libraryViewMode = iota
	libraryViewAlbums
	libraryViewAlbumTracks
	libraryViewPlaylists
	libraryViewPlaylistTracks
)

type queueSource int

const (
	queueSourceNone queueSource = iota
	queueSourceAll
	queueSourceAlbum
	queueSourceFiltered
	queueSourcePlaylist
)

// App is the top-level bubbletea model.
type App struct {
	log       *log.Logger
	theme     *theme.Theme
	styles    *Styles
	keys      keyMap
	options   Options
	engine    *audio.Engine
	scanner   *library.Scanner
	playlists *playlist.Store

	width, height int
	leftW         int

	trackList list.Model
	delegate  list.DefaultDelegate
	progress  progress.Model

	tracks     []*library.Track
	albums     []*library.Album
	queue      []*library.Track
	queueSrc   queueSource
	current    int // index into tracks, -1 if none
	loading    bool
	lyric      *lyrics.Lyric
	lyricPath  string
	coverImage image.Image
	coverURL   string

	leftContent          leftContentMode
	libraryView          libraryViewMode
	currentAlbum         int
	currentPlaylist      string
	pendingPlaylistTrack *library.Track
	creatingPlaylist     bool
	playlistNameInput    textinput.Model
	coverScale           coverScaleMode
	lyricAlign           lyricAlignMode
	lyricHighlight       lyricHighlightMode
	spectrumEnabled      bool
	coverProtocol        string
	cellPixelW           int
	cellPixelH           int
	lastKittyCover       string
	kittyCoverDrawn      bool
	lastKittyProgressPx  int
	kittyProgressImageID int
	// lastKittyFingerprint captures the state that determines the kitty overlay
	// appearance. Compared on every tick to skip the expensive RenderKitty()
	// (PNG encode + base64) when nothing has changed.
	lastKittyFingerprint string

	// playback state mirror (polled from engine)
	pos        int
	dur        int
	state      audio.State
	lastState  audio.State
	volume     int
	speed      float64
	errMsg     string
	engineErr  error
	wasPlaying bool

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
	lyricAlign := lyricAlignFromString(opts.LyricsAlign)
	lyricHighlight := lyricHighlightFromString(opts.LyricsHighlightMode)
	coverProtocol := cover.SelectProtocol(opts.CoverProtocol, os.Getenv)

	trackList := list.New([]list.Item{}, delegate, 40, 20)
	trackList.Title = "Tracks"
	trackList.Styles = newListComponentStyles(t)
	trackList.SetShowHelp(false)
	trackList.SetShowTitle(true)
	trackList.SetShowStatusBar(false)
	trackList.SetFilteringEnabled(true)
	trackList.DisableQuitKeybindings()
	playlistNameInput := textinput.New()
	playlistNameInput.Prompt = "New playlist: "

	pbar := newProgressBar(t)

	fl.Debug("app created",
		"engine", eng != nil,
		"scanner", sc != nil,
		"theme_mode", t.Mode,
		"keybindings", 16,
	)

	return &App{
		log:                 lg.WithModule("ui"),
		theme:               t,
		styles:              styles,
		keys:                keys,
		options:             opts,
		engine:              eng,
		scanner:             sc,
		trackList:           trackList,
		delegate:            delegate,
		progress:            pbar,
		current:             -1,
		volume:              80,
		speed:               1.0,
		leftContent:         leftContentBoth,
		currentAlbum:        -1,
		coverScale:          coverScale,
		lyricAlign:          lyricAlign,
		lyricHighlight:      lyricHighlight,
		spectrumEnabled:     opts.SpectrumEnabled,
		coverProtocol:       coverProtocol,
		playlists:           opts.PlaylistStore,
		playlistNameInput:   playlistNameInput,
		lastState:           audio.StateStopped,
		lastLyricRender:     lyricRenderState{line: -1, word: -1},
		lastKittyProgressPx: -1,
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

// ThemeChangedMsg is delivered from the platform appearance watcher.
type ThemeChangedMsg struct{ Theme *theme.Theme }

// DBusCommandMsg carries a player command received from D-Bus into the UI
// event loop. The UI remains the single owner of playback/list state.
type DBusCommandMsg struct{ Command mpris.Command }

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

const (
	tickInterval          = time.Second / 30
	lyricFinalWordGraceMs = 60
)

// tickCmd emits a tick for progress/state polling.
func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// --- bubbletea Model ---

// Init starts the app.
func (a *App) Init() tea.Cmd {
	a.log.WithFunc("Init").Debug("init started")
	return tea.Batch(tickCmd(), tea.Raw(ansi.WindowOp(ansi.RequestCellSizeWinOp)))
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
		return a, a.resetKittyProgressCmd()

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
		library.SortTracks(a.tracks, library.SortField(a.options.LibrarySortField), a.options.LibrarySortOrder)
		a.albums = library.GroupByAlbum(a.tracks)
		a.currentAlbum = -1
		if a.options.GroupByAlbum {
			a.setLibraryView(libraryViewAlbums)
		} else {
			a.setLibraryView(libraryViewTracks)
		}
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

	case DBusCommandMsg:
		return a, a.handleDBusCommand(msg.Command)

	case ThemeChangedMsg:
		if msg.Theme == nil {
			return a, nil
		}
		a.applyTheme(msg.Theme)
		return a, a.themeRedrawCmd(a.resetKittyProgressCmd())

	case tickMsg:
		prevLyric := a.lastLyricRender
		a.pollEngine()
		a.publishMPRISSnapshot()
		if a.trackEndedNaturally() {
			return a, tea.Batch(tickCmd(), a.autoAdvanceAfterEnd())
		}
		a.wasPlaying = a.state == audio.StatePlaying
		newLyric := a.currentLyricRenderState()
		a.lastLyricRender = newLyric
		// When the active lyric cell range changes, force a full screen redraw
		// to bypass the diff engine's mishandling of SGR transitions on CJK wide
		// chars.
		if newLyric != prevLyric {
			return a, tea.Batch(tickCmd(), a.lyricChangeCmd())
		}
		return a, tea.Batch(tickCmd(), a.kittyCoverCmd(), a.kittyProgressCmd())

	case errMsg:
		fl := a.log.WithFunc("Update")
		fl.Debug("received msg", "type", "errMsg", "err", msg.err)
		a.errMsg = msg.err.Error()
		return a, nil

	case uv.CellSizeEvent:
		fl := a.log.WithFunc("Update")
		fl.Debug("received msg", "type", "CellSizeEvent", "width", msg.Width, "height", msg.Height)
		a.cellPixelW = msg.Width
		a.cellPixelH = msg.Height
		a.lastKittyCover = ""
		a.kittyCoverDrawn = false
		a.lastKittyFingerprint = ""
		return a, a.resetKittyProgressCmd()
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
	a.engineErr = a.engine.Err()
	if a.engineErr != nil {
		a.errMsg = a.engineErr.Error()
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

func (a *App) MPRISSnapshot() mpris.Snapshot {
	var track *library.Track
	if a.current >= 0 && a.current < len(a.tracks) {
		track = a.tracks[a.current]
	}

	lineIdx := -1
	wordIdx := -1
	currentLine := ""
	lyricText := ""
	lyricFormat := ""
	synced := false
	if a.lyric != nil && len(a.lyric.Lines) > 0 {
		state := a.currentLyricRenderState()
		lineIdx = state.line
		wordIdx = state.word
		if lineIdx >= 0 && lineIdx < len(a.lyric.Lines) {
			currentLine = a.lyric.Lines[lineIdx].Text
		}
		if lyricHasTiming(a.lyric) {
			lyricText = lyricLRCText(a.lyric)
			lyricFormat = "lrc"
			synced = true
		} else {
			lyricText = lyricPlainText(a.lyric)
			lyricFormat = "plain"
		}
	}

	return mpris.Snapshot{
		Track:          track,
		CurrentIndex:   a.current,
		PlaybackStatus: mprisPlaybackStatus(a.state),
		LoopStatus:     mprisLoopStatus(a.playbackRepeat()),
		Shuffle:        a.options.PlaybackShuffle,
		PositionMS:     a.pos,
		DurationMS:     a.dur,
		Volume:         a.volume,
		Speed:          a.speed,
		CoverURL:       a.coverURL,
		LyricText:      lyricText,
		LyricFormat:    lyricFormat,
		CurrentLine:    currentLine,
		CurrentLineIdx: lineIdx,
		CurrentWordIdx: wordIdx,
		Synced:         synced,
	}
}

func (a *App) publishMPRISSnapshot() {
	if a.options.MPRISSink != nil {
		a.options.MPRISSink(a.MPRISSnapshot())
	}
}

func mprisPlaybackStatus(state audio.State) mpris.PlaybackStatus {
	switch state {
	case audio.StatePlaying:
		return mpris.StatusPlaying
	case audio.StatePaused:
		return mpris.StatusPaused
	default:
		return mpris.StatusStopped
	}
}

func mprisLoopStatus(repeat string) mpris.LoopStatus {
	switch repeat {
	case "one":
		return mpris.LoopTrack
	case "list":
		return mpris.LoopPlaylist
	default:
		return mpris.LoopNone
	}
}

func lyricLRCText(ly *lyrics.Lyric) string {
	if ly == nil || len(ly.Lines) == 0 {
		return ""
	}
	lines := make([]string, 0, len(ly.Lines))
	for _, line := range ly.Lines {
		text := line.Text
		if text == "" && len(line.Words) > 0 {
			text = wordsText(line.Words)
		}
		if text == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("[%02d:%05.2f]%s", line.StartMs/60000, float64(line.StartMs%60000)/1000.0, text))
	}
	return strings.Join(lines, "\n")
}

func lyricPlainText(ly *lyrics.Lyric) string {
	if ly == nil || len(ly.Lines) == 0 {
		return ""
	}
	lines := make([]string, 0, len(ly.Lines))
	for _, line := range ly.Lines {
		text := line.Text
		if text == "" && len(line.Words) > 0 {
			text = wordsText(line.Words)
		}
		if text != "" {
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, "\n")
}

func lyricHasTiming(ly *lyrics.Lyric) bool {
	if ly == nil {
		return false
	}
	for _, line := range ly.Lines {
		if line.StartMs > 0 || line.EndMs > 0 || len(line.Words) > 0 {
			return true
		}
	}
	return false
}

// handleKey processes key messages.
func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	fl := a.log.WithFunc("handleKey")
	keyStr := msg.String()
	if a.creatingPlaylist {
		switch msg.String() {
		case "esc":
			a.creatingPlaylist = false
			a.playlistNameInput.Blur()
			return a, nil
		case "enter":
			a.createPlaylist()
			return a, nil
		}
		var cmd tea.Cmd
		a.playlistNameInput, cmd = a.playlistNameInput.Update(msg)
		return a, cmd
	}

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
		if a.libraryView == libraryViewPlaylists {
			fl.Debug("key matched", "key", keyStr, "action", "enterPlaylist")
			if a.pendingPlaylistTrack != nil {
				a.addPendingTrackToSelectedPlaylist()
			} else if a.enterPlaylist() {
				a.resizeComponents()
			}
			return a, nil
		}
		fl.Debug("key matched", "key", keyStr, "action", "playSelected")
		return a, a.playSelected()

	case key.Matches(msg, a.keys.TogglePlaylists):
		fl.Debug("key matched", "key", keyStr, "action", "togglePlaylistView")
		a.pendingPlaylistTrack = nil
		a.togglePlaylistView()
		a.resizeComponents()
		return a, nil

	case key.Matches(msg, a.keys.ToggleFavorite):
		fl.Debug("key matched", "key", keyStr, "action", "toggleFavorite")
		a.toggleFavorite()
		return a, nil

	case key.Matches(msg, a.keys.RemoveFromList):
		fl.Debug("key matched", "key", keyStr, "action", "removeSelectedFromPlaylist")
		a.removeSelectedFromPlaylist()
		return a, nil

	case key.Matches(msg, a.keys.SortPlaylist):
		fl.Debug("key matched", "key", keyStr, "action", "sortCurrentPlaylist")
		a.sortCurrentPlaylist()
		return a, nil

	case key.Matches(msg, a.keys.DeletePlaylist):
		fl.Debug("key matched", "key", keyStr, "action", "deleteCurrentPlaylist")
		a.deleteCurrentPlaylist()
		return a, nil

	case key.Matches(msg, a.keys.AddToPlaylist):
		fl.Debug("key matched", "key", keyStr, "action", "choosePlaylistForSelectedTrack")
		a.choosePlaylistForSelectedTrack()
		return a, nil

	case key.Matches(msg, a.keys.NewPlaylist):
		fl.Debug("key matched", "key", keyStr, "action", "startPlaylistCreation")
		a.startPlaylistCreation()
		return a, nil

	case key.Matches(msg, a.keys.PlayPause):
		fl.Debug("key matched", "key", keyStr, "action", "togglePlayPause")
		return a, a.togglePlayPause()

	case key.Matches(msg, a.keys.Next):
		fl.Debug("key matched", "key", keyStr, "action", "nextTrack")
		return a, a.nextTrack()

	case key.Matches(msg, a.keys.Prev):
		fl.Debug("key matched", "key", keyStr, "action", "prevTrack")
		return a, a.prevTrack()

	case key.Matches(msg, a.keys.ToggleRepeat):
		fl.Debug("key matched", "key", keyStr, "action", "toggleRepeat")
		a.toggleRepeat()
		return a, nil

	case key.Matches(msg, a.keys.ToggleShuffle):
		fl.Debug("key matched", "key", keyStr, "action", "toggleShuffle")
		a.toggleShuffle()
		return a, nil

	case key.Matches(msg, a.keys.ToggleLyricAlign):
		fl.Debug("key matched", "key", keyStr, "action", "toggleLyricAlign")
		a.toggleLyricAlign()
		return a, nil

	case key.Matches(msg, a.keys.ToggleLyricHighlight):
		fl.Debug("key matched", "key", keyStr, "action", "toggleLyricHighlight")
		a.toggleLyricHighlight()
		return a, nil

	case key.Matches(msg, a.keys.ToggleSpectrum):
		fl.Debug("key matched", "key", keyStr, "action", "toggleSpectrum")
		a.spectrumEnabled = !a.spectrumEnabled
		return a, a.clearScreenAndKittyCoverCmd()

	case key.Matches(msg, a.keys.ToggleView):
		fl.Debug("key matched", "key", keyStr, "action", "toggleLeftContent")
		a.toggleLeftContent()
		return a, a.clearScreenAndKittyCoverCmd()

	case key.Matches(msg, a.keys.ToggleScale):
		fl.Debug("key matched", "key", keyStr, "action", "toggleCoverScale")
		a.toggleCoverScale()
		return a, a.clearScreenAndKittyCoverCmd()

	case key.Matches(msg, a.keys.ToggleList):
		fl.Debug("key matched", "key", keyStr, "action", "toggleLibraryView")
		a.toggleLibraryView()
		a.resizeComponents()
		return a, nil

	case key.Matches(msg, a.keys.Back):
		fl.Debug("key matched", "key", keyStr, "action", "backToAlbums")
		if a.backToAlbums() {
			a.resizeComponents()
			return a, nil
		}
		if a.backToPlaylistList() {
			a.resizeComponents()
			return a, nil
		}
		if a.libraryView == libraryViewPlaylists && a.pendingPlaylistTrack != nil {
			a.pendingPlaylistTrack = nil
			a.setLibraryView(libraryViewTracks)
			return a, nil
		}

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

func (a *App) choosePlaylistForSelectedTrack() {
	if a.playlists == nil || a.libraryView == libraryViewAlbums || a.libraryView == libraryViewPlaylists {
		return
	}
	idx := a.selectedTrackIndex()
	if idx < 0 || idx >= len(a.tracks) {
		return
	}
	a.pendingPlaylistTrack = a.tracks[idx]
	// A track search must not also filter the destination playlist list.
	a.trackList.ResetFilter()
	a.setLibraryView(libraryViewPlaylists)
}

func (a *App) addPendingTrackToSelectedPlaylist() {
	if a.playlists == nil || a.pendingPlaylistTrack == nil {
		return
	}
	item, ok := a.trackList.SelectedItem().(playlistItem)
	if !ok {
		return
	}
	if a.playlists.Add(item.playlist.ID, a.pendingPlaylistTrack.Path) {
		if err := a.playlists.Save(); err != nil {
			a.errMsg = fmt.Sprintf("save playlists: %v", err)
			return
		}
		a.errMsg = fmt.Sprintf("added to %s", item.playlist.Name)
	} else {
		a.errMsg = fmt.Sprintf("already in %s", item.playlist.Name)
	}
	a.pendingPlaylistTrack = nil
	a.setLibraryView(libraryViewTracks)
}

func (a *App) startPlaylistCreation() {
	if a.playlists == nil || a.libraryView != libraryViewPlaylists {
		return
	}
	a.creatingPlaylist = true
	a.playlistNameInput.SetValue("")
	a.playlistNameInput.Focus()
}

func (a *App) createPlaylist() {
	if a.playlists == nil {
		return
	}
	playlist, err := a.playlists.Create(a.playlistNameInput.Value())
	if err != nil {
		a.errMsg = "playlist name cannot be empty"
		return
	}
	if err := a.playlists.Save(); err != nil {
		a.errMsg = fmt.Sprintf("save playlists: %v", err)
		return
	}
	if a.pendingPlaylistTrack != nil {
		if a.playlists.Add(playlist.ID, a.pendingPlaylistTrack.Path) {
			if err := a.playlists.Save(); err != nil {
				a.errMsg = fmt.Sprintf("save playlists: %v", err)
				return
			}
		}
		a.pendingPlaylistTrack = nil
	}
	a.creatingPlaylist = false
	a.playlistNameInput.Blur()
	a.setLibraryView(libraryViewPlaylists)
	a.errMsg = fmt.Sprintf("created %s", playlist.Name)
}

func (a *App) toggleFavorite() {
	if a.playlists == nil {
		return
	}
	idx := a.selectedTrackIndex()
	if idx < 0 || idx >= len(a.tracks) {
		return
	}
	track := a.tracks[idx]
	favorited := a.playlists.ToggleFavorite(track.Path)
	if err := a.playlists.Save(); err != nil {
		a.errMsg = fmt.Sprintf("save favorites: %v", err)
		return
	}
	a.refreshFavoriteMarkers()
	if favorited {
		a.errMsg = "added to Favorites"
	} else {
		a.errMsg = "removed from Favorites"
	}
}

func (a *App) refreshFavoriteMarkers() {
	items := a.trackList.Items()
	updated := make([]list.Item, len(items))
	for i, item := range items {
		track, ok := item.(trackItem)
		if !ok {
			updated[i] = item
			continue
		}
		updated[i] = a.newTrackItem(track.track)
	}
	filter := a.trackList.FilterValue()
	wasFiltered := a.trackList.IsFiltered()
	selected := a.trackList.Index()
	a.trackList.SetItems(updated)
	if wasFiltered {
		a.trackList.SetFilterText(filter)
		if visible := len(a.trackList.VisibleItems()); visible > 0 {
			a.trackList.Select(min(selected, visible-1))
		}
	}
}

func (a *App) removeSelectedFromPlaylist() {
	if a.libraryView != libraryViewPlaylistTracks || a.playlists == nil {
		return
	}
	item, ok := a.trackList.SelectedItem().(trackItem)
	if !ok || item.track == nil {
		return
	}
	if !a.playlists.Remove(a.currentPlaylist, item.track.Path) {
		return
	}
	if err := a.playlists.Save(); err != nil {
		a.errMsg = fmt.Sprintf("save playlists: %v", err)
		return
	}
	a.setLibraryView(libraryViewPlaylistTracks)
}

func (a *App) sortCurrentPlaylist() {
	if a.libraryView != libraryViewPlaylistTracks || a.playlists == nil {
		return
	}
	titles := make(map[string]string, len(a.tracks))
	for _, track := range a.tracks {
		titles[track.Path] = track.Title
	}
	if err := a.playlists.Sort(a.currentPlaylist, func(path string) string { return titles[path] }); err != nil {
		a.errMsg = fmt.Sprintf("sort playlist: %v", err)
		return
	}
	if err := a.playlists.Save(); err != nil {
		a.errMsg = fmt.Sprintf("save playlists: %v", err)
		return
	}
	a.setLibraryView(libraryViewPlaylistTracks)
}

func (a *App) deleteCurrentPlaylist() {
	if a.libraryView != libraryViewPlaylists || a.playlists == nil {
		return
	}
	item, ok := a.trackList.SelectedItem().(playlistItem)
	if !ok {
		return
	}
	if err := a.playlists.Delete(item.playlist.ID); err != nil {
		if errors.Is(err, playlist.ErrProtectedPlaylist) {
			a.errMsg = "Favorites cannot be deleted"
		} else {
			a.errMsg = fmt.Sprintf("delete playlist: %v", err)
		}
		return
	}
	if err := a.playlists.Save(); err != nil {
		a.errMsg = fmt.Sprintf("save playlists: %v", err)
		return
	}
	a.setLibraryView(libraryViewPlaylists)
}

// handleMouse processes mouse click messages.
func (a *App) handleMouse(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	fl := a.log.WithFunc("handleMouse")
	leftW := a.leftPaneWidth()
	const topBarTotalH = 2 // 1 content + bottom border
	playerBarTotalH := a.playerBarHeight()

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
		if msg.Button == tea.MouseLeft && newIdx >= 0 && (newIdx != prevIdx || a.libraryView == libraryViewAlbums) {
			if a.libraryView == libraryViewPlaylists {
				if a.pendingPlaylistTrack != nil {
					a.addPendingTrackToSelectedPlaylist()
				} else if a.enterPlaylist() {
					a.resizeComponents()
				}
				return a, nil
			}
			return a, a.playSelected()
		}
		return a, cmd
	}

	return a, nil
}

func (a *App) setLibraryView(mode libraryViewMode) {
	a.libraryView = mode
	switch mode {
	case libraryViewAlbums:
		a.currentAlbum = -1
		items := make([]list.Item, len(a.albums))
		for i, album := range a.albums {
			items[i] = albumItem{album: album}
		}
		a.trackList.SetItems(items)
		a.trackList.Title = fmt.Sprintf("Albums (%d)", len(a.albums))
	case libraryViewAlbumTracks:
		if a.currentAlbum < 0 || a.currentAlbum >= len(a.albums) {
			a.setLibraryView(libraryViewAlbums)
			return
		}
		album := a.albums[a.currentAlbum]
		items := make([]list.Item, len(album.Tracks))
		for i, track := range album.Tracks {
			items[i] = a.newTrackItem(track)
		}
		a.trackList.SetItems(items)
		a.trackList.Title = fmt.Sprintf("%s (%d)", album.Name, len(album.Tracks))
	case libraryViewPlaylists:
		a.currentAlbum = -1
		a.currentPlaylist = ""
		items := make([]list.Item, 0)
		if a.playlists != nil {
			items = make([]list.Item, len(a.playlists.Playlists))
			for i, playlist := range a.playlists.Playlists {
				items[i] = playlistItem{playlist: playlist}
			}
		}
		a.trackList.SetItems(items)
		a.trackList.Title = fmt.Sprintf("Playlists (%d)", len(items))
	case libraryViewPlaylistTracks:
		playlist, ok := a.currentPlaylistValue()
		if !ok {
			a.setLibraryView(libraryViewPlaylists)
			return
		}
		tracks := a.playlistTracks(playlist)
		items := make([]list.Item, len(tracks))
		for i, track := range tracks {
			items[i] = a.newTrackItem(track)
		}
		a.trackList.SetItems(items)
		a.trackList.Title = fmt.Sprintf("%s (%d)", playlist.Name, len(tracks))
	default:
		a.currentAlbum = -1
		items := make([]list.Item, len(a.tracks))
		for i, track := range a.tracks {
			items[i] = a.newTrackItem(track)
		}
		a.trackList.SetItems(items)
		a.trackList.Title = fmt.Sprintf("Tracks (%d)", len(a.tracks))
	}
	a.trackList.ResetSelected()
}

func (a *App) newTrackItem(track *library.Track) trackItem {
	return trackItem{track: track, favorite: a.playlists != nil && a.playlists.IsFavorite(track.Path)}
}

func (a *App) currentPlaylistValue() (playlist.Playlist, bool) {
	if a.playlists == nil || a.currentPlaylist == "" {
		return playlist.Playlist{}, false
	}
	return a.playlists.Get(a.currentPlaylist)
}

func (a *App) playlistTracks(playlist playlist.Playlist) []*library.Track {
	byPath := make(map[string]*library.Track, len(a.tracks))
	for _, track := range a.tracks {
		byPath[track.Path] = track
	}
	tracks := make([]*library.Track, 0, len(playlist.Paths))
	for _, path := range playlist.Paths {
		if track := byPath[path]; track != nil {
			tracks = append(tracks, track)
		}
	}
	return tracks
}

func (a *App) toggleLibraryView() {
	if a.libraryView == libraryViewPlaylists || a.libraryView == libraryViewPlaylistTracks {
		a.setLibraryView(libraryViewTracks)
		return
	}
	if a.libraryView == libraryViewTracks {
		a.setLibraryView(libraryViewAlbums)
		return
	}
	a.setLibraryView(libraryViewTracks)
}

func (a *App) togglePlaylistView() {
	if a.libraryView == libraryViewPlaylists || a.libraryView == libraryViewPlaylistTracks {
		a.setLibraryView(libraryViewTracks)
		return
	}
	a.setLibraryView(libraryViewPlaylists)
}

func (a *App) enterAlbum() bool {
	if a.libraryView != libraryViewAlbums {
		return false
	}
	idx := a.trackList.Index()
	if idx < 0 || idx >= len(a.albums) {
		return false
	}
	a.currentAlbum = idx
	a.setLibraryView(libraryViewAlbumTracks)
	return true
}

func (a *App) enterPlaylist() bool {
	if a.libraryView != libraryViewPlaylists {
		return false
	}
	item, ok := a.trackList.SelectedItem().(playlistItem)
	if !ok {
		return false
	}
	a.currentPlaylist = item.playlist.ID
	a.setLibraryView(libraryViewPlaylistTracks)
	return true
}

func (a *App) backToAlbums() bool {
	if a.libraryView != libraryViewAlbumTracks {
		return false
	}
	a.setLibraryView(libraryViewAlbums)
	return true
}

func (a *App) backToPlaylistList() bool {
	if a.libraryView != libraryViewPlaylistTracks {
		return false
	}
	a.setLibraryView(libraryViewPlaylists)
	return true
}

func (a *App) selectedTrackIndex() int {
	if item, ok := a.trackList.SelectedItem().(trackItem); ok {
		return a.trackIndex(item.track)
	}

	idx := a.trackList.Index()
	switch a.libraryView {
	case libraryViewTracks:
		if idx < 0 || idx >= len(a.tracks) {
			return -1
		}
		return idx
	}
	return -1
}

func (a *App) selectCurrentInLibraryView() {
	if a.current < 0 || a.current >= len(a.tracks) {
		return
	}
	switch a.libraryView {
	case libraryViewTracks:
		if a.trackList.IsFiltered() {
			currentTrack := a.tracks[a.current]
			for i, item := range a.trackList.VisibleItems() {
				if track, ok := item.(trackItem); ok && track.track == currentTrack {
					a.trackList.Select(i)
					return
				}
			}
			return
		}
		a.trackList.Select(a.current)
	case libraryViewAlbumTracks:
		if a.currentAlbum < 0 || a.currentAlbum >= len(a.albums) {
			return
		}
		currentTrack := a.tracks[a.current]
		for i, track := range a.albums[a.currentAlbum].Tracks {
			if track == currentTrack {
				a.trackList.Select(i)
				return
			}
		}
	case libraryViewPlaylistTracks:
		playlist, ok := a.currentPlaylistValue()
		if !ok {
			return
		}
		currentTrack := a.tracks[a.current]
		for i, track := range a.playlistTracks(playlist) {
			if track == currentTrack {
				a.trackList.Select(i)
				return
			}
		}
	}
}

// --- playback commands ---

func (a *App) playTrackAt(idx int) tea.Cmd {
	fl := a.log.WithFunc("playTrackAt")
	if idx < 0 || idx >= len(a.tracks) {
		fl.Debug("invalid index", "idx", idx, "tracks", len(a.tracks))
		return nil
	}
	a.current = idx
	a.selectCurrentInLibraryView()
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

func (a *App) playbackRepeat() string {
	switch a.options.PlaybackRepeat {
	case "none", "one", "list":
		return a.options.PlaybackRepeat
	default:
		return "list"
	}
}

func (a *App) trackIndex(track *library.Track) int {
	for i, candidate := range a.tracks {
		if candidate == track {
			return i
		}
	}
	return -1
}

func (a *App) setQueue(source queueSource, tracks []*library.Track) {
	a.queueSrc = source
	a.queue = append(a.queue[:0], tracks...)
}

func (a *App) setQueueForCurrentSelection() {
	if a.libraryView == libraryViewTracks && a.trackList.IsFiltered() {
		filtered := a.filteredTrackQueue()
		if len(filtered) > 0 {
			a.setQueue(queueSourceFiltered, filtered)
			return
		}
	}
	switch a.libraryView {
	case libraryViewAlbumTracks:
		if a.currentAlbum >= 0 && a.currentAlbum < len(a.albums) {
			a.setQueue(queueSourceAlbum, a.albums[a.currentAlbum].Tracks)
			return
		}
	case libraryViewPlaylistTracks:
		if current, ok := a.currentPlaylistValue(); ok {
			a.setQueue(queueSourcePlaylist, a.playlistTracks(current))
			return
		}
	}
	a.setQueue(queueSourceAll, a.tracks)
}

func (a *App) filteredTrackQueue() []*library.Track {
	items := a.trackList.VisibleItems()
	out := make([]*library.Track, 0, len(items))
	for _, item := range items {
		if track, ok := item.(trackItem); ok && track.track != nil {
			out = append(out, track.track)
		}
	}
	return out
}

func (a *App) queueSourceLabel() string {
	switch a.queueSrc {
	case queueSourceAlbum:
		return "album"
	case queueSourceFiltered:
		return "filtered"
	case queueSourcePlaylist:
		return "playlist"
	case queueSourceAll:
		return "all"
	default:
		return "none"
	}
}

func (a *App) queuePosition() int {
	if len(a.queue) == 0 || a.current < 0 || a.current >= len(a.tracks) {
		return 0
	}
	currentTrack := a.tracks[a.current]
	for i, track := range a.queue {
		if track == currentTrack {
			return i + 1
		}
	}
	return 0
}

func (a *App) playbackScope() []*library.Track {
	if len(a.queue) > 0 {
		return a.queue
	}
	if a.libraryView == libraryViewAlbumTracks &&
		a.currentAlbum >= 0 &&
		a.currentAlbum < len(a.albums) &&
		a.current >= 0 &&
		a.current < len(a.tracks) {
		currentTrack := a.tracks[a.current]
		albumTracks := a.albums[a.currentAlbum].Tracks
		for _, track := range albumTracks {
			if track == currentTrack {
				return albumTracks
			}
		}
	}
	return a.tracks
}

func (a *App) playbackScopeWithCurrent() ([]*library.Track, int) {
	scope := a.playbackScope()
	if len(scope) == 0 {
		return scope, -1
	}
	scopeIdx := -1
	if a.current >= 0 && a.current < len(a.tracks) {
		currentTrack := a.tracks[a.current]
		for i, track := range scope {
			if track == currentTrack {
				scopeIdx = i
				break
			}
		}
	}
	return scope, scopeIdx
}

func (a *App) nextTrackIndex(autoAdvance bool) int {
	scope, scopeIdx := a.playbackScopeWithCurrent()
	if len(scope) == 0 {
		return -1
	}
	if scopeIdx < 0 {
		return a.trackIndex(scope[0])
	}
	if autoAdvance {
		switch a.playbackRepeat() {
		case "one":
			return a.current
		case "none":
			if scopeIdx >= len(scope)-1 {
				return -1
			}
		}
	}
	if a.options.PlaybackShuffle && len(scope) > 1 {
		next := rand.N(len(scope) - 1)
		if next >= scopeIdx {
			next++
		}
		return a.trackIndex(scope[next])
	}
	return a.trackIndex(scope[(scopeIdx+1)%len(scope)])
}

func (a *App) prevTrackIndex() int {
	scope, scopeIdx := a.playbackScopeWithCurrent()
	if len(scope) == 0 {
		return -1
	}
	if scopeIdx < 0 {
		return a.trackIndex(scope[0])
	}
	return a.trackIndex(scope[(scopeIdx-1+len(scope))%len(scope)])
}

func (a *App) trackEndedNaturally() bool {
	return a.wasPlaying &&
		a.state == audio.StateStopped &&
		a.engineErr == nil &&
		(a.dur == 0 || a.pos >= a.dur)
}

func (a *App) autoAdvanceAfterEnd() tea.Cmd {
	fl := a.log.WithFunc("autoAdvanceAfterEnd")
	nextIdx := a.nextTrackIndex(true)
	if nextIdx < 0 {
		fl.Debug("playback ended, no auto-advance", "current", a.current, "repeat", a.playbackRepeat())
		a.wasPlaying = false
		return nil
	}
	fl.Debug("playback ended, auto-advancing", "current", a.current, "next", nextIdx, "repeat", a.playbackRepeat(), "shuffle", a.options.PlaybackShuffle)
	a.wasPlaying = false
	return a.playTrackAt(nextIdx)
}

func (a *App) playSelected() tea.Cmd {
	fl := a.log.WithFunc("playSelected")
	if a.libraryView == libraryViewAlbums {
		if a.enterAlbum() {
			a.resizeComponents()
		}
		return nil
	}
	idx := a.selectedTrackIndex()
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
	a.setQueueForCurrentSelection()
	return a.playTrackAt(idx)
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

func (a *App) handleDBusCommand(command mpris.Command) tea.Cmd {
	switch command.Kind {
	case mpris.CmdNext:
		return a.nextTrack()
	case mpris.CmdPrevious:
		return a.prevTrack()
	case mpris.CmdPlay:
		switch a.state {
		case audio.StatePaused:
			if err := a.engine.Resume(); err != nil {
				return func() tea.Msg { return errMsg{err: err} }
			}
			return nil
		case audio.StateStopped:
			return a.playSelected()
		default:
			return nil
		}
	case mpris.CmdPause:
		if a.state == audio.StatePlaying {
			if err := a.engine.Pause(); err != nil {
				return func() tea.Msg { return errMsg{err: err} }
			}
		}
		return nil
	case mpris.CmdPlayPause:
		return a.togglePlayPause()
	case mpris.CmdStop:
		a.engine.Stop()
		a.wasPlaying = false
		return nil
	case mpris.CmdSeek:
		return a.seekRelative(int(command.OffsetUS / 1000))
	case mpris.CmdSetPosition:
		if !a.dbusSetPositionTrackMatches(command.TrackID) {
			return nil
		}
		return a.seekTo(int(command.PositionUS / 1000))
	default:
		return nil
	}
}

func (a *App) dbusSetPositionTrackMatches(trackID string) bool {
	return trackID == "" || trackID == string(mpris.TrackID(a.MPRISSnapshot()))
}

func (a *App) nextTrack() tea.Cmd {
	fl := a.log.WithFunc("nextTrack")
	if len(a.tracks) == 0 {
		return nil
	}
	prevIdx := a.current
	nextIdx := a.nextTrackIndex(false)
	if nextIdx < 0 {
		fl.Debug("no next track", "prevIdx", prevIdx)
		return nil
	}
	fl.Debug("next track", "prevIdx", prevIdx, "newIdx", nextIdx)
	return a.playTrackAt(nextIdx)
}

func (a *App) prevTrack() tea.Cmd {
	fl := a.log.WithFunc("prevTrack")
	if len(a.tracks) == 0 {
		return nil
	}
	prevIdx := a.current
	nextIdx := a.prevTrackIndex()
	if nextIdx < 0 {
		fl.Debug("no previous track", "prevIdx", prevIdx)
		return nil
	}
	fl.Debug("prev track", "prevIdx", prevIdx, "newIdx", nextIdx)
	return a.playTrackAt(nextIdx)
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

func (a *App) toggleRepeat() {
	switch a.playbackRepeat() {
	case "list":
		a.options.PlaybackRepeat = "one"
	case "one":
		a.options.PlaybackRepeat = "none"
	default:
		a.options.PlaybackRepeat = "list"
	}
}

func (a *App) toggleShuffle() {
	a.options.PlaybackShuffle = !a.options.PlaybackShuffle
}

func (a *App) toggleLyricAlign() {
	switch a.lyricAlign {
	case lyricAlignLeft:
		a.lyricAlign = lyricAlignCenter
	case lyricAlignCenter:
		a.lyricAlign = lyricAlignRight
	default:
		a.lyricAlign = lyricAlignLeft
	}
}

func (a *App) toggleLyricHighlight() {
	if a.lyricHighlight == lyricHighlightPlayed {
		a.lyricHighlight = lyricHighlightCurrent
		return
	}
	a.lyricHighlight = lyricHighlightPlayed
}

func lyricAlignFromString(s string) lyricAlignMode {
	switch s {
	case "center":
		return lyricAlignCenter
	case "right":
		return lyricAlignRight
	default:
		return lyricAlignLeft
	}
}

func lyricHighlightFromString(s string) lyricHighlightMode {
	if s == "current" {
		return lyricHighlightCurrent
	}
	return lyricHighlightPlayed
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

// kittyCoverFingerprint returns a lightweight string that captures all state
// affecting the kitty cover overlay. When this string is unchanged, the
// expensive renderKittyCoverOverlay() (PNG encode + base64) can be skipped.
func (a *App) kittyCoverFingerprint() string {
	return fmt.Sprintf("%d|%d|%t|%p|%d|%d|%d|%d",
		a.leftContent,
		a.coverScale,
		a.spectrumEnabled,
		a.coverImage,
		a.cellPixelW,
		a.cellPixelH,
		a.leftPaneWidth(),
		a.bodyHeight(),
	)
}

func (a *App) kittyCoverCmd() tea.Cmd {
	// Fast path: if nothing affecting the kitty overlay has changed since
	// the last render, skip the expensive renderKittyCoverOverlay() entirely.
	// This prevents PNG encode + base64 from running every 50ms tick, which
	// blocked the event loop and made all keys feel laggy.
	fp := a.kittyCoverFingerprint()
	if fp == a.lastKittyFingerprint {
		return nil
	}

	seq := a.renderKittyCoverOverlay()
	a.lastKittyFingerprint = fp
	if seq == "" {
		return nil
	}
	isDraw := strings.Contains(seq, "\x1b_Ga=T")
	if isDraw && a.kittyCoverDrawn && seq == a.lastKittyCover {
		return nil
	}
	if !isDraw && !a.kittyCoverDrawn && seq == a.lastKittyCover {
		return nil
	}
	a.lastKittyCover = seq
	a.kittyCoverDrawn = isDraw
	return tea.Raw(seq)
}

const (
	kittyProgressImageA = 3
	kittyProgressImageB = 4
)

func (a *App) kittyProgressEnabled() bool {
	return a.usesKittyProgressOverlay() && a.width > 0 && a.height > 0
}

func (a *App) usesKittyProgressOverlay() bool {
	return a.coverProtocol == cover.ProtocolKitty && a.usesSeparatorProgress()
}

func (a *App) kittyProgressCmd() tea.Cmd {
	if !a.kittyProgressEnabled() {
		return a.resetKittyProgressCmd()
	}
	cellW := a.cellPixelW
	if cellW <= 0 {
		cellW = 10
	}
	pixelWidth := a.width * cellW
	playedPixels := int(a.playbackPercent() * float64(pixelWidth))
	if playedPixels == a.lastKittyProgressPx {
		return nil
	}
	nextID := kittyProgressImageA
	if a.kittyProgressImageID == kittyProgressImageA {
		nextID = kittyProgressImageB
	}
	y := 2 + a.bodyHeight() + 1 // top bar, body, then 1-based separator row.
	seq, err := cover.RenderKittyGradientProgressLine(
		nextID, 1, y, a.width, a.cellPixelW, a.cellPixelH, playedPixels,
		a.options.SeparatorProgressThickness,
		a.theme.ProgressGradient, a.theme.Muted,
	)
	if err != nil {
		a.log.WithFunc("kittyProgressCmd").Warn("kitty progress render failed", "err", err)
		return nil
	}
	if a.kittyProgressImageID != 0 {
		seq += cover.ClearKittyImage(a.kittyProgressImageID)
	}
	a.kittyProgressImageID = nextID
	a.lastKittyProgressPx = playedPixels
	return tea.Raw(seq)
}

func (a *App) resetKittyProgressCmd() tea.Cmd {
	seq := a.invalidateKittyProgress()
	if seq == "" {
		return nil
	}
	return tea.Raw(seq)
}

func (a *App) invalidateKittyProgress() string {
	if a.kittyProgressImageID == 0 {
		a.lastKittyProgressPx = -1
		return ""
	}
	// Either alternating ID can survive an interrupted raw write. Clear both
	// IDs whenever the overlay is reset so no stale line remains after a view
	// switch or resize.
	seq := cover.ClearKittyImage(kittyProgressImageA) + cover.ClearKittyImage(kittyProgressImageB)
	a.kittyProgressImageID = 0
	a.lastKittyProgressPx = -1
	return seq
}

func (a *App) clearScreenAndKittyCoverCmd() tea.Cmd {
	a.lastKittyCover = ""
	a.kittyCoverDrawn = false
	a.lastKittyFingerprint = ""
	if a.coverProtocol == cover.ProtocolKitty {
		// ClearScreen erases kitty virtual placements. Refresh overlays in a
		// deterministic z-order instead: cover first, then the progress line.
		// The separator geometry is unchanged by v/c, so keep its placement live.
		return tea.Sequence(a.kittyCoverCmd(), a.kittyProgressCmd())
	}
	return tea.Sequence(func() tea.Msg { return tea.ClearScreen() }, a.kittyCoverCmd())
}

// applyTheme replaces every style derived from the palette while preserving
// playback and layout state. Image overlays are invalidated by the caller.
func (a *App) applyTheme(next *theme.Theme) {
	a.theme = next
	a.styles = NewStyles(next)
	a.delegate = newListDelegate(next)
	a.trackList.SetDelegate(a.delegate)
	a.trackList.Styles = newListComponentStyles(next)
	a.progress = newProgressBar(next)
	a.resizeComponents()
}

func (a *App) themeRedrawCmd(clearProgress tea.Cmd) tea.Cmd {
	a.lastKittyCover = ""
	a.kittyCoverDrawn = false
	a.lastKittyFingerprint = ""
	if a.coverProtocol == cover.ProtocolKitty {
		return tea.Sequence(clearProgress, a.kittyCoverCmd(), a.kittyProgressCmd())
	}
	return tea.Sequence(clearProgress, func() tea.Msg { return tea.ClearScreen() })
}

func (a *App) lyricChangeCmd() tea.Cmd {
	if a.coverProtocol == cover.ProtocolKitty {
		return a.kittyCoverCmd()
	}
	return a.clearScreenAndKittyCoverCmd()
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
	a.coverURL = ""
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
	coverURL, err := a.cacheCoverURL(path, img)
	if err != nil {
		fl.Debug("cover cache skipped", "path", path, "err", err)
	} else {
		a.coverURL = coverURL
	}
	fl.Info("cover loaded", "path", path, "bounds", img.Bounds().String())
}

func (a *App) cacheCoverURL(audioPath string, img image.Image) (string, error) {
	if audioPath == "" || img == nil {
		return "", nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil || cacheDir == "" {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}
	dir := filepath.Join(cacheDir, "musicli", "covers")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create cover cache dir: %w", err)
	}
	name := fmt.Sprintf("%08x-%08x.png", fnv32a(audioPath), imageHash(img))
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("create cached cover: %w", err)
	}
	encodeErr := png.Encode(f, img)
	closeErr := f.Close()
	if encodeErr != nil {
		return "", fmt.Errorf("encode cached cover: %w", encodeErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close cached cover: %w", closeErr)
	}
	u := url.URL{Scheme: "file", Path: path}
	return u.String(), nil
}

func fnv32a(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

func imageHash(img image.Image) uint32 {
	if img == nil {
		return 0
	}
	bounds := img.Bounds()
	h := fnv32a(bounds.String())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			for _, v := range []uint32{uint32(r), uint32(g), uint32(b), uint32(a)} {
				h ^= v
				h *= 16777619
			}
		}
	}
	return h
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
	const topBarH = 2 // 1 content + bottom border
	h := a.height - topBarH - a.playerBarHeight()
	if h < 1 {
		h = 1
	}
	return h
}

func (a *App) usesSeparatorProgress() bool {
	return a.options.ProgressStyle == "separator"
}

func (a *App) playerBarHeight() int {
	if a.usesSeparatorProgress() {
		return 3 // progress separator + status + help
	}
	return 4 // border + status + progress + help
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

	// Keep title styles inline-only. Filter highlighting renders titles in
	// multiple styled chunks, and fixed-width title chunks insert padding
	// between the chunks.
	s := newListStyles(a.theme)
	s.NormalTitle = s.NormalTitle.Inline(true)
	s.NormalDesc = s.NormalDesc.Inline(true)
	s.SelectedTitle = s.SelectedTitle.Inline(true)
	s.SelectedDesc = s.SelectedDesc.Inline(true)
	s.DimmedTitle = s.DimmedTitle.Inline(true)
	s.DimmedDesc = s.DimmedDesc.Inline(true)
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

	topBar := fitBlock(a.renderTopBar(), a.width, 2)

	rightPaneW := rightW
	if rightPaneW < 1 {
		rightPaneW = 1
	}

	rightPaneContent := a.trackList.View()
	if a.creatingPlaylist {
		rightPaneContent = lipgloss.Place(rightPaneW, bodyH, lipgloss.Center, lipgloss.Center, a.playlistNameInput.View())
	}
	rightPane := fitBlock(rightPaneContent, rightPaneW, bodyH)

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
	body = fitBlock(body, a.width, bodyH)

	bar := fitBlock(a.renderPlayerBar(), a.width, a.playerBarHeight())

	full := lipgloss.JoinVertical(lipgloss.Left, topBar, body, bar)
	full = fitBlock(full, a.width, a.height)

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
	if a.spectrumEnabled {
		return a.renderLeftPaneWithSpectrum(contentW, contentH)
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

const spectrumMinWidth = 8
const spectrumMinHeight = 3

type spectrumPaneLayout struct {
	visible              bool
	coverX, coverY       int
	coverW, coverH       int
	lyricsX, lyricsY     int
	lyricsW, lyricsH     int
	spectrumX, spectrumY int
	spectrumW, spectrumH int
}

// spectrumLayout owns all left-pane geometry. Each rectangle is disjoint so
// a resize cannot place the cover, lyrics, and spectrum on top of one another.
func (a *App) spectrumLayout(w, h int) spectrumPaneLayout {
	if !a.spectrumEnabled || w < spectrumMinWidth || h < spectrumMinHeight {
		return spectrumPaneLayout{}
	}
	l := spectrumPaneLayout{visible: true}
	switch a.leftContent {
	case leftContentLyrics:
		if w < 2*spectrumMinWidth+1 {
			return spectrumPaneLayout{}
		}
		l.spectrumW = w / 2
		l.spectrumH = h
		l.lyricsX = l.spectrumW + 1
		l.lyricsW = w - l.lyricsX
		l.lyricsH = h
	case leftContentCover:
		l.coverW = w
		l.coverH = a.coverHeightAboveSpectrum(w, h)
		l.spectrumY, l.spectrumW, l.spectrumH = l.coverH, w, h-l.coverH
	default:
		if a.options.DisableCover || w < 2*spectrumMinWidth+1 {
			return spectrumPaneLayout{}
		}
		coverW := w / 2
		l.coverW = coverW
		l.coverH = a.coverHeightAboveSpectrum(coverW, h)
		l.spectrumY, l.spectrumW, l.spectrumH = l.coverH, coverW, h-l.coverH
		l.lyricsX, l.lyricsW, l.lyricsH = coverW+1, w-coverW-1, h
	}
	if l.spectrumW < spectrumMinWidth || l.spectrumH < spectrumMinHeight {
		return spectrumPaneLayout{}
	}
	return l
}

func (a *App) coverHeightAboveSpectrum(width, height int) int {
	if a.coverScale == coverScaleStretch || a.coverImage == nil {
		return max(1, height-spectrumMinHeight)
	}
	_, drawH := cover.DrawSize(a.coverImage.Bounds(), width, height, a.coverScaleMode(), a.cellPixelW, a.cellPixelH)
	// A fit cover owns exactly its visible image rows. The remaining rows
	// belong to the spectrum, which starts at the cover's actual bottom edge.
	return min(max(1, drawH), height)
}

func (a *App) renderLeftPaneWithSpectrum(w, h int) string {
	l := a.spectrumLayout(w, h)
	if !l.visible {
		return a.renderLeftPaneWithoutSpectrum(w, h)
	}
	if a.leftContent == leftContentLyrics {
		return fitBlock(lipgloss.JoinHorizontal(lipgloss.Top,
			fitBlock(a.renderSpectrumPane(l.spectrumW, l.spectrumH), l.spectrumW, l.spectrumH),
			fitBlock("", 1, h),
			fitBlock(a.renderLyricsOrPlaceholder(l.lyricsW, l.lyricsH), l.lyricsW, l.lyricsH),
		), w, h)
	}
	cover := fitBlock(a.renderCoverPane(l.coverW, l.coverH), l.coverW, l.coverH)
	spectrum := fitBlock(a.renderSpectrumPane(l.spectrumW, l.spectrumH), l.spectrumW, l.spectrumH)
	left := fitBlock(lipgloss.JoinVertical(lipgloss.Left, cover, spectrum), l.coverW, h)
	if a.leftContent == leftContentCover {
		return fitBlock(left, w, h)
	}
	return fitBlock(lipgloss.JoinHorizontal(lipgloss.Top, left, fitBlock("", 1, h), fitBlock(a.renderLyricsOrPlaceholder(l.lyricsW, l.lyricsH), l.lyricsW, l.lyricsH)), w, h)
}

func (a *App) renderLeftPaneWithoutSpectrum(w, h int) string {
	switch a.leftContent {
	case leftContentCover:
		return a.renderCoverPane(w, h)
	case leftContentLyrics:
		return a.renderLyricsOrPlaceholder(w, h)
	default:
		return a.renderCoverAndLyricsPane(w, h)
	}
}

func (a *App) renderSpectrumPane(w, h int) string {
	if w < 1 || h < 1 {
		return ""
	}
	levels := make([]float64, w*2)
	if a.engine != nil {
		levels = a.engine.SpectrumLevels(w * 2)
	}
	return a.renderSpectrumLevels(levels, w, h)
}

// renderSpectrumLevels packs two thin bars into each Braille cell. Color is
// selected by row, producing a vertical gradient independent of a column's
// current intensity.
func (a *App) renderSpectrumLevels(levels []float64, w, h int) string {
	if w < 1 || h < 1 {
		return ""
	}
	rows := make([]string, h)
	for y := range rows {
		var b strings.Builder
		style := a.spectrumStyleForRow(y, h)
		for x := 0; x < w; x++ {
			left, right := spectrumBrailleDots(levelAt(levels, x*2), levelAt(levels, x*2+1), y, h)
			if left|right == 0 {
				b.WriteByte(' ')
				continue
			}
			b.WriteString(style.Render(string(rune(0x2800 + left + right))))
		}
		rows[y] = fitLine(b.String(), w)
	}
	return strings.Join(rows, "\n")
}

func (a *App) spectrumStyleForRow(row, height int) lipgloss.Style {
	position := 1.0
	if height > 1 {
		position = 1 - float64(row)/float64(height-1)
	}
	return lipgloss.NewStyle().Foreground(theme.GradientAt(a.theme.SpectrumGradient, position))
}

func levelAt(levels []float64, index int) float64 {
	if index < 0 || index >= len(levels) {
		return 0
	}
	return levels[index]
}

func spectrumBrailleDots(left, right float64, row, height int) (int, int) {
	// Braille has four vertical dots per column. Each terminal row represents
	// four subrows, giving thin columns four times the former vertical detail.
	const leftBits = 0x01 | 0x02 | 0x04 | 0x40
	const rightBits = 0x08 | 0x10 | 0x20 | 0x80
	filled := func(level float64, bits [4]int) int {
		barDots := int(math.Round(level * float64(height*4)))
		result := 0
		for dot := 0; dot < 4; dot++ {
			global := (height-row-1)*4 + dot
			if global < barDots {
				result |= bits[dot]
			}
		}
		return result
	}
	return filled(left, [4]int{0x01, 0x02, 0x04, 0x40}) & leftBits,
		filled(right, [4]int{0x08, 0x10, 0x20, 0x80}) & rightBits
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
	return fitBlock(cover.RenderHalfBlockWithScale(a.coverImage, w, h, a.coverScaleMode(), a.cellPixelW, a.cellPixelH), w, h)
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
	topAlign := false
	if a.leftContent == leftContentBoth && w >= 12 {
		coverW = w / 2
	}
	if a.spectrumEnabled {
		layout := a.spectrumLayout(w, h)
		if layout.visible && a.leftContent != leftContentLyrics {
			coverW = layout.coverW
			h = layout.coverH
			topAlign = true
		}
	}
	if coverW < 1 {
		return cover.ClearKittyImage(kittyImageID)
	}

	seq, err := cover.RenderKitty(a.coverImage, cover.KittyPlacement{
		ID:       kittyImageID,
		X:        x,
		Y:        y,
		Width:    coverW,
		Height:   h,
		Scale:    a.coverScaleMode(),
		CellW:    a.cellPixelW,
		CellH:    a.cellPixelH,
		TopAlign: topAlign,
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
				rendered = append(rendered, a.alignLyricLine(a.renderCurrentLyricLine(a.lyric.Lines[visual.lineIdx], w), w))
			} else {
				text := truncateCellText(a.lyric.Lines[visual.lineIdx].Text, w)
				rendered = append(rendered, a.alignLyricLine(a.styles.muted.Render(text), w))
			}
		case lyricRowTranslation:
			text := truncateCellText(visual.text, w)
			rendered = append(rendered, a.alignLyricLine(a.styles.muted.Render(text), w))
		default:
			rendered = append(rendered, strings.Repeat(" ", max(0, w)))
		}
	}
	return fitBlock(strings.Join(rendered, "\n"), w, h)
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

	current := a.lyricHighlightWordIndex(line)
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
	if a.lyricHighlight == lyricHighlightPlayed {
		if writeRun(accent, prefix+active) {
			b.WriteString(ansi.ResetStyle)
			return padCellText(b.String(), width)
		}
	} else {
		if writeRun(muted, prefix) {
			b.WriteString(ansi.ResetStyle)
			return padCellText(b.String(), width)
		}
		if writeRun(accent, active) {
			b.WriteString(ansi.ResetStyle)
			return padCellText(b.String(), width)
		}
	}
	writeRun(muted, suffix)
	b.WriteString(ansi.ResetStyle)
	return padCellText(b.String(), width)
}

func (a *App) alignLyricLine(line string, width int) string {
	if a.lyricAlign != lyricAlignLeft {
		line = strings.TrimRight(line, " ")
	}
	if ansi.StringWidth(line) == width {
		return fitLine(line, width)
	}
	return alignCellText(line, width, a.lyricAlign)
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
		if line.StartMs <= a.pos && isLineInFinalWordGrace(line, a.pos) {
			return i
		}
	}
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
	return lyricRenderState{
		line: lineIdx,
		word: a.lyricHighlightWordIndex(a.lyric.Lines[lineIdx]),
	}
}

// lyricHighlightWordIndex returns the word used for the current render mode.
// In played mode, a timing gap keeps the most recently started word lit.
func (a *App) lyricHighlightWordIndex(line lyrics.Line) int {
	lastStarted := -1
	for i, word := range line.Words {
		if word.StartMs > a.pos {
			break
		}
		lastStarted = i
		if wordActiveAt(word, i == len(line.Words)-1, a.pos) {
			return i
		}
	}
	if a.lyricHighlight == lyricHighlightPlayed {
		return lastStarted
	}
	return -1
}

func wordActiveAt(word lyrics.Word, final bool, pos int) bool {
	if word.StartMs > pos {
		return false
	}
	end := word.EndMs
	if final {
		end += lyricFinalWordGraceMs
	}
	return pos < end
}

func isLineInFinalWordGrace(line lyrics.Line, pos int) bool {
	if len(line.Words) == 0 {
		return false
	}
	last := line.Words[len(line.Words)-1]
	return last.EndMs <= pos && pos < last.EndMs+lyricFinalWordGraceMs
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

func fitLine(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) > width {
		s = ansi.Truncate(s, width, "")
	}
	return padCellText(s, width)
}

func bestFitLine(candidates []string, width int) string {
	if width <= 0 {
		return ""
	}
	for _, candidate := range candidates {
		if ansi.StringWidth(candidate) <= width {
			return padCellText(candidate, width)
		}
	}
	if len(candidates) == 0 {
		return strings.Repeat(" ", width)
	}
	return fitLine(candidates[len(candidates)-1], width)
}

func alignCellText(s string, width int, align lyricAlignMode) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) > width {
		s = ansi.Truncate(s, width, "")
	}
	pad := width - ansi.StringWidth(s)
	if pad <= 0 {
		return s
	}
	switch align {
	case lyricAlignCenter:
		left := pad / 2
		return strings.Repeat(" ", left) + s + strings.Repeat(" ", pad-left)
	case lyricAlignRight:
		return strings.Repeat(" ", pad) + s
	default:
		return s + strings.Repeat(" ", pad)
	}
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
	style := a.styles.player
	if w <= a.styles.player.GetHorizontalFrameSize() {
		style = style.Padding(0, 0)
	}
	contentW := w - style.GetHorizontalFrameSize()
	if contentW < 1 {
		contentW = 1
	}

	if a.usesSeparatorProgress() {
		content := strings.Join([]string{
			a.renderPlayerStatusLine(contentW),
			a.renderPlayerHelpLine(contentW),
		}, "\n")
		style = style.BorderTop(false)
		return a.renderSeparatorProgress(w) + "\n" + style.Width(w).Render(content)
	}

	content := strings.Join([]string{
		a.renderPlayerStatusLine(contentW),
		a.renderProgressBar(contentW),
		a.renderPlayerHelpLine(contentW),
	}, "\n")
	return style.Width(w).Render(content)
}

func (a *App) renderSeparatorProgress(width int) string {
	if width < 1 {
		return ""
	}
	if a.usesKittyProgressOverlay() {
		// The kitty overlay owns the complete line. Text underneath would mix
		// font strokes with the one-pixel image whenever progress crosses a cell.
		return strings.Repeat(" ", width)
	}
	played := int(a.playbackPercent() * float64(width))
	if played < 0 {
		played = 0
	}
	if played > width {
		played = width
	}
	var b strings.Builder
	for i := 0; i < played; i++ {
		position := 0.0
		if width > 1 {
			position = float64(i) / float64(width-1)
		}
		b.WriteString(lipgloss.NewStyle().Foreground(theme.GradientAt(a.theme.ProgressGradient, position)).Render("─"))
	}
	b.WriteString(a.styles.muted.Render(strings.Repeat("─", width-played)))
	return b.String()
}

func (a *App) playerStateIcon() string {
	icon := "▶"
	switch a.state {
	case audio.StatePlaying:
		icon = "▶"
	case audio.StatePaused:
		icon = "⏸"
	case audio.StateStopped:
		icon = "⏹"
	}
	return icon
}

func (a *App) playbackPercent() float64 {
	if a.dur <= 0 {
		return 0
	}
	percent := float64(a.pos) / float64(a.dur)
	if percent < 0 {
		return 0
	}
	if percent > 1 {
		return 1
	}
	return percent
}

func (a *App) renderProgressBar(width int) string {
	if width < 1 {
		width = 1
	}
	p := a.progress
	p.SetWidth(width)
	return fitLine(p.ViewAs(a.playbackPercent()), width)
}

func (a *App) renderPlayerStatusLine(width int) string {
	if width < 1 {
		width = 1
	}
	timeStr := fmt.Sprintf("%s / %s", fmtDuration(time.Duration(a.pos)*time.Millisecond),
		fmtDuration(time.Duration(a.dur)*time.Millisecond))
	compactTimeStr := fmt.Sprintf("%s/%s", fmtDuration(time.Duration(a.pos)*time.Millisecond),
		fmtDuration(time.Duration(a.dur)*time.Millisecond))
	shuffle := "off"
	if a.options.PlaybackShuffle {
		shuffle = "on"
	}
	if a.errMsg != "" {
		return fitLine(fmt.Sprintf("%s  %s  ! %s", a.playerStateIcon(), compactTimeStr, a.errMsg), width)
	}
	queueLong := a.queueStatusText(false)
	queueShort := a.queueStatusText(true)

	candidates := []string{
		fmt.Sprintf("%s  %s  vol %d%%  speed %.1fx  repeat %s  shuffle %s  %s",
			a.playerStateIcon(), timeStr, a.volume, a.speed, a.playbackRepeat(), shuffle, queueLong),
		fmt.Sprintf("%s  %s  v%d  x%.1f  %s  shuf %s",
			a.playerStateIcon(), compactTimeStr, a.volume, a.speed, a.playbackRepeat(), shuffle),
		fmt.Sprintf("%s  %s  v%d  %s  %s",
			a.playerStateIcon(), compactTimeStr, a.volume, a.playbackRepeat(), queueShort),
		fmt.Sprintf("%s  %s  v%d  %s",
			a.playerStateIcon(), compactTimeStr, a.volume, a.playbackRepeat()),
		fmt.Sprintf("%s  %s", a.playerStateIcon(), compactTimeStr),
		a.playerStateIcon(),
	}
	return bestFitLine(candidates, width)
}

func (a *App) queueStatusText(short bool) string {
	pos := a.queuePosition()
	total := len(a.queue)
	if pos <= 0 || total <= 0 {
		return "queue none"
	}
	if short {
		return fmt.Sprintf("q %s %d/%d", a.queueSourceLabel(), pos, total)
	}
	return fmt.Sprintf("queue %s %d/%d", a.queueSourceLabel(), pos, total)
}

func (a *App) renderPlayerHelpLine(width int) string {
	if width < 1 {
		width = 1
	}
	candidates := []string{
		"q quit  ⏎ play  ␣ pause  n/b next/prev  p playlists  f favorite  m add  r repeat  s shuffle  a align  h highlight  z spectrum  v view  c scale  ←→ seek  / filter",
		"q quit  ⏎ play  ␣ pause  n/b  r repeat  s shuffle  a align  h highlight  z spectrum",
		"q  ⏎  ␣  n/b  r  s  a  h  z",
		"q ⏎ ␣",
		"",
	}
	return bestFitLine(candidates, width)
}

func (a *App) helpLine() string {
	return a.renderPlayerHelpLine(a.width)
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
