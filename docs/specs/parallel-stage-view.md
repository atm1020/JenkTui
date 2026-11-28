# Parallel Stage View & Timeline

## Problem Statement

When a Jenkins pipeline executes stages in parallel (such as running unit tests, integration tests, and linting concurrently inside a `parallel` block), JenkTui currently flattens every stage into a single horizontal strip of equal-width boxes on both the Job page and the Build page.

This creates several problems for developers monitoring and debugging builds:

- **Parallel work looks sequential:** Concurrent stages appear side by side as if they ran one after another, hiding true concurrency and making wall-clock duration hard to reconcile with individual stage durations.
- **Bottlenecks are invisible:** Equal-width boxes give a 3-second lint stage the same visual footprint as a 7-minute integration suite, so users cannot tell at a glance which parallel branch is holding up the build.
- **Wide pipelines overflow horizontally:** Pipelines with many parallel branches stretch far beyond the terminal width, forcing horizontal scrolling on both the Job page and the Build page.
- **Failed builds open on the wrong stage:** Opening a failed build focuses the first stage (typically checkout) rather than jumping straight to the failing parallel branch.

## Solution

Replace the flat stage strip on the Build page with an Omaci-inspired **Stage Timeline View** and group parallel stages into compact **Parallel Group Cards** on the Job page's Stage View:

- **Build page Stage Timeline View:** Every stage is rendered as a row on a shared horizontal time axis using monospace block bars (`█` for completed/elapsed time, shaded/pulsing bars for active stages, and a vertical `now` marker while running). Parallel branches appear indented under their parallel group with tree connectors (`├─`, `└─`), showing exact concurrent overlap, critical-path bottlenecks, status icons, and durations in one scannable pane with zero horizontal overflow.
- **Automatic hierarchy accuracy with graceful fallback:** Whenever a Jenkins controller has the optional `pipeline-graph-view` plugin installed, JenkTui silently detects it once per server and uses its authoritative recursive stage tree to drive exact nesting (including nested parallel/sequential blocks, scripted parallels, and skipped branches) inside the timeline view. On servers without `pipeline-graph-view`, JenkTui automatically falls back to standard `pipeline-rest-api` (`wfapi`) stage timing overlap and wrapper grouping to render the exact same timeline experience with zero extra plugin requirements.
- **Job page Parallel Group Cards:** On the multi-run Job Stage View, parallel stages within each run collapse into a single fixed-height card displaying branch rows with `├─`/`└─` tree connectors, status indicators, and durations—eliminating horizontal sprawl while preserving smooth vertical scrolling and fast animation frames.
- **Smart auto-focus:** Opening a failed or running build automatically focuses the first failed stage (or active/running branch if none failed) and loads its stage details immediately.

## User Stories

