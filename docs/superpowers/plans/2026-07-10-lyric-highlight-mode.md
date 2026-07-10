# Lyric Highlight Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users choose whether word-timed lyrics highlight only the active word or every word that has begun, defaulting to the latter.

**Architecture:** Add a validated `lyrics.highlight_mode` setting and pass it into the UI. The UI stores a two-value mode, toggles it with `h`, and changes only the highlighted word range while preserving the existing fixed-width CJK rendering path.

**Tech Stack:** Go, TOML, Bubble Tea v2, charmbracelet ANSI utilities.

---

### Task 1: Add Configuration And UI State

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config.example.toml`
- Modify: `internal/config/config_test.go`
- Modify: `cmd/musicli/main.go`
- Modify: `internal/ui/app.go`
- Test: `internal/config/config_test.go`
- Test: `internal/ui/app_test.go`

- [ ] Write failing tests for invalid TOML fallback to `played` and UI initialization from `current`.
- [ ] Run `go test ./internal/config ./internal/ui -run 'TestLoadRejectsInvalidLyricHighlightMode|TestConfiguredLyricHighlightModeInitializesMode' -count=1` and confirm failure.
- [ ] Add `Lyrics.HighlightMode`, validation for `played|current`, default config `highlight_mode = "played"`, `Options.LyricsHighlightMode`, and main-to-UI wiring.
- [ ] Re-run the focused tests and confirm success.

### Task 2: Toggle And Render Modes

**Files:**
- Modify: `internal/ui/styles.go`
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/app_test.go`
- Test: `internal/ui/app_test.go`

- [ ] Write failing tests: default mode highlights words through the active word, `current` highlights only the active word, and `h` cycles both modes.
- [ ] Run `go test ./internal/ui -run 'TestToggleLyricHighlightCyclesModes|TestRenderCurrentLyricLineHighlightsPlayedWordsByDefault|TestRenderCurrentLyricLineCanHighlightOnlyCurrentWord' -count=1` and confirm failure.
- [ ] Remove lowercase `h` from previous track, bind it to `ToggleLyricHighlight`, add the two-mode toggle, and choose the highlighted prefix in `renderCurrentLyricLine` without changing clipping or ANSI reset behavior.
- [ ] Re-run the focused tests plus existing CJK rendering tests and confirm success.

### Task 3: Document And Verify

**Files:**
- Modify: `docs/handoff/musicli-handoff.md`
- Modify: `internal/ui/app.go`

- [ ] Add `h highlight` to player-bar help and document the TOML values/default in the handoff.
- [ ] Run non-device-dependent package tests, `make build`, and `git diff --check`.
- [ ] Commit the implementation and plan as `feat: add lyric highlight modes`.
