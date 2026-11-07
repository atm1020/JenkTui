# AGENTS.md

Canonical agent entrypoint. `CODING_STANDARDS.md` owns consequential rules and
completion gates; read only the sections triggered by your task, before editing
or reviewing. Multiple routes can apply.

## Safety

Do not read, print, commit, or copy Jenkins credentials. Never modify a developer's
`~/.config/JenkTui/config.json` for a repo change; use disposable test state.
Resolve versions, commands, and behavior from `go.mod`, `Taskfile.yml`, and source,
not old plans or examples. Check design-note status banners before following them.

## Task routes

- **Code changes/reviews:** [completion gates](CODING_STANDARDS.md#completion-gates)
  and [testing](CODING_STANDARDS.md#testing).
- **UI models, dialogs, navigation:** [UI architecture](CODING_STANDARDS.md#ui-architecture).
- **Polling, runs, triggers, persistence, notifications, job identity/status:**
  [polling and alerting](CODING_STANDARDS.md#polling-and-alerting).
- **Config, directories, keybindings:** [configuration](CODING_STANDARDS.md#configuration).
  Before implementing configuration, directory/XDG, cross-platform, or Bubble Tea
  patterns, follow [reference-repos](.agents/skills/reference-repos/SKILL.md).
- **Rendering/styles:** [styling](CODING_STANDARDS.md#styling).
- **Application changes:** mandatory [live verification](CODING_STANDARDS.md#live-verification)
  for `src/{ui,poll,store,notification,config,context,buildstatus,jobref}/` or `main.go`.
- **Frame-path changes or any performance claim:** mandatory
  [performance verification](CODING_STANDARDS.md#performance-verification).
- **Features/keys/views/dialogs/footer, defaults/wait handles/history/config/paths,
  verification harnesses/workloads/benchmarks/CLI/output, seed jobs/fixtures,
  deliberate job/build performance changes:**
  [same-change maintenance](CODING_STANDARDS.md#verification-maintenance).

