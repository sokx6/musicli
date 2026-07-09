package mpris

import (
	"testing"
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
