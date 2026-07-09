package mpris

import (
	"testing"

	godbus "github.com/godbus/dbus/v5"
	"github.com/locxl/musicli/internal/library"
)

func TestMetadataIncludesTrackAndLyrics(t *testing.T) {
	snapshot := Snapshot{
		Track: &library.Track{
			Path:     "/music/song.flac",
			Title:    "Song",
			Artist:   "Artist",
			Album:    "Album",
			Duration: 123456,
		},
		CurrentIndex: 2,
		DurationMS:   123456,
		LyricText:    "[00:00.00]hello\n[00:01.00]world",
		CoverURL:     "file:///tmp/musicli-cover.png",
	}

	metadata := Metadata(snapshot)

	if got := variantValue[godbus.ObjectPath](t, metadata, "mpris:trackid"); got != "/org/mpris/MediaPlayer2/track/2" {
		t.Fatalf("trackid = %q", got)
	}
	if got := variantValue[int64](t, metadata, "mpris:length"); got != 123456000 {
		t.Fatalf("length = %d, want 123456000", got)
	}
	if got := variantValue[string](t, metadata, "xesam:title"); got != "Song" {
		t.Fatalf("title = %q", got)
	}
	if got := variantValue[[]string](t, metadata, "xesam:artist"); len(got) != 1 || got[0] != "Artist" {
		t.Fatalf("artist = %#v", got)
	}
	if got := variantValue[string](t, metadata, "xesam:album"); got != "Album" {
		t.Fatalf("album = %q", got)
	}
	if got := variantValue[string](t, metadata, "xesam:url"); got != "file:///music/song.flac" {
		t.Fatalf("url = %q", got)
	}
	if got := variantValue[string](t, metadata, "xesam:asText"); got != snapshot.LyricText {
		t.Fatalf("asText = %q", got)
	}
	if got := variantValue[string](t, metadata, "xesam:comment"); got != snapshot.LyricText {
		t.Fatalf("comment = %q", got)
	}
	if got := variantValue[string](t, metadata, "mpris:artUrl"); got != snapshot.CoverURL {
		t.Fatalf("artUrl = %q", got)
	}
}

func variantValue[T any](t *testing.T, metadata map[string]godbus.Variant, key string) T {
	t.Helper()
	variant, ok := metadata[key]
	if !ok {
		t.Fatalf("metadata missing %s", key)
	}
	value, ok := variant.Value().(T)
	if !ok {
		t.Fatalf("metadata[%s] has type %T", key, variant.Value())
	}
	return value
}
