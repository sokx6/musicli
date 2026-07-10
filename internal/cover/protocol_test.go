package cover

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

// extractKittyPayload extracts and concatenates the base64 payload from a
// (possibly chunked) kitty graphics sequence. Each APC chunk has the form
// \x1b_G...;<payload>\x1b\\. The payloads are concatenated to reconstruct
// the full base64 string.
func extractKittyPayload(seq string) string {
	var payload strings.Builder
	for {
		// Find the next APC start: \x1b_G
		apcStart := strings.Index(seq, "\x1b_G")
		if apcStart < 0 {
			break
		}
		seq = seq[apcStart+3:] // skip \x1b_G
		// Find the APC terminator: \x1b\\
		apcEnd := strings.Index(seq, "\x1b\\")
		if apcEnd < 0 {
			break
		}
		apcBody := seq[:apcEnd]
		seq = seq[apcEnd+2:] // skip \x1b\\
		// The payload is after the last ; in the APC body.
		semi := strings.LastIndex(apcBody, ";")
		if semi >= 0 {
			payload.WriteString(apcBody[semi+1:])
		}
	}
	return payload.String()
}

func TestSelectProtocolUsesKittyOnlyWhenAvailable(t *testing.T) {
	cases := []struct {
		name  string
		want  string
		env   map[string]string
		proto string
	}{
		{name: "explicit halfblock", proto: "halfblock", want: "halfblock"},
		{name: "explicit kitty", proto: "kitty", want: "kitty"},
		{name: "explicit sixel falls back", proto: "sixel", env: map[string]string{"KITTY_WINDOW_ID": "1"}, want: "halfblock"},
		{name: "explicit iterm falls back", proto: "iterm", env: map[string]string{"KITTY_WINDOW_ID": "1"}, want: "halfblock"},
		{name: "auto kitty window", proto: "auto", env: map[string]string{"KITTY_WINDOW_ID": "1"}, want: "kitty"},
		{name: "auto kitty term", proto: "auto", env: map[string]string{"TERM": "xterm-kitty"}, want: "kitty"},
		{name: "tmux falls back", proto: "auto", env: map[string]string{"KITTY_WINDOW_ID": "1", "TMUX": "/tmp/tmux"}, want: "halfblock"},
		{name: "unknown falls back", proto: "auto", want: "halfblock"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SelectProtocol(tc.proto, func(key string) string { return tc.env[key] })
			if got != tc.want {
				t.Fatalf("SelectProtocol() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestKittyRenderSequenceContainsDeletePositionAndPayload(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})

	seq, err := RenderKitty(img, KittyPlacement{
		ID:     42,
		X:      3,
		Y:      4,
		Width:  5,
		Height: 6,
		Scale:  ScaleFit,
	})
	if err != nil {
		t.Fatalf("RenderKitty: %v", err)
	}
	if !strings.Contains(seq, "\x1b_Ga=d,d=I,i=42\x1b\\") {
		t.Fatalf("sequence missing delete command: %q", seq)
	}
	if !strings.Contains(seq, "\x1b[4;3H") {
		t.Fatalf("sequence missing 1-based cursor position: %q", seq)
	}
	if !strings.Contains(seq, "\x1b_Ga=T,t=d,f=100,i=42,c=5,r=6,z=1;") {
		t.Fatalf("sequence missing kitty transmit header: %q", seq)
	}
	payload := extractKittyPayload(seq)
	if _, err := base64.StdEncoding.DecodeString(payload); err != nil {
		t.Fatalf("payload is not base64: %v", err)
	}
}

func TestKittyRenderPreservesTerminalCursor(t *testing.T) {
	seq, err := RenderKitty(image.NewRGBA(image.Rect(0, 0, 2, 2)), KittyPlacement{
		ID: 1, X: 1, Y: 3, Width: 4, Height: 4, Scale: ScaleFit,
	})
	if err != nil {
		t.Fatalf("RenderKitty: %v", err)
	}
	if !strings.HasPrefix(seq, "\x1b7") || !strings.HasSuffix(seq, "\x1b8") {
		t.Fatalf("kitty cover must save and restore cursor: %q", seq)
	}
}

func TestKittyProgressLineUsesSinglePixelAndNoDelete(t *testing.T) {
	seq, err := RenderKittyProgressLine(
		7, 2, 3, 4, 10, 20, 13, 1,
		color.RGBA{R: 1, G: 2, B: 3, A: 255},
		color.RGBA{R: 4, G: 5, B: 6, A: 255},
	)
	if err != nil {
		t.Fatalf("RenderKittyProgressLine: %v", err)
	}
	if strings.Contains(seq, "a=d") {
		t.Fatalf("progress line must not delete an image before drawing: %q", seq)
	}
	if !strings.HasPrefix(seq, "\x1b7") || !strings.HasSuffix(seq, "\x1b8") {
		t.Fatalf("progress line must save and restore cursor: %q", seq)
	}
	if !strings.Contains(seq, "\x1b[3;2H") || !strings.Contains(seq, "i=7,c=4,r=1,z=2;") {
		t.Fatalf("progress line placement missing: %q", seq)
	}
	raw, err := base64.StdEncoding.DecodeString(extractKittyPayload(seq))
	if err != nil {
		t.Fatalf("decode progress payload: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode progress png: %v", err)
	}
	if got, want := img.Bounds(), image.Rect(0, 0, 40, 20); got != want {
		t.Fatalf("progress bounds = %v, want %v", got, want)
	}
	if got := color.NRGBAModel.Convert(img.At(12, 10)); got != (color.NRGBA{R: 1, G: 2, B: 3, A: 255}) {
		t.Fatalf("filled progress pixel = %#v", got)
	}
	if got := color.NRGBAModel.Convert(img.At(13, 10)); got != (color.NRGBA{R: 4, G: 5, B: 6, A: 255}) {
		t.Fatalf("unfilled progress pixel = %#v", got)
	}
}

func TestKittyProgressLineThickness(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cellH     int
		thickness int
		firstRow  int
		rowCount  int
	}{
		{name: "centered three pixels", cellH: 8, thickness: 3, firstRow: 3, rowCount: 3},
		{name: "clamped to cell height", cellH: 4, thickness: 8, firstRow: 0, rowCount: 4},
		{name: "minimum one pixel", cellH: 6, thickness: 0, firstRow: 3, rowCount: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seq, err := RenderKittyProgressLine(
				7, 1, 1, 2, 10, tc.cellH, 7, tc.thickness,
				color.RGBA{R: 1, G: 2, B: 3, A: 255},
				color.RGBA{R: 4, G: 5, B: 6, A: 255},
			)
			if err != nil {
				t.Fatalf("RenderKittyProgressLine: %v", err)
			}
			raw, err := base64.StdEncoding.DecodeString(extractKittyPayload(seq))
			if err != nil {
				t.Fatalf("decode progress payload: %v", err)
			}
			img, err := png.Decode(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("decode progress png: %v", err)
			}

			for y := 0; y < tc.cellH; y++ {
				wantOpaque := y >= tc.firstRow && y < tc.firstRow+tc.rowCount
				for _, x := range []int{2, 12} { // Played and remaining segments.
					alpha := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA).A
					if got := alpha > 0; got != wantOpaque {
						t.Fatalf("pixel (%d,%d) opaque = %t, want %t", x, y, got, wantOpaque)
					}
				}
			}
		})
	}
}

func TestKittyRenderResamplesToCellPixelCanvas(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 10), G: uint8(y * 10), A: 255})
		}
	}

	seq, err := RenderKitty(img, KittyPlacement{
		ID:     42,
		X:      1,
		Y:      1,
		Width:  5,
		Height: 6,
		Scale:  ScaleStretch,
	})
	if err != nil {
		t.Fatalf("RenderKitty: %v", err)
	}
	payload := extractKittyPayload(seq)
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("payload is not base64: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("payload is not png: %v", err)
	}
	if decoded.Bounds().Dx() != 50 || decoded.Bounds().Dy() != 120 {
		t.Fatalf("decoded bounds = %v, want 50x120", decoded.Bounds())
	}
}

