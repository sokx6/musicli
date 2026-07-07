package cover

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHalfBlockRendererStaysWithinBounds(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 32), G: uint8(y * 32), B: 80, A: 255})
		}
	}

	rendered := RenderHalfBlock(img, 5, 3)
	lines := strings.Split(rendered, "\n")
	if len(lines) != 3 {
		t.Fatalf("height = %d, want 3:\n%q", len(lines), rendered)
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != 5 {
			t.Fatalf("line %d width = %d, want 5: %q", i, got, line)
		}
	}
	if !strings.Contains(rendered, "▀") {
		t.Fatalf("halfblock render missing upper-half blocks: %q", rendered)
	}
}

func TestHalfBlockRendererHandlesEmptyBounds(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if got := RenderHalfBlock(img, 0, 3); got != "" {
		t.Fatalf("zero width render = %q, want empty", got)
	}
	if got := RenderHalfBlock(img, 3, 0); got != "" {
		t.Fatalf("zero height render = %q, want empty", got)
	}
	if got := RenderHalfBlock(nil, 3, 3); got != "" {
		t.Fatalf("nil image render = %q, want empty", got)
	}
}

func TestExtractLoadsSidecarCover(t *testing.T) {
	dir := t.TempDir()
	audioPath := filepath.Join(dir, "song.flac")
	if err := os.WriteFile(audioPath, []byte("not real audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	coverPath := filepath.Join(dir, "cover.png")
	f, err := os.Create(coverPath)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 3))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := Extract(audioPath)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if got.Bounds().Dx() != 2 || got.Bounds().Dy() != 3 {
		t.Fatalf("bounds = %v, want 2x3", got.Bounds())
	}
}
