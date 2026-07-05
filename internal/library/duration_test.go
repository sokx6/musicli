package library

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestProbeDurationWrapsFFProbeError(t *testing.T) {
	_, err := probeDuration("/definitely/missing/audio.mp3")
	if err == nil {
		t.Fatal("probeDuration returned nil error")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error does not wrap *exec.ExitError: %v", err)
	}
	if !strings.Contains(err.Error(), "ffprobe duration") {
		t.Fatalf("error missing ffprobe context: %v", err)
	}
}