func TestKittyFitDrawnAreaIsOpaque(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 120, G: 80, B: 40, A: 255})
		}
	}

	seq, err := RenderKitty(img, KittyPlacement{
		ID:     42,
		X:      1,
		Y:      1,
		Width:  8,
		Height: 4,
		Scale:  ScaleFit,
	})
	if err != nil {
		t.Fatalf("RenderKitty: %v", err)
	}
	payload := extractKittyPayload(seq)
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("payload is not base64: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("payload is not png: %v", err)
	}
	for _, point := range []image.Point{
		{X: decoded.Bounds().Dx()/2 - 1, Y: decoded.Bounds().Min.Y},
		{X: decoded.Bounds().Dx()/2 + 1, Y: decoded.Bounds().Min.Y},
		{X: decoded.Bounds().Dx()/2 - 1, Y: decoded.Bounds().Max.Y - 1},
		{X: decoded.Bounds().Dx()/2 + 1, Y: decoded.Bounds().Max.Y - 1},
	} {
		_, _, _, a := decoded.At(point.X, point.Y).RGBA()
		if a != 0xffff {
			t.Fatalf("drawn pixel %v alpha = %#x, want opaque", point, a)
		}
	}
}

func TestKittyFitUsesPixelAspectInsideCanvas(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 40, B: 20, A: 255})
		}
	}

	canvas := imageCanvas(img, 10, 5, ScaleFit, 0, 0)

	for _, point := range []image.Point{
		canvas.Bounds().Min,
		{X: canvas.Bounds().Max.X - 1, Y: canvas.Bounds().Min.Y},
		{X: canvas.Bounds().Min.X, Y: canvas.Bounds().Max.Y - 1},
		{X: canvas.Bounds().Max.X - 1, Y: canvas.Bounds().Max.Y - 1},
	} {
		r, g, b, _ := canvas.At(point.X, point.Y).RGBA()
		if r == 0 && g == 0 && b == 0 {
			t.Fatalf("square artwork should fill a square kitty pixel canvas; corner %v is still background", point)
		}
	}
}

