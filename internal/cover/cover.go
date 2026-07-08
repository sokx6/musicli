// Package cover extracts and renders album covers.
package cover

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
)

// ScaleMode controls how artwork maps into the available terminal cells.
type ScaleMode string

const (
	ScaleFit     ScaleMode = "fit"
	ScaleStretch ScaleMode = "stretch"
)

// Extract loads album art for an audio file. It prefers embedded tag artwork
// and falls back to common image names in the same directory.
func Extract(path string) (image.Image, error) {
	if path == "" {
		return nil, fmt.Errorf("empty audio path")
	}
	if img, err := extractEmbedded(path); err == nil {
		return img, nil
	}
	return extractSidecar(path)
}

func extractEmbedded(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open audio: %w", err)
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return nil, fmt.Errorf("read tags: %w", err)
	}
	pic := m.Picture()
	if pic == nil || len(pic.Data) == 0 {
		return nil, fmt.Errorf("no embedded picture")
	}
	img, _, err := image.Decode(bytes.NewReader(pic.Data))
	if err != nil {
		return nil, fmt.Errorf("decode embedded picture: %w", err)
	}
	return img, nil
}

func extractSidecar(path string) (image.Image, error) {
	dir := filepath.Dir(path)
	names := []string{
		"cover.jpg", "cover.jpeg", "cover.png",
		"folder.jpg", "folder.jpeg", "folder.png",
		"front.jpg", "front.jpeg", "front.png",
	}
	for _, name := range names {
		img, err := decodeImageFile(filepath.Join(dir, name))
		if err == nil {
			return img, nil
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read cover dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if !strings.HasSuffix(name, ".jpg") && !strings.HasSuffix(name, ".jpeg") && !strings.HasSuffix(name, ".png") {
			continue
		}
		img, err := decodeImageFile(filepath.Join(dir, entry.Name()))
		if err == nil {
			return img, nil
		}
	}
	return nil, os.ErrNotExist
}

func decodeImageFile(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	return img, nil
}

// RenderHalfBlock renders an image as terminal text using upper-half block
// cells. The output is exactly width cells by height rows unless width or
// height is zero.
func RenderHalfBlock(img image.Image, width, height int) string {
	return RenderHalfBlockWithScale(img, width, height, ScaleStretch, 0, 0)
}

// RenderHalfBlockWithScale renders an image as terminal text using upper-half
// block cells. Fit mode preserves the image aspect ratio and centers it inside
// the requested bounds; stretch mode fills the full bounds. cellW and cellH
// are the terminal cell pixel dimensions used for aspect ratio calculation;
// zero defaults to 10x20.
func RenderHalfBlockWithScale(img image.Image, width, height int, mode ScaleMode, cellW, cellH int) string {
	if img == nil || width <= 0 || height <= 0 {
		return ""
	}
	drawW, drawH := coverDrawSize(img.Bounds(), width, height, mode, cellW, cellH)
	offsetX := (width - drawW) / 2
	offsetY := (height - drawH) / 2

	var b strings.Builder
	for row := 0; row < height; row++ {
		if row > 0 {
			b.WriteByte('\n')
		}
		styled := false
		for col := 0; col < width; col++ {
			if col < offsetX || col >= offsetX+drawW || row < offsetY || row >= offsetY+drawH {
				if styled {
					b.WriteString("\x1b[0m")
					styled = false
				}
				b.WriteByte(' ')
				continue
			}
			localX := col - offsetX
			localY := row - offsetY
			top := sample(img, localX, localY*2, drawW, drawH*2)
			bottom := sample(img, localX, localY*2+1, drawW, drawH*2)
			b.WriteString(sgr(top, bottom))
			b.WriteRune('▀')
			styled = true
		}
		if styled {
			b.WriteString("\x1b[0m")
		}
	}
	return b.String()
}

func coverDrawSize(bounds image.Rectangle, width, height int, mode ScaleMode, cellW, cellH int) (int, int) {
	if mode == ScaleStretch {
		return width, height
	}
	imgW := bounds.Dx()
	imgH := bounds.Dy()
	if imgW <= 0 || imgH <= 0 {
		return width, height
	}
	if cellW <= 0 {
		cellW = kittyCellPixelWidth
	}
	if cellH <= 0 {
		cellH = kittyCellPixelHeight
	}
	// Compute draw size in actual terminal pixels, then convert to cells.
	availW := width * cellW
	availH := height * cellH
	pxW := availW
	pxH := imgH * pxW / imgW
	if pxH > availH {
		pxH = availH
		pxW = imgW * pxH / imgH
	}
	if pxW < 1 {
		pxW = 1
	}
	if pxH < 1 {
		pxH = 1
	}
	if pxW > availW {
		pxW = availW
	}
	if pxH > availH {
		pxH = availH
	}
	cellsW := pxW / cellW
	cellsH := (pxH + cellH - 1) / cellH
	if cellsW < 1 {
		cellsW = 1
	}
	if cellsH < 1 {
		cellsH = 1
	}
	if cellsW > width {
		cellsW = width
	}
	if cellsH > height {
		cellsH = height
	}
	return cellsW, cellsH
}

func sample(img image.Image, x, y, width, height int) color.RGBA {
	bounds := img.Bounds()
	srcX := bounds.Min.X + x*bounds.Dx()/max(1, width)
	srcY := bounds.Min.Y + y*bounds.Dy()/max(1, height)
	if srcX >= bounds.Max.X {
		srcX = bounds.Max.X - 1
	}
	if srcY >= bounds.Max.Y {
		srcY = bounds.Max.Y - 1
	}
	r, g, b, a := img.At(srcX, srcY).RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}

func sgr(fg, bg color.RGBA) string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm", fg.R, fg.G, fg.B, bg.R, bg.G, bg.B)
}
