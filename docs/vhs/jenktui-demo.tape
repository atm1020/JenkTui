# JenkTui showcase
#
# Prerequisites:
#   - Install VHS and DroidSansM Nerd Font.
#   - Configure JenkTui against the repository's local Jenkins controller.
#   - Seed the demo jobs with `task jenkins:lts` and `task seed`.
#   - Use JenkTui's default keybindings.
#   - Do not trigger other jobs concurrently during the recording.
#
# Existing My Runs history is OK: replay the run created by this recording,
# not a second row that may not exist. Log waits support full/medium headers.
#
# Render from the repository root with `task demo`.

# Intermediate recording; `task demo` converts it to the README GIF and removes it.
Output dist/jenktui-demo.mp4

Require ./jenktui

Set Shell "bash"
Set Theme "GitHub Dark"
Set FontFamily "DroidSansM Nerd Font"
Set FontSize 28
Set LineHeight 1.1
Set Width 2560
Set Height 1440
Set Padding 0
Set Framerate 20
Set PlaybackSpeed 1.0
Set LoopOffset 0%
Set CursorBlink false
Set TypingSpeed 50ms
Set WaitTimeout 30s

# Start directly in the TUI; do not record the shell command or startup redraws.
Hide
Type "./jenktui"
Enter
Wait+Screen /Jenkins/
Sleep 500ms
Show
Sleep 1s

# Find the seeded mobile pipeline and open its job page.
Ctrl+P
Wait+Screen /Global Search/
Ctrl+R
Wait+Screen /Global Search/
Type@50ms "mobile-app"
Enter
Wait+Screen /‣ .*mobile-app/
Enter
Wait+Screen /│ #[0-9]+ │/
Sleep 700ms

# Trigger mobile-app, then show its live row in the job-scoped My Runs view.
# Open that row rather than relying on the job page's selected build.
Ctrl+B
Wait+Screen /Are you sure you want to trigger/
Sleep 300ms
Type "y"
Wait+Screen /Build triggered/
Sleep 3s
Type "m"
Wait+Screen /My Runs/
Wait+Screen /In progress/
Sleep 1500ms
Enter
Wait+Screen /Status: In progress/
Sleep 2500ms

# Return to My Runs and replay this recording's run. Jenkins exposes the
# replay script even while it is running; no older completed row is needed.
Type "m"
Wait+Screen /My Runs/
Wait+Screen /In progress/
Sleep 1500ms
Type "r"
Wait+Screen /pipeline/
Sleep 700ms
Type "r"
Wait+Screen /Build triggered/
Wait+Screen /│ #[0-9]+ │/
Sleep 1500ms

# Find the seeded large-log pipeline.
Ctrl+P
Wait+Screen /Global Search/
Ctrl+R
Wait+Screen /Global Search/
Type@50ms "log-heavy-job"
Enter
Wait+Screen /‣ .*log-heavy-job/
Enter
Wait+Screen /│ #[0-9]+ │/
Sleep 700ms

# Open its console log, jump to the end and back to the beginning,
# then search, move through three matches, and jump back to the top.
# Medium-width layouts omit Loaded: and Scroll:, but retain Line: and percent.
Type "L"
Wait+Screen /log-heavy-job.*console/
Wait+Screen /Line: 1([ \/]).*\| (Scroll: )?0%/
Sleep 700ms
Type "G"
Wait+Screen /\| (Scroll: )?100%/
Sleep 700ms
Type@70ms "gg"
Wait+Screen /Line: 1([ \/]).*\| (Scroll: )?0%/
Sleep 700ms

# Page down three times and back up three times using the default { / } bindings.
Type@50ms "{{{"
Sleep 300ms
Type@50ms "}}}"
Wait+Screen /Line: 1([ \/]).*\| (Scroll: )?0%/
Sleep 500ms

Type "/"
Type@50ms "Processing batch"
Sleep 500ms
Enter
Wait+Screen /1\/500/
Sleep 700ms
Type@30ms "nnn"
Wait+Screen /4\/500/
Sleep 300ms
Type@70ms "gg"
Wait+Screen /Line: 1([ \/]).*\| (Scroll: )?0%/
Sleep 700ms

# Exit without exposing the shell prompt in the recording.
Hide
Ctrl+C
Sleep 1s