func TestKittyFitDoesNotPaintLetterboxBackground(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 40, B: 20, A: 255})
		}
	}

	canvas := imageCanvas(img, 10, 10, ScaleFit, 0, 0)
	bounds := canvas.Bounds()
	for _, point := range []image.Point{
		{X: bounds.Min.X, Y: bounds.Min.Y},
		{X: bounds.Max.X - 1, Y: bounds.Min.Y},
		{X: bounds.Min.X, Y: bounds.Max.Y - 1},
		{X: bounds.Max.X - 1, Y: bounds.Max.Y - 1},
	} {
		_, _, _, a := canvas.At(point.X, point.Y).RGBA()
		if a != 0 {
			t.Fatalf("fit letterbox pixel %v alpha = %#x, want transparent", point, a)
		}
	}
}

func TestKittyCanvasUsesNonDefaultCellSize(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 40, B: 20, A: 255})
		}
	}

	canvas := imageCanvas(img, 5, 3, ScaleStretch, 9, 20)
	if got := canvas.Bounds().Dx(); got != 45 {
		t.Fatalf("canvas width = %d, want 45 (5*9)", got)
	}
	if got := canvas.Bounds().Dy(); got != 60 {
		t.Fatalf("canvas height = %d, want 60 (3*20)", got)
	}
}

func TestClearKittyImage(t *testing.T) {
	if got := ClearKittyImage(7); got != "\x1b_Ga=d,d=I,i=7\x1b\\" {
		t.Fatalf("ClearKittyImage() = %q", got)
	}
}

func TestKittyRenderChunksLargePayload(t *testing.T) {
	// Large enough canvas (100*10 × 50*20 = 1000×1000 px) that the base64
	// PNG payload exceeds kittyMaxChunkSize (4096).
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x*7 + y*3), G: uint8(x*5 + y*11), B: uint8(x*13 + y*7), A: 255})
		}
	}

	seq, err := RenderKitty(img, KittyPlacement{
		ID:     1,
		X:      1,
		Y:      1,
		Width:  100,
		Height: 50,
		Scale:  ScaleStretch,
	})
	if err != nil {
		t.Fatalf("RenderKitty: %v", err)
	}

	// First chunk: full command header + m=1 (more data follows).
	if !strings.Contains(seq, "\x1b_Ga=T,t=d,f=100,i=1,c=100,r=50,z=1,m=1;") {
		t.Fatalf("first chunk must have m=1 when payload is chunked: %q", seq)
	}

	// Middle chunks: only m=1, no i=ID (per kitty spec).
	if !strings.Contains(seq, "\x1b_Gm=1;") {
		t.Fatalf("large payload should have m=1 continuation chunks: %q", seq)
	}

	// Last chunk: m=0 (sequence complete).
	if !strings.Contains(seq, "\x1b_Gm=0;") {
		t.Fatalf("chunked payload must end with m=0 terminator: %q", seq)
	}

	// Subsequent chunks must NOT contain i=ID (spec: correlation is by ordering).
	if strings.Contains(seq, "\x1b_Gm=1,i=") || strings.Contains(seq, "\x1b_Gm=0,i=") {
		t.Fatalf("continuation chunks must not have i=ID: %q", seq)
	}

	// The concatenated payload must be valid base64 that decodes to a PNG.
	payload := extractKittyPayload(seq)
	if len(payload) <= kittyMaxChunkSize {
		t.Fatalf("payload should exceed chunk size: got %d bytes", len(payload))
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("concatenated payload is not base64: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(raw)); err != nil {
		t.Fatalf("concatenated payload is not png: %v", err)
	}

	// Each individual chunk payload must be <= kittyMaxChunkSize.
	parts := strings.Split(seq, "\x1b_G")
	for _, part := range parts {
		if part == "" || strings.HasPrefix(part, "a=d") {
			continue
		}
		semi := strings.Index(part, ";")
		if semi < 0 {
			continue
		}
		st := strings.Index(part[semi+1:], "\x1b\\")
		if st < 0 {
			continue
		}
		chunkPayload := part[semi+1 : semi+1+st]
		if len(chunkPayload) > kittyMaxChunkSize {
			t.Fatalf("chunk payload %d bytes exceeds max %d", len(chunkPayload), kittyMaxChunkSize)
		}
	}
}

func TestKittyRenderSmallPayloadNotChunked(t *testing.T) {
	// Small image: base64 payload fits in a single chunk (<= 4096 bytes).
	// Must NOT use m=1/m=0 — just a plain single APC.
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})

	seq, err := RenderKitty(img, KittyPlacement{
		ID:     1,
		X:      1,
		Y:      1,
		Width:  2,
		Height: 2,
		Scale:  ScaleStretch,
	})
	if err != nil {
		t.Fatalf("RenderKitty: %v", err)
	}
	if strings.Contains(seq, "m=1") || strings.Contains(seq, "m=0") {
		t.Fatalf("small payload should not be chunked (no m= flag): %q", seq)
	}
	if !strings.Contains(seq, "\x1b_Ga=T,t=d,f=100,i=1,c=2,r=2,z=1;") {
		t.Fatalf("small payload should have standard transmit header: %q", seq)
	}
}
