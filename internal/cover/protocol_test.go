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
	if !strings.Contains(seq, "\x1b_Ga=T,t=d,f=100,i=42,c=5,r=6;") {
		t.Fatalf("sequence missing kitty transmit header: %q", seq)
	}
	payload := seq[strings.LastIndex(seq, ";")+1 : strings.LastIndex(seq, "\x1b\\")]
	if _, err := base64.StdEncoding.DecodeString(payload); err != nil {
		t.Fatalf("payload is not base64: %v", err)
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
	payload := seq[strings.LastIndex(seq, ";")+1 : strings.LastIndex(seq, "\x1b\\")]
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("payload is not base64: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("payload is not png: %v", err)
	}
	if decoded.Bounds().Dx() != 5 || decoded.Bounds().Dy() != 12 {
		t.Fatalf("decoded bounds = %v, want 5x12", decoded.Bounds())
	}
}

func TestClearKittyImage(t *testing.T) {
	if got := ClearKittyImage(7); got != "\x1b_Ga=d,d=I,i=7\x1b\\" {
		t.Fatalf("ClearKittyImage() = %q", got)
	}
}
