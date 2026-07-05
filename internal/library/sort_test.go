package library

import (
	"testing"
)

func TestSortTracks(t *testing.T) {
	tracks := []*Track{
		{Title: "Charlie", Artist: "B", Album: "Z", Size: 300, Year: 2020},
		{Title: "Alpha", Artist: "C", Album: "Y", Size: 100, Year: 0},
		{Title: "Bravo", Artist: "A", Album: "X", Size: 200, Year: 1990},
	}

	// Title ascending
	SortTracks(tracks, SortByTitle, "asc")
	if tracks[0].Title != "Alpha" || tracks[1].Title != "Bravo" || tracks[2].Title != "Charlie" {
		t.Errorf("title asc order wrong: %v", titles(tracks))
	}

	// Title descending
	SortTracks(tracks, SortByTitle, "desc")
	if tracks[0].Title != "Charlie" || tracks[1].Title != "Bravo" || tracks[2].Title != "Alpha" {
		t.Errorf("title desc order wrong: %v", titles(tracks))
	}

	// Artist ascending
	SortTracks(tracks, SortByArtist, "asc")
	if tracks[0].Artist != "A" || tracks[1].Artist != "B" || tracks[2].Artist != "C" {
		t.Errorf("artist asc order wrong: %v", artists(tracks))
	}

	// Album ascending
	SortTracks(tracks, SortByAlbum, "asc")
	if tracks[0].Album != "X" || tracks[1].Album != "Y" || tracks[2].Album != "Z" {
		t.Errorf("album asc order wrong: %v", albums(tracks))
	}

	// Size ascending
	SortTracks(tracks, SortBySize, "asc")
	if tracks[0].Size != 100 || tracks[1].Size != 200 || tracks[2].Size != 300 {
		t.Errorf("size asc order wrong: %v", sizes(tracks))
	}

	// Size descending
	SortTracks(tracks, SortBySize, "desc")
	if tracks[0].Size != 300 || tracks[1].Size != 200 || tracks[2].Size != 100 {
		t.Errorf("size desc order wrong: %v", sizes(tracks))
	}

	// Year ascending — 0 sorts last
	SortTracks(tracks, SortByYear, "asc")
	if tracks[0].Year != 1990 || tracks[1].Year != 2020 || tracks[2].Year != 0 {
		t.Errorf("year asc order wrong: %v", years(tracks))
	}

	// Year descending — 0 sorts last
	SortTracks(tracks, SortByYear, "desc")
	if tracks[0].Year != 2020 || tracks[1].Year != 1990 || tracks[2].Year != 0 {
		t.Errorf("year desc order wrong: %v", years(tracks))
	}
}

func TestSortTracksEmptyStringLast(t *testing.T) {
	tracks := []*Track{
		{Title: ""},
		{Title: "Beta"},
		{Title: "Alpha"},
	}
	SortTracks(tracks, SortByTitle, "asc")
	if tracks[0].Title != "Alpha" || tracks[1].Title != "Beta" || tracks[2].Title != "" {
		t.Errorf("empty string should be last in asc: %v", titles(tracks))
	}

	SortTracks(tracks, SortByTitle, "desc")
	if tracks[0].Title != "Beta" || tracks[1].Title != "Alpha" || tracks[2].Title != "" {
		t.Errorf("empty string should be last in desc: %v", titles(tracks))
	}
}

func titles(tracks []*Track) []string {
	out := make([]string, len(tracks))
	for i, tr := range tracks {
		out[i] = tr.Title
	}
	return out
}

func artists(tracks []*Track) []string {
	out := make([]string, len(tracks))
	for i, tr := range tracks {
		out[i] = tr.Artist
	}
	return out
}

func albums(tracks []*Track) []string {
	out := make([]string, len(tracks))
	for i, tr := range tracks {
		out[i] = tr.Album
	}
	return out
}

func sizes(tracks []*Track) []int64 {
	out := make([]int64, len(tracks))
	for i, tr := range tracks {
		out[i] = tr.Size
	}
	return out
}

func years(tracks []*Track) []int {
	out := make([]int, len(tracks))
	for i, tr := range tracks {
		out[i] = tr.Year
	}
	return out
}
