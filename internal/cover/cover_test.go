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

func TestHalfBlockRendererFitPreservesAspectRatio(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: uint8(y * 24), B: uint8(x * 48), A: 255})
		}
	}

	rendered := RenderHalfBlockWithScale(img, 8, 4, ScaleFit, 0, 0)
	lines := strings.Split(rendered, "\n")
	if len(lines) != 4 {
		t.Fatalf("height = %d, want 4:\n%q", len(lines), rendered)
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != 8 {
			t.Fatalf("line %d width = %d, want 8: %q", i, got, line)
		}
	}
	plain := ansi.Strip(rendered)
	if strings.Contains(plain, "▀▀▀▀▀▀▀▀") {
		t.Fatalf("fit mode should not stretch narrow artwork across full width:\n%s", plain)
	}
	if !strings.Contains(plain, "  ▀▀▀▀  ") {
		t.Fatalf("fit mode should center narrower artwork with padding:\n%s", plain)
	}
	for _, line := range lines {
		if strings.HasSuffix(line, "\x1b[0m") {
			t.Fatalf("fit mode should reset style before right padding, not after it: %q", line)
		}
	}
}

func TestHalfBlockRendererStretchFillsBounds(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: uint8(y * 24), B: uint8(x * 48), A: 255})
		}
	}

	rendered := RenderHalfBlockWithScale(img, 8, 4, ScaleStretch, 0, 0)
	plain := ansi.Strip(rendered)
	if !strings.Contains(plain, "▀▀▀▀▀▀▀▀") {
		t.Fatalf("stretch mode should fill full width:\n%s", plain)
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

func TestCoverDrawSizeDefaultMatchesOldBehavior(t *testing.T) {
	// With default cell size (10x20), the new pixel-based coverDrawSize must
	// produce the same cell counts as the old halfblock-ratio formula.
	cases := []struct {
		name          string
		imgW, imgH    int
		width, height int
	}{
		{"square in square", 8, 8, 8, 8},
		{"square in odd width", 8, 8, 5, 8},
		{"portrait in landscape", 4, 8, 8, 4},
		{"landscape in portrait", 8, 4, 4, 8},
		{"small image large area", 2, 2, 10, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bounds := image.Rect(0, 0, tc.imgW, tc.imgH)
			gotW, gotH := coverDrawSize(bounds, tc.width, tc.height, ScaleFit, 0, 0)

			// Old formula: terminalPixelH = height*2, drawPixelH = imgH*width/imgW
			terminalPixelH := tc.height * 2
			oldDrawW := tc.width
			oldDrawPixelH := tc.imgH * oldDrawW / tc.imgW
			if oldDrawPixelH > terminalPixelH {
				oldDrawPixelH = terminalPixelH
				oldDrawW = tc.imgW * oldDrawPixelH / tc.imgH
			}
			oldDrawH := (oldDrawPixelH + 1) / 2
			if oldDrawW < 1 {
				oldDrawW = 1
			}
			if oldDrawH < 1 {
				oldDrawH = 1
			}
			if oldDrawW > tc.width {
				oldDrawW = tc.width
			}
			if oldDrawH > tc.height {
				oldDrawH = tc.height
			}

			if gotW != oldDrawW || gotH != oldDrawH {
				t.Fatalf("coverDrawSize(%s) = %d,%d; old formula = %d,%d",
					tc.name, gotW, gotH, oldDrawW, oldDrawH)
			}
		})
	}
}

func TestCoverDrawSizeNonSquareCellProducesCorrectAspect(t *testing.T) {
	// A square image in a square cell area should produce a square display
	// when the cell pixel dimensions are correct, even if non-1:2.
	squareImg := image.Rect(0, 0, 100, 100)
	cellW, cellH := 9, 20
	areaW, areaH := 20, 10 // 180x200 actual pixels (nearly square)

	drawW, drawH := coverDrawSize(squareImg, areaW, areaH, ScaleFit, cellW, cellH)
	// Display pixels: drawW*cellW x drawH*cellH — should be roughly square.
	dispW := drawW * cellW
	dispH := drawH * cellH
	if dispW == 0 || dispH == 0 {
		t.Fatalf("zero draw size: %d,%d", drawW, drawH)
	}
	ratio := float64(dispW) / float64(dispH)
	if ratio < 0.95 || ratio > 1.05 {
		t.Fatalf("square image aspect ratio = %.2f (dispW=%d dispH=%d), want ~1.0", ratio, dispW, dispH)
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
