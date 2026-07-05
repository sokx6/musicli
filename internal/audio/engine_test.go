package audio

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/locxl/musicli/internal/log"
)

// findTestMP3 locates a test audio file in the project root.
func findTestMP3(t *testing.T) string {
	t.Helper()
	// Tests run in internal/audio; project root is two dirs up.
	root := filepath.Join("..", "..")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Skipf("cannot read project root: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(e.Name()), ".mp3") {
			return filepath.Join(root, e.Name())
		}
	}
	t.Skip("no .mp3 test file in project root")
	return ""
}

func TestProbeDuration(t *testing.T) {
	path := findTestMP3(t)
	ms, err := probeDuration(path, log.Discard())
	if err != nil {
		t.Fatalf("probeDuration: %v", err)
	}
	if ms <= 0 {
		t.Errorf("duration = %d, want > 0", ms)
	}
	t.Logf("duration: %d ms (%.1f s)", ms, float64(ms)/1000)
}

func TestEnginePlayPauseStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping audio playback test in short mode")
	}
	path := findTestMP3(t)
	ctx := context.Background()
	eng, err := New(ctx, log.Discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Play
	if err := eng.Play(path); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if s := eng.State(); s != StatePlaying {
		t.Fatalf("state = %v, want playing", s)
	}
	if d := eng.Duration(); d <= 0 {
		t.Errorf("duration = %d, want > 0", d)
	}

	// Let it play for 500ms
	time.Sleep(500 * time.Millisecond)
	pos1 := eng.Position()
	if pos1 <= 0 {
		t.Errorf("position after 500ms = %d, want > 0", pos1)
	}
	t.Logf("position after 500ms: %d ms", pos1)

	// Pause
	if err := eng.Pause(); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if s := eng.State(); s != StatePaused {
		t.Fatalf("state = %v, want paused", s)
	}
	pos2 := eng.Position()
	time.Sleep(300 * time.Millisecond)
	pos3 := eng.Position()
	if pos3 != pos2 {
		t.Errorf("position moved while paused: %d -> %d", pos2, pos3)
	}

	// Resume
	if err := eng.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if s := eng.State(); s != StatePlaying {
		t.Fatalf("state = %v, want playing", s)
	}
	time.Sleep(300 * time.Millisecond)
	pos4 := eng.Position()
	if pos4 <= pos2 {
		t.Errorf("position did not advance after resume: %d -> %d", pos2, pos4)
	}

	// Seek
	seekTarget := pos4 + 5000
	if seekTarget > eng.Duration() {
		seekTarget = eng.Duration() / 2
	}
	if err := eng.Seek(seekTarget); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	pos5 := eng.Position()
	// allow some tolerance for ffmpeg restart latency
	if pos5 < seekTarget-2000 {
		t.Errorf("after seek to %d, position = %d (too low)", seekTarget, pos5)
	}

	// Stop
	eng.Stop()
	if s := eng.State(); s != StateStopped {
		t.Fatalf("state = %v, want stopped", s)
	}

	if err := eng.Err(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEngineSetVolume(t *testing.T) {
	eng, err := New(context.Background(), log.Discard())
	if err != nil {
		t.Skipf("oto context unavailable (headless?): %v", err)
	}
	eng.SetVolume(50)
	if v := eng.Volume(); v != 50 {
		t.Errorf("volume = %d, want 50", v)
	}
	eng.SetVolume(150)
	if v := eng.Volume(); v != 100 {
		t.Errorf("volume = %d, want 100 (clamped)", v)
	}
	eng.SetVolume(-10)
	if v := eng.Volume(); v != 0 {
		t.Errorf("volume = %d, want 0 (clamped)", v)
	}
}

func TestEngineSetSpeed(t *testing.T) {
	eng, err := New(context.Background(), log.Discard())
	if err != nil {
		t.Skipf("oto context unavailable (headless?): %v", err)
	}
	eng.SetSpeed(1.5)
	if s := eng.Speed(); s != 1.5 {
		t.Errorf("speed = %v, want 1.5", s)
	}
	eng.SetSpeed(5.0)
	if s := eng.Speed(); s != 2.0 {
		t.Errorf("speed = %v, want 2.0 (clamped)", s)
	}
	eng.SetSpeed(0.1)
	if s := eng.Speed(); s != 0.5 {
		t.Errorf("speed = %v, want 0.5 (clamped)", s)
	}
}
