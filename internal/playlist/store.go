// Package playlist persists user-managed track lists by audio-file path.
package playlist

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const FavoritesID = "favorites"

var (
	ErrProtectedPlaylist = errors.New("protected playlist")
	ErrPlaylistNotFound  = errors.New("playlist not found")
	ErrInvalidName       = errors.New("invalid playlist name")
)

// Playlist stores audio paths in explicit user order.
type Playlist struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Paths []string `json:"paths"`
}

// Store is the persistent playlist collection.
type Store struct {
	path      string
	Playlists []Playlist `json:"playlists"`
}

// Load opens a store at path and guarantees that Favorites exists.
func Load(path string) (*Store, error) {
	store := &Store{path: path}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read playlists %q: %w", path, err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, store); err != nil {
			return nil, fmt.Errorf("parse playlists %q: %w", path, err)
		}
		store.path = path
	}
	createdFavorites := store.ensureFavorites()
	if createdFavorites {
		if err := store.Save(); err != nil {
			return nil, err
		}
	}
	return store, nil
}

// Save atomically persists the store.
func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create playlist directory: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode playlists: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".playlists-*.tmp")
	if err != nil {
		return fmt.Errorf("create playlist temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write playlists: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close playlists: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace playlists: %w", err)
	}
	return nil
}

// Get returns a playlist copy by ID.
func (s *Store) Get(id string) (Playlist, bool) {
	for _, playlist := range s.Playlists {
		if playlist.ID == id {
			return playlist, true
		}
	}
	return Playlist{}, false
}

// Create adds an empty user playlist with a unique ID.
func (s *Store) Create(name string) (Playlist, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Playlist{}, ErrInvalidName
	}
	base := playlistID(name)
	if base == "" {
		return Playlist{}, ErrInvalidName
	}
	id := base
	for suffix := 2; s.hasID(id); suffix++ {
		id = fmt.Sprintf("%s-%d", base, suffix)
	}
	playlist := Playlist{ID: id, Name: name}
	s.Playlists = append(s.Playlists, playlist)
	return playlist, nil
}

// Delete removes a user playlist. Favorites is permanent.
func (s *Store) Delete(id string) error {
	if id == FavoritesID {
		return ErrProtectedPlaylist
	}
	for i, playlist := range s.Playlists {
		if playlist.ID == id {
			s.Playlists = append(s.Playlists[:i], s.Playlists[i+1:]...)
			return nil
		}
	}
	return ErrPlaylistNotFound
}

// Add appends a path unless it is already present.
func (s *Store) Add(id, path string) bool {
	playlist, ok := s.playlist(id)
	if !ok || path == "" {
		return false
	}
	for _, existing := range playlist.Paths {
		if existing == path {
			return false
		}
	}
	playlist.Paths = append(playlist.Paths, path)
	return true
}

// Remove removes a path from a playlist.
func (s *Store) Remove(id, path string) bool {
	playlist, ok := s.playlist(id)
	if !ok {
		return false
	}
	for i, existing := range playlist.Paths {
		if existing == path {
			playlist.Paths = append(playlist.Paths[:i], playlist.Paths[i+1:]...)
			return true
		}
	}
	return false
}

// ToggleFavorite adds path to Favorites, or removes it when already present.
// It returns true when the path is now favorited.
func (s *Store) ToggleFavorite(path string) bool {
	if s.Remove(FavoritesID, path) {
		return false
	}
	s.Add(FavoritesID, path)
	return true
}

// IsFavorite reports whether path is in Favorites.
func (s *Store) IsFavorite(path string) bool {
	favorites, ok := s.Get(FavoritesID)
	if !ok {
		return false
	}
	for _, existing := range favorites.Paths {
		if existing == path {
			return true
		}
	}
	return false
}

// Sort orders paths using title for the current library snapshot.
func (s *Store) Sort(id string, title func(path string) string) error {
	playlist, ok := s.playlist(id)
	if !ok {
		return ErrPlaylistNotFound
	}
	sort.SliceStable(playlist.Paths, func(i, j int) bool {
		return strings.ToLower(title(playlist.Paths[i])) < strings.ToLower(title(playlist.Paths[j]))
	})
	return nil
}

func (s *Store) ensureFavorites() bool {
	if _, ok := s.Get(FavoritesID); !ok {
		s.Playlists = append([]Playlist{{ID: FavoritesID, Name: "Favorites"}}, s.Playlists...)
		return true
	}
	return false
}

func playlistID(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func (s *Store) hasID(id string) bool {
	_, ok := s.Get(id)
	return ok
}

func (s *Store) playlist(id string) (*Playlist, bool) {
	for i := range s.Playlists {
		if s.Playlists[i].ID == id {
			return &s.Playlists[i], true
		}
	}
	return nil, false
}
