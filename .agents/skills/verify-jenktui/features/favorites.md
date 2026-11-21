# Favorites scope

`f` opens the shared search picker in Favorites. The same list contains jobs,
folders, and multibranch pipelines; icons identify each type. Favorites are
stored per server in `runs.db`.

## Driving it with jt

Start from a fresh `jt up` and `jt doctor`. Check that `select count(*) from favorites`
returns `0` via `jt db`, and capture the initial screen with `jt snap`.

1. Open `mobile-app` via [Jenkins search](./search-and-browse.md) and wait for
   build boxes. Press `F`, wait for `Added to favorites`, then press `f` and
   wait for `─ Favorites` and `1 item`. Check that the divider spans the
   picker above the results and ends with Favorites at the right edge. The
   command row below the results still shows `S switch scope`. With the input
   focused, no row is highlighted. Press Enter to focus and highlight the
   first row without opening it. Verify the footer shows `enter navigate` and
   `r go to run history`, then Tab to return to the input.
2. Type `DevTeam/Frontend` without Enter; `mobile-app` should still match.
   Snap the picker. Verify `url`, `server_id`, and `kind` in `favorites`; expect
   `/job/DevTeam/job/Frontend/job/mobile-app/`, `local`, `job`.
3. Clear with `C-r`, type `Backend`, and wait for `No matching favorites`.
   Press `S` (Shift+S), wait for `─ Jenkins` and `1 item`, then snapshot
   the preserved `Backend` query and its result. Enter opens the folder.
4. Press `F` from the Backend folder listing to favorite Backend itself, not
   its selected child. Press `f` and wait for `2 items`; snapshot the mixed
   results. Check for the gear on the job and folder icon on Backend, with
   names aligned and no star column. Verify SQLite contains both `folder` and `job`.
5. `Tab r` on Backend warns `Containers have no run history`. `j r` opens
   My Runs filtered to the job. Press `f` again, type `Backend`, then press
   Enter to focus results and Enter again to open the folder.
6. Open `f` again, then `Tab d` on Backend. Wait for `Removed from favorites`
   and `1 item`; verify only the job remains in SQLite and snap the result.
7. Close with `C-c`, reopen with `f`, and verify the remaining job is still listed.

See [server selection](./server-selector.md) for the server isolation check.
Finish with `jt down RUN`.
