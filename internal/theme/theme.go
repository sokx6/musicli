// Package theme holds the color palette and theme model for musicli.
//
// Phase 3 ships a minimal hardcoded dark palette. Phase 10 adds system
// detection (XDG portal / gsettings / OSC11), TOML customization, and a
// light palette. The Theme struct exists from day one so styles.go binds
// to it without a later refactor (oracle Adj-A).
package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
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
	Mode      Mode
	Bg        color.Color
	Fg        color.Color
	Muted     color.Color
	Accent    color.Color
	Subtle    color.Color
	Highlight color.Color
}

// Default returns the built-in dark theme.
func Default() *Theme {
	return &Theme{
		Mode:      ModeDark,
		Bg:        lipgloss.Color("#1a1b26"),
		Fg:        lipgloss.Color("#c0caf5"),
		Muted:     lipgloss.Color("#565f89"),
		Accent:    lipgloss.Color("#7aa2f7"),
		Subtle:    lipgloss.Color("#292e42"),
		Highlight: lipgloss.Color("#bb9af7"),
	}
}

// ProgressColor returns the accent color for progress bars.
func (t *Theme) ProgressColor() color.Color { return t.Accent }
