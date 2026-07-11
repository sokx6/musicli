# Theme Files

Select separate dark and light `.theme` files from `config.toml`:

```toml
[theme]
mode = "auto"             # auto | dark | light
dark = "themes/night.theme"
light = "themes/day.theme"
```

Relative paths are resolved from the directory containing `config.toml`.
Empty paths use the built-in palette for the selected mode.

```toml
[meta]
name = "Night"

[colors]
bg = "#1a1b26"
fg = "#c0caf5"
muted = "#565f89"
accent = "#7aa2f7"
subtle = "#292e42"
highlight = "#bb9af7"

[gradients]
progress = ["#7aa2f7", "#bb9af7", "#f7768e"]
spectrum = ["#565f89", "#7aa2f7", "#bb9af7", "#f7768e"]
```

All colors use `#RRGGBB`. A gradient needs at least two colors. Invalid or
missing entries fall back to the built-in value for the active mode and emit a
warning in the log.

`progress` is interpolated per terminal cell for normal bars and per pixel for
Kitty separator bars. `spectrum` is interpolated from the bottom to the top of
the Braille spectrum.