1. As a Jenkins developer inspecting a build with parallel stages, I want to see all stages plotted on a shared horizontal time axis, so that I can immediately tell which stages ran concurrently and which branch is the bottleneck.
2. As a Jenkins developer monitoring a running build, I want to see a live `now` marker on the timeline and animated/shaded bars for currently running stages, so that I know how far along each parallel branch is in real time.
3. As a Jenkins developer inspecting a build with parallel stages, I want parallel branches visually indented under their parent parallel group with tree connectors, so that the hierarchy between sequential stages and concurrent branches is immediately readable.
4. As a Jenkins developer debugging a failed build, I want the Build page to automatically focus the first failed stage or parallel branch on open, so that I can inspect the failing steps and logs without manually hunting across stages.
5. As a Jenkins developer watching an active build with no failures yet, I want the Build page to focus the currently executing or paused stage on open, so that I immediately see live progress.
6. As a Jenkins developer on the Build page, I want to navigate through all stages and parallel branches using the existing previous-stage and next-stage keys, so that I do not have to learn new navigation mechanics to inspect parallel branches.
7. As a Jenkins developer selecting a parallel branch in the timeline, I want the Stage Details pane below to load that branch's flow nodes and allow opening step logs as usual, so that debugging a parallel branch feels identical to debugging a sequential stage.
8. As a Jenkins developer navigating a Declarative parallel block whose outer wrapper stage contains no steps of its own, I want stage navigation to step cleanly across the actionable parallel branches while still showing the wrapper as the group heading with aggregate status and wall-clock duration, so that I do not waste keypresses on empty container stages.
9. As a Jenkins developer browsing a build with many stages or large parallel matrices, I want the Build page timeline to scroll vertically within its viewport without horizontal overflow, so that all branches remain reachable on standard terminal sizes.
10. As a Jenkins developer viewing a build on a narrow terminal, I want stage names and timeline bars to truncate and scale cleanly to the available width, so that the layout never wraps or corrupts rows.
11. As a Jenkins developer whose Jenkins server has the `pipeline-graph-view` plugin installed, I want JenkTui to detect it automatically without requiring configuration or administrator permissions, so that I get exact parallel nesting out of the box.
12. As a Jenkins developer using a least-privilege Jenkins token (read-only job access without administrator privileges), I want plugin detection to work without querying admin-only plugin manager endpoints, so that parallel stages work under enterprise security policies.
13. As a Jenkins developer whose Jenkins server does not have `pipeline-graph-view` installed, I want JenkTui to fall back seamlessly to `wfapi` timing data without showing error dialogs or repeating failed probes on every build, so that the app remains fast and quiet.
14. As a Jenkins developer using an older or newer version of `pipeline-graph-view`, I want JenkTui to support both its modern and legacy tree endpoints and tolerate minor field-format variations, so that the feature works reliably across Jenkins installations.
15. As a Jenkins developer experiencing a transient server error when fetching the optional graph tree, I want JenkTui to fall back to the `wfapi` timeline for that build without permanently disabling graph detection for the session, so that temporary hiccups self-heal on later builds.
16. As a Jenkins developer working across multiple Jenkins servers, I want graph-plugin capability detection to be scoped per server and reset when I switch servers, so that one server's missing plugin never disables graph support on another server.
17. As a Jenkins developer watching a live running build on a `pipeline-graph-view` server, I want the stage tree and timeline to refresh as polling snapshots arrive, so that newly started parallel branches appear automatically without manually reloading.
18. As a Jenkins developer with a pipeline containing nested sequential stages inside parallel branches (or nested parallels) on a `pipeline-graph-view` server, I want the timeline to reflect the nested hierarchy accurately, so that complex matrix pipelines remain understandable.
19. As a Jenkins developer with a scripted `parallel` pipeline on a `pipeline-graph-view` server, I want each parallel branch to appear in the timeline even when it does not wrap an inner Declarative stage, so that scripted pipelines benefit from parallel visualization too.
20. As a Jenkins developer with a Declarative pipeline on a `wfapi`-only server, I want overlapping parallel branches to group cleanly under their preceding empty wrapper stage and display the group's true wall-clock span rather than only the pre-fork setup duration, so that the summary timing matches reality.
21. As a Jenkins developer with a pipeline containing fast zero-millisecond or not-executed stages on a `wfapi`-only server, I want sequential instant stages never to be falsely merged into a parallel group, so that linear pipelines still render linearly.
22. As a Jenkins developer viewing a build that failed before reaching any stages (such as a Jenkinsfile syntax error), I want the Build page to continue showing its clear "no stage data" placeholder, so that broken builds remain informative.
23. As a Jenkins developer viewing a queued build with no stages yet, I want the Build page to continue showing its queued indicator until stages begin, so that queue state stays clear.
24. As a Jenkins developer scanning build history on the Job page's Stage View, I want parallel stages in each build row to collapse into a single compact Parallel Group Card instead of stretching horizontally across the screen, so that I can see an entire pipeline run at a glance.
25. As a Jenkins developer scanning a Job page whose builds contain more than five parallel branches, I want the Parallel Group Card to stay fixed-height and prioritize showing failed, running, and paused branches first along with an overflow summary count (such as `+N more`), so that row height and scrolling stay consistent while urgent branches stay visible.
26. As a Jenkins developer viewing the Job page, I want a Parallel Group Card's border color and status indicator to reflect the worst status among its branches (failed over running over paused over unstable over success), so that a single red branch inside a parallel block is immediately obvious from the job overview.
27. As a Jenkins developer viewing running builds on both the Job page and the Build page, I want animations (spinners and running-stage indicators) to stay smooth and low-CPU via existing section and row caching, so that parallel stage rendering does not degrade terminal responsiveness.
28. As a Jenkins developer using custom or built-in JenkTui themes, I want timeline bars, tree connectors, focus highlights, and status icons to use semantic theme palette tokens and preserve the transparent terminal background, so that the view looks cohesive across all themes.
29. As a Jenkins developer leaving the Build page while a graph tree request is still in flight, I want the request to be cancelled cleanly and any stale response dropped, so that navigation stays responsive and cannot corrupt another page.

## Implementation Decisions

