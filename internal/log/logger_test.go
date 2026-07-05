package log

import (
	"bytes"
	"os"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in   string
		want string // slog.Level String()
	}{
		{"debug", "DEBUG"},
		{"info", "INFO"},
		{"warning", "WARN"},
		{"error", "ERROR"},
		{"bogus", "INFO"}, // default
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
	// first session: write a line
	l1, err := New(LevelInfo, path)
	if err != nil {
		t.Fatal(err)
	}
	l1.Info("first session line")
	l1.Close()

	// second session: truncate; old line must be gone
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
