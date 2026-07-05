package library

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/locxl/musicli/internal/log"
)

// Scanner walks file-system paths and builds Track records.
type Scanner struct {
	log  *log.Logger
	exts map[string]bool
}

// NewScanner creates a Scanner ready to walk audio files.
func NewScanner(lg *log.Logger) *Scanner {
	s := &Scanner{
		log:  lg,
		exts: make(map[string]bool),
	}
	for _, e := range []string{
		".mp3", ".flac", ".ogg", ".wav", ".m4a",
		".aac", ".opus", ".aiff", ".wma",
	} {
		s.exts[e] = true
	}
	return s
}

// ScanPath scans a single file or a directory recursively.
func (s *Scanner) ScanPath(path string) ([]*Track, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	var tracks []*Track
	if !info.IsDir() {
		if t, ok := s.processFile(path); ok {
			tracks = append(tracks, t)
		}
		return tracks, nil
	}

	walkErr := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			s.log.WithModule("library").Warn("walk error", "path", p, "error", err)
			return nil // keep walking
		}
		if d.IsDir() {
			return nil
		}
		if t, ok := s.processFile(p); ok {
			tracks = append(tracks, t)
		}
		return nil
	})
	if walkErr != nil {
		return tracks, walkErr
	}
	return tracks, nil
}

// processFile attempts to read tags and duration for a single path.
// It returns (track, true) for every audio file, even when tags fail.
func (s *Scanner) processFile(path string) (*Track, bool) {
	if !s.isAudio(path) {
		return nil, false
	}

	t, err := ReadTags(path, s.log)
	if err != nil {
		s.log.WithModule("library").Warn("tag read failed",
			"path", path, "error", err)
	}

	stat, err := os.Stat(path)
	if err != nil {
		s.log.WithModule("library").Warn("stat failed",
			"path", path, "error", err)
	} else {
		t.Size = stat.Size()
	}

	dur, err := probeDuration(path)
	if err != nil {
		s.log.WithModule("library").Warn("duration probe failed",
			"path", path, "error", err)
	}
	t.Duration = dur

	return &t, true
}

// isAudio reports whether path has a supported extension (case-insensitive).
func (s *Scanner) isAudio(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return s.exts[ext]
}
