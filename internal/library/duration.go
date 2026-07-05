package library

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// probeDuration returns the track duration in milliseconds via ffprobe.
// Returns 0 (with error) if ffprobe fails — callers treat 0 as "unknown".
func probeDuration(path string) (int, error) {
	out, err := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	).Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe duration %q: %w", path, err)
	}
	s := strings.TrimSpace(string(out))
	if s == "" || s == "N/A" {
		return 0, nil
	}
	secs, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parse ffprobe duration %q output %q: %w", path, s, err)
	}
	return int(secs * 1000), nil
}