- **Single Build-Page Visualization (No 2D Fork/Join Box Graph):** The Omaci-inspired Stage Timeline View is the sole parallel-capable stage visualization on the Build page. No separate 2D fork/join box-diagram renderer or view-mode toggle is included; both data sources feed the same unified timeline layout.
- **Unified Stage Graph Domain Module:** Introduce a dedicated, UI-independent stage graph module responsible for representing a build's stage hierarchy and timeline geometry. Both the Job page and the Build page consume this module so parallel grouping rules never diverge between views.
  - Core domain representation captures the data source (`plugin-graph` vs. `wfapi`), run start/end bounds, active/running state, and a recursive list of stage nodes carrying node ID, name, normalized pipeline status (reusing the existing build-status domain package), start timestamp in milliseconds, duration in milliseconds, execution agent name, parallel flag, synthetic/wrapper flag, and child nodes.
  - Timeline layout calculation lives in the same domain module as pure functions mapping stage intervals to horizontal bar start/length cells, axis tick labels, and `now` marker placement for a given viewport width.
- **Optional `pipeline-graph-view` Client & Per-Server Capability Cache:** Introduce a lightweight Jenkins graph client module paired with a thread-safe per-server capability cache held on the shared program context and cleared whenever the active Jenkins server changes.
  - Capability states: `unknown`, `overview` (modern tree endpoint), `legacy` (older tree endpoint), and `unavailable`.
  - Probing happens lazily on the first build open for a server: attempt the modern `pipeline-overview/tree` endpoint first, then the legacy `pipeline-graph/tree` endpoint on HTTP 404. Only HTTP 404 on both endpoints transitions the cache to `unavailable`. Transient network errors or 5xx responses fall back to `wfapi` for that build without poisoning the cache.
  - Decoding tolerates schema differences across `pipeline-graph-view` versions (including recursive `children`, `nextSibling` chains, stage types such as `STAGE`/`PARALLEL`/`PARALLEL_BLOCK`, and numeric or formatted duration/timestamp fields). Exact millisecond `startTimeMillis` and `durationMillis` from `wfapi` are joined onto plugin tree nodes by matching node ID whenever available so timeline bars always have precise timing.
- **`wfapi` Parallel Inference (Universal Baseline):** When `pipeline-graph-view` is unavailable (and always on the multi-run Job page), reconstruct parallel groups purely from `wfapi` stage data with zero additional HTTP calls:
  - Cluster consecutive stages whose active execution intervals strictly overlap (requiring positive duration or simultaneous `IN_PROGRESS` status, with a small jitter guard so adjacent zero-millisecond stages are not falsely merged).
  - Attach an immediately preceding Declarative wrapper stage when its flow-node list is empty (when full stage details are present) or its end time aligns tightly with the start of the overlapping cluster, computing the group's displayed wall-clock span from the earliest start to the latest branch end.
  - Leave non-executed or zero-start stages outside overlap clusters unless already grouped by the plugin tree.
- **Build Page Integration:**
  - During build loading, fetch the optional plugin tree concurrently or alongside existing build/run metadata under the page's cancellable request context and generation counter; drop stale responses and cancel in-flight requests on `CleanUp`.
  - During live polling updates (`ApplyPollSnapshot`), update `wfapi` stage data immediately and refresh the plugin tree when active and running, guarded by generation and cancellation hooks.
  - Render the timeline inside the existing memoized flow section so static sections (header info, artifacts, idle stage details) remain cached across animation frames.
  - Upgrade initial stage focus selection to prioritize: first failed navigable stage → first executing/paused navigable stage → first navigable stage.
  - Preserve existing `PrevStage` / `NextStage` navigation bindings, stepping cleanly over navigable stages in tree order and updating `StageDetails` (and step log fetching via `wfapi`) using each stage's Jenkins node ID.
- **Job Page Stage View Integration:**
  - Continue relying solely on `wfapi/runs` (never issuing per-run plugin-graph requests from the Job list).
  - Render single sequential stages with the existing stage box, and render parallel groups as fixed-height Parallel Group Cards matching the standard row height so vertical scrolling math, row caching, and fast viewport windowing remain intact.
  - Cap visible branches inside a Parallel Group Card to fit the fixed height (prioritizing failed, running, and paused branches, then remaining branches, with an overflow line such as `+N more` when exceeded) and color the card border by aggregate worst child status.
  - Include stage start timestamps in the row cache stamp so live polling updates that change parallel grouping invalidate the affected row cleanly, and ensure running branches inside a group keep row animations active.
- **Styling & Theme Compliance:** Use semantic tokens from the shared theme styles package (`Success`, `Error`, `Warning`, `Primary`, `SubtleText`, `Border`, `SelectedBackground`, and stage status tokens) with zero hardcoded hex colors and without painting full-screen `BgBase` backgrounds.

