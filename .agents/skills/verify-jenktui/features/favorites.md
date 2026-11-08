# Favorites

Favorites let the user star jobs, folders, and multibranch pipelines and
reopen them from one view with tabs per kind. Favorites are stored per
server in `runs.db` and survive restarts.

## Sub-features

- `fav-toggle`: `F` on a job page favorites that job. `F` on a folder
  page favorites **the folder being viewed**, not the selected row (verified
  in `folder.go` `toggleFavorite`, which uses `m.target`). It adds or
  removes the favorite. The footer toast reads `Added to favorites` (or the removal
  message).
- `fav-view`: `f` opens Favorites, with tabs
  `Jobs (n)   Folders (n)   Multibranch (n)` and columns `NAME` and
  `UPDATED`.
- `fav-tabs`: `J`, `F`, and `M` switch to the Jobs, Folders, and
  Multibranch tabs.
- `fav-open`: Enter opens the selected job or container. `r` opens My
  Runs filtered to a selected job; containers warn `Containers have no run
  history`.
- `fav-delete`: `d` removes the selected favorite.
- `fav-search`: `/` filters the list.
- `fav-persist`: favorites survive an app restart that keeps the same
  `runs.db`.

## How to get to it (user POV)

- `f` from any page opens the view.
- `F` on a job page favorites that job.
- `F` while viewing a folder (or a multibranch project) favorites that
  container. To favorite a child, open it first.

## Driving it with jt

Preconditions:

- The instance is freshly started (`$J up RUN`), so its `runs.db` has no
  favorites. Check with `$J db RUN 'select count(*) from favorites'`.
  Store initialization creates the database; no first favorite is needed.

Steps:

- **Open a job.**
  - Follow `search-open-job` in
    [search-and-browse.md](./search-and-browse.md) for `mobile-app`. End on
    `$J wait RUN '│ #[0-9]+ │'`, because `F` is dropped while the job page
    loads.
  - Run `$J snap RUN fav-toggle-before`.
- **Toggle the favorite.**
  - Run `$J keys RUN F && $J wait RUN 'Added to favorites' 5`.
  - The toast appears in the footer.
- **Open Favorites.**
  - Run `$J keys RUN f && $J wait RUN 'Jobs \(1\)'`.
  - One row matches `DevTeam.*Frontend.*mobile-app`, with today's date
    under `UPDATED`.
- **Persisted row.**
  - Run `$J db RUN "select url,server_id,kind from favorites"`.
  - Expect exactly one row:
    `http://127.0.0.1:8080/job/DevTeam/job/Frontend/job/mobile-app/ | local | job`.
- **Open from Favorites.**
  - Run `$J keys RUN Enter && $J crumb RUN 'Frontend.*mobile-app' && $J wait RUN '│ #[0-9]+ │'`.
  - A known pipeline may first show centered `Loading job...`; the header
    or breadcrumb alone does not mean it is ready for keys.
- **Delete.**
  - Run `$J keys RUN f && $J wait RUN 'Jobs \(1\)'`.
  - Then run `$J keys RUN d && $J wait RUN 'Jobs \(0\)'`.
  - Then run `$J db RUN 'select count(*) from favorites'`, which should
    return `0`.
- **Proof.**
  - Run `$J snap RUN fav-view` after opening Favorites.
  - Run `$J snap RUN fav-deleted` after the delete, along with the two
    `db` outputs (both are in `transcript.log`).

## Gotchas

- In the Favorites view, `F` means the **Folders tab**, not "favorite". It
  toggles a favorite only on job and folder pages.
- Favorites are keyed by `server_id`. The harness server id is `local`, so
  rows from a developer's real servers never appear here.
- Pressing `F` twice on the same job removes the favorite again. Always read
  the toast before you assume the state.
- `d` deletes **immediately**, with no confirmation and **no toast**. The
  footer may still show the stale `Added to favorites` toast. Prove the
  delete with `Jobs (0)` and `$J db`.
- Favorites are server-scoped. That is proven in
  [server-selector.md](./server-selector.md).
