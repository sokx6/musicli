// Package ui implements the musicli TUI using Bubble Tea v2.
//
// Layout:
//   - Top bar: current track info (title/artist/album)
//   - Left pane: cover + lyrics placeholder
//   - Right pane: track list (bubbles/list)
//   - Bottom: player bar (progress + controls + state)
//
// Responsive: left pane hides on narrow terminals (<80 cols).
// Keyboard + mouse supported. KeyMap is hardcoded (phase 11 adds TOML override).
package ui

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"
	"github.com/locxl/musicli/internal/theme"
)

// Styles holds lipgloss styles derived from the current theme.
type Styles struct {
	theme     *theme.Theme
	doc       lipgloss.Style
	topBar    lipgloss.Style
	leftPane  lipgloss.Style
	rightPane lipgloss.Style
	player    lipgloss.Style
	title     lipgloss.Style
	muted     lipgloss.Style
	accent    lipgloss.Style
	help      lipgloss.Style
}

// NewStyles builds styles from a theme. No backgrounds — transparent,
// using the terminal's native background color. Visual separation between
// panes is via borders only.
func NewStyles(t *theme.Theme) *Styles {
	borderColor := t.Muted
	return &Styles{
		theme: t,
		doc:   lipgloss.NewStyle().Foreground(t.Fg),
		topBar: lipgloss.NewStyle().
			Foreground(t.Fg).
			Align(lipgloss.Center).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(borderColor),
		leftPane: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(borderColor),
		rightPane: lipgloss.NewStyle(),
		player: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(borderColor).
			Padding(0, 1),
		title:  lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		muted:  lipgloss.NewStyle().Foreground(t.Muted),
		accent: lipgloss.NewStyle().Foreground(t.Accent),
		help:   lipgloss.NewStyle().Foreground(t.Muted),
	}
}

// newListStyles themes the list item delegate styles.
// Uses fresh lipgloss.NewStyle() (not inheriting from NewDefaultItemStyles)
// to avoid carrying default PaddingLeft/border that offset content right
// and cause the double-border (││) artifact.
func newListStyles(t *theme.Theme) list.DefaultItemStyles {
	return list.DefaultItemStyles{
		NormalTitle:   lipgloss.NewStyle().Foreground(t.Fg).PaddingLeft(1),
		NormalDesc:    lipgloss.NewStyle().Foreground(t.Muted).PaddingLeft(1),
		SelectedTitle: lipgloss.NewStyle().Foreground(t.Accent).Background(t.Subtle).Bold(true).PaddingLeft(1),
		SelectedDesc:  lipgloss.NewStyle().Foreground(t.Accent).Background(t.Subtle).PaddingLeft(1),
		DimmedTitle:   lipgloss.NewStyle().Foreground(t.Muted).PaddingLeft(1),
		DimmedDesc:    lipgloss.NewStyle().Foreground(t.Muted).PaddingLeft(1),
		FilterMatch:   lipgloss.NewStyle().Foreground(t.Highlight),
	}
}

// newListComponentStyles themes the list's own chrome (title bar, status,
// pagination, filter) — using fresh Styles to avoid inherited backgrounds.
func newListComponentStyles(t *theme.Theme) list.Styles {
	s := list.DefaultStyles(t.Mode == theme.ModeDark)
	// Overwrite with fresh styles (no Background) to kill default colored bars.
	s.TitleBar = lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	s.Title = lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Padding(0, 0, 0, 1)
	s.Spinner = lipgloss.NewStyle().Foreground(t.Accent)
	s.StatusBar = lipgloss.NewStyle().Foreground(t.Muted)
	s.StatusEmpty = lipgloss.NewStyle().Foreground(t.Muted)
	s.StatusBarActiveFilter = lipgloss.NewStyle().Foreground(t.Highlight)
	s.StatusBarFilterCount = lipgloss.NewStyle().Foreground(t.Muted)
	s.NoItems = lipgloss.NewStyle().Foreground(t.Muted)
	s.PaginationStyle = lipgloss.NewStyle().Foreground(t.Muted)
	s.HelpStyle = lipgloss.NewStyle().Foreground(t.Muted)
	return s
}

// newProgressBar creates a themed progress bar.
func newProgressBar(t *theme.Theme) progress.Model {
	p := progress.New(
		progress.WithColors(t.Accent, t.Highlight),
		progress.WithoutPercentage(),
	)
	return p
}

// keyMap defines the default keybindings (phase 11 adds TOML override).
type keyMap struct {
	PlayPause   key.Binding
	Next        key.Binding
	Prev        key.Binding
	ToggleView  key.Binding
	ToggleScale key.Binding
	SeekFwd     key.Binding
	SeekBack    key.Binding
	VolUp       key.Binding
	VolDown     key.Binding
	SpeedUp     key.Binding
	SpeedDown   key.Binding
	Quit        key.Binding
	Enter       key.Binding
	Up          key.Binding
	Down        key.Binding
	Filter      key.Binding
}

// defaultKeyMap returns the built-in keybindings.
func defaultKeyMap() keyMap {
	return keyMap{
		PlayPause:   key.NewBinding(key.WithKeys("space", "p"), key.WithHelp("␣/p", "play/pause")),
		Next:        key.NewBinding(key.WithKeys("n", "l"), key.WithHelp("n", "next")),
		Prev:        key.NewBinding(key.WithKeys("b", "h"), key.WithHelp("b", "prev")),
		ToggleView:  key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "cover/lyrics")),
		ToggleScale: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "cover scale")),
		SeekFwd:     key.NewBinding(key.WithKeys("right", "L"), key.WithHelp("→", "seek +5s")),
		SeekBack:    key.NewBinding(key.WithKeys("left", "H"), key.WithHelp("←", "seek -5s")),
		VolUp:       key.NewBinding(key.WithKeys("+", "="), key.WithHelp("+", "vol up")),
		VolDown:     key.NewBinding(key.WithKeys("-", "_"), key.WithHelp("-", "vol down")),
		SpeedUp:     key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "speed up")),
		SpeedDown:   key.NewBinding(key.WithKeys("["), key.WithHelp("[", "speed down")),
		Quit:        key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Enter:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "play selected")),
		Up:          key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:        key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Filter:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	}
}
