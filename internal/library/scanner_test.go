package library

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestScanPathAvoidsPerFileDebugLogNoise(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "musicli.log")
	logger, err := log.New(log.LevelDebug, logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	musicDir := filepath.Join(dir, "music")
	if err := os.Mkdir(musicDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one.mp3", "two.flac", "note.txt"} {
		if err := os.WriteFile(filepath.Join(musicDir, name), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	s := NewScanner(logger)
	if _, err := s.ScanPath(musicDir); err != nil {
		t.Fatalf("ScanPath error: %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, noisy := range []string{"file found", "processing", "tags read", "stat ok", "[ReadTags] read tags"} {
		if strings.Contains(text, noisy) {
			t.Fatalf("scan log contains per-file debug message %q:\n%s", noisy, text)
		}
	}
	if !strings.Contains(text, "scan started") || !strings.Contains(text, "scan completed") {
		t.Fatalf("scan log missing aggregate scan messages:\n%s", text)
	}
}

func TestScanPathCachedUpdatesIndexWhenMusicFilesChange(t *testing.T) {
	dir := t.TempDir()
	musicDir := filepath.Join(dir, "music")
	if err := os.Mkdir(musicDir, 0o755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(musicDir, "first.mp3")
	if err := os.WriteFile(first, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(dir, "library-index.json")
	s := NewScanner(log.Discard())

	tracks, err := s.ScanPathCached(musicDir, indexPath, true)
	if err != nil {
		t.Fatalf("first ScanPathCached: %v", err)
	}
	if len(tracks) != 1 || tracks[0].Title != "first" {
		t.Fatalf("first tracks = %#v, want first.mp3", tracks)
	}
	processCalls := 0
	originalProcess := s.processPath
	s.processPath = func(path string) (*Track, bool) {
		processCalls++
		return originalProcess(path)
	}
	tracks, err = s.ScanPathCached(musicDir, indexPath, true)
	if err != nil {
		t.Fatalf("unchanged ScanPathCached: %v", err)
	}
	if len(tracks) != 1 || tracks[0].Title != "first" {
		t.Fatalf("cached tracks = %#v, want first.mp3", tracks)
	}
	if processCalls != 0 {
		t.Fatalf("unchanged scan processed %d files, want 0", processCalls)
	}

	second := filepath.Join(musicDir, "second.mp3")
	if err := os.Rename(first, second); err != nil {
		t.Fatal(err)
	}
	tracks, err = s.ScanPathCached(musicDir, indexPath, true)
	if err != nil {
		t.Fatalf("second ScanPathCached: %v", err)
	}
	if len(tracks) != 1 || tracks[0].Title != "second" || tracks[0].Path != second {
		t.Fatalf("updated tracks = %#v, want second.mp3", tracks)
	}
	if processCalls != 1 {
		t.Fatalf("changed scan processed %d files, want 1", processCalls)
	}

	idx, err := loadLibraryIndex(indexPath)
	if err != nil {
		t.Fatalf("loadLibraryIndex: %v", err)
	}
	root := idx.Roots[filepath.Clean(musicDir)]
	if len(root.Entries) != 1 {
		t.Fatalf("index entries = %d, want 1", len(root.Entries))
	}
	if _, ok := root.Entries[second]; !ok {
		t.Fatalf("index missing %q", second)
	}
}
