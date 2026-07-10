package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/locxl/musicli/internal/log"
)

// Scanner walks file-system paths and builds Track records.
type Scanner struct {
	log         *log.Logger
	exts        map[string]bool
	processPath func(string) (*Track, bool)
}

// NewScanner creates a Scanner ready to walk audio files.
func NewScanner(lg *log.Logger) *Scanner {
	s := &Scanner{
		log:  lg,
		exts: make(map[string]bool),
	}
	extList := []string{
		".mp3", ".flac", ".ogg", ".wav", ".m4a",
		".aac", ".opus", ".aiff", ".wma",
	}
	for _, e := range extList {
		s.exts[e] = true
	}
	s.processPath = s.processFile
	fl := lg.WithModule("library").WithFunc("NewScanner")
	fl.Debug("scanner created", "extensions", strings.Join(extList, ","))
	return s
}

// ScanPath scans a single file or a directory recursively.
func (s *Scanner) ScanPath(path string) ([]*Track, error) {
	fl := s.log.WithModule("library").WithFunc("ScanPath")
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}

	fl.Debug("scan started", "path", path, "is_dir", info.IsDir())

	var tracks []*Track
	totalFiles := 0
	if !info.IsDir() {
		totalFiles++
		if t, ok := s.process(path); ok {
			tracks = append(tracks, t)
		}
		fl.Debug("scan completed", "path", path, "total_files", totalFiles, "total_tracks", len(tracks), "duration_ms", 0)
		return tracks, nil
	}

	start := time.Now()
	walkErr := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			fl.Warn("walk error", "path", p, "err", err)
			return nil // keep walking
		}
		if d.IsDir() {
			return nil
		}
		totalFiles++
		if t, ok := s.process(p); ok {
			tracks = append(tracks, t)
		}
		return nil
	})
	if walkErr != nil {
		return tracks, fmt.Errorf("walk %q: %w", path, walkErr)
	}
	dur := time.Since(start)
	fl.Debug("scan completed",
		"path", path,
		"total_files", totalFiles,
		"total_tracks", len(tracks),
		"duration_ms", dur.Milliseconds(),
	)
	return tracks, nil
}

// ScanPathCached scans path while reusing metadata for unchanged audio files.
// indexPath is updated atomically after a successful scan. When cache is false,
// it behaves like ScanPath and does not read or write an index.
func (s *Scanner) ScanPathCached(path, indexPath string, cache bool) ([]*Track, error) {
	if !cache || indexPath == "" {
		return s.ScanPath(path)
	}

	fl := s.log.WithModule("library").WithFunc("ScanPathCached")
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}
	if !info.IsDir() {
		return s.ScanPath(path)
	}

	rootPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve library root %q: %w", path, err)
	}
	rootPath = filepath.Clean(rootPath)
	idx, err := loadLibraryIndex(indexPath)
	if err != nil {
		fl.Warn("library index ignored", "path", indexPath, "err", err)
		idx = libraryIndex{Version: libraryIndexVersion, Roots: make(map[string]libraryIndexRoot)}
	}
	previous := idx.Roots[rootPath]
	entries := make(map[string]libraryIndexEntry)
	tracks := make([]*Track, 0, len(previous.Entries))
	reused := 0
	refreshed := 0

	walkErr := filepath.WalkDir(rootPath, func(filePath string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			fl.Warn("walk error", "path", filePath, "err", walkErr)
			return nil
		}
		if d.IsDir() || !s.isAudio(filePath) {
			return nil
		}
		fileInfo, err := d.Info()
		if err != nil {
			fl.Warn("file info failed", "path", filePath, "err", err)
			return nil
		}
		if cached, ok := previous.Entries[filePath]; ok && cached.Track != nil &&
			cached.Size == fileInfo.Size() && cached.ModTime == fileInfo.ModTime().UnixNano() {
			track := *cached.Track
			tracks = append(tracks, &track)
			entries[filePath] = cached
			reused++
			return nil
		}
		track, ok := s.process(filePath)
		if !ok {
			return nil
		}
		tracks = append(tracks, track)
		entries[filePath] = libraryIndexEntry{
			Size:    fileInfo.Size(),
			ModTime: fileInfo.ModTime().UnixNano(),
			Track:   track,
		}
		refreshed++
		return nil
	})
	if walkErr != nil {
		return tracks, fmt.Errorf("walk %q: %w", rootPath, walkErr)
	}

	idx.Roots[rootPath] = libraryIndexRoot{Entries: entries}
	if err := saveLibraryIndex(indexPath, idx); err != nil {
		fl.Warn("library index save failed", "path", indexPath, "err", err)
	}
	fl.Info("cached scan completed", "path", rootPath, "tracks", len(tracks), "reused", reused, "refreshed", refreshed)
	return tracks, nil
}

func (s *Scanner) process(path string) (*Track, bool) {
	return s.processPath(path)
}

// processFile attempts to read tags for a single path.
// It returns (track, true) for every audio file, even when tags fail.
func (s *Scanner) processFile(path string) (*Track, bool) {
	fl := s.log.WithModule("library").WithFunc("processFile")
	if !s.isAudio(path) {
		return nil, false
	}

	t, err := ReadTags(path, s.log)
	if err != nil {
		fl.Warn("tag read failed", "path", path, "err", err)
	}

	stat, err := os.Stat(path)
	if err != nil {
		fl.Warn("stat failed", "path", path, "err", err)
	} else {
		t.Size = stat.Size()
	}

	return &t, true
}

// isAudio reports whether path has a supported extension (case-insensitive).
func (s *Scanner) isAudio(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return s.exts[ext]
}
