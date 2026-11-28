# Console log viewer (integrated Build & Log page)

The console log viewer is integrated directly into the Build page's `log` panel
(replacing separate `joblog` and `flowlogviewer` screens). Opening a log via
`L` from Job or My Runs lands straight in the Build page's zoomed full-console
log view (`zoomed`), and pressing `z` (or `Esc`) unzooms into the full split
Build + Stages + Steps + Log view. It loads logs in chunks, indexes stage/step/
outside-of-stage lines from Jenkins' `/logText/progressiveHtml` (with
`/consoleText` fallback), supports vim-style movement, and searches within the
log with a match counter and next/previous navigation.

## Sub-features

- `log-open`: `L` on a job page (stage pane's selected build) or My Runs row
  opens the unified Build page in zoomed full console log mode (`zoomed`,
  footer breadcrumb ends with `console`). On the build page, `L` toggles Full
  Console ↔ scoped step/stage log; `z` toggles zoomed tall-log mode (`Esc`
  leaves zoom).
- `log-scopes`: `1` full log (with instant stage/step jump), `2` stage log
  (de-interleaved across parallel branches), `3` step log, `4` outside-of-stage
  log (Pre-stage setup & Post-stage errors/stacktraces).
- `log-status`: a status line `Loaded: a-b/N | Line: x | Scroll: y%` in the
  embedded log toolbar.
- `log-move`: when the log pane is focused (`Tab`/`ctrl+l`/zoomed), `j`/`k`
  move by line, `{` pages down, `}` pages up, `G` (or `end`) jumps to the
  bottom, and `gg` (or `home`) jumps to the top.
- `log-search`: `/` focuses the inline search input (also works directly from
  the steps pane). Type the query and press Enter to show a `k/N` match
  counter (`Esc` cancels search). `n` and `p` go to the next and previous match.
- `log-wrap`: `w` toggles line wrapping.
- `log-visual`: `v` starts visual (line selection) mode, `y` yanks the
  selection to the clipboard, and `esc` exits.

## How to get to it (user POV)

- Job page → `L` (unified Build page opened in zoomed full console log mode).
- Build page → inline `log` pane (auto-loaded on open; `Tab`/`ctrl+l` focuses
  it, `1`/`2`/`3`/`4` switch scopes, `L` toggles full/step, `z` zooms).
- My Runs row → `L` (unified Build page opened in zoomed full console log mode).

## Driving it with jt

Preconditions:

- The instance is ready. `DevTeam/Backend/log-heavy-job` has a completed
  build (check with
  `$J jenkins RUN '/job/DevTeam/job/Backend/job/log-heavy-job/api/json?tree=lastCompletedBuild[number]'`).
  If it has none, trigger one first, following
  [trigger-and-my-runs.md](./trigger-and-my-runs.md).

Steps:

- **Open the job.**
  - Run `$J keys RUN C-p && $J wait RUN 'Global Search'`.
  - Then run `$J keys RUN C-r && $J wait RUN 'Global Search' && $J type RUN log-heavy-job && $J keys RUN Enter && $J wait RUN '‣ .*log-heavy-job'`.
  - Then run `$J keys RUN Enter && $J crumb RUN 'Backend.*log-heavy-job' && $J wait RUN '│ #[0-9]+ │'`.
    If you skip the build-box wait, the job page silently drops `L`.
- **Open the log.**
  - Run `$J keys RUN L && $J crumb RUN 'log-heavy-job.*console' && $J wait RUN 'Loaded:' && $J wait RUN 'zoomed'`.
  - Expect `Loaded: a-b/N | Line: 1 | Scroll: 0%`, with the actual log's
    total. Buffer windows depend on viewport size, not fixed 250-line
    chunks. The footer matches `log-heavy-job.*console`.
- **Jump to the bottom and back.**
  - Run `$J keys RUN G && $J wait RUN 'Scroll: 100%'`. The status line
    shows a loaded range ending at the actual total, proving the tail
    window was fetched.
  - Then run `$J type RUN gg && $J wait RUN 'Line: 1 .*Scroll: 0%'`.
- **Search.**
  - Run `$J keys RUN / && $J type RUN 'Processing batch' && $J keys RUN Enter && $J wait RUN '1/500'`.
  - The first match is highlighted.
- **Next and previous match.**
  - Run `$J type RUN nnn && $J wait RUN '4/500'`.
  - Then run `$J keys RUN p && $J wait RUN '3/500'`.
- **Unzoom into the unified Build view.**
  - Run `$J keys RUN z && $J wait RUN 'Build #' && $J wait RUN 'stages'`.
  - Confirms `Job -> L` opens the integrated Build + Log page and preserves the
    inline search/cursor state when switching between zoomed and split views.
- **Proof.**
  - Run `$J snap RUN log-search-1` after the search.
  - Run `$J snap RUN log-search-4` after `nnn`.
  - The `Loaded: …/N` total should match the line count of
    `$J jenkins RUN '/job/DevTeam/job/Backend/job/log-heavy-job/lastBuild/consoleText'`
    (pipe it through `wc -l`, allowing ±1 for a trailing newline).

## Gotchas

- `gg` is a two-key chord. Send both keys with `$J type RUN gg`.
  An intervening movement key clears the pending `g`.
- Page keys look reversed: by default, `{` pages **down** and `}` pages
  **up**.
- After a search, the focus stays in the search box until you press Enter.
  Typing `n` before Enter adds an `n` to the query.
- `y` writes the **host** clipboard (it escapes the sandbox). Use it only
  when verifying yank, and say so in the proof.
- Full console opening downloads a snapshot; it does not continuously
  tail a running build. Reopening can fetch newer output. Use a completed,
  numbered build for count comparisons.
- Full `Loaded:` and match-counter handles require a wide terminal; keep
  the default 160 columns for these recipes.
