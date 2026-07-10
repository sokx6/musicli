package playlist

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCreatesFavoritesPlaylist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "playlists.json")
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	favorites, ok := store.Get(FavoritesID)
	if !ok {
		t.Fatal("Favorites playlist missing")
	}
	if favorites.Name != "Favorites" {
		t.Fatalf("Favorites name = %q, want Favorites", favorites.Name)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Favorites store was not persisted: %v", err)
	}
}

func TestFavoritesDeduplicatesAndPersistsPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "playlists.json")
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed := store.ToggleFavorite("/music/one.mp3"); !changed {
		t.Fatal("adding favorite reported unchanged")
	}
	if changed := store.ToggleFavorite("/music/one.mp3"); changed {
		t.Fatal("removing favorite reported changed")
	}
	store.ToggleFavorite("/music/one.mp3")
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	favorites, _ := reloaded.Get(FavoritesID)
	if len(favorites.Paths) != 1 || favorites.Paths[0] != "/music/one.mp3" {
		t.Fatalf("favorites paths = %#v, want one path", favorites.Paths)
	}
}

func TestFavoritesCannotBeDeleted(t *testing.T) {
	store, err := Load(filepath.Join(t.TempDir(), "playlists.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(FavoritesID); !errors.Is(err, ErrProtectedPlaylist) {
		t.Fatalf("Delete(Favorites) error = %v, want ErrProtectedPlaylist", err)
	}
}

func TestSortPlaylistOrdersTracksByTitle(t *testing.T) {
	store, err := Load(filepath.Join(t.TempDir(), "playlists.json"))
	if err != nil {
		t.Fatal(err)
	}
	pl, err := store.Create("Road Trip")
	if err != nil {
		t.Fatal(err)
	}
	store.Add(pl.ID, "/music/zebra.mp3")
	store.Add(pl.ID, "/music/alpha.mp3")
	if err := store.Sort(pl.ID, func(path string) string {
		if path == "/music/zebra.mp3" {
			return "Zebra"
		}
		return "Alpha"
	}); err != nil {
		t.Fatal(err)
	}
	pl, _ = store.Get(pl.ID)
	if got, want := pl.Paths[0], "/music/alpha.mp3"; got != want {
		t.Fatalf("first path = %q, want %q", got, want)
	}
}

func TestCreateNormalizesPlaylistID(t *testing.T) {
	store, err := Load(filepath.Join(t.TempDir(), "playlists.json"))
	if err != nil {
		t.Fatal(err)
	}
	pl, err := store.Create("Night Drive! 2026")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pl.ID, "night-drive-2026"; got != want {
		t.Fatalf("playlist ID = %q, want %q", got, want)
	}
}
