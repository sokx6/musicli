// Package ui implements the musicli TUI using Bubble Tea v2.
//
// Layout:
//   - Top bar: current track info (title/artist/album)
//   - Left pane: cover + lyrics placeholder
//   - Right pane: track list (bubbles/list)
//   - Bottom: player bar (progress + controls + state)
//
// Responsive: left pane hides on narrow terminals (<80 cols).
// Keyboard + mouse supported. Built-in keybindings can be overridden from TOML.
package ui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"
	"github.com/locxl/musicli/internal/theme"
)

// Styles holds lipgloss styles derived from the current theme.
type Styles struct {
	theme        *theme.Theme
	doc          lipgloss.Style
	topBar       lipgloss.Style
	leftPane     lipgloss.Style
	rightPane    lipgloss.Style
	player       lipgloss.Style
	title        lipgloss.Style
	muted        lipgloss.Style
	accent       lipgloss.Style
	help         lipgloss.Style
	spectrumLow  lipgloss.Style
	spectrumMid  lipgloss.Style
	spectrumHigh lipgloss.Style
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
		title:        lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		muted:        lipgloss.NewStyle().Foreground(t.Muted),
		accent:       lipgloss.NewStyle().Foreground(t.Accent),
		help:         lipgloss.NewStyle().Foreground(t.Muted),
		spectrumLow:  lipgloss.NewStyle().Foreground(theme.GradientAt(t.SpectrumGradient, 0)),
		spectrumMid:  lipgloss.NewStyle().Foreground(theme.GradientAt(t.SpectrumGradient, 0.5)),
		spectrumHigh: lipgloss.NewStyle().Foreground(theme.GradientAt(t.SpectrumGradient, 1)),
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
	s.TitleBar = lipgloss.NewStyle().Foreground(t.Accent).Bold(true).PaddingBottom(1)
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
		progress.WithColors(t.ProgressGradient...),
		progress.WithScaled(true),
		progress.WithoutPercentage(),
	)
	return p
}

// keyMap defines the built-in keybindings and their help labels.
type keyMap struct {
	PlayPause            key.Binding
	Next                 key.Binding
	Prev                 key.Binding
	ToggleRepeat         key.Binding
	ToggleShuffle        key.Binding
	ToggleLyricAlign     key.Binding
	ToggleLyricHighlight key.Binding
	ToggleSpectrum       key.Binding
	ToggleView           key.Binding
	ToggleScale          key.Binding
	ToggleList           key.Binding
	TogglePlaylists      key.Binding
	ToggleFavorite       key.Binding
	RemoveFromList       key.Binding
	SortPlaylist         key.Binding
	DeletePlaylist       key.Binding
	AddToPlaylist        key.Binding
	NewPlaylist          key.Binding
	Back                 key.Binding
	SeekFwd              key.Binding
	SeekBack             key.Binding
	VolUp                key.Binding
	VolDown              key.Binding
	SpeedUp              key.Binding
	SpeedDown            key.Binding
	Quit                 key.Binding
	Enter                key.Binding
	Up                   key.Binding
	Down                 key.Binding
	Filter               key.Binding
}

// defaultKeyMap returns the built-in keybindings.
func defaultKeyMap() keyMap {
	return keyMap{
		PlayPause:            key.NewBinding(key.WithKeys("space"), key.WithHelp("␣", "play/pause")),
		Next:                 key.NewBinding(key.WithKeys("n", "l"), key.WithHelp("n", "next")),
		Prev:                 key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "prev")),
		ToggleRepeat:         key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "repeat")),
		ToggleShuffle:        key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "shuffle")),
		ToggleLyricAlign:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "align lyrics")),
		ToggleLyricHighlight: key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "highlight lyrics")),
		ToggleSpectrum:       key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "spectrum")),
		ToggleView:           key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "cover/lyrics")),
		ToggleScale:          key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "cover scale")),
		ToggleList:           key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "tracks/albums")),
		TogglePlaylists:      key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "playlists")),
		ToggleFavorite:       key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "favorite")),
		RemoveFromList:       key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "remove playlist track")),
		SortPlaylist:         key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "sort playlist")),
		DeletePlaylist:       key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete playlist")),
		AddToPlaylist:        key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "add to playlist")),
		NewPlaylist:          key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "new playlist")),
		Back:                 key.NewBinding(key.WithKeys("esc", "backspace"), key.WithHelp("esc", "back")),
		SeekFwd:              key.NewBinding(key.WithKeys("right", "L"), key.WithHelp("→", "seek +5s")),
		SeekBack:             key.NewBinding(key.WithKeys("left", "H"), key.WithHelp("←", "seek -5s")),
		VolUp:                key.NewBinding(key.WithKeys("+", "="), key.WithHelp("+", "vol up")),
		VolDown:              key.NewBinding(key.WithKeys("-", "_"), key.WithHelp("-", "vol down")),
		SpeedUp:              key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "speed up")),
		SpeedDown:            key.NewBinding(key.WithKeys("["), key.WithHelp("[", "speed down")),
		Quit:                 key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Enter:                key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "play selected")),
		Up:                   key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:                 key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Filter:               key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	}
}

