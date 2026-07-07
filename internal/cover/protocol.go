package cover

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"strings"
)

const (
	ProtocolAuto      = "auto"
	ProtocolHalfBlock = "halfblock"
	ProtocolKitty     = "kitty"
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
	ID     int
	X      int
	Y      int
	Width  int
	Height int
	Scale  ScaleMode
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
func RenderKitty(img image.Image, placement KittyPlacement) (string, error) {
	if img == nil || placement.ID <= 0 || placement.Width <= 0 || placement.Height <= 0 {
		return ClearKittyImage(placement.ID), nil
	}

	renderImg := imageCanvas(img, placement.Width, placement.Height, placement.Scale)

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, renderImg); err != nil {
		return "", fmt.Errorf("encode kitty png: %w", err)
	}
	payload := base64.StdEncoding.EncodeToString(pngBuf.Bytes())

	var b strings.Builder
	b.WriteString(ClearKittyImage(placement.ID))
	b.WriteString(fmt.Sprintf("\x1b[%d;%dH", placement.Y, placement.X))
	b.WriteString(fmt.Sprintf("\x1b_Ga=T,t=d,f=100,i=%d,c=%d,r=%d;", placement.ID, placement.Width, placement.Height))
	b.WriteString(payload)
	b.WriteString("\x1b\\")
	return b.String(), nil
}

func imageCanvas(img image.Image, width, height int, scale ScaleMode) image.Image {
	drawW, drawH := coverDrawSize(img.Bounds(), width, height, scale)
	canvas := image.NewRGBA(image.Rect(0, 0, width, height*2))
	offsetX := (width - drawW) / 2
	offsetY := (height - drawH) / 2
	for row := 0; row < drawH*2; row++ {
		for col := 0; col < drawW; col++ {
			canvas.Set(offsetX+col, offsetY*2+row, sample(img, col, row, drawW, drawH*2))
		}
	}
	return canvas
}
