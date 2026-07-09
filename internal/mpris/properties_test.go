package mpris

import (
	"testing"

	godbus "github.com/godbus/dbus/v5"
)

func TestLyricsPropertiesExposeCurrentLineAndWord(t *testing.T) {
	snapshot := Snapshot{
		CurrentLine:    "hello world",
		CurrentLineIdx: 3,
		CurrentWordIdx: 1,
		Synced:         true,
		LyricText:      "[00:00.00]hello world",
		LyricFormat:    "lrc",
	}

	props := lyricsProperties(snapshot)

	if got := variantValue[string](t, props, "CurrentLine"); got != "hello world" {
		t.Fatalf("CurrentLine = %q", got)
	}
	if got := variantValue[int32](t, props, "CurrentLineIndex"); got != 3 {
		t.Fatalf("CurrentLineIndex = %d", got)
	}
	if got := variantValue[int32](t, props, "CurrentWordIndex"); got != 1 {
		t.Fatalf("CurrentWordIndex = %d", got)
	}
	if got := variantValue[bool](t, props, "Synced"); !got {
		t.Fatal("Synced = false, want true")
	}
	if got := variantValue[string](t, props, "LyricText"); got != "[00:00.00]hello world" {
		t.Fatalf("LyricText = %q", got)
	}
	if got := variantValue[string](t, props, "LyricFormat"); got != "lrc" {
		t.Fatalf("LyricFormat = %q", got)
	}
}

func TestPlayerPropertiesReflectNoCurrentTrack(t *testing.T) {
	props := playerProperties(Snapshot{CurrentIndex: -1, PlaybackStatus: StatusStopped})

	metadata := variantValue[map[string]godbus.Variant](t, props, "Metadata")
	if len(metadata) != 0 {
		t.Fatalf("metadata without current track = %#v, want empty", metadata)
	}
	if got := variantValue[bool](t, props, "CanPlay"); got {
		t.Fatal("CanPlay without current track = true, want false")
	}
	if got := variantValue[bool](t, props, "CanPause"); got {
		t.Fatal("CanPause without current track = true, want false")
	}
	if got := variantValue[bool](t, props, "CanSeek"); got {
		t.Fatal("CanSeek without current track = true, want false")
	}
	if got := variantValue[bool](t, props, "CanGoNext"); got {
		t.Fatal("CanGoNext without current track = true, want false")
	}
	if got := variantValue[bool](t, props, "CanGoPrevious"); got {
		t.Fatal("CanGoPrevious without current track = true, want false")
	}
}
