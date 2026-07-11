// Package theme holds the color palette and theme model for musicli.
//
// Phase 3 ships a minimal hardcoded dark palette. Phase 10 adds system
// detection (XDG portal / gsettings / OSC11), TOML customization, and a
// light palette. The Theme struct exists from day one so styles.go binds
// to it without a later refactor (oracle Adj-A).
package theme

import (
	"fmt"
	"image/color"
	"math"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/pelletier/go-toml/v2"
)

// Mode is dark or light.
type Mode int

const (
	ModeDark Mode = iota
	ModeLight
)

// Theme is a color palette + derived lipgloss styles. All UI code reads
// from this struct; swapping the struct re-skins the whole app.
type Theme struct {
	Name             string
	Mode             Mode
	Bg               color.Color
	Fg               color.Color
	Muted            color.Color
	Accent           color.Color
	Subtle           color.Color
	Highlight        color.Color
	ProgressGradient []color.Color
	SpectrumGradient []color.Color
}

// Default returns the built-in dark theme.
func Default() *Theme {
	return DefaultForMode(ModeDark)
}

// DefaultForMode returns a complete built-in palette for the requested mode.
func DefaultForMode(mode Mode) *Theme {
	if mode == ModeLight {
		return &Theme{
			Name: "Default Light", Mode: ModeLight,
			Bg: lipgloss.Color("#f5f7fb"), Fg: lipgloss.Color("#26324a"),
			Muted: lipgloss.Color("#66728a"), Accent: lipgloss.Color("#2463c5"),
			Subtle: lipgloss.Color("#dce5f2"), Highlight: lipgloss.Color("#8a4bbd"),
			ProgressGradient: []color.Color{lipgloss.Color("#2463c5"), lipgloss.Color("#8a4bbd"), lipgloss.Color("#c34b72")},
			SpectrumGradient: []color.Color{lipgloss.Color("#66728a"), lipgloss.Color("#2463c5"), lipgloss.Color("#8a4bbd"), lipgloss.Color("#c34b72")},
		}
	}
	return &Theme{
		Name:             "Default Dark",
		Mode:             ModeDark,
		Bg:               lipgloss.Color("#1a1b26"),
		Fg:               lipgloss.Color("#c0caf5"),
		Muted:            lipgloss.Color("#565f89"),
		Accent:           lipgloss.Color("#7aa2f7"),
		Subtle:           lipgloss.Color("#292e42"),
		Highlight:        lipgloss.Color("#bb9af7"),
		ProgressGradient: []color.Color{lipgloss.Color("#7aa2f7"), lipgloss.Color("#bb9af7"), lipgloss.Color("#f7768e")},
		SpectrumGradient: []color.Color{lipgloss.Color("#565f89"), lipgloss.Color("#7aa2f7"), lipgloss.Color("#bb9af7"), lipgloss.Color("#f7768e")},
	}
}

// ProgressColor returns the accent color for progress bars.
func (t *Theme) ProgressColor() color.Color { return t.Accent }

type fileTheme struct {
	Meta struct {
		Name string `toml:"name"`
	} `toml:"meta"`
	Colors struct {
		Bg        string `toml:"bg"`
		Fg        string `toml:"fg"`
		Muted     string `toml:"muted"`
		Accent    string `toml:"accent"`
		Subtle    string `toml:"subtle"`
		Highlight string `toml:"highlight"`
	} `toml:"colors"`
	Gradients struct {
		Progress []string `toml:"progress"`
		Spectrum []string `toml:"spectrum"`
	} `toml:"gradients"`
}

// Load reads a .theme TOML file. Empty paths retain the built-in palette.
// Bad fields fall back independently so a theme never prevents startup.
func Load(path string, mode Mode) (*Theme, []string) {
	result := DefaultForMode(mode)
	if path == "" {
		return result, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return result, []string{fmt.Sprintf("theme %q: %v; using built-in", path, err)}
	}
	var parsed fileTheme
	if err := toml.Unmarshal(raw, &parsed); err != nil {
		return result, []string{fmt.Sprintf("theme %q: %v; using built-in", path, err)}
	}
	var warnings []string
	if parsed.Meta.Name != "" {
		result.Name = parsed.Meta.Name
	}
	set := func(field, value string, dst *color.Color) {
		if value == "" {
			return
		}
		c, err := parseHex(value)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("theme %s %q invalid; using default", field, value))
			return
		}
		*dst = c
	}
	set("colors.bg", parsed.Colors.Bg, &result.Bg)
	set("colors.fg", parsed.Colors.Fg, &result.Fg)
	set("colors.muted", parsed.Colors.Muted, &result.Muted)
	set("colors.accent", parsed.Colors.Accent, &result.Accent)
	set("colors.subtle", parsed.Colors.Subtle, &result.Subtle)
	set("colors.highlight", parsed.Colors.Highlight, &result.Highlight)
	if g, ok := parseGradient(parsed.Gradients.Progress); ok {
		result.ProgressGradient = g
	} else if len(parsed.Gradients.Progress) > 0 {
		warnings = append(warnings, "theme gradients.progress invalid; using default")
	}
	if g, ok := parseGradient(parsed.Gradients.Spectrum); ok {
		result.SpectrumGradient = g
	} else if len(parsed.Gradients.Spectrum) > 0 {
		warnings = append(warnings, "theme gradients.spectrum invalid; using default")
	}
	return result, warnings
}

func parseHex(value string) (color.Color, error) {
	if len(value) != 7 || value[0] != '#' {
		return nil, fmt.Errorf("expected #RRGGBB")
	}
	var r, g, b uint8
	if _, err := fmt.Sscanf(strings.ToLower(value), "#%02x%02x%02x", &r, &g, &b); err != nil {
		return nil, err
	}
	return color.RGBA{R: r, G: g, B: b, A: 255}, nil
}

func parseGradient(values []string) ([]color.Color, bool) {
	if len(values) < 2 {
		return nil, false
	}
	gradient := make([]color.Color, len(values))
	for i, value := range values {
		c, err := parseHex(value)
		if err != nil {
			return nil, false
		}
		gradient[i] = c
	}
	return gradient, true
}

// GradientAt interpolates a multi-stop gradient at a normalized position.
func GradientAt(stops []color.Color, position float64) color.Color {
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
	index := int(math.Floor(scaled))
	fraction := scaled - float64(index)
	a := color.NRGBAModel.Convert(stops[index]).(color.NRGBA)
	b := color.NRGBAModel.Convert(stops[index+1]).(color.NRGBA)
	lerp := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*fraction)) }
	return color.NRGBA{R: lerp(a.R, b.R), G: lerp(a.G, b.G), B: lerp(a.B, b.B), A: lerp(a.A, b.A)}
}
