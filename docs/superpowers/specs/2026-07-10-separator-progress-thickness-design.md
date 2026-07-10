# Separator Progress Thickness Design

## Goal

Make the kitty pixel separator progress line thickness configurable without changing the existing 1-pixel default or the text fallback used by terminals without kitty graphics support.

## Configuration

Add this field under `[ui]`:

```toml
separator_progress_thickness = 1
```

The value is measured in pixels. Valid values are `1` through `8`. Missing values retain the default of `1`; invalid values produce a configuration warning and fall back to `1`.

## Data Flow

`config.UI.SeparatorProgressThickness` is loaded in `internal/config`, logged at startup, and passed through `ui.Options`. The UI supplies the value to `cover.RenderKittyProgressLine` whenever it builds the kitty progress overlay.

The renderer clamps the configured thickness to the actual terminal cell pixel height before drawing. It centers the horizontal line vertically within the separator row. Both the played and remaining portions use the same thickness, so progress changes cannot alternate between thin and thick segments.

## Compatibility

- The default remains visually identical at `1` pixel.
- The setting affects only `progress_style = "separator"` when kitty graphics are active.
- Halfblock and other text fallbacks continue to render a single `─` row and ignore pixel thickness.
- Existing cursor preservation, alternating image IDs, and draw-before-delete replacement remain unchanged to avoid flicker and terminal text corruption.

## Testing

- Configuration tests cover the default, accepted boundary values, and invalid-value fallback.
- Protocol tests decode the generated PNG and verify the requested number of centered colored rows.
- UI tests verify that the configured thickness is passed into the kitty renderer while the non-kitty separator remains unchanged.
- Existing cover switching, view switching, pixel progress, narrow layout, and full targeted package tests must continue to pass.
