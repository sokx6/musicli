package library

import (
	"os"
	"path/filepath"
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
