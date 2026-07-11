package theme

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCustomThemeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.theme")
	raw := `[meta]
name = "Custom"
[colors]
bg = "#101112"
fg = "#f0f1f2"
muted = "#303132"
accent = "#4060a0"
subtle = "#202122"
highlight = "#a04080"
[gradients]
progress = ["#102030", "#405060"]
spectrum = ["#111111", "#222222", "#333333"]
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	theme, warnings := Load(path, ModeDark)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if theme.Name != "Custom" || colorHex(theme.Accent) != "#4060a0" {
		t.Fatalf("theme = %#v, want custom palette", theme)
	}
	if len(theme.ProgressGradient) != 2 || len(theme.SpectrumGradient) != 3 {
		t.Fatalf("gradient lengths = %d/%d, want 2/3", len(theme.ProgressGradient), len(theme.SpectrumGradient))
	}
}

func TestLoadThemeFallsBackPerInvalidField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.theme")
	if err := os.WriteFile(path, []byte("[colors]\naccent = \"blue\"\n[gradients]\nprogress = [\"#123456\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, warnings := Load(path, ModeLight)
	base := DefaultForMode(ModeLight)
	if colorHex(loaded.Accent) != colorHex(base.Accent) || len(loaded.ProgressGradient) != len(base.ProgressGradient) {
		t.Fatalf("invalid fields did not fall back: %#v", loaded)
	}
	if len(warnings) < 2 {
		t.Fatalf("warnings = %v, want invalid color and gradient warnings", warnings)
	}
}

func TestGradientAtInterpolatesStops(t *testing.T) {
	gradient := []color.Color{color.RGBA{R: 0, A: 255}, color.RGBA{R: 255, B: 255, A: 255}}
	got := GradientAt(gradient, 0.5)
	r, g, b, _ := got.RGBA()
	if r>>8 < 126 || r>>8 > 129 || g != 0 || b>>8 < 126 || b>>8 > 129 {
		t.Fatalf("midpoint = %#x %#x %#x, want purple", r, g, b)
	}
}

func TestResolveModeHonorsFixedModes(t *testing.T) {
	if got, warnings := ResolveMode("light"); got != ModeLight || len(warnings) != 0 {
		t.Fatalf("light = %v, %v", got, warnings)
	}
	if got, warnings := ResolveMode("dark"); got != ModeDark || len(warnings) != 0 {
		t.Fatalf("dark = %v, %v", got, warnings)
	}
}

func colorHex(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8))
}
