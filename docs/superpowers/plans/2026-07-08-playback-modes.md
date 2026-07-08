# Playback Modes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement phase 7 playback modes so natural track end, manual next/prev, repeat mode, and shuffle behavior follow `[playback] repeat` and `shuffle`.

**Architecture:** Keep playback policy in the UI layer because the UI owns the ordered track slice, current index, list selection, and album/list view mapping. Add small deterministic helper methods on `App` for track-end detection and next-index selection, then route auto-advance through the same play-by-index path used by manual controls.

**Tech Stack:** Go 1.26, Bubble Tea v2, current `audio.Engine` polling model, existing TOML config loader, existing `go test` package tests.

---

## Scope

This phase includes:

- Use `cfg.Playback.Repeat` and `cfg.Playback.Shuffle` in `ui.Options`.
- Auto-play the correct next track when the current track ends naturally.
- Support `repeat = "none"`, `repeat = "one"`, and `repeat = "list"`.
- Support `shuffle = true` for automatic next and manual next.
- Keep manual previous deterministic: go to previous track in list order, because shuffle history is not planned for this phase.
- Show repeat/shuffle state in the player bar.
- Add focused tests for playback policy.

This phase does not include:

- Playlist management.
- Persistent queue history.
- Weighted shuffle or no-repeat-until-all-played bags.
- User-configurable keybindings.
- New config keys beyond the existing `repeat` and `shuffle`.

## File Structure

- Modify `internal/ui/app.go`
  - Add playback options to `Options`.
  - Track whether playback was previously active.
  - Detect natural track completion during `tickMsg`.
  - Add helper methods for playing by index and choosing next index.
  - Show repeat/shuffle state in the player bar.

- Modify `cmd/musicli/main.go`
  - Pass `cfg.Playback.Repeat` and `cfg.Playback.Shuffle` into `ui.NewWithOptions`.

- Modify `internal/ui/app_test.go`
  - Add unit tests for next-index policy and natural end handling.

- Optionally modify `docs/handoff/musicli-handoff.md`
  - Move phase 7 into completed status after implementation and verification.

## Behavior Decisions

- `repeat = "list", shuffle = false`: track end advances `current + 1`; the final track wraps to `0`.
- `repeat = "list", shuffle = true`: track end chooses a random index different from `current` when there are at least two tracks.
- `repeat = "one"`: track end replays `current`. `shuffle` does not affect automatic end behavior in this mode.
- `repeat = "none"`: track end advances until the final track finishes; then it stops and keeps `current` on the final track.
- Manual `n` / next:
  - If `shuffle = true`, choose a random different track when possible.
  - If `shuffle = false`, move to `current + 1`, wrapping at the end.
  - Manual next ignores `repeat = "none"` at the final track, because explicit navigation should still wrap like it currently does.
- Manual `b` / previous:
  - Keep current deterministic list-order behavior and wrap at the beginning.
- Natural end detection:
  - `audio.Engine` sets `StateStopped` and position to duration on natural completion.
  - UI should trigger auto-advance only on transition from active playback to stopped at end.
  - Explicit pause/stop or startup should not auto-advance.

## Task 1: Wire Playback Options Into UI

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `cmd/musicli/main.go`
- Test: `internal/ui/app_test.go`

- [ ] **Step 1: Write the failing option test**

Add this test near the other option/config-driven UI tests in `internal/ui/app_test.go`:

```go
func TestPlaybackOptionsAreStored(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		PlaybackRepeat:  "one",
		PlaybackShuffle: true,
	})

	if app.options.PlaybackRepeat != "one" {
		t.Fatalf("playback repeat = %q, want one", app.options.PlaybackRepeat)
	}
	if !app.options.PlaybackShuffle {
		t.Fatal("playback shuffle = false, want true")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```bash
