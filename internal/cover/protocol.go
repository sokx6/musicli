package cover

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
)

const (
	ProtocolAuto      = "auto"
	ProtocolHalfBlock = "halfblock"
	ProtocolKitty     = "kitty"

	kittyCellPixelWidth  = 10
	kittyCellPixelHeight = 20

	// kittyMaxChunkSize is the maximum base64 payload per APC. The kitty
	// graphics protocol recommends chunking payloads larger than this to
	// avoid overwhelming the terminal's APC parser, especially when other
	// escape sequences follow in the same renderer flush.
	kittyMaxChunkSize = 4096
)

// SelectProtocol resolves a configured protocol using environment values.
// It does not query the terminal, so it is safe to call inside a Bubble Tea app.
func SelectProtocol(configured string, getenv func(string) string) string {
	if configured == ProtocolKitty || configured == ProtocolHalfBlock {
		return configured
	}
	if configured == "sixel" || configured == "iterm" {
		return ProtocolHalfBlock
	}
	if getenv("TMUX") != "" {
		return ProtocolHalfBlock
	}
	term := getenv("TERM")
	if getenv("KITTY_WINDOW_ID") != "" || term == "xterm-kitty" {
		return ProtocolKitty
	}
	return ProtocolHalfBlock
}

// KittyPlacement describes where a kitty image should be drawn.
type KittyPlacement struct {
	ID       int
	X        int
	Y        int
	Width    int
	Height   int
	Scale    ScaleMode
	CellW    int // terminal cell pixel width for aspect ratio; 0 = default 10
	CellH    int // terminal cell pixel height for aspect ratio; 0 = default 20
	TopAlign bool
}

// ClearKittyImage returns a kitty graphics command that deletes one image id.
func ClearKittyImage(id int) string {
	if id <= 0 {
		return ""
	}
	return fmt.Sprintf("\x1b_Ga=d,d=I,i=%d\x1b\\", id)
}

// RenderKitty returns escape sequences that delete and redraw a kitty image.
// X and Y are 1-based terminal coordinates.
//
// Large base64 payloads are split into <= kittyMaxChunkSize byte APC chunks
// using the m=1/m=0 continuation protocol, per the kitty graphics protocol
// spec. Without chunking, a single massive APC can overwhelm the terminal's
// parser when SGR diff output follows in the same flush — causing base64 to
// leak as plain text on screen.
func RenderKitty(img image.Image, placement KittyPlacement) (string, error) {
	if img == nil || placement.ID <= 0 || placement.Width <= 0 || placement.Height <= 0 {
		return ClearKittyImage(placement.ID), nil
	}

	renderImg := imageCanvasAligned(img, placement.Width, placement.Height, placement.Scale, placement.CellW, placement.CellH, placement.TopAlign)

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, renderImg); err != nil {
		return "", fmt.Errorf("encode kitty png: %w", err)
	}
	payload := base64.StdEncoding.EncodeToString(pngBuf.Bytes())

	var b strings.Builder
	b.WriteString("\x1b7")
	b.WriteString(ClearKittyImage(placement.ID))
	b.WriteString(fmt.Sprintf("\x1b[%d;%dH", placement.Y, placement.X))

	if len(payload) <= kittyMaxChunkSize {
		// Single chunk — no m flag needed (default m=0 = complete message).
		b.WriteString(fmt.Sprintf("\x1b_Ga=T,t=d,f=100,i=%d,c=%d,r=%d,z=1;", placement.ID, placement.Width, placement.Height))
		b.WriteString(payload)
		b.WriteString("\x1b\\")
	} else {
		// Chunked transmission: first chunk has m=1 (more data follows),
		// middle chunks have m=1, last chunk has m=0 (sequence complete).
		// Per kitty spec: only m is needed in continuation chunks (no i=ID).
		b.WriteString(fmt.Sprintf("\x1b_Ga=T,t=d,f=100,i=%d,c=%d,r=%d,z=1,m=1;", placement.ID, placement.Width, placement.Height))
		b.WriteString(payload[:kittyMaxChunkSize])
		b.WriteString("\x1b\\")

		remaining := payload[kittyMaxChunkSize:]
		for len(remaining) > kittyMaxChunkSize {
			b.WriteString("\x1b_Gm=1;")
			b.WriteString(remaining[:kittyMaxChunkSize])
			b.WriteString("\x1b\\")
			remaining = remaining[kittyMaxChunkSize:]
		}

		// Last chunk: m=0 signals the sequence is complete.
		b.WriteString("\x1b_Gm=0;")
		b.WriteString(remaining)
		b.WriteString("\x1b\\")
	}
	b.WriteString("\x1b8")

	return b.String(), nil
}

// RenderKittyProgressLine draws a one-pixel line over a terminal row without
// deleting an existing image. Callers can display a replacement first, then
// delete the previous image ID to avoid a visible blank frame.
func RenderKittyProgressLine(id, x, y, width, cellW, cellH, playedPixels, thickness int, playedColor, remainingColor color.Color) (string, error) {
	return RenderKittyGradientProgressLine(id, x, y, width, cellW, cellH, playedPixels, thickness, []color.Color{playedColor}, remainingColor)
}

