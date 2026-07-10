# Separator Progress Thickness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a validated pixel-thickness setting for the kitty separator progress overlay while preserving its 1-pixel default and text fallback.

**Architecture:** Extend the existing `[ui]` configuration and `ui.Options` data path with an integer thickness. Pass it into the kitty protocol renderer, which clamps it to the terminal cell height and paints a vertically centered band for both played and remaining colors.

**Tech Stack:** Go, pelletier/go-toml v2, Bubble Tea v2, kitty graphics protocol, standard image/png.

---

### Task 1: Configuration and startup wiring

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config.example.toml`
- Modify: `internal/config/config_test.go`
- Modify: `cmd/musicli/main.go`
- Modify: `internal/ui/app.go`

- [x] **Step 1: Write failing configuration tests**

Add assertions that the default is `1`, valid values `1` and `8` load unchanged, and `0`/`9` produce warnings and fall back to `1`.

- [x] **Step 2: Run tests and verify failure**

Run: `go test ./internal/config -run 'TestDefaultsRoundtrip|TestLoadSeparatorProgressThickness' -count=1`

Expected: FAIL because `SeparatorProgressThickness` does not exist.

- [x] **Step 3: Add configuration field and validation**

Add `SeparatorProgressThickness int` with TOML key `separator_progress_thickness`, default it to `1`, and validate the inclusive range `1..8` in `applyDefaults`.

- [x] **Step 4: Wire the value into UI options**

Log `ui.separator_progress_thickness`, pass it from `cmd/musicli/main.go`, and add the corresponding field to `ui.Options`.

- [x] **Step 5: Run configuration tests**

Run: `go test ./internal/config ./cmd/musicli -count=1`

Expected: PASS.

### Task 2: Centered pixel-band rendering

**Files:**
- Modify: `internal/cover/protocol.go`
- Modify: `internal/cover/protocol_test.go`
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/app_test.go`

- [x] **Step 1: Write failing protocol tests**

Decode the generated progress PNG and assert that thickness `3` paints exactly three centered rows across both played and remaining segments. Add cases where thickness exceeds cell height and is clamped.

- [x] **Step 2: Run protocol tests and verify failure**

Run: `go test ./internal/cover -run TestKittyProgressLineThickness -count=1`

Expected: FAIL because the renderer only paints one row and has no thickness argument.

- [x] **Step 3: Implement centered thickness**

Add a `thickness` argument to `RenderKittyProgressLine`, normalize values below `1` to `1`, clamp above `cellH`, calculate `startY := (cellH-thickness)/2`, and paint rows `[startY, startY+thickness)`.

- [x] **Step 4: Pass UI option to renderer**

Use `a.options.SeparatorProgressThickness` in `kittyProgressCmd`. Preserve the text fallback path unchanged.

- [x] **Step 5: Run protocol and UI tests**

Run: `go test ./internal/cover ./internal/ui -count=1`

Expected: PASS.

### Task 3: Documentation and full verification

**Files:**
- Modify: `docs/handoff/musicli-handoff.md`

- [x] **Step 1: Document the setting**

Add `separator_progress_thickness = 1` under `[ui]`, noting that it accepts `1..8` pixels and only affects kitty separator overlays.

- [x] **Step 2: Run targeted regression tests**

Run: `go test ./internal/cover ./internal/ui -run 'TestKittyRenderPreservesTerminalCursor|TestKittyProgressLine|TestKittySeparator|TestViewToggle|TestResetKittyProgress' -count=1`

Expected: PASS.

- [x] **Step 3: Run full targeted suite and build**

Run: `go test ./cmd/musicli ./internal/config ./internal/library ./internal/playlist ./internal/cover ./internal/ui ./internal/mpris -count=1 && make build && git diff --check`

Expected: all tests PASS, build exits 0, and diff check has no output.
