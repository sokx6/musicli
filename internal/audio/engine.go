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
	"strings"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/locxl/musicli/internal/log"
)

// PCM format constants — the single source of truth shared by the ffmpeg
// command construction and the oto context init (oracle Risk-C pitfall #4).
const (
	SampleRate      = 48000
	ChannelCount    = 2
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
	log *log.Logger
	oto *oto.Context

	mu sync.Mutex // protects all fields below

	state    State
	err      error   // last async error (ffmpeg crash etc.)
	path     string  // current file path
	duration int     // ms (from ffprobe)
	position int     // ms, last anchor
	speed    float64 // 0.5-2.0
	volume   float64 // 0.0-1.0

	// position tracking
	playStartWall time.Time     // wall time when playback (re)started at position
	pausedAccum   time.Duration // total time spent paused since playStartWall

	// current playback goroutine control
	ffmpegCmd     *exec.Cmd
	player        *oto.Player   // current oto player (for immediate pause/mute)
	readerDone    chan struct{} // closed when reader goroutine exits
	cancelReader  context.CancelFunc
	spectrum      *SpectrumAnalyzer
	spectrumEpoch uint64
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
	l := logger.WithModule("audio").WithFunc("New")
	l.Debug("oto context ready", "sample_rate", SampleRate, "channels", ChannelCount, "format", "s16le")
	l.Debug("oto context created", "ready", true)
	spectrum, err := NewSpectrumAnalyzer()
	if err != nil {
		return nil, fmt.Errorf("create spectrum analyzer: %w", err)
	}
	return &Engine{
		log:      l,
		oto:      otoCtx,
		spectrum: spectrum,
		state:    StateStopped,
		speed:    1.0,
		volume:   1.0,
	}, nil
}

// SpectrumLevels returns the latest PCM spectrum for UI display.
func (e *Engine) SpectrumLevels(bands int) []float64 {
	if e == nil || e.spectrum == nil {
		return make([]float64, max(0, bands))
	}
	return e.spectrum.Levels(bands)
}

// Play starts playback of path from the beginning. Any current playback is
// stopped first (ffmpeg killed, player dropped, goroutine reaped).
func (e *Engine) Play(path string) error {
	fl := e.log.WithFunc("Play")
	// Stop old playback without waiting for readerLoop to fully exit
	// (fire-and-forget the cleanup; new playback starts immediately).
	e.stopInternal(false)
	e.setErr(nil)

	e.mu.Lock()
	e.path = path
	e.position = 0
	e.playStartWall = time.Now()
	e.pausedAccum = 0
	e.state = StatePlaying
	e.duration = 0 // reset; probe updates async
	e.mu.Unlock()

	// Probe duration in background — don't block playback start.
	go func() {
		dur, err := probeDuration(path, fl)
		if err != nil {
			fl.Warn("ffprobe duration failed; using 0", "path", path, "err", fmt.Errorf("probeDuration: %w", err))
			return
		}
		e.mu.Lock()
		// Only update if this is still the current track.
		if e.path == path {
			e.duration = dur
		}
		e.mu.Unlock()
		fl.Debug("duration probed", "path", path, "duration_ms", dur)
	}()

	fl.Info("play requested", "path", path)
	return e.startFFmpeg(0)
}

// Pause pauses playback.
// Pause pauses playback. Stops ffmpeg + oto player (audio actually stops).
// Resume will restart ffmpeg from the paused position.
func (e *Engine) Pause() error {
	fl := e.log.WithFunc("Pause")
	e.mu.Lock()
	if e.state != StatePlaying {
		e.mu.Unlock()
		return nil
	}
	e.position = e.computePositionLocked()
	e.state = StatePaused
	e.mu.Unlock()

	// Immediately mute+pause oto player (audio stops instantly) and SIGKILL
	// ffmpeg. Don't wait for readerLoop — the mute makes the remaining oto
	// buffer inaudible, and readerLoop cleans up in the background.
	e.stopInternal(false)
	fl.Debug("paused", "position_ms", e.position)
	return nil
}

// Resume resumes playback from the paused position.
func (e *Engine) Resume() error {
	fl := e.log.WithFunc("Resume")
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

	fl.Debug("resumed", "position_ms", pos)
	// Re-spawn ffmpeg at the paused position (oto player was discarded on
	// pause; we restart from the desired offset).
	return e.startFFmpeg(pos)
}

// Seek jumps to positionMs. Kills current ffmpeg, drops player, restarts at -ss.
func (e *Engine) Seek(positionMs int) error {
	fl := e.log.WithFunc("Seek")
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
		e.mu.Lock()
		e.position = pos
		e.mu.Unlock()
		return nil
	}

	e.stopInternal(true)
	e.mu.Lock()
	e.position = pos
	e.playStartWall = time.Now()
	e.pausedAccum = 0
	e.state = StatePlaying
	e.mu.Unlock()

	fl.Debug("seeked", "position_ms", pos)
	return e.startFFmpeg(pos)
}

