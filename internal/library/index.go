package library

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const libraryIndexVersion = 1

type libraryIndex struct {
	Version int                         `json:"version"`
	Roots   map[string]libraryIndexRoot `json:"roots"`
}

type libraryIndexRoot struct {
	Entries map[string]libraryIndexEntry `json:"entries"`
}

type libraryIndexEntry struct {
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time_unix_nano"`
	Track   *Track `json:"track"`
}

func loadLibraryIndex(path string) (libraryIndex, error) {
	idx := libraryIndex{Version: libraryIndexVersion, Roots: make(map[string]libraryIndexRoot)}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return idx, nil
	}
	if err != nil {
		return idx, fmt.Errorf("read library index %q: %w", path, err)
	}
	if err := json.Unmarshal(data, &idx); err != nil {
		return libraryIndex{}, fmt.Errorf("parse library index %q: %w", path, err)
	}
	if idx.Version != libraryIndexVersion {
		return libraryIndex{Version: libraryIndexVersion, Roots: make(map[string]libraryIndexRoot)}, nil
	}
	if idx.Roots == nil {
		idx.Roots = make(map[string]libraryIndexRoot)
	}
	return idx, nil
}

func saveLibraryIndex(path string, idx libraryIndex) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create library index directory: %w", err)
	}
	data, err := json.Marshal(idx)
	if err != nil {
		return fmt.Errorf("encode library index: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".library-index-*.tmp")
	if err != nil {
		return fmt.Errorf("create library index temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write library index: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close library index: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace library index: %w", err)
	}
	return nil
}
