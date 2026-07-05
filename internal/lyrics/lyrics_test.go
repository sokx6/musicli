package lyrics

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSPLParserParsesLRCAndTranslations(t *testing.T) {
	text := `[ti:Song]
[ar:Artist]
[00:10.00]First line
First translation
[00:10.00]Same timestamp translation
[00:20.50][00:40.50]Repeated
`
	got, err := SPLParser{}.Parse(text)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Tags["ti"] != "Song" || got.Tags["ar"] != "Artist" {
		t.Fatalf("tags not parsed: %#v", got.Tags)
	}
	if len(got.Lines) != 3 {
		t.Fatalf("lines = %d, want 3: %#v", len(got.Lines), got.Lines)
	}
	if got.Lines[0].StartMs != 10000 || got.Lines[0].EndMs != 20500 || got.Lines[0].Text != "First line" {
		t.Fatalf("line 0 mismatch: %#v", got.Lines[0])
	}
	if got.Lines[0].Translation != "First translation\nSame timestamp translation" {
		t.Fatalf("translation = %q", got.Lines[0].Translation)
	}
	if got.Lines[1].StartMs != 20500 || got.Lines[2].StartMs != 40500 {
		t.Fatalf("repeated starts mismatch: %#v", got.Lines)
	}
}

func TestSPLParserAppliesOffsetTag(t *testing.T) {
	text := `[offset:250]
[00:01.00]One
[00:02.00]Two`
	got, err := SPLParser{}.Parse(text)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Lines[0].StartMs != 1250 || got.Lines[0].EndMs != 2250 {
		t.Fatalf("line 0 timing = %d..%d", got.Lines[0].StartMs, got.Lines[0].EndMs)
	}
}

func TestSPLParserParsesWordTimingAndDelayMarkers(t *testing.T) {
	text := `[05:20.22]<05:21.22>Hello <05:23.22>world[05:24.22]`
	got, err := SPLParser{}.Parse(text)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got.Lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(got.Lines))
	}
	line := got.Lines[0]
	if line.StartMs != 320220 || line.EndMs != 324220 {
		t.Fatalf("line timing = %d..%d", line.StartMs, line.EndMs)
	}
	if line.Text != "Hello world" {
		t.Fatalf("line text = %q", line.Text)
	}
	if len(line.Words) != 2 {
		t.Fatalf("words = %d, want 2: %#v", len(line.Words), line.Words)
	}
	if line.Words[0] != (Word{Text: "Hello ", StartMs: 321220, EndMs: 323220}) {
		t.Fatalf("word 0 mismatch: %#v", line.Words[0])
	}
	if line.Words[1] != (Word{Text: "world", StartMs: 323220, EndMs: 324220}) {
		t.Fatalf("word 1 mismatch: %#v", line.Words[1])
	}
}

func TestLoadLocalPrefersSPLThenLRC(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "song.flac")
	if err := os.WriteFile(audio, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "song.lrc"), []byte("[00:01.00]lrc"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "song.spl"), []byte("[00:02.00]spl"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, path, err := LoadLocal(audio)
	if err != nil {
		t.Fatalf("LoadLocal: %v", err)
	}
	if filepath.Base(path) != "song.spl" {
		t.Fatalf("loaded %q, want song.spl", path)
	}
	if len(got.Lines) != 1 || got.Lines[0].Text != "spl" {
		t.Fatalf("loaded lyric mismatch: %#v", got)
	}
}
