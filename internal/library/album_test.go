package library

import (
	"testing"
)

func TestGroupByAlbum(t *testing.T) {
	tracks := []*Track{
		{Title: "A1", Album: "Alpha", AlbumArtist: "Artist A", DiscNo: 1, TrackNo: 2},
		{Title: "A2", Album: "Alpha", AlbumArtist: "Artist A", DiscNo: 1, TrackNo: 1},
		{Title: "A3", Album: "Alpha", AlbumArtist: "", DiscNo: 2, TrackNo: 1},
		{Title: "B1", Album: "Beta", AlbumArtist: "Artist B", DiscNo: 1, TrackNo: 1},
		{Title: "B2", Album: "Beta", AlbumArtist: "Artist B", DiscNo: 1, TrackNo: 2},
		{Title: "B3", Album: "Beta", AlbumArtist: "Artist B", DiscNo: 1, TrackNo: 3},
	}

	albums := GroupByAlbum(tracks)
	if len(albums) != 2 {
		t.Fatalf("expected 2 albums, got %d", len(albums))
	}

	// Albums sorted by name: Alpha, Beta
	if albums[0].Name != "Alpha" {
		t.Errorf("expected first album Alpha, got %q", albums[0].Name)
	}
	if albums[0].AlbumArtist != "Artist A" {
		t.Errorf("expected AlbumArtist Artist A, got %q", albums[0].AlbumArtist)
	}
	if len(albums[0].Tracks) != 3 {
		t.Errorf("expected 3 tracks in Alpha, got %d", len(albums[0].Tracks))
	}
	// Sorted by DiscNo then TrackNo then Title: (1,1), (1,2), (2,1)
	if albums[0].Tracks[0].Title != "A2" {
		t.Errorf("expected first track A2, got %q", albums[0].Tracks[0].Title)
	}
	if albums[0].Tracks[1].Title != "A1" {
		t.Errorf("expected second track A1, got %q", albums[0].Tracks[1].Title)
	}
	if albums[0].Tracks[2].Title != "A3" {
		t.Errorf("expected third track A3, got %q", albums[0].Tracks[2].Title)
	}

	if albums[1].Name != "Beta" {
		t.Errorf("expected second album Beta, got %q", albums[1].Name)
	}
	if albums[1].AlbumArtist != "Artist B" {
		t.Errorf("expected AlbumArtist Artist B, got %q", albums[1].AlbumArtist)
	}
	if len(albums[1].Tracks) != 3 {
		t.Errorf("expected 3 tracks in Beta, got %d", len(albums[1].Tracks))
	}
}

func TestGroupByAlbumEmptyName(t *testing.T) {
	tracks := []*Track{
		{Title: "X1", Album: "", AlbumArtist: ""},
	}
	albums := GroupByAlbum(tracks)
	if len(albums) != 1 {
		t.Fatalf("expected 1 album, got %d", len(albums))
	}
	if albums[0].Name != "Unknown Album" {
		t.Errorf("expected Unknown Album, got %q", albums[0].Name)
	}
	if albums[0].AlbumArtist != "Unknown Artist" {
		t.Errorf("expected Unknown Artist, got %q", albums[0].AlbumArtist)
	}
}