## Testing Decisions

A good test verifies observable behavior at a stable external boundary—inputs in, rendered state, navigation transitions, emitted commands, and HTTP traffic out—without asserting on private helper structure or brittle full-screen string snapshots that break on terminal width changes.

- **Primary Seam — Fake Jenkins HTTP Server + Bubble Tea Page Models (Build page and Job page):**
  - Prior art: existing Build page and Job page test suites already drive `Init`, execute returned commands, dispatch `Update` messages, and assert on state, emitted commands, rendered output, and HTTP interactions against `httptest.NewServer`.
  - Test the full Build page flow against fake Jenkins servers covering:
    - `pipeline-graph-view` modern endpoint (`200`), legacy endpoint fallback (`404` → `200`), and missing plugin (`404`/`404` → cached `unavailable` with zero repeat probes on a second build load).
    - Non-sticky fallback on transient `5xx` errors and cache reset on server switch.
    - Exact cancellation on `CleanUp` and dropping stale generation results.
    - Timeline rendering for both plugin-sourced and `wfapi`-sourced parallel builds, including auto-focusing failed and running parallel branches, `PrevStage`/`NextStage` navigation updating `StageDetails`, live `ApplyPollSnapshot` updates, and queued/no-stage-data states.
  - Test the Job page Stage View flow against fake Jenkins `/wfapi/runs` payloads covering:
    - Parallel Group Card rendering, aggregate worst-status styling, fixed row-height preservation, overflow prioritization (>5 branches), animation detection for running branches, and row cache invalidation when stage start times or statuses change.
- **Secondary Seam — Pure Stage Graph & Timeline Domain Module:**
  - Table-driven unit tests for algorithmic edge cases that are noisy to drive through HTTP: nested `parallel → sequential → parallel`, scripted parallel branches, `nextSibling` chains, mixed numeric/string plugin durations, `0ms` sequential stages not falsely grouped, live open-ended `IN_PROGRESS` overlap, wrapper attachment with real `webapp-build`-shaped timestamps, overflow branch prioritization, and timeline cell/axis math across narrow and wide widths.
- **Repo Verification Gates:**
  - Standard check and build gates (`task check` with race detection and `task build`).
  - Live verification via the repo's real-binary tmux harness (`verify-jenktui`) against local Docker Jenkins using the existing parallel pipeline seed job (`DevTeam/Frontend/webapp-build`), plus updating the matching job-and-build live verification recipe.
  - Performance A/B verification via `jenktui-perf-test` on both the Job page and the Build page to confirm no frame-path regression.

## Out of Scope

- **2D fork/join box-diagram graph renderer** and view-mode switching between diagram and timeline views.
- **Blue Ocean REST API** (`/blue/rest/...`) support (officially deprecated by Jenkins).
- **HTML scraping of `/flowGraphTable/`** to infer stage trees.
- **Historical ETA forecasting, uncertainty bands, build-history sparklines, and previous-build comparison deltas** (separate future specs).
- **MultiBranch folder graph-style branch hierarchy** (separate future spec).
- **Inline split-pane step log viewer or progressive live log tailing** (separate future specs).
- **Requiring users to install `pipeline-graph-view`** on their Jenkins controllers.

## Further Notes

- **Live Jenkins `wfapi` Behavior Verified (`2.568.2`):** Against the existing `DevTeam/Frontend/webapp-build` seed pipeline (`Install` → `Parallel Tests { Unit, Integration, Lint }` → `Bundle`), `/wfapi/describe` returns a flat `stages` list with no `parentNodes` on top-level stage objects. Notably, the Declarative wrapper stage (`Parallel Tests`) reports only its brief pre-fork setup time (`~350ms`) and `stageFlowNodes: []`, ending at the exact millisecond the first parallel branch (`Unit`) begins, while `Unit`, `Integration`, and `Lint` overlap in `[startTimeMillis, startTimeMillis + durationMillis]`. The `wfapi` grouping logic is explicitly designed around this real wire shape rather than assuming wrapper stages span the full parallel duration.
- **Least-Privilege Compatibility:** Avoiding `/pluginManager/api/json` keeps JenkTui aligned with its documented minimal Jenkins permission requirements (`Overall/Read` and `Job/Read`).
- **Verification Maintenance:** Implementing this spec should update the job-and-build live verification recipe to cover parallel stage rendering on `DevTeam/Frontend/webapp-build` (and update performance reference notes if any deliberate frame-path changes occur).
