package log

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"debug", "DEBUG"},
		{"info", "INFO"},
		{"warning", "WARN"},
		{"error", "ERROR"},
		{"bogus", "INFO"},
		{"", "INFO"},
	}
	for _, c := range cases {
		got := parseLevel(c.in).String()
		if got != c.want {
			t.Errorf("parseLevel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNewTruncates(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/musicli.log"
	l1, err := New(LevelInfo, path)
	if err != nil {
		t.Fatal(err)
	}
	l1.Info("first session line")
	l1.Close()

	l2, err := New(LevelInfo, path)
	if err != nil {
		t.Fatal(err)
	}
	l2.Info("second session line")
	l2.Close()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("first session line")) {
		t.Errorf("old log not truncated; got:\n%s", b)
	}
	if !bytes.Contains(b, []byte("second session line")) {
		t.Errorf("new log line missing; got:\n%s", b)
	}
}

func TestFormat(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/musicli.log"
	l, err := New(LevelDebug, path)
	if err != nil {
		t.Fatal(err)
	}
	l.WithModule("audio").WithFunc("startFFmpeg").
		Error("ffmpeg start failed", "path", "/x.mp3", "err", "exit code 1")

	l.WithModule("audio").WithFunc("Play").
		Info("playback started", "path", "/y.flac")

	l.Close()

	b, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %s", len(lines), b)
	}

	// Line 1: ...[ERROR][audio][startFFmpeg] ffmpeg start failed path="/x.mp3" err="exit code 1"
	if !strings.Contains(lines[0], "[ERROR]") {
		t.Errorf("line 1 missing [ERROR]: %s", lines[0])
	}
	if !strings.Contains(lines[0], "[audio]") {
		t.Errorf("line 1 missing [audio]: %s", lines[0])
	}
	if !strings.Contains(lines[0], "[startFFmpeg]") {
		t.Errorf("line 1 missing [startFFmpeg]: %s", lines[0])
	}
	if !strings.Contains(lines[0], "ffmpeg start failed") {
		t.Errorf("line 1 missing message: %s", lines[0])
	}

	// Line 2: ...[INFO][audio][Play] playback started path=/y.flac
	if !strings.Contains(lines[1], "[INFO]") {
		t.Errorf("line 2 missing [INFO]: %s", lines[1])
	}
	if !strings.Contains(lines[1], "[Play]") {
		t.Errorf("line 2 missing [Play]: %s", lines[1])
	}
	if !strings.Contains(lines[1], "playback started") {
		t.Errorf("line 2 missing message: %s", lines[1])
	}
}

func TestAllFourLevels(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/musicli.log"
	l, err := New(LevelDebug, path)
	if err != nil {
		t.Fatal(err)
	}
	ml := l.WithModule("test").WithFunc("fn")
	ml.Debug("debug msg")
	ml.Info("info msg")
	ml.Warn("warn msg")
	ml.Error("error msg")
	l.Close()

	b, _ := os.ReadFile(path)
	for _, lvl := range []string{"[DEBUG]", "[INFO]", "[WARN]", "[ERROR]"} {
		if !bytes.Contains(b, []byte(lvl)) {
			t.Errorf("missing level %s in log:\n%s", lvl, b)
		}
	}
}
