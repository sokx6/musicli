package lyrics

import (
	"os"
	"path/filepath"
	"strings"
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

func TestSPLParserKeepsPunctuationWithEqualTimestamps(t *testing.T) {
	text := `[00:01.550]词[00:01.718]：[00:01.718]い[00:01.886]よ[00:02.038]わ[00:02.198]`
	got, err := SPLParser{}.Parse(text)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got.Lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(got.Lines))
	}
	line := got.Lines[0]
	if line.Text != "词：いよわ" {
		t.Fatalf("line text = %q, want %q", line.Text, "词：いよわ")
	}
	// ： shares timestamp 1718 with い and merges into it so they highlight
	// together. Result: 4 words, not 5.
	if len(line.Words) != 4 {
		t.Fatalf("words = %d, want 4: %#v", len(line.Words), line.Words)
	}
	if line.Words[1].Text != "：い" {
		t.Fatalf("word 1 = %q, want ：い: %#v", line.Words[1].Text, line.Words)
	}
	if line.Words[1].StartMs != 1718 || line.Words[1].EndMs != 1886 {
		t.Fatalf("word 1 timing = %d..%d, want 1718..1886", line.Words[1].StartMs, line.Words[1].EndMs)
	}
	// Concatenation of all word texts must equal line.Text.
	var concat string
	for _, w := range line.Words {
		concat += w.Text
	}
	if concat != line.Text {
		t.Fatalf("word concat = %q, line text = %q", concat, line.Text)
	}
}

func TestSPLParserMergesSameStartTimedTranslation(t *testing.T) {
	text := `[00:01.399]ツ[00:01.589]ギ[00:01.739]ハ[00:01.879]ギ[00:02.129]だ[00:02.309]ら[00:02.999]け[00:03.199]の[00:03.949]君[00:04.788]と[00:04.948]の[00:05.158]時[00:05.508]間[00:05.798]も[00:06.368]
[00:01.399]我们尽是东拼西凑的时光[00:06.540]
[00:06.548]そ[00:06.708]ろ[00:07.328]そ[00:07.508]ろ[00:08.207]終[00:08.407]わ[00:08.597]り[00:08.807]に[00:09.007]し[00:09.177]よ[00:09.357]う[00:09.817]`
	got, err := SPLParser{}.Parse(text)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got.Lines) != 2 {
		t.Fatalf("lines = %d, want 2: %#v", len(got.Lines), got.Lines)
	}
	if got.Lines[0].Text != "ツギハギだらけの君との時間も" {
		t.Fatalf("line text = %q", got.Lines[0].Text)
	}
	if got.Lines[0].Translation != "我们尽是东拼西凑的时光" {
		t.Fatalf("translation = %q", got.Lines[0].Translation)
	}
	if len(got.Lines[0].Words) == 0 {
		t.Fatalf("expected original words: %#v", got.Lines[0])
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

func TestLoadLocalFallsBackToEmbeddedTagLyrics(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(audio, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := Loader{
		ReadEmbedded: func(path string) (string, error) {
			if !strings.HasSuffix(path, "song.mp3") {
				t.Fatalf("unexpected embedded lyric path %q", path)
			}
			return "[00:01.00]embedded", nil
		},
	}

	got, source, err := loader.Load(audio)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if source != "tag:"+audio {
		t.Fatalf("source = %q, want tag:%s", source, audio)
	}
	if len(got.Lines) != 1 || got.Lines[0].Text != "embedded" {
		t.Fatalf("loaded lyric mismatch: %#v", got)
	}
}
