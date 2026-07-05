package library

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
	"github.com/locxl/musicli/internal/log"
)

// ReadTags extracts metadata from an audio file at path.
// On error it returns a Track with Path and Title derived from the filename
// so the file remains visible in the library.
func ReadTags(path string, lg *log.Logger) (Track, error) {
	f, err := os.Open(path)
	if err != nil {
		return fallbackTrack(path), err
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return fallbackTrack(path), err
	}

	t := Track{
		Path:        path,
		Title:       nonEmpty(m.Title(), filenameTitle(path)),
		Artist:      m.Artist(),
		AlbumArtist: m.AlbumArtist(),
		Album:       m.Album(),
		Composer:    m.Composer(),
		Genre:       m.Genre(),
		Year:        m.Year(),
		HasCover:    m.Picture() != nil,
	}

	trackNo, _ := m.Track()
	t.TrackNo = trackNo

	discNo, _ := m.Disc()
	t.DiscNo = discNo

	lg.WithModule("library").Debug("read tags",
		"path", path,
		"title", t.Title,
		"artist", t.Artist,
		"album", t.Album,
		"year", t.Year,
	)

	return t, nil
}

// fallbackTrack creates a minimal Track when tag reading fails.
func fallbackTrack(path string) Track {
	return Track{
		Path:     path,
		Title:    filenameTitle(path),
		HasCover: false,
	}
}

// filenameTitle returns the base name without extension.
func filenameTitle(path string) string {
	base := filepath.Base(path)
	if ext := filepath.Ext(base); ext != "" {
		return strings.TrimSuffix(base, ext)
	}
	return base
}

// nonEmpty returns a if non-empty, otherwise b.
func nonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
