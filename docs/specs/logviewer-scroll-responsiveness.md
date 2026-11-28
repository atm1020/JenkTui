# Log Viewer Scroll Responsiveness & Release-Stop

> **Status:** Planned (not yet implemented). Resolve file paths, symbols, and behavior against current source before implementing.

## Problem Statement

When a user holds the `Down` or `Up` scroll key (or `j`/`k`) for a sustained period in the log viewer (currently embedded in the Build page's inline `logPane`), scrolling continues executing for seconds *after* the user releases the key. Key repeats accumulate in the terminal input buffer and keep draining into the log panel after release.

### Root Cause

Two compounding issues cause the input backlog:

1. **Per-keystroke work falls behind OS key repeat (~30–50 keys/sec):**
   Bubble Tea v2's `Program.eventLoop` runs `model.Update(msg)` and `model.View()` synchronously for every single `tea.KeyPressMsg` before reading the next message from `p.msgs`. On each single-line scroll today:
   - `src/ui/components/logviewer/buffered_viewport.go`:
     - `BufferedViewport.ScrollDown` / `ScrollUp` calls `IsLastVisibleLine()` → `LastVisibleLine()` → `viewport.VisibleLineCount()`. In `charm.land/bubbles/v2/viewport`, `VisibleLineCount()` calls `len(m.visibleLines())`, which clones the visible slice, runs `ansi.Cut`, and renders every line's left gutter via lipgloss just to count how many lines are visible.
     - `updateCursorHighlight()` → `pushContent()` allocates and copies the entire ~250–300 line `rawLines` window and calls `viewport.SetContentLines(display)`. Inside `bubbles/v2/viewport`, `SetContentLines` scans every line in the buffer for `\r\n`, runs `maxLineWidth` (`ansi.StringWidth` across all 250–300 lines), and when `SoftWrap` is enabled runs `calculateLine(0)` (`ansi.StringWidth` across all 250–300 lines a second time).
   - `src/ui/components/build/build.go` & `sections.go`:
     - `Model.handleMsg` unconditionally calls `m.sections.invalidate()` on every non-`animation.FrameMsg` message, invalidating the cached `info`, `flow`, `details`, and `log` blocks on every log scroll keypress (and even when a keypress is dropped during chunk loading).
   - `src/ui/components/logviewer/file_loader.go`:
     - Crossing a chunk boundary (~every 100 lines) runs `Loader.LoadChunk(from, to, styleFn)`, which opens the file and linearly scans from line 0 with `bufio.Scanner` (taking ~5–6ms per chunk near the end of a 100k-line log before styling).
   - Whenever `Update + View` exceeds the OS key-repeat interval (~20–33ms), repeat keystrokes pile up in the kernel PTY buffer and `ultraviolet.TerminalReader`'s read buffer, then drain one full `Update + View` cycle at a time after key release.

2. **No input-level backlog shedding or key-release handling:**
   - JenkTui does not request `tea.View.KeyboardEnhancements.ReportEventTypes` (no `Key.IsRepeat` or `tea.KeyReleaseMsg`), and has no `tea.WithFilter` to shed queued scroll repeats when input bursts arrive faster than the frame rate (e.g., high OS repeat rate, very long lines, slow terminal emulator, or SSH burst).

## Agreed Design Decisions

Settled during interactive design review:

1. **Dual-layer fix (Performance + Input Guard):** Eliminate the per-keystroke render/update bottleneck *and* add input-level backlog protection so scrolling is fast enough that a backlog rarely forms and still stops immediately on release under extreme bursts or slow terminals.
2. **Backlog handling policy:** Coalesce/cap queued scroll bursts up to a small per-frame cap (preserving intentional rapid manual taps like `j-j-j`) and stop immediately on `tea.KeyReleaseMsg` on terminals that support Kitty keyboard event types.
3. **Scope:**
   - Render optimization: `logviewer` (`BufferedViewport`, `Model`) and Build page's `logPane` / `pageSections`.
   - Input backlog protection: scoped to `logviewer` scroll keys (`Up`, `Down`, `PageUp`, `PageDown`, `Left`, `Right` — in both normal and visual mode, never while `SearchView` or any dialog is active).
4. **Chunk-boundary loading:** Keep chunk loading asynchronous, cleanly drop scroll keys while loading without triggering full Build page re-renders, and add a byte-offset checkpoint index built during `ScanLines` so `LoadChunk` seeks in O(1) instead of rescanning from line 0.
5. **Input filter & Kitty `ReportEventTypes`:** Wire a testable `tea.WithFilter` in `main.go` paired with `view.KeyboardEnhancements.ReportEventTypes = true` in `src/ui/ui.go`, explicitly intercepting `tea.KeyReleaseMsg` in `App.Update` (and migrating `logviewer` from `tea.KeyMsg` to `tea.KeyPressMsg`) so release events never leak into other components that switch on `tea.KeyMsg`.

## Implementation Plan

### Phase 1: `logviewer` Scroll & Render Hot-Path Optimization

**Files:**
- `src/ui/components/logviewer/buffered_viewport.go`
- `src/ui/components/logviewer/viewer.go`
- `src/ui/components/logviewer/buffered_viewport_test.go`
- `src/ui/components/logviewer/logviewer_bench_test.go`

**Changes:**
1. **Eliminate per-step `viewport.SetContentLines` calls in `BufferedViewport`:**
   - Only call `viewport.SetContentLines` when the underlying chunk lines actually change (`SetContentLines`), when wrapping toggles, or when search-match styling changes.
   - Move cursor/visual-selection row background highlighting (`renderLineBackground`) so cursor movement does **not** clone all ~250–300 lines or re-run `maxLineWidth` across the whole buffer.
   - Options to evaluate during implementation (pick the cleanest one that passes all `buffered_viewport_test.go` assertions):
     - **Preferred:** Render visible rows directly in `BufferedViewport.View()` from `rawLines` (or maintain a lightweight cached `[]string` + cached `longestLineWidth` and apply `renderLineBackground` + current search match + gutter only to the `height` visible rows). This avoids `bubbles/v2/viewport.SetContentLines` altogether on scroll, keeps `View()` O(visible height) instead of O(buffer window), and lets us keep exact `XOffset`, `SoftWrap`, and gutter width behavior under our control.
     - **Alternative:** Keep `viewport.Model` for `View()`, using `viewport.StyleLineFunc` or patching only the 1–2 rows whose highlight state changed without triggering `maxLineWidth`. (Check `viewport.go:368-376`: `StyleLineFunc` wraps the line in `lipgloss.Style.Render`, which does *not* reassert background across embedded SGR resets the way `renderLineBackground` does — so rendering the visible window directly in `BufferedViewport` is cleaner and preserves the exact `renderLineBackground` contract.)
2. **Eliminate `viewport.VisibleLineCount()` on the scroll hot path:**
   - When `SoftWrap == false`, the visible line capacity is simply `max(0, min(len(rawLines)-yOffset, contentHeight))` — O(1) arithmetic with zero allocations, no `ansi.Cut`, and no gutter rendering.
   - When `SoftWrap == true`, compute wrapped line counts incrementally or cache per-line wrapped heights keyed by `(contentRev, contentWidth)` so scrolling never re-measures all 250–300 lines.
3. **Narrow `logviewer.Model.Update` key handling:**
   - Change `case tea.KeyMsg:` in `viewer.go` to `case tea.KeyPressMsg:` so `tea.KeyReleaseMsg` can never trigger a key action in `logviewer`.
   - Ensure `handleLogViewKeys` / `handleVisualViewKeys` return an observable signal (or expose `CursorLine` / `XOffset` / `YOffset` / `IsLoading`) if needed so the parent Build page can tell whether a keypress mutated state.

**Invariants to preserve:**
- Cursor highlight background continues across embedded ANSI foreground/background resets (`renderLineBackground`).
- Visual selection range (`v` + `Up`/`Down`/`gg`/`G` + `y`) highlights every selected visible line and yanks from disk via `Loader.LoadChunk`.
- Search matches + distinct current-match styling + `EnsureColumnVisible` horizontal scroll continue working identically.
- `JumpToLine`, `AdjustForScrollUpTo`, `AdjustForScrollDownTo`, `PageUp`, `PageDown`, `Left`, `Right`, `Wrap` toggle, and standalone + embedded modes behave identically.

---

### Phase 2: O(1) Chunk Seeking in `Loader`

**Files:**
- `src/ui/components/logviewer/file_loader.go`
- `src/ui/components/logviewer/file_loader_test.go`
- `src/ui/components/logviewer/logviewer_bench_test.go`

**Changes:**
1. **Build a sparse byte-offset index during `ScanLines()`:**
   - Store checkpoint byte offsets every `lineIndexStride` lines (e.g., `64` lines) in `Loader.lineOffsets []int64` while `ScanLines` already scans every byte of the file.
   - Memory footprint stays minimal even for huge files (~250 KB for a 2,000,000-line log vs ~16 MB for per-line offsets), while seeking requires scanning at most 63 lines before `from`.
2. **Use `file.Seek` in `LoadChunk(from, to, styleFn)`:**
   - Look up `checkpoint := from / lineIndexStride`. If `checkpoint < len(l.lineOffsets)`, `file.Seek(l.lineOffsets[checkpoint], io.SeekStart)` and start `idx := checkpoint * lineIndexStride` before creating the `bufio.Scanner`.
   - If `ScanLines()` was never called (e.g., direct `NewLoader` in tests), fall back gracefully to `idx = 0` from byte 0.
   - Guard concurrent safety: `ScanLines()` runs in `Init()` before any `LoadChunk()` is scheduled; store the completed slice once before returning from `ScanLines()` so `LoadChunk()` only reads an immutable slice (verify under `-race`).

---

### Phase 3: Narrow Build Page Section Invalidation

**Files:**
- `src/ui/components/build/build.go`
- `src/ui/components/build/sections.go`
- `src/ui/components/build/sections_test.go`

**Changes:**
1. **Split log-section cache invalidation from page-wide invalidation:**
   - In `pageSections` (`sections.go`), add an independent `logRev uint64` (or `invalidateLog()` method) so `cachedLogBlock` keys on `(rev + logRev)` while `info`, `flow`, and `details` key only on `rev`.
   - In `build.Model.handleMsg` (`build.go`):
     - When a `tea.KeyPressMsg` is handled strictly inside the focused `logPane` (`m.focusPane == focusLog` and not a page-level pane switch, zoom toggle, full-log toggle, or shared build action), invalidate **only** `m.sections.invalidateLog()` instead of `m.sections.invalidate()`.
     - When `m.logPane.viewer != nil && m.logPane.viewer.IsLoading()` drops a scroll key, skip invalidating `m.sections` altogether so dropped keys during chunk load cost near-zero CPU.
   - Result: scrolling the inline log pane reuses cached `build info`, `stages timeline`, and `steps` blocks verbatim on every frame.

---

### Phase 4: Input-Level Backlog Guard & Kitty `ReportEventTypes`

**Files:**
- `main.go`
- `src/ui/ui.go`
- `src/ui/scroll_filter.go` (new)
- `src/ui/scroll_filter_test.go` (new)
- `src/ui/components/build/build.go` (expose active log-viewer state)
- `src/ui/components/logviewer/viewer.go` (expose scroll-key matching helper)

**Changes:**
1. **Enable Kitty event-type reporting safely:**
   - In `App.View()` (`src/ui/ui.go`), set `view.KeyboardEnhancements.ReportEventTypes = true`.
   - In `App.Update()` (`src/ui/ui.go`), add an explicit `case tea.KeyReleaseMsg:` branch that returns `a, nil` (never letting `KeyReleaseMsg` fall through to `default: a.updateComponents(msg)`).
   - **Critical safety guard:** 7 other components (`errorpage`, `globalsearch`, `themeselector`, `favorites`, `folder`, `myruns`, `scriptview`) currently switch on `case tea.KeyMsg:`, which matches *both* `KeyPressMsg` and `KeyReleaseMsg`. Intercepting `tea.KeyReleaseMsg` in `App.Update` ensures zero double-triggering across the entire application.
2. **Expose scroll-mode query from `App` → `build.Model` → `logviewer.Model`:**
   - Add a narrow interface on active views (e.g., `LogScrollKeyMatcher` or `MatchLogScrollKey(tea.KeyPressMsg) (scrollAction, bool)`):
     - Returns `false` whenever `a.dialog != nil`.
     - On `build.Model`, returns `true` only when `m.focusPane == focusLog`, `m.logPane != nil`, `m.logPane.viewer != nil`, and `!m.logPane.viewer.InSearchView()`.
     - Matches against the user's configured `logviewer.KeyMap` bindings for `Up`, `Down`, `PageUp`, `PageDown`, `Left`, `Right` (so custom user keybindings in `config.json` work automatically, and visual-mode `Up`/`Down` are covered while search typing is never filtered).
     - Also matches `KeyReleaseMsg` against the same scroll bindings so the filter knows which scroll key was released.
3. **Implement `ScrollBacklogFilter` (`tea.WithFilter`) in `src/ui/scroll_filter.go`:**
   - Wire `tea.WithFilter(filter.Filter)` in `main.go` when constructing `tea.NewProgram(app, ...)`.
   - Inject a `now func() time.Time` clock for deterministic unit testing (defaulting to `time.Now`).
   - Track whether the terminal reported `tea.KeyboardEnhancementsMsg` with `msg.SupportsEventTypes()`.
   - **Behavior:**
     - **Non-scroll keys** (e.g., `Esc`, `/`, `q`, `v`, `y`, `z`, `Tab`, `gg`, `G`): always pass through immediately and reset scroll-burst state.
     - **Direction / action change** (e.g., `Down` → `Up`, or `Down` → `Left`): immediately reset burst state and pass through with zero delay.
     - **`tea.KeyReleaseMsg` for a scroll key:** clear active repeat/burst state and consume (return `nil` so it doesn't even trigger a redundant `Update`+`View`).
     - **When `SupportsEventTypes()` is true (Kitty-protocol terminals — Ghostty, Kitty, WezTerm, Alacritty, Foot, iTerm2, Windows Terminal):**
       - Fresh key press (`!msg.Key().IsRepeat`): always pass through immediately (zero risk of dropping intentional rapid taps or `tmux send-keys` sequences).
       - Repeat key press (`msg.Key().IsRepeat`): allow at most 1 repeat per frame budget (e.g., `16ms`, matching 60fps); any backlogged repeat arriving earlier returns `nil` (skipping both `Update()` and `View()` in nanoseconds).
     - **Fallback when `SupportsEventTypes()` is false (legacy terminals, SSH, tmux without `extended-keys`):**
       - Maintain a per-frame burst budget (e.g., up to 3 same-direction scroll keypresses per `16ms` window).
       - Rapid manual taps (`j-j-j`, typically 65–100ms apart, or small `tmux send-keys` bursts of ≤3 keys) pass through cleanly; deeper backlogs from a held key are shed before `Update()` and `View()` run so the buffer drains in microseconds on release.
     - **While `logviewer.IsLoading()` is true:** drop queued scroll keys in the filter itself (returning `nil`) so neither `Update()` nor `View()` runs while waiting for the async chunk read.

---

## Testing & Verification Plan

Per `CODING_STANDARDS.md`:

### 1. Unit & Behavior Tests
- **`src/ui/components/logviewer/buffered_viewport_test.go`:**
  - Verify `ScrollDown` / `ScrollUp`, cursor highlight background preservation across embedded SGR resets, visual selection range highlighting, horizontal `XOffset` scrolling, soft-wrap scrolling, and `JumpToLine` produce identical behavior after removing per-step `SetContentLines`.
- **`src/ui/components/logviewer/file_loader_test.go`:**
  - Verify `LoadChunk` with byte-offset index matches exact line contents across chunk boundaries (start, middle, end, stride boundaries, empty lines, unterminated final line, CRLF, long >64KiB lines, and `LoadChunk` called without prior `ScanLines`).
- **`src/ui/components/build/sections_test.go`:**
  - Verify scrolling inside focused `logPane` invalidates `sections.log` without invalidating cached `info`, `flow`, or `details` blocks, and that dropped scroll keys during `logviewer.IsLoading()` do not invalidate any section.
- **`src/ui/scroll_filter_test.go`:**
  - Deterministic fake-clock tests covering:
    - Kitty `SupportsEventTypes`: fresh taps (`IsRepeat=false`) never dropped; held repeats (`IsRepeat=true`) shed inside frame window; `KeyReleaseMsg` clears repeat state immediately.
    - Legacy fallback (`SupportsEventTypes=false`): manual taps and small bursts (≤cap) preserved; 200-key held-down backlog shed cleanly.
    - Direction reversal (`Down` → `Up`) passes through immediately.
    - Non-scroll keys (`Esc`, `/`, `q`, `v`, `y`) never filtered.
    - Dialog open or `logviewer` in `SearchView` bypasses filter completely (typing `j`/`k`/`h`/`l` in search is untouched).
    - Custom user keybindings from `LogviewerKeyMapConfig` are respected.
    - `App.Update` ignores `tea.KeyReleaseMsg` and never forwards it to `errorpage`, `globalsearch`, `themeselector`, `favorites`, `folder`, `myruns`, `scriptview`, or `logviewer`.

### 2. Benchmarks & Performance Gate
- Add benchmarks in `src/ui/components/logviewer/logviewer_bench_test.go` for `BufferedViewport.ScrollDown` + `View()` (soft-wrap off & soft-wrap on, 250-line window with styled lines) and compare `BenchmarkLoaderLoadChunk/end` before vs after.
- Run mandatory performance A/B verification per `CODING_STANDARDS.md#performance-verification`:
  - `go run ./dev/perf doctor`
  - `go run ./dev/perf ab logviewer-scroll --base <before-ref> --page build`
  - Evaluate adding a dedicated log-scroll offline workload on `src/ui/perfprofile` (per `CODING_STANDARDS.md#verification-maintenance`).

### 3. Completion Gates & Live Verification
- Run `nix develop --command task fmt && nix develop --command task check && nix develop --command task build` (includes `CGO_ENABLED=1 go test -race ./...` and `golangci-lint`).
- Run mandatory live verification per `CODING_STANDARDS.md#live-verification` using `dist/bin/jt` against local Docker Jenkins:
  - Open a build with a multi-hundred-line log in both split and zoomed (`z`) log pane modes.
  - Send a sustained burst of `Down` / `Up` keys, verify scrolling advances cleanly and stops immediately after the burst ends (no post-release drift across consecutive snaps), and verify search (`/`), visual select (`v`/`y`), wrap (`w`), and chunk boundaries work cleanly.
  - Always run `dist/bin/jt down` after verification.

## Risks & Edge Cases to Watch

- **`tea.KeyMsg` double-fire hazard:** Enabling `ReportEventTypes = true` causes supporting terminals to emit `tea.KeyReleaseMsg` for every key. `App.Update` **must** explicitly intercept `tea.KeyReleaseMsg` before `default:` so none of the 7 views/dialogs matching `tea.KeyMsg` fire twice.
- **`dev/jt` / `tmux send-keys` bursts:** Live verification scripts and tmux macros send multiple keys rapidly without `IsRepeat`. Keeping a small fallback burst cap (e.g., 3–4 keys per 16ms) ensures `jt` recipes sending a few `Down` keys keep working deterministically.
- **Soft-wrap height math:** When `SoftWrap` is enabled, a single logical line can occupy multiple visual rows. Visible-line calculations and cursor scrolling must preserve existing soft-wrap behavior and pass all existing `buffered_viewport_test.go` tests.
- **Visual selection & search highlighting:** Moving highlighting out of `SetContentLines` must preserve the layering order: base styled line → search match highlight → current match highlight → cursor/visual row background (reasserted across embedded SGR resets).
- **Custom keybindings:** Users can rebind `Up`/`Down`/`PageUp`/`PageDown`/`Left`/`Right` in `config.json`. The input filter must match against `ctx.Config.KeyMapConfig.Logviewer` (via `logviewer.KeyMap`), never hardcoded `"up"`/`"down"`/`"j"`/`"k"`.
