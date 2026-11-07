# Global search and folder browsing

Global search lets the user find any Jenkins job or folder by name with
Jenkins's own search. Selecting a result opens the job page or the folder
listing, and `b` walks back through navigation history.

## Sub-features

- `search-open`: `ctrl+p` opens the `Global Search` dialog from any view.
- `search-query`: typing a name and pressing Enter lists matching items with
  `N item(s)`, a `»` breadcrumb, and a `/job/...` path.
- `search-clear`: `ctrl+r` clears the input.
- `search-open-job`: Enter on a job result opens its job page.
- `search-open-folder`: Enter on a folder result opens the folder listing,
  with columns `TYPE NAME STATUS W LAST SUCCESS LAST FAILURE LAST DURATION`.
- `browse-folder`: in a folder, `j`/`k` move the selection, and Enter opens
  the selected child.
- `nav-back`: `b` returns to the previous view. `]` goes forward again.

## How to get to it (user POV)

- Press `ctrl+p` from any page (home, job, build, Favorites, My Runs).
- To open a folder or job, select a search result, a Favorites row, or a row
  in another folder listing.

## Driving it with jt

Preconditions:

- The instance is ready and on the home view (`$J screen RUN` shows the
  splash, and the footer reads `home`).

Steps:

- **Open search.**
  - Run `$J keys RUN C-p && $J wait RUN 'Global Search'`.
  - The search dialog appears over the current view.
- **Search for a job.**
  - Run `$J type RUN mobile-app && $J keys RUN Enter && $J wait RUN '1 item'`.
  - The result row reads `DevTeam » Frontend » mobile-app` and
    `/job/DevTeam/job/Frontend/job/mobile-app/`.
- **Open the job.**
  - Run `$J keys RUN Enter && $J crumb RUN 'Frontend.*mobile-app' && $J wait RUN '│ #[0-9]+ │'`.
  - The footer breadcrumb matches `DevTeam.*Frontend.*mobile-app`, and
    build boxes are drawn, which means the page is ready for keys.
- **Search for a folder.**
  - Run `$J keys RUN C-p && $J wait RUN 'Global Search'`.
  - Then run `$J keys RUN C-r && $J wait RUN 'Global Search' && $J type RUN Backend && $J keys RUN Enter && $J wait RUN 'DevTeam » Backend'`.
- **Open the folder.**
  - Run `$J keys RUN Enter && $J crumb RUN 'DevTeam.*Backend' && $J wait RUN '6 items'`.
  - The listing shows `ansi-color-log`, `api-service`, `log-heavy-job`, and
    so on. The footer matches `DevTeam.*Backend`.
- **Browse into a child.**
  - Run `$J keys RUN j && $J keys RUN Enter && $J crumb RUN 'Backend.*api-service' && $J wait RUN '│ #[0-9]+ │'`.
  - The footer matches `DevTeam.*Backend.*api-service`.
- **Back.**
  - Run `$J keys RUN b && $J wait RUN '6 items'`.
  - The folder listing is back.
- **Forward.**
  - Run `$J keys RUN ']' && $J crumb RUN 'Backend.*api-service'`.
  - Verified 2026-10-03 (`dist/verify/proof-search/`).
- **Proof.**
  - Run `$J snap RUN search-folder` after the folder opens.
  - Run `$J snap RUN search-back` after pressing `b`.

## Gotchas

- `Global Search` is the input's **placeholder**, not a title. It
  disappears once a query is typed, so wait for it only right after `C-p`
  or `C-r`.
- `Escape` does **not** close the search dialog. `ctrl+c` does, and it
  closes only the dialog. If no dialog is open, `ctrl+c` quits the app.
- Back history keeps only 3 views. After a longer path, `b` stops short of
  home.
- The dialog keeps the previous query. Press `ctrl+r` before typing a new
  one, or the new text is appended to the old.
- Results come from Jenkins's search API, so a short query such as `app`
  can return many pages (`h`/`l` change page). Use exact seed names.
- The search box starts focused. Once results are shown, `j`/`k` move the
  selection instead of typing into the box. Press `Tab` to move focus back
  to the input.
- The seed folders are `DevTeam`, `Ops`, `QA`, and `Dynamic`. The live
  Jenkins also holds extra top-level jobs from other work (such as
  `badge-*`). Do not count on exact top-level item counts.