go test ./internal/ui -run TestPlaybackOptionsAreStored -count=1
```

Expected: FAIL because `Options` does not yet have `PlaybackRepeat` and `PlaybackShuffle`.

- [ ] **Step 3: Add option fields**

In `internal/ui/app.go`, extend `Options`:

```go
type Options struct {
	// TrackListMaxWidth caps the content-fit track list width. Zero means no cap.
	TrackListMaxWidth int
	DisableCover      bool
	CoverScale        string
	CoverProtocol     string
	LibrarySortField  string
	LibrarySortOrder  string
	GroupByAlbum      bool
	PlaybackRepeat    string
	PlaybackShuffle   bool
}
```

- [ ] **Step 4: Pass config from main**

In `cmd/musicli/main.go`, update the `ui.NewWithOptions` call:

```go
	app := ui.NewWithOptions(eng, sc, t, logger, ui.Options{
		TrackListMaxWidth: cfg.UI.TrackListMaxWidth,
		DisableCover:      !cfg.Cover.Show,
		CoverScale:        cfg.Cover.Scale,
		CoverProtocol:     cfg.Cover.Protocol,
		LibrarySortField:  cfg.Library.SortField,
		LibrarySortOrder:  cfg.Library.SortOrder,
		GroupByAlbum:      cfg.Library.GroupByAlbum,
		PlaybackRepeat:    cfg.Playback.Repeat,
		PlaybackShuffle:   cfg.Playback.Shuffle,
	})