func applyKeybindingOverrides(keys *keyMap, overrides map[string][]string, warn func(string, ...any)) {
	if len(overrides) == 0 {
		return
	}
	actions := make([]string, 0, len(overrides))
	for action := range overrides {
		actions = append(actions, action)
	}
	sort.Strings(actions)
	for _, action := range actions {
		binding := keybindingForAction(keys, action)
		if binding == nil {
			warn("keybinding ignored", "action", action, "reason", "unknown action")
			continue
		}
		configured := overrides[action]
		if len(configured) == 0 {
			continue
		}
		candidate := make([]string, 0, len(configured))
		valid := true
		for _, value := range configured {
			value = strings.TrimSpace(value)
			if value == "" {
				valid = false
				break
			}
			candidate = append(candidate, value)
		}
		if !valid {
			warn("keybinding ignored", "action", action, "reason", "empty key")
			continue
		}
		if conflict := keybindingConflict(*keys, action, candidate); conflict != "" {
			warn("keybinding ignored", "action", action, "reason", fmt.Sprintf("conflicts with %s", conflict))
			continue
		}
		binding.SetKeys(candidate...)
		binding.SetHelp(displayKey(candidate[0]), binding.Help().Desc)
	}
}

func keybindingConflict(keys keyMap, action string, candidate []string) string {
	for _, other := range keybindingActions(&keys) {
		if other.name == action {
			continue
		}
		for _, want := range candidate {
			for _, used := range other.binding.Keys() {
				if want == used {
					return other.name
				}
			}
		}
	}
	return ""
}

type namedKeybinding struct {
	name    string
	binding *key.Binding
}

func keybindingActions(keys *keyMap) []namedKeybinding {
	return []namedKeybinding{
		{"play_pause", &keys.PlayPause}, {"next", &keys.Next}, {"prev", &keys.Prev},
		{"repeat", &keys.ToggleRepeat}, {"shuffle", &keys.ToggleShuffle}, {"lyric_align", &keys.ToggleLyricAlign},
		{"lyric_highlight", &keys.ToggleLyricHighlight}, {"spectrum", &keys.ToggleSpectrum}, {"view", &keys.ToggleView},
		{"scale", &keys.ToggleScale}, {"list", &keys.ToggleList}, {"playlists", &keys.TogglePlaylists},
		{"favorite", &keys.ToggleFavorite}, {"remove_playlist_track", &keys.RemoveFromList}, {"sort_playlist", &keys.SortPlaylist},
		{"delete_playlist", &keys.DeletePlaylist}, {"add_playlist", &keys.AddToPlaylist}, {"new_playlist", &keys.NewPlaylist},
		{"back", &keys.Back}, {"seek_forward", &keys.SeekFwd}, {"seek_backward", &keys.SeekBack},
		{"volume_up", &keys.VolUp}, {"volume_down", &keys.VolDown}, {"speed_up", &keys.SpeedUp},
		{"speed_down", &keys.SpeedDown}, {"quit", &keys.Quit}, {"enter", &keys.Enter},
		{"up", &keys.Up}, {"down", &keys.Down}, {"filter", &keys.Filter},
	}
}

func keybindingForAction(keys *keyMap, action string) *key.Binding {
	for _, named := range keybindingActions(keys) {
		if named.name == action {
			return named.binding
		}
	}
	return nil
}

func displayKey(value string) string {
	switch value {
	case "space":
		return "␣"
	case "enter":
		return "⏎"
	case "left":
		return "←"
	case "right":
		return "→"
	case "up":
		return "↑"
	case "down":
		return "↓"
	}
	return value
}