// RenderKittyGradientProgressLine draws a pixel-level progress gradient. The
// palette is interpolated from left to right across the played portion.
func RenderKittyGradientProgressLine(id, x, y, width, cellW, cellH, playedPixels, thickness int, gradient []color.Color, remainingColor color.Color) (string, error) {
	if id <= 0 || x <= 0 || y <= 0 || width <= 0 {
		return "", nil
	}
	if cellW <= 0 {
		cellW = kittyCellPixelWidth
	}
	if cellH <= 0 {
		cellH = kittyCellPixelHeight
	}
	pixelWidth := width * cellW
	if playedPixels < 0 {
		playedPixels = 0
	}
	if playedPixels > pixelWidth {
		playedPixels = pixelWidth
	}
	if thickness < 1 {
		thickness = 1
	}
	if thickness > cellH {
		thickness = cellH
	}

	line := image.NewRGBA(image.Rect(0, 0, pixelWidth, cellH))
	// Anchor odd thicknesses on the same center row used by the original 1px
	// line, so the default remains visually unchanged.
	startY := cellH/2 - thickness/2
	remaining := color.NRGBAModel.Convert(remainingColor)
	for py := startY; py < startY+thickness; py++ {
		for px := 0; px < pixelWidth; px++ {
			line.Set(px, py, remaining)
		}
	}
	for py := startY; py < startY+thickness; py++ {
		for px := 0; px < playedPixels; px++ {
			line.Set(px, py, gradientColor(gradient, float64(px)/float64(max(1, pixelWidth-1))))
		}
	}

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, line); err != nil {
		return "", fmt.Errorf("encode kitty progress line: %w", err)
	}
	payload := base64.StdEncoding.EncodeToString(pngBuf.Bytes())
	if len(payload) > kittyMaxChunkSize {
		return "", fmt.Errorf("kitty progress line payload too large: %d", len(payload))
	}

	// Bubble Tea owns the terminal cursor for its diff renderer. Preserve its
	// position around the absolute placement command so this overlay cannot
	// redirect the next text update into the player bar.
	return fmt.Sprintf("\x1b7\x1b[%d;%dH\x1b_Ga=T,t=d,f=100,i=%d,c=%d,r=1,z=2;%s\x1b\\\x1b8", y, x, id, width, payload), nil
}

func gradientColor(stops []color.Color, position float64) color.Color {
	if len(stops) == 0 {
		return color.RGBA{}
	}
	if len(stops) == 1 || position <= 0 {
		return stops[0]
	}
	if position >= 1 {
		return stops[len(stops)-1]
	}
	scaled := position * float64(len(stops)-1)
	index := int(scaled)
	fraction := scaled - float64(index)
	a := color.NRGBAModel.Convert(stops[index]).(color.NRGBA)
	b := color.NRGBAModel.Convert(stops[index+1]).(color.NRGBA)
	lerp := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*fraction + 0.5) }
	return color.NRGBA{R: lerp(a.R, b.R), G: lerp(a.G, b.G), B: lerp(a.B, b.B), A: lerp(a.A, b.A)}
}

func imageCanvas(img image.Image, width, height int, scale ScaleMode, cellW, cellH int) image.Image {
	return imageCanvasAligned(img, width, height, scale, cellW, cellH, false)
}

func imageCanvasAligned(img image.Image, width, height int, scale ScaleMode, cellW, cellH int, topAlign bool) image.Image {
	if cellW <= 0 {
		cellW = kittyCellPixelWidth
	}
	if cellH <= 0 {
		cellH = kittyCellPixelHeight
	}
	canvasW := width * cellW
	canvasH := height * cellH
	drawW, drawH := pixelDrawSize(img.Bounds(), canvasW, canvasH, scale)
	canvas := image.NewRGBA(image.Rect(0, 0, canvasW, canvasH))
	offsetX := (canvasW - drawW) / 2
	offsetY := (canvasH - drawH) / 2
	if topAlign {
		offsetY = 0
	}
	for row := 0; row < drawH; row++ {
		for col := 0; col < drawW; col++ {
			canvas.Set(offsetX+col, offsetY+row, sample(img, col, row, drawW, drawH))
		}
	}
	return canvas
}

func pixelDrawSize(bounds image.Rectangle, width, height int, mode ScaleMode) (int, int) {
	if mode == ScaleStretch {
		return width, height
	}
	imgW := bounds.Dx()
	imgH := bounds.Dy()
	if imgW <= 0 || imgH <= 0 {
		return width, height
	}

	drawW := width
	drawH := imgH * drawW / imgW
	if drawH > height {
		drawH = height
		drawW = imgW * drawH / imgH
	}
	if drawW < 1 {
		drawW = 1
	}
	if drawH < 1 {
		drawH = 1
	}
	if drawW > width {
		drawW = width
	}
	if drawH > height {
		drawH = height
	}
	return drawW, drawH
}
