// Package audio implements the musicli audio engine.
//
// The engine uses ffmpeg as the decode + resample + atempo (speed, pitch
// preserved) backend, piping signed-16-bit LE 48kHz stereo PCM into an
// ebitengine/oto/v3 player.
//
// Design (see docs/superpowers/specs/2026-07-05-musicli-tui-player-design.md §4.3):
//   - oto Context is created once and reused for every track.
//   - One oto Player per track (and per seek). Old players are discarded;
//     the oto finalizer cleans up the internal mux.Player. This clears the
//     internal buffer naturally without using the deprecated Reset().
//   - Position is tracked client-side (start timestamp + wall clock * speed,
//     minus paused durations). ffmpeg is never queried for position.
//   - Seek = kill ffmpeg + drop player + spawn new ffmpeg at -ss + new player.
//   - A reader goroutine copies ffmpeg stdout into an oto player's io.Reader
//     via a pipe. On seek/stop the old goroutine sees EOF and exits; the
//     caller waits on a done channel before starting a new one (prevents two
//     goroutines writing to oto concurrently = corrupted audio).
//
// The package is pure: it does not import bubbletea. The UI polls
// Position()/State()/Err() via a 30fps tick.
package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/locxl/musicli/internal/log"
)

// PCM format constants — the single source of truth shared by the ffmpeg
// command construction and the oto context init (oracle Risk-C pitfall #4).
const (
	SampleRate     = 48000
	ChannelCount   = 2
	BitDepthInBytes = 2 // signed 16-bit
)

// State is the playback state.
type State int

const (
	StateStopped State = iota
	StatePlaying
	StatePaused
)

func (s State) String() string {
	switch s {
	case StatePlaying:
		return "playing"
	case StatePaused:
		return "paused"
	default:
		return "stopped"
	}
}

// Engine plays audio files via ffmpeg + oto. Concrete type (single
// implementation; no interface needed per oracle Sim-F).
type Engine struct {
	log  *log.Logger
	oto  *oto.Context

	mu sync.Mutex // protects all fields below

	state    State
	err      error          // last async error (ffmpeg crash etc.)
	path     string         // current file path
	duration int            // ms (from ffprobe)
	position int            // ms, last anchor
	speed    float64        // 0.5-2.0
	volume   float64        // 0.0-1.0

	// position tracking
	playStartWall time.Time // wall time when playback (re)started at position
	pausedAccum   time.Duration // total time spent paused since playStartWall

	// current playback goroutine control
	ffmpegCmd   *exec.Cmd
	readerDone  chan struct{} // closed when reader goroutine exits
	cancelReader context.CancelFunc
}

// New creates an Engine. The oto context is created once here and reused
// for the engine's lifetime (oracle Risk-B2: never recreate per track).
func New(ctx context.Context, logger *log.Logger) (*Engine, error) {
	otoCtx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   SampleRate,
		ChannelCount: ChannelCount,
		Format:       oto.FormatSignedInt16LE,
	})
	if err != nil {
		return nil, fmt.Errorf("create oto context: %w", err)
	}
	<-ready
	l := logger.WithModule("audio")
	l.Debug("oto context ready", "sample_rate", SampleRate, "channels", ChannelCount)
	return &Engine{
		log:    l,
		oto:    otoCtx,
		state:  StateStopped,
		speed:  1.0,
		volume: 1.0,
	}, nil
}

// Play starts playback of path from the beginning. Any current playback is
// stopped first (ffmpeg killed, player dropped, goroutine reaped).
func (e *Engine) Play(path string) error {
	e.stopInternal(true) // reap old goroutine + kill ffmpeg
	e.setErr(nil)

	dur, err := probeDuration(path)
	if err != nil {
		e.log.Warn("ffprobe duration failed", "path", path, "err", err)
		dur = 0 // non-fatal: UI shows --:--, seek-by-% disabled
	}

	e.mu.Lock()
	e.path = path
	e.duration = dur
	e.position = 0
	e.playStartWall = time.Now()
	e.pausedAccum = 0
	e.state = StatePlaying
	e.mu.Unlock()

	e.log.Info("play", "path", path, "duration_ms", dur)
	return e.startFFmpeg(0)
}

// Pause pauses playback.
func (e *Engine) Pause() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StatePlaying {
		return nil
	}
	// Freeze position at the current computed value.
	e.position = e.computePositionLocked()
	e.state = StatePaused
	e.log.Debug("pause", "position_ms", e.position)
	return nil
}

