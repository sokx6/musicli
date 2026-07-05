package library

import (
	"sort"
	"strings"
)

// SortField defines the dimension to sort tracks by.
type SortField string

// Supported sort fields.
const (
	SortByTitle  SortField = "title"
	SortByArtist SortField = "artist"
	SortByAlbum  SortField = "album"
	SortBySize   SortField = "size"
	SortByYear   SortField = "year"
)

// SortTracks sorts the slice in place by field and order.
// order is "asc" or "desc"; invalid values default to "asc".
func SortTracks(tracks []*Track, field SortField, order string) {
	asc := order != "desc"

	less := trackLess(field, asc)
	sort.Slice(tracks, func(i, j int) bool {
		return less(tracks[i], tracks[j])
	})
}

func trackLess(field SortField, asc bool) func(a, b *Track) bool {
	switch field {
	case SortByTitle:
		return cmpStr(func(t *Track) string { return t.Title }, asc)
	case SortByArtist:
		return cmpStr(func(t *Track) string { return t.Artist }, asc)
	case SortByAlbum:
		return cmpStr(func(t *Track) string { return t.Album }, asc)
	case SortBySize:
		return cmpInt(func(t *Track) int64 { return t.Size }, asc)
	case SortByYear:
		return cmpInt(func(t *Track) int64 { return int64(t.Year) }, asc)
	default:
		return cmpStr(func(t *Track) string { return t.Title }, asc)
	}
}

// cmpStr builds a less function for string fields.
// Empty strings sort last regardless of order.
func cmpStr(get func(*Track) string, asc bool) func(a, b *Track) bool {
	return func(a, b *Track) bool {
		aVal, bVal := get(a), get(b)
		aEmpty, bEmpty := aVal == "", bVal == ""
		if aEmpty && bEmpty {
			return false
		}
		if aEmpty {
			return false
		}
		if bEmpty {
			return true
		}
		cmp := strings.Compare(strings.ToLower(aVal), strings.ToLower(bVal))
		if asc {
			return cmp < 0
		}
		return cmp > 0
	}
}

// cmpInt builds a less function for numeric fields.
// Zero sorts last regardless of order.
func cmpInt(get func(*Track) int64, asc bool) func(a, b *Track) bool {
	return func(a, b *Track) bool {
		aVal, bVal := get(a), get(b)
		aZero, bZero := aVal == 0, bVal == 0
		if aZero && bZero {
			return false
		}
		if aZero {
			return false
		}
		if bZero {
			return true
		}
		if asc {
			return aVal < bVal
		}
		return aVal > bVal
	}
}