// SetVolume sets volume (0-100, clamped to [0,1] for oto).
func (e *Engine) SetVolume(v int) error {
	fl := e.log.WithFunc("SetVolume")
	old := -1
	e.mu.Lock()
	old = int(e.volume * 100)
	e.mu.Unlock()
	orig := v
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	e.mu.Lock()
	e.volume = float64(v) / 100.0
	e.mu.Unlock()
	fl.Debug("volume set", "old", old, "new", v, "clamped", orig != v)
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
	fl := e.log.WithFunc("SetSpeed")
	e.mu.Lock()
	old := e.speed
	e.speed = s
	pos := e.computePositionLocked()
	playing := e.state == StatePlaying
	path := e.path
	e.mu.Unlock()

	if old == s {
		fl.Debug("speed unchanged, skipping restart", "speed", s)
		return nil
	}
	fl.Debug("speed changed", "from", old, "to", s)

	if playing && path != "" {
		fl.Debug("restarting ffmpeg for speed change", "position_ms", pos, "speed", s)
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
	fl.Debug("speed changed without restart (not playing)", "speed", s)
	return nil
}

// Stop stops playback and clears the current track.
func (e *Engine) Stop() {
	fl := e.log.WithFunc("Stop")
	e.stopInternal(true)
	e.mu.Lock()
	e.path = ""
	e.position = 0
	e.duration = 0
	e.state = StateStopped
	e.mu.Unlock()
	fl.Debug("stopped", "path_cleared", true, "state", "stopped")
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
	// ponytail: very verbose position trace — called every UI tick (~100ms)
	e.log.WithFunc("computePositionLocked").Debug("position computed",
		"state", e.state.String(),
		"anchor_position", e.position,
		"playStartWall", e.playStartWall.Format(time.RFC3339Nano),
		"pausedAccum_ms", e.pausedAccum.Milliseconds(),
		"speed", e.speed,
		"result_ms", pos,
	)
	return pos
}

func (e *Engine) setErr(err error) {
	e.mu.Lock()
	oldErr := e.err
	oldState := e.state
	e.err = err
	if err != nil {
		e.state = StateStopped
	}
	newState := e.state
	e.mu.Unlock()
	if err != nil {
		e.log.WithFunc("setErr").Debug("error set, state changed",
			"old_err", oldErr,
			"new_err", err,
			"old_state", oldState.String(),
			"new_state", newState.String(),
		)
	}
}

// startFFmpeg spawns ffmpeg at the given offset and pipes PCM into a new
// oto player via an io.Pipe middleman. The middleman goroutine copies
// ffmpeg stdout → pipe writer; oto's mux reads from pipe reader. This gives
// us full control of the goroutine lifecycle: on stop/seek we close the pipe
// writer, oto's reader sees EOF immediately, and oto's mux stops polling the
// old player (prevents the mux from blocking on a stale reader and starving
// the new player — the root cause of "no sound" after the first track).
func (e *Engine) startFFmpeg(offsetMs int) error {
	fl := e.log.WithFunc("startFFmpeg")
	e.mu.Lock()
	path := e.path
	speed := e.speed
	vol := e.volume
	e.spectrumEpoch++
	spectrumEpoch := e.spectrumEpoch
	if e.spectrum != nil {
		e.spectrum.Reset()
	}
	e.mu.Unlock()

	atempo := fmt.Sprintf("atempo=%.3f", speed)
	args := []string{
		"-ss", strconv.FormatFloat(float64(offsetMs)/1000.0, 'f', 3, 64),
		"-i", path,
		"-filter:a", atempo,
		"-f", "s16le",
		"-ar", strconv.Itoa(SampleRate),
		"-ac", strconv.Itoa(ChannelCount),
		"-vn",
		"pipe:1",
	}
	fl.Debug("spawning ffmpeg", "path", path, "offset_ms", offsetMs, "speed", speed, "atempo", atempo)
	fl.Debug("ffmpeg command", "cmd", "ffmpeg "+strings.Join(args, " "))

	cmd := exec.Command("ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fl.Error("ffmpeg stdout pipe failed", "path", path, "err", fmt.Errorf("StdoutPipe: %w", err))
		e.setErr(fmt.Errorf("ffmpeg pipe: %w", err))
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr := &bytesBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		fl.Error("ffmpeg start failed", "path", path, "err", fmt.Errorf("Start: %w", err))
		e.setErr(fmt.Errorf("ffmpeg start: %w", err))
		return fmt.Errorf("ffmpeg start: %w", err)
	}
	fl.Debug("ffmpeg started", "pid", cmd.Process.Pid, "path", path)

	pr, pw := io.Pipe()
	fl.Debug("io.Pipe created")
	player := e.oto.NewPlayer(pr)
	player.SetVolume(vol)
	player.Play()

	fl.Debug("oto player created + Play() called",
		"pid", cmd.Process.Pid, "volume", vol, "is_playing", player.IsPlaying())

	readerCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	e.mu.Lock()
	e.ffmpegCmd = cmd
	e.player = player
	e.readerDone = done
	e.cancelReader = cancel
	e.mu.Unlock()

	go e.readerLoop(readerCtx, cmd, player, stdout, pr, pw, done, path, spectrumEpoch)
	fl.Debug("reader goroutine launched", "pid", cmd.Process.Pid)
	return nil
}

// readerLoop copies ffmpeg stdout into the oto pipe and waits for ffmpeg to
// exit. It signals completion via done. On track end (ffmpeg exits cleanly)
// the state moves to Stopped.
func (e *Engine) readerLoop(ctx context.Context, cmd *exec.Cmd, player *oto.Player, stdout io.ReadCloser, pr *io.PipeReader, pw *io.PipeWriter, done chan struct{}, path string, spectrumEpoch uint64) {
	fl := e.log.WithFunc("readerLoop")
	defer close(done)
	defer stdout.Close()
	defer pr.Close()
	defer pw.Close()

	fl.Debug("io.Copy starting", "path", path)
	buf := make([]byte, 32*1024)
	var n int64
	var copyErr error
	for {
		readN, readErr := stdout.Read(buf)
		if readN > 0 {
			n += int64(readN)
			e.writeSpectrumPCM(spectrumEpoch, buf[:readN])
			if _, err := pw.Write(buf[:readN]); err != nil {
				copyErr = err
				break
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				copyErr = readErr
			}
			break
		}
	}
	fl.Debug("pcm copy finished", "path", path, "bytes", n, "copy_err", copyErr)

	fl.Debug("closing pipe writer")
	pw.Close()

	fl.Debug("waiting for ffmpeg exit")
	waitErr := cmd.Wait()
	fl.Debug("ffmpeg exited", "wait_err", waitErr)

	select {
	case <-ctx.Done():
		fl.Debug("reader cancelled (seek/stop)", "path", path, "bytes_copied", n)
		return
	default:
	}

	if waitErr != nil {
		fl.Error("ffmpeg exited with error",
			"path", path, "err", fmt.Errorf("cmd.Wait: %w", waitErr),
			"stderr", stderrContent(cmd), "bytes_copied", n)
		e.setErr(fmt.Errorf("ffmpeg exited: %w", waitErr))
		return
	}

	e.mu.Lock()
	if e.ffmpegCmd == cmd {
		e.state = StateStopped
		e.position = e.duration
	}
	e.mu.Unlock()
	fl.Debug("track ended naturally", "path", path, "bytes_copied", n, "is_playing", player.IsPlaying())
}

func (e *Engine) writeSpectrumPCM(epoch uint64, pcm []byte) {
	e.mu.Lock()
	current := e.spectrumEpoch == epoch
	spectrum := e.spectrum
	e.mu.Unlock()
	if current && spectrum != nil {
		spectrum.WritePCM(pcm)
	}
}

// stopInternal kills the current ffmpeg (if any), cancels the reader, and
// waits for the goroutine to exit. reap=true waits synchronously; this is
// called on every seek/play/stop to prevent zombie processes (oracle Risk-B)
// and concurrent oto writers (oracle Risk-C #2).
func (e *Engine) stopInternal(reap bool) {
	fl := e.log.WithFunc("stopInternal")
	e.mu.Lock()
	cmd := e.ffmpegCmd
	player := e.player
	done := e.readerDone
	cancel := e.cancelReader
	e.ffmpegCmd = nil
	e.player = nil
	e.readerDone = nil
	e.cancelReader = nil
	e.spectrumEpoch++
	if e.spectrum != nil {
		e.spectrum.Reset()
	}
	e.mu.Unlock()

	if cmd == nil {
		fl.Debug("no active ffmpeg, nothing to stop")
		return
	}
	fl.Debug("stopping ffmpeg", "pid", cmd.Process.Pid, "player_nil", player == nil, "cancel_nil", cancel == nil, "reap", reap)
	// Immediately mute + pause the oto player so buffered audio stops
	// instantly (oto keeps playing its internal buffer after ffmpeg dies).
	if player != nil {
		player.SetVolume(0)
		player.Pause()
	}
	if cancel != nil {
		cancel()
		fl.Debug("reader cancel called")
	}
	// SIGKILL — immediate process death. stdout closes → io.Copy returns
	// → pw.Close() → oto reader EOF → readerLoop exits.
	_ = cmd.Process.Signal(killSignal)
	// Don't wait for readerLoop to fully exit — it will finish in the
	// background. The new playback can start immediately. oto's mux won't
	// block on the old player because pw.Close() makes its reader EOF.
	if reap && done != nil {
		select {
		case <-done:
			fl.Debug("reader goroutine reaped", "pid", cmd.Process.Pid)
		case <-time.After(500 * time.Millisecond):
			fl.Warn("reader goroutine still exiting after 500ms", "pid", cmd.Process.Pid)
		}
	} else {
		fl.Debug("reap skipped or no done channel", "reap", reap, "done_nil", done == nil)
	}
}
