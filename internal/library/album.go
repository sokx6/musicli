package library

import (
	"sort"
	"strings"
)

// GroupByAlbum groups tracks by album name and sorts the results.
func GroupByAlbum(tracks []*Track) []*Album {
	byName := make(map[string][]*Track)
	for _, tr := range tracks {
		name := tr.Album
		if name == "" {
			name = "Unknown Album"
		}
		byName[name] = append(byName[name], tr)
	}

	albums := make([]*Album, 0, len(byName))
	for name, tracks := range byName {
		albumArtist := ""
		for _, tr := range tracks {
			if tr.AlbumArtist != "" {
				albumArtist = tr.AlbumArtist
				break
			}
		}
		if albumArtist == "" {
			albumArtist = "Unknown Artist"
		}

		sort.Slice(tracks, func(i, j int) bool {
			if tracks[i].DiscNo != tracks[j].DiscNo {
				return tracks[i].DiscNo < tracks[j].DiscNo
			}
			if tracks[i].TrackNo != tracks[j].TrackNo {
				return tracks[i].TrackNo < tracks[j].TrackNo
			}
			return strings.ToLower(tracks[i].Title) < strings.ToLower(tracks[j].Title)
		})

		albums = append(albums, &Album{
			Name:        name,
			AlbumArtist: albumArtist,
			Tracks:      tracks,
		})
	}

	sort.Slice(albums, func(i, j int) bool {
		return strings.ToLower(albums[i].Name) < strings.ToLower(albums[j].Name)
	})

	return albums
}
