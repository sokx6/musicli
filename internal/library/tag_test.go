package library

import (
	"path/filepath"
	"testing"

	"github.com/locxl/musicli/internal/log"
)

// TestReadTagsAgainstRealMP3 reads one of the test MP3s at project root.
func TestReadTagsAgainstRealMP3(t *testing.T) {
	// Project root is two directories above internal/library.
	root := filepath.Join("..", "..")
	matches, err := filepath.Glob(filepath.Join(root, "*.mp3"))
	if err != nil || len(matches) == 0 {
		t.Skip("no real .mp3 files found in project root")
	}

	tr, err := ReadTags(matches[0], log.Discard())
	if err != nil {
		t.Fatalf("ReadTags error: %v", err)
	}
	if tr.Title == "" {
		t.Errorf("expected non-empty Title, got %q", tr.Title)
	}
	if !tr.HasCover {
		t.Errorf("expected HasCover=true for test MP3")
	}
}