```

- [ ] **Step 5: Run the focused test**

Run:

```bash
go test ./internal/ui -run TestPlaybackOptionsAreStored -count=1
```

Expected: PASS.

## Task 2: Add Shared Play-By-Index Helper

**Files:**
- Modify: `internal/ui/app.go`
- Test: `internal/ui/app_test.go`

- [ ] **Step 1: Write the failing same-track guard test**

Add this test near existing playback command tests in `internal/ui/app_test.go`:

```go
func TestPlayTrackAtRejectsInvalidIndex(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	app.tracks = []*library.Track{{Path: "one.mp3", Title: "One"}}
	app.current = 0

	cmd := app.playTrackAt(-1)
	if cmd != nil {
		t.Fatalf("playTrackAt(-1) returned command %#v, want nil", cmd)
	}
	if app.current != 0 {
		t.Fatalf("current = %d, want unchanged 0", app.current)
	}

	cmd = app.playTrackAt(1)
	if cmd != nil {
		t.Fatalf("playTrackAt(1) returned command %#v, want nil", cmd)
	}
	if app.current != 0 {
		t.Fatalf("current = %d, want unchanged 0", app.current)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```bash
go test ./internal/ui -run TestPlayTrackAtRejectsInvalidIndex -count=1
```

Expected: FAIL because `playTrackAt` is not defined.

- [ ] **Step 3: Add `playTrackAt` helper**

In `internal/ui/app.go`, place this helper above `playSelected`:

```go
func (a *App) playTrackAt(idx int) tea.Cmd {
	fl := a.log.WithFunc("playTrackAt")
	if idx < 0 || idx >= len(a.tracks) {
		fl.Debug("invalid index", "idx", idx, "tracks", len(a.tracks))
		return nil
	}
	a.current = idx
	a.selectCurrentInLibraryView()
	t := a.tracks[idx]
	fl.Debug("playing track", "idx", idx, "title", t.Title, "path", t.Path)
	if err := a.engine.Play(t.Path); err != nil {
		fl.Error("Play failed", "err", err)
		return func() tea.Msg { return errMsg{err: err} }
	}
	a.loadCurrentLyrics()
	a.loadCurrentCover()
	return a.kittyCoverCmd()
}
```

- [ ] **Step 4: Refactor existing play methods to use it**

Update `playSelected` after the same-track guard:

```go
	return a.playTrackAt(idx)
```

Update `nextTrack` after computing the next index:

```go
	return a.playTrackAt(a.current)
```

Update `prevTrack` after computing the previous index:

```go
	return a.playTrackAt(a.current)
```

Remove duplicated engine play, lyrics load, cover load, and kitty command blocks from those methods.

- [ ] **Step 5: Run focused UI tests**

Run:

```bash
go test ./internal/ui -run 'TestPlayTrackAtRejectsInvalidIndex|TestAlbumEnterBackAndTrackMapping|TestTabTogglesTracksAndAlbums' -count=1
```

Expected: PASS.

## Task 3: Implement Next-Index Policy

**Files:**
- Modify: `internal/ui/app.go`
- Test: `internal/ui/app_test.go`

- [ ] **Step 1: Write failing policy tests**

Add these tests in `internal/ui/app_test.go`:

```go
func TestNextTrackIndexListOrderWraps(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		PlaybackRepeat: "list",
	})
	app.tracks = []*library.Track{
		{Title: "One"},
		{Title: "Two"},
		{Title: "Three"},
	}
	app.current = 1
	if got := app.nextTrackIndex(false); got != 2 {
		t.Fatalf("next from 1 = %d, want 2", got)
	}
	app.current = 2
	if got := app.nextTrackIndex(false); got != 0 {
		t.Fatalf("next from 2 = %d, want wrap to 0", got)
	}
}

func TestNextTrackIndexRepeatNoneStopsAtEnd(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		PlaybackRepeat: "none",
	})
	app.tracks = []*library.Track{
		{Title: "One"},
		{Title: "Two"},
	}
	app.current = 0
	if got := app.nextTrackIndex(true); got != 1 {
		t.Fatalf("auto next from 0 = %d, want 1", got)
	}
	app.current = 1
	if got := app.nextTrackIndex(true); got != -1 {
		t.Fatalf("auto next from final track = %d, want -1", got)
	}
	if got := app.nextTrackIndex(false); got != 0 {
		t.Fatalf("manual next from final track = %d, want wrap to 0", got)
	}
}

func TestNextTrackIndexRepeatOneReplaysOnAutoAdvance(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		PlaybackRepeat: "one",
	})
	app.tracks = []*library.Track{
		{Title: "One"},
		{Title: "Two"},
	}
	app.current = 1
	if got := app.nextTrackIndex(true); got != 1 {
		t.Fatalf("auto next in repeat one = %d, want current 1", got)
	}
	if got := app.nextTrackIndex(false); got != 0 {
		t.Fatalf("manual next in repeat one = %d, want normal wrap 0", got)
	}
}

func TestNextTrackIndexShuffleAvoidsCurrentWhenPossible(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		PlaybackRepeat:  "list",
		PlaybackShuffle: true,
	})
	app.tracks = []*library.Track{
		{Title: "One"},
		{Title: "Two"},
		{Title: "Three"},
	}
	app.current = 1
	for i := 0; i < 30; i++ {
		got := app.nextTrackIndex(true)
		if got < 0 || got >= len(app.tracks) {
			t.Fatalf("shuffle index = %d, want valid track index", got)
		}
		if got == app.current {
			t.Fatalf("shuffle index = current %d, want different track", got)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/ui -run 'TestNextTrackIndex' -count=1
```

Expected: FAIL because `nextTrackIndex` is not defined.

- [ ] **Step 3: Add random import**

In `internal/ui/app.go`, add `math/rand/v2` to imports:

```go
import (
	"context"
	"errors"
	"fmt"
	"image"
	"math/rand/v2"
	"os"
	"strings"
	"time"
)
```

- [ ] **Step 4: Add policy helpers**

In `internal/ui/app.go`, place these helpers near playback commands:

```go
func (a *App) playbackRepeat() string {
	switch a.options.PlaybackRepeat {
	case "none", "one", "list":
		return a.options.PlaybackRepeat
	default:
		return "list"
	}
}

func (a *App) nextTrackIndex(autoAdvance bool) int {
	if len(a.tracks) == 0 {
		return -1
	}
	if a.current < 0 || a.current >= len(a.tracks) {
		return 0
	}
	if autoAdvance {
		switch a.playbackRepeat() {
		case "one":
			return a.current
		case "none":
			if a.current >= len(a.tracks)-1 {
				return -1
			}
		}
	}
	if a.options.PlaybackShuffle && len(a.tracks) > 1 {
		next := rand.N(len(a.tracks) - 1)
		if next >= a.current {
			next++
		}
		return next
	}
	return (a.current + 1) % len(a.tracks)
}
```

- [ ] **Step 5: Update manual next**

Replace the body of `nextTrack` with:

```go
func (a *App) nextTrack() tea.Cmd {
	fl := a.log.WithFunc("nextTrack")
	if len(a.tracks) == 0 {
		return nil
	}
	prevIdx := a.current
	nextIdx := a.nextTrackIndex(false)
	if nextIdx < 0 {
		fl.Debug("no next track", "prevIdx", prevIdx)
		return nil
	}
	fl.Debug("next track", "prevIdx", prevIdx, "newIdx", nextIdx)
	return a.playTrackAt(nextIdx)
}
```

- [ ] **Step 6: Run policy tests**

Run:

```bash
go test ./internal/ui -run 'TestNextTrackIndex|TestPlayTrackAtRejectsInvalidIndex' -count=1
```

Expected: PASS.

## Task 4: Detect Natural End And Auto-Advance

**Files:**
- Modify: `internal/ui/app.go`
- Test: `internal/ui/app_test.go`

- [ ] **Step 1: Add playback active state**

In `internal/ui/app.go`, add a field near the playback mirror fields:

```go
	wasPlaying bool
```

- [ ] **Step 2: Write failing end detection tests**

Add these tests to `internal/ui/app_test.go`:

```go
func TestTrackEndedNaturallyRequiresStoppedAtDurationAfterPlaying(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	app.wasPlaying = true
	app.state = audio.StateStopped
	app.pos = 1000
	app.dur = 1000
	if !app.trackEndedNaturally() {
		t.Fatal("trackEndedNaturally = false, want true")
	}
}

func TestTrackEndedNaturallyIgnoresStartupStoppedState(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	app.wasPlaying = false
	app.state = audio.StateStopped
	app.pos = 1000
	app.dur = 1000
	if app.trackEndedNaturally() {
		t.Fatal("trackEndedNaturally = true, want false")
	}
}

func TestTrackEndedNaturallyRequiresKnownDuration(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})
	app.wasPlaying = true
	app.state = audio.StateStopped
	app.pos = 0
	app.dur = 0
	if app.trackEndedNaturally() {
		t.Fatal("trackEndedNaturally = true, want false for unknown duration")
	}
}
```

Because these tests reference `audio.StateStopped`, add this import to `internal/ui/app_test.go`:

```go
	"github.com/locxl/musicli/internal/audio"
```

- [ ] **Step 3: Run tests to verify they fail**

Run:

```bash
go test ./internal/ui -run 'TestTrackEndedNaturally' -count=1
```

Expected: FAIL because `trackEndedNaturally` is not defined.

- [ ] **Step 4: Add end detection helper**

In `internal/ui/app.go`, add:

```go
func (a *App) trackEndedNaturally() bool {
	return a.wasPlaying &&
		a.state == audio.StateStopped &&
		a.dur > 0 &&
		a.pos >= a.dur
}
```

- [ ] **Step 5: Add auto-advance helper**

In `internal/ui/app.go`, add:

```go
func (a *App) autoAdvanceAfterEnd() tea.Cmd {
	fl := a.log.WithFunc("autoAdvanceAfterEnd")
	nextIdx := a.nextTrackIndex(true)
	if nextIdx < 0 {
		fl.Debug("playback ended, no auto-advance", "current", a.current, "repeat", a.playbackRepeat())
		a.wasPlaying = false
		return nil
	}
	fl.Debug("playback ended, auto-advancing", "current", a.current, "next", nextIdx, "repeat", a.playbackRepeat(), "shuffle", a.options.PlaybackShuffle)
	a.wasPlaying = false
	return a.playTrackAt(nextIdx)
}
```

- [ ] **Step 6: Wire tick handling**

In the `tickMsg` branch of `Update`, after `a.pollEngine()` and before lyric state comparison, add:

```go
		if a.trackEndedNaturally() {
			return a, tea.Batch(tickCmd(), a.autoAdvanceAfterEnd())
		}
		a.wasPlaying = a.state == audio.StatePlaying
```

The full branch should keep existing lyric redraw behavior after this new block.

- [ ] **Step 7: Run focused tests**

Run:

```bash
go test ./internal/ui -run 'TestTrackEndedNaturally|TestNextTrackIndex' -count=1
```

Expected: PASS.

## Task 5: Show Repeat And Shuffle State In Player Bar

**Files:**
- Modify: `internal/ui/app.go`
- Test: `internal/ui/app_test.go`

- [ ] **Step 1: Write failing render test**

Add this test near rendering tests in `internal/ui/app_test.go`:

```go
func TestPlayerBarShowsPlaybackMode(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		PlaybackRepeat:  "one",
		PlaybackShuffle: true,
	})
	app.width = 100
	app.volume = 80
	app.speed = 1.0

	plain := ansi.Strip(app.renderPlayerBar())
	if !strings.Contains(plain, "repeat one") {
		t.Fatalf("player bar missing repeat mode:\n%s", plain)
	}
	if !strings.Contains(plain, "shuffle on") {
		t.Fatalf("player bar missing shuffle mode:\n%s", plain)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/ui -run TestPlayerBarShowsPlaybackMode -count=1
```

Expected: FAIL because the player bar only shows volume and speed.

- [ ] **Step 3: Update player info string**

In `renderPlayerBar`, replace:

```go
	info := fmt.Sprintf("vol %d%%  speed %.1fx", a.volume, a.speed)
```

with:

```go
	shuffle := "off"
	if a.options.PlaybackShuffle {
		shuffle = "on"
	}
	info := fmt.Sprintf("vol %d%%  speed %.1fx  repeat %s  shuffle %s",
		a.volume, a.speed, a.playbackRepeat(), shuffle)
```

- [ ] **Step 4: Run render test**

Run:

```bash
go test ./internal/ui -run TestPlayerBarShowsPlaybackMode -count=1
```

Expected: PASS.

## Task 6: Add Runtime Toggle Keys For Repeat And Shuffle

**Files:**
- Modify: `internal/ui/styles.go`
- Modify: `internal/ui/app.go`
- Test: `internal/ui/app_test.go`

- [ ] **Step 1: Write failing key tests**

Add these tests in `internal/ui/app_test.go`:

```go
func TestToggleRepeatCyclesModes(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{
		PlaybackRepeat: "list",
	})

	app.toggleRepeat()
	if app.options.PlaybackRepeat != "one" {
		t.Fatalf("after first toggle repeat = %q, want one", app.options.PlaybackRepeat)
	}
	app.toggleRepeat()
	if app.options.PlaybackRepeat != "none" {
		t.Fatalf("after second toggle repeat = %q, want none", app.options.PlaybackRepeat)
	}
	app.toggleRepeat()
	if app.options.PlaybackRepeat != "list" {
		t.Fatalf("after third toggle repeat = %q, want list", app.options.PlaybackRepeat)
	}
}

func TestToggleShuffleFlipsMode(t *testing.T) {
	app := NewWithOptions(nil, nil, theme.Default(), log.Discard(), Options{})

	app.toggleShuffle()
	if !app.options.PlaybackShuffle {
		t.Fatal("after first toggle shuffle = false, want true")
	}
	app.toggleShuffle()
	if app.options.PlaybackShuffle {
		t.Fatal("after second toggle shuffle = true, want false")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/ui -run 'TestToggleRepeat|TestToggleShuffle' -count=1
```

Expected: FAIL because toggle helpers are not defined.

- [ ] **Step 3: Extend key map**

In `internal/ui/styles.go`, add these fields to `keyMap`:

```go
	ToggleRepeat  key.Binding
	ToggleShuffle key.Binding
```

In `defaultKeyMap`, add:

```go
		ToggleRepeat:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "repeat")),
		ToggleShuffle: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "shuffle")),
```

Use `r` and `s` because they are currently free in the default map.

- [ ] **Step 4: Add toggle helpers**

In `internal/ui/app.go`, add:

```go
func (a *App) toggleRepeat() {
	switch a.playbackRepeat() {
	case "list":
		a.options.PlaybackRepeat = "one"
	case "one":
		a.options.PlaybackRepeat = "none"
	default:
		a.options.PlaybackRepeat = "list"
	}
}

func (a *App) toggleShuffle() {
	a.options.PlaybackShuffle = !a.options.PlaybackShuffle
}
```

- [ ] **Step 5: Wire keys**

In `handleKey`, add cases after previous/next:

```go
	case key.Matches(msg, a.keys.ToggleRepeat):
		fl.Debug("key matched", "key", keyStr, "action", "toggleRepeat")
		a.toggleRepeat()
		return a, nil

	case key.Matches(msg, a.keys.ToggleShuffle):
		fl.Debug("key matched", "key", keyStr, "action", "toggleShuffle")
		a.toggleShuffle()
		return a, nil
```

- [ ] **Step 6: Update help line**

In `helpLine`, replace the string with:

```go
	return "q quit  ⏎ play  ␣ pause  n/b next/prev  r repeat  s shuffle  v view  c scale  ←→ seek  / filter"
```

- [ ] **Step 7: Run toggle tests**

Run:

```bash
go test ./internal/ui -run 'TestToggleRepeat|TestToggleShuffle|TestPlayerBarShowsPlaybackMode' -count=1
```

Expected: PASS.

## Task 7: Integration Verification

**Files:**
- Modify: `docs/handoff/musicli-handoff.md`

- [ ] **Step 1: Run targeted tests**

Run:

```bash
go test ./cmd/musicli ./internal/config ./internal/library ./internal/ui -count=1
```

Expected: PASS.

- [ ] **Step 2: Run full test suite**

Run:

```bash
go test ./... -count=1
```

Expected: PASS except possible known local ALSA/JACK failure in `internal/audio`. If `internal/audio` fails only because local audio devices are unavailable, record that in the final report and rely on the targeted command above.

- [ ] **Step 3: Run diff whitespace check**

Run:

```bash
git diff --check
```

Expected: no output.

- [ ] **Step 4: Update handoff docs**

In `docs/handoff/musicli-handoff.md`:

- Add phase 7 to completed stages.
- Update the pending phase list so phase 8 is next.
- Note the behavior decisions: `none`, `one`, `list`, shuffle, manual next/prev.

- [ ] **Step 5: Commit**

Run:

```bash
git status --short
git add cmd/musicli/main.go internal/ui/app.go internal/ui/styles.go internal/ui/app_test.go docs/handoff/musicli-handoff.md
git commit -m "feat: add playback modes"
```

Expected: commit succeeds on `dev`.

## Manual Test Checklist

Run the app with a small folder containing at least three short tracks:

```bash
go run ./cmd/musicli /path/to/music
```

Check:

- With `repeat = "list"` and `shuffle = false`, a finished track advances to the next track.
- The final track wraps to the first track.
- Pressing `r` changes player bar text through `repeat one`, `repeat none`, `repeat list`.
- With `repeat one`, the same song restarts when it ends.
- With `repeat none`, the final song stops after finishing.
- Pressing `s` changes player bar text between `shuffle on` and `shuffle off`.
- With shuffle on and at least two tracks, `n` does not stay on the same track.
- Album view selection still follows the actual current track after auto/manual advance.

## Self-Review

- Spec coverage: Covers phase 7 playback modes from the handoff: random playback, single-track repeat, and list repeat.
- Placeholder scan: No unfinished placeholder markers remain.
- Type consistency: Uses existing `Options`, `App`, `audio.State`, `tea.Cmd`, `trackList`, `library.Track`, and current test helper imports.
- Scope check: Does not implement playlists, spectrum, theme, lyric fetching, or keybinding config.
