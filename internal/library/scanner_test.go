package library

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/locxl/musicli/internal/log"
)

// TestScanPathOnProjectRoot scans the project root for the two test MP3s.
func TestScanPathOnProjectRoot(t *testing.T) {
	root := filepath.Join("..", "..")
	matches, err := filepath.Glob(filepath.Join(root, "*.mp3"))
	if err != nil || len(matches) == 0 {
		t.Skip("no real .mp3 files found in project root")
	}

	s := NewScanner(log.Discard())
	tracks, err := s.ScanPath(root)
	if err != nil {
		t.Fatalf("ScanPath error: %v", err)
	}
	if len(tracks) < 2 {
		t.Fatalf("expected at least 2 tracks, got %d", len(tracks))
	}
	for _, tr := range tracks {
		if tr.Path == "" {
			t.Errorf("track has empty Path")
		}
	}
}

// TestScanPathFakeMP3 creates an empty .mp3 file and verifies the fallback title.
func TestScanPathFakeMP3(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake_song.mp3")
	if err := os.WriteFile(fake, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	s := NewScanner(log.Discard())
	tracks, err := s.ScanPath(fake)
	if err != nil {
		t.Fatalf("ScanPath error: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(tracks))
	}
	if tracks[0].Title != "fake_song" {
		t.Errorf("expected Title %q, got %q", "fake_song", tracks[0].Title)
	}
}

func TestScanPathDoesNotProbeDurationForEveryTrack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script ffprobe shim")
	}

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "ffprobe-called")
	ffprobe := filepath.Join(binDir, "ffprobe")
	if err := os.WriteFile(ffprobe, []byte("#!/bin/sh\necho called >> \"$FFPROBE_MARKER\"\necho 1.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FFPROBE_MARKER", marker)

	musicDir := filepath.Join(dir, "music")
	if err := os.Mkdir(musicDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one.mp3", "two.flac", "three.ogg"} {
		if err := os.WriteFile(filepath.Join(musicDir, name), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	s := NewScanner(log.Discard())
	tracks, err := s.ScanPath(musicDir)
	if err != nil {
		t.Fatalf("ScanPath error: %v", err)
	}
	if len(tracks) != 3 {
		t.Fatalf("tracks = %d, want 3", len(tracks))
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("ScanPath invoked ffprobe during library scan")
	}
	for _, tr := range tracks {
		if tr.Duration != 0 {
			t.Fatalf("track %q duration = %d, want 0 until playback probes it", tr.Path, tr.Duration)
		}
	}
}
