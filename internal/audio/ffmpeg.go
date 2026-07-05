package audio

import (
	"bytes"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/locxl/musicli/internal/log"
)

// termSignal / killSignal: ffmpeg process management.
var (
	termSignal = syscall.SIGTERM
	killSignal = syscall.SIGKILL
)

// bytesBuffer is a tiny bytes.Buffer wrapper used to capture ffmpeg stderr
// for error logging without importing bytes everywhere.
type bytesBuffer struct{ b bytes.Buffer }

func (b *bytesBuffer) Write(p []byte) (int, error) { return b.b.Write(p) }
func (b *bytesBuffer) String() string              { return b.b.String() }

// stderrContent extracts the captured stderr from a finished *exec.Cmd.
// We stored a *bytesBuffer on cmd.Stderr before Start; recover it.
func stderrContent(cmd *exec.Cmd) string {
	if buf, ok := cmd.Stderr.(*bytesBuffer); ok {
		s := buf.String()
		if len(s) > 512 {
			return s[:512] + "...(truncated)"
		}
		return s
	}
	return ""
}

// probeDuration returns the track duration in milliseconds via ffprobe.
// Returns 0 (with error) if ffprobe fails — callers treat 0 as "unknown".
func probeDuration(path string, logger *log.Logger) (int, error) {
	fl := logger.WithFunc("probeDuration")
	args := []string{
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	}
	fl.Debug("running ffprobe", "cmd", "ffprobe "+strings.Join(args, " "))
	out, err := exec.Command("ffprobe", args...).Output()
	if err != nil {
		return 0, err
	}
	// ffprobe prints "263.123456\n"
	s := strings.TrimSpace(string(out))
	fl.Debug("ffprobe output", "raw", s)
	if s == "" || s == "N/A" {
		return 0, nil
	}
	secs, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	ms := int(secs * 1000)
	fl.Debug("duration parsed", "seconds", secs, "ms", ms)
	return ms, nil
}