// Resume resumes playback from the paused position.
func (e *Engine) Resume() error {
	e.mu.Lock()
	if e.state != StatePaused {
		e.mu.Unlock()
		return nil
	}
	pos := e.position
	e.state = StatePlaying
	e.playStartWall = time.Now()
	e.pausedAccum = 0
	e.mu.Unlock()

	e.log.Debug("resume", "position_ms", pos)
	// Re-spawn ffmpeg at the paused position (oto player was discarded on
	// pause; we restart from the desired offset).
	return e.startFFmpeg(pos)
}

// Seek jumps to positionMs. Kills current ffmpeg, drops player, restarts at -ss.
func (e *Engine) Seek(positionMs int) error {
	e.mu.Lock()
	if e.path == "" {
		e.mu.Unlock()
		return errors.New("no track loaded")
	}
	wasPlaying := e.state == StatePlaying || e.state == StatePaused
	pos := positionMs
	if e.duration > 0 && pos > e.duration {
		pos = e.duration
	}
	if pos < 0 {
		pos = 0
	}
	e.mu.Unlock()

	if !wasPlaying {
		// Just update the anchor; next Resume/Play picks it up.
		e.mu.Lock()
		e.position = pos
		e.mu.Unlock()
		return nil
	}

	// Stop current playback (kill ffmpeg + drop player + reap goroutine),
	// then restart at the new position.
	e.stopInternal(true)
	e.mu.Lock()
	e.position = pos
	e.playStartWall = time.Now()
	e.pausedAccum = 0
	e.state = StatePlaying
	e.mu.Unlock()

	e.log.Debug("seek", "position_ms", pos)
	return e.startFFmpeg(pos)
}

// SetVolume sets volume (0-100, clamped to [0,1] for oto).
func (e *Engine) SetVolume(v int) error {
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	e.mu.Lock()
	e.volume = float64(v) / 100.0
	e.mu.Unlock()
	return nil
}

// SetSpeed sets playback speed (0.5-2.0, pitch preserved via ffmpeg atempo).
// Takes effect on next ffmpeg spawn (seek/restart). Mid-playback speed change
// requires an ffmpeg restart.
func (e *Engine) SetSpeed(s float64) error {
	if s < 0.5 {
		s = 0.5
	}
	if s > 2.0 {
		s = 2.0
	}
	e.mu.Lock()
	old := e.speed
	e.speed = s
	pos := e.computePositionLocked()
	playing := e.state == StatePlaying
	path := e.path
	e.mu.Unlock()

	if old == s {
		return nil
	}
	e.log.Debug("speed", "from", old, "to", s)

	if playing && path != "" {
		// Restart ffmpeg at current position with new atempo.
		e.stopInternal(true)
		e.mu.Lock()
		e.position = pos
		e.playStartWall = time.Now()
		e.pausedAccum = 0
		e.state = StatePlaying
		e.mu.Unlock()
		return e.startFFmpeg(pos)
	}
	return nil
}

// Stop stops playback and clears the current track.
func (e *Engine) Stop() {
	e.stopInternal(true)
	e.mu.Lock()
	e.path = ""
	e.position = 0
	e.duration = 0
	e.state = StateStopped
	e.mu.Unlock()
}

// Position returns the current playback position in ms (client-side computed).
func (e *Engine) Position() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.computePositionLocked()
}

// Duration returns the total duration in ms (0 if unknown).
func (e *Engine) Duration() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.duration
}

// State returns the current playback state.
func (e *Engine) State() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state
}

// Err returns the last async error (e.g. ffmpeg crash). UI polls this.
func (e *Engine) Err() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.err
}

// Path returns the current file path.
func (e *Engine) Path() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.path
}

// Speed returns the current speed.
func (e *Engine) Speed() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.speed
}

// Volume returns the current volume (0-100).
func (e *Engine) Volume() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return int(e.volume * 100)
}

// --- internal ---

// computePositionLocked returns the current position. Caller must hold e.mu.
func (e *Engine) computePositionLocked() int {
	if e.state != StatePlaying {
		return e.position
	}
	elapsed := time.Since(e.playStartWall) - e.pausedAccum
	pos := e.position + int(elapsed.Milliseconds()*int64(e.speed*1000)/1000)
	if e.duration > 0 && pos > e.duration {
		pos = e.duration
	}
	return pos
}

func (e *Engine) setErr(err error) {
	e.mu.Lock()
	e.err = err
	if err != nil {
		e.state = StateStopped
	}
	e.mu.Unlock()
}

