package lyrics

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
)

// Loader loads lyrics from local sidecar files, then embedded audio tags.
type Loader struct {
	ReadEmbedded func(path string) (string, error)
}

// LoadLocal loads a same-basename .spl or .lrc file next to an audio file.
func LoadLocal(audioPath string) (*Lyric, string, error) {
	return Loader{}.Load(audioPath)
}

// Load loads a lyric near or inside an audio file.
func (l Loader) Load(audioPath string) (*Lyric, string, error) {
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
	readEmbedded := l.ReadEmbedded
	if readEmbedded == nil {
		readEmbedded = ReadEmbedded
	}
	raw, err := readEmbedded(audioPath)
	if err != nil {
		return nil, "", fmt.Errorf("read embedded lyric %q: %w", audioPath, err)
	}
	raw = strings.TrimSpace(raw)
	if raw != "" {
		ly, err := SPLParser{}.Parse(raw)
		if err != nil {
			return nil, "", fmt.Errorf("parse embedded lyric %q: %w", audioPath, err)
		}
		return ly, "tag:" + audioPath, nil
	}
	return nil, "", os.ErrNotExist
}

// ReadEmbedded extracts an unsynchronized lyric string from audio metadata.
func ReadEmbedded(audioPath string) (string, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return "", fmt.Errorf("open %q: %w", audioPath, err)
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return "", fmt.Errorf("read tags %q: %w", audioPath, err)
	}
	return m.Lyrics(), nil
}

func trimExt(path string) string {
	ext := filepath.Ext(path)
	return path[:len(path)-len(ext)]
}
