# API-token trigger and replay preflight

Trigger and replay use the canonical `/crumbIssuer/api/json` preflight URL. An unavailable issuer may return HTML 404 without preventing an API-token write. Existing HTML error diagnostics remain available for genuine JSON-response failures.

## Sub-features

- `crumb-url`: request `/crumbIssuer/api/json` once, including under a Jenkins context path.
- `crumb-token`: API-token auth reaches the preflight and the subsequent write.
- `crumb-unavailable`: a non-200 issuer response permits a crumbless write; transport errors and invalid HTTP 200 JSON still fail.
- `crumb-trigger-replay`: successful writes create Jenkins builds and tracked intents.

## How to get to it (user POV)

Trigger with `ctrl+b` from a job, build, or My Runs row. Replay with `r` from each of those entry points, then `r` again after the script loads.

## Driving it with jt

Preconditions: follow `SKILL.md`; provision a disposable API token through local Jenkins administration and export it as `JT_JENKINS_TOKEN` without printing it. Use an isolated RUN per server. Seed data must exist. Repeat against LTS on port 8080 and Jenkins 2.479.3 on port 8081 using `JT_JENKINS_URL`.

- **Strict-routing regressions:** run `go test ./src/ui/buildaction ./src/ui/components/scriptview`. The API-token tests require the exact crumb URL and assert one write, for both a valid crumb and HTML 404. Run the dependency's `TestAPITokenTriggerAndReplayCrumbPreflight` for context-path coverage. Docker success alone cannot prove canonical routing: some Jenkins versions accept duplicated `/api/json` segments.
- **Start:** `$J up RUN && $J doctor RUN`. Record Jenkins version and API-token auth (not its value) with `$J note`.
- **Trigger entry points:** follow the non-parameterized job/build/My Runs steps in [trigger-and-my-runs.md](trigger-and-my-runs.md). Snap before confirmation and after the success toast. Verify each intent resolves to its Jenkins queue/build. Repeat the three entry points with `deploy-service` and verify its `ENVIRONMENT` parameter reaches Jenkins.
- **Replay entry points:** open the selected completed build's script from the job page, build page, and My Runs. After each `r`, wait for `mainScript.groovy` and pipeline source before the second `r`. Snap before submission and after `Build triggered, polling started`. Verify the new Jenkins build has `Replayed #N` in its cause and that its queue-less intent resolves in `runs.db`.
- **Diagnostics non-regression:** follow [error-details.md](error-details.md) using a disposable deleted build; verify HTTP 404, the sensitive-data warning, and the copy action without rendering HTML. Clipboard dispatch bytes are also covered by `go test ./src/ui/components/dialogs/errorpage`.
- **Cleanup:** `$J doctor RUN && $J down RUN` after every attempt. Revoke only the disposable token, unset `JT_JENKINS_TOKEN`, and remove only disposable diagnostic jobs. Preserve `dist/verify/RUN/` as evidence without credentials.

Verified 2026-11-20 with published gojenkins `.15` on Docker Jenkins 2.568.2: `dist/verify/crumb-token-lts/` records six triggers (plain and parameterized, all three entry points), three replays, nine `SUCCESS` rows, and nine resolved intents. `dist/verify/crumb-html-lts/` records a genuine HTML 404 and its warning/copy action; clipboard dispatch bytes were unit-tested, not copied live. Tokens were revoked and sessions torn down. The 2.479.3 build was blocked because unpinned plugins require newer core; see `dist/verify/crumb-token-2479/docker-build.log`. This is not evidence of older-version live compatibility.

## Gotchas

- `JT_JENKINS_TOKEN` defaults to the local account password, not a generated API token. Set it explicitly for this recipe; keep the same environment for `up`, `doctor`, and Jenkins probes.
- Match test paths exactly. Prefix matches accept the malformed `/crumbIssuer/api/json/api/json` URL and hide this regression.
- Successful replay returns HTML after its redirect; that response is not a JSON-decoding failure.