// startFFmpeg spawns ffmpeg at the given offset and pipes PCM into a new
// oto player. A reader goroutine copies stdout; it signals completion via
// readerDone. On track end (ffmpeg exits cleanly) the state moves to Stopped.
func (e *Engine) startFFmpeg(offsetMs int) error {
	e.mu.Lock()
	path := e.path
	speed := e.speed
	vol := e.volume
	e.mu.Unlock()

	// Build atempo filter. Single atempo covers 0.5-2.0; chain for wider.
	atempo := fmt.Sprintf("atempo=%.3f", speed)
	filter := atempo
	// (If we ever support >2.0, chain: atempo=2.0,atempo=<remaining>.)

	args := []string{
		"-ss", strconv.FormatFloat(float64(offsetMs)/1000.0, 'f', 3, 64),
		"-i", path,
		"-filter:a", filter,
		"-f", "s16le",
		"-ar", strconv.Itoa(SampleRate),
		"-ac", strconv.Itoa(ChannelCount),
		"-vn", // no video (some files have embedded cover art as video stream)
		"pipe:1",
	}
	cmd := exec.Command("ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		e.log.Error("ffmpeg stdout pipe failed", "path", path, "err", err)
		e.setErr(fmt.Errorf("ffmpeg pipe: %w", err))
		return err
	}
	stderr := &bytesBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		e.log.Error("ffmpeg start failed", "path", path, "err", err)
		e.setErr(fmt.Errorf("ffmpeg start: %w", err))
		return err
	}

	// oto player reads from a pipe we write to. But oto's NewPlayer takes an
	// io.Reader; its internal mux goroutine pulls from that reader. We give
	// it the ffmpeg stdout directly (oto pulls = backpressure on ffmpeg).
	player := e.oto.NewPlayer(stdout)
	player.SetVolume(vol)
	player.Play()

	readerCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	e.mu.Lock()
	e.ffmpegCmd = cmd
	e.readerDone = done
	e.cancelReader = cancel
	e.mu.Unlock()

	go e.readerLoop(readerCtx, cmd, player, stdout, done, path)
	return nil
}

// readerLoop waits for ffmpeg to exit, then finalises state.
//
// oto's mux pulls from stdout asynchronously; we don't copy bytes ourselves.
// We only need to: (1) detect ffmpeg exit, (2) reap the process, (3) handle
// clean end-of-track vs error, (4) signal done so a seek can proceed.
func (e *Engine) readerLoop(ctx context.Context, cmd *exec.Cmd, player *oto.Player, stdout io.ReadCloser, done chan struct{}, path string) {
	defer close(done)
	defer stdout.Close()

	// Wait for ffmpeg process to exit (it exits when stdin EOFs or is killed).
	waitErr := cmd.Wait()

	// If we were cancelled (seek/stop), don't treat the kill as an error.
	select {
	case <-ctx.Done():
		return
	default:
	}

	// Not cancelled: ffmpeg exited on its own.
	if waitErr != nil {
		// Non-zero exit: real error (corrupt file, bad codec, etc.)
		e.log.Error("ffmpeg exited with error", "path", path, "err", waitErr, "stderr", stderrContent(cmd))
		e.setErr(fmt.Errorf("ffmpeg exited: %w", waitErr))
		return
	}

	// Clean exit = track ended naturally.
	e.mu.Lock()
	// Only mark stopped if this is still the active ffmpeg (not a newer one).
	if e.ffmpegCmd == cmd {
		e.state = StateStopped
		e.position = e.duration // clamp to end
	}
	e.mu.Unlock()
	_ = player // oto finalizer cleans up; Close() is deprecated no-op
	e.log.Debug("track ended", "path", path)
}

// stopInternal kills the current ffmpeg (if any), cancels the reader, and
// waits for the goroutine to exit. reap=true waits synchronously; this is
// called on every seek/play/stop to prevent zombie processes (oracle Risk-B)
// and concurrent oto writers (oracle Risk-C #2).
func (e *Engine) stopInternal(reap bool) {
	e.mu.Lock()
	cmd := e.ffmpegCmd
	done := e.readerDone
	cancel := e.cancelReader
	e.ffmpegCmd = nil
	e.readerDone = nil
	e.cancelReader = nil
	e.mu.Unlock()

	if cmd == nil {
		return
	}
	if cancel != nil {
		cancel()
	}
	// SIGTERM first; ffmpeg handles it and exits. Don't SIGKILL immediately
	// or we might leave the pipe half-flushed.
	_ = cmd.Process.Signal(termSignal)
	if reap && done != nil {
		// Wait for reader goroutine to finish (it calls cmd.Wait()).
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			// Goroutine hung; force kill.
			_ = cmd.Process.Signal(killSignal)
			<-done
		}
	}
}
