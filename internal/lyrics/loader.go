package lyrics

import (
	"fmt"
	"os"
	"path/filepath"
)

// LoadLocal loads a same-basename .spl or .lrc file next to an audio file.
func LoadLocal(audioPath string) (*Lyric, string, error) {
	base := trimExt(audioPath)
	for _, ext := range []string{".spl", ".lrc"} {
		path := base + ext
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, "", fmt.Errorf("read lyric %q: %w", path, err)
		}
		ly, err := SPLParser{}.Parse(string(raw))
		if err != nil {
			return nil, "", fmt.Errorf("parse lyric %q: %w", path, err)
		}
		return ly, path, nil
	}
	return nil, "", os.ErrNotExist
}

func trimExt(path string) string {
	ext := filepath.Ext(path)
	return path[:len(path)-len(ext)]
}
