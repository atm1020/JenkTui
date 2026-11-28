# Completion footer

A build completion shows one colored footer toast per completion per server session. Retrying a failed desktop notification or acknowledgement must not redisplay that toast.

## Sub-features

- `completion-footer-once`: the toast expires and stays absent on delivery retries.
- `completion-footer-recovery`: delivery retries still run after the toast expires.

## How to get to it (user POV)

Trigger `DevTeam/Frontend/mobile-app` from its job page and keep the page open through completion. The app shell handles completions independently of the active page.

## Driving it with jt

Preconditions: local seeded Jenkins; a fresh isolated RUN. This recipe deliberately makes desktop delivery fail without launching host popups.

- **Launch:** run `env -u DISPLAY -u WAYLAND_DISPLAY DBUS_SESSION_BUS_ADDRESS=unix:path=/tmp/jenktui-no-notification-bus JT_CONFIG_JSON='{"poll_interval":3,"notification":{"desktop_enabled":true,"sound_enabled":false}}' $J up RUN`, then `$J doctor RUN`. The bus path must not exist. If installed fallback tools deliver a real popup, this is not a failure-path proof.
- **Trigger:** use the canonical mobile-app search sequence in the live skill; wait for build boxes, `$J snap RUN before`, then `$J keys RUN C-b && $J wait RUN 'Are you sure' && $J keys RUN y && $J wait RUN 'Build triggered'`.
- **Completion:** `$J wait RUN 'completed: Success' 90 && $J snap RUN completion`. Record the actual build number.
- **Retries:** inspect `$J log RUN 30` for repeated `completion delivery was not acknowledged` errors. Poll `$J screen RUN` until the toast disappears, then sample for at least 10 seconds across multiple retries. Fail if the same completion toast reappears. Save samples and `$J snap RUN after-retries`.
- **Side effects:** `$J db RUN 'select build_id,status from runs'`, `$J db RUN 'select build_id,delivered_at from completion_outbox'`, and `$J jenkins RUN '/job/DevTeam/job/Frontend/job/mobile-app/lastBuild/api/json?tree=number,result,building'`. Jenkins and the stored run must agree on the completed build. Record acknowledgement state separately from footer behavior.
- **Cleanup:** `$J down RUN`, including after failures.

## Gotchas

- The footer is deduplicated independently of desktop delivery. Its absence does not prove that desktop delivery succeeded or that the outbox was acknowledged correctly.
- `TestCompletionRetryDoesNotRepeatFooter` covers both desktop and acknowledgement failures, plus recovery, without requiring a broken host service.
- Verified footer expiry/no reappearance against local Jenkins in `dist/verify/footer-retry-live/`: mobile-app #95, forced desktop failure, 46 post-expiry samples over 10 seconds. This proves the footer behavior, not outbox acknowledgement semantics.
