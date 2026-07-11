package lyrics

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var ErrNotFound = errors.New("lyrics not found")

// FetchRequest is sent to an external lyric bridge as one JSON object.
type FetchRequest struct {
	Title      string   `json:"title"`
	Artist     string   `json:"artist"`
	Album      string   `json:"album"`
	DurationMS int      `json:"duration_ms"`
	Path       string   `json:"path"`
	Sources    []string `json:"sources"`
}

// FetchResult is a validated lyric returned by an external lyric bridge.
type FetchResult struct {
	Source string
	Timing string
	Raw    string
	Lyric  *Lyric
}

// Fetcher invokes a JSON-lines lyric bridge such as lddc-fetch.
type Fetcher struct {
	Command string
	Timeout time.Duration
}

type bridgeResponse struct {
	Found  bool   `json:"found"`
	Reason string `json:"reason"`
	Source string `json:"source"`
	Format string `json:"format"`
	Timing string `json:"timing"`
	Lyrics string `json:"lyrics"`
}

// Fetch requests lyrics without blocking beyond the configured timeout.
func (f Fetcher) Fetch(ctx context.Context, request FetchRequest) (*FetchResult, error) {
	if f.Command == "" {
		return nil, fmt.Errorf("lyric fetch command is empty")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode lyric fetch request: %w", err)
	}
	timeout := f.Timeout
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, f.Command)
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("lyric fetch timed out: %w", ctx.Err())
		}
		return nil, fmt.Errorf("run lyric fetch command: %w", err)
	}
	var response bridgeResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("decode lyric fetch response: %w", err)
	}
	if !response.Found {
		if response.Reason == "not_found" {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("lyric fetch failed: %s", response.Reason)
	}
	if response.Format != "lrc" || response.Lyrics == "" {
		return nil, fmt.Errorf("unsupported lyric fetch response format %q", response.Format)
	}
	lyric, err := (SPLParser{}).Parse(response.Lyrics)
	if err != nil || len(lyric.Lines) == 0 {
		if err == nil {
			err = errors.New("no lyric lines")
		}
		return nil, fmt.Errorf("parse fetched lyric: %w", err)
	}
	return &FetchResult{Source: response.Source, Timing: response.Timing, Raw: response.Lyrics, Lyric: lyric}, nil
}

// CachePath returns the stable path for lyrics fetched for an audio file.
func CachePath(dir, audioPath string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(audioPath)))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".lrc")
}

// SaveCached atomically stores successfully parsed fetched lyrics.
func SaveCached(dir, audioPath, raw string) (string, error) {
	if _, err := (SPLParser{}).Parse(raw); err != nil {
		return "", fmt.Errorf("parse lyric before caching: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create lyric cache directory: %w", err)
	}
	path := CachePath(dir, audioPath)
	tmp, err := os.CreateTemp(dir, ".lyrics-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create lyric cache file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(raw); err != nil {
		tmp.Close()
		return "", fmt.Errorf("write lyric cache file: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return "", fmt.Errorf("set lyric cache permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close lyric cache file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", fmt.Errorf("replace lyric cache file: %w", err)
	}
	return path, nil
}

// LoadCached reads an earlier successful lyric fetch for an audio file.
func LoadCached(dir, audioPath string) (*Lyric, string, error) {
	path := CachePath(dir, audioPath)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	lyric, err := (SPLParser{}).Parse(string(raw))
	if err != nil {
		return nil, "", fmt.Errorf("parse cached lyric %q: %w", path, err)
	}
	return lyric, path, nil
}
