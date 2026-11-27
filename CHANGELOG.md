# Changelog

What changed in removarr, written for the person who runs it.

**Format.** Changes are collected under "Unreleased" and move under a version heading with its date when a release is cut. Every entry has three parts:

- **Scope:** what is new, changed, fixed or removed, in terms a user or operator can notice.
- **Deployment:** what to do when updating. Database migrations, new or changed environment variables, settings to review, and any manual step. Write "Nothing to do" when that is the case.
- **Breaking changes:** anything that worked before and no longer does, or behaves differently, with what to do about it. Write "None" when that is the case.

## Unreleased

### Stage 3: the web UI (issue #6)

removarr now has a web UI, served by the same binary on the same port. Everything the API offers can be done in the browser.

#### Scope

- **Navigation:** a side panel on the left with an icon and a label per page, and the Settings pages nested below Settings. The menu button in the header hides and shows it. On a wide screen it sits beside the content and the browser remembers whether you hid it; on a narrow screen it opens as a drawer over the content and closes with Escape, a click beside it or a chosen page. It slides open and closed in a short animation, with none when the system asks for reduced motion.
- **Header:** the menu button and the name on the left; the Scan button at the right end, with a dry run badge before it when `REMOVARR_DRY_RUN=true`.
- **Scan details:** a strip of its own below the header, beside the side panel: when the last scan ran and its summary per list, the free space of the library root folders, the last scan's error, and a notice that the lists may be out of date when the rules or an app's address or user name changed since the last scan. A click on its first line, "Last scan …", collapses it to that line and brings it back; the line carries a "Needs attention" mark when the lists are out of date, a list could not be produced or the last scan failed, and the browser remembers whether you collapsed it.
- **Lists:** Movies, Series and Torrents, each sorted by size, watched date or title, with a filter. Select all selects only the rows shown, and narrowing the filter or reloading the list drops selected rows that are no longer shown, so an action never names a row you cannot see. While rows are selected, a bulk bar with the count and size of the selection is fixed at the bottom of the window, beside the side panel, and the list leaves room so that its last row is never hidden behind it. Wide screens get a table, narrow ones cards. A list that could not be produced shows its error and no rows.
- **Ignore:** ignore selected rows, and a "Show ignored" view that lists the ignores, says when each was made, which are no longer in the scan and what a held row waits for, and offers Un-ignore.
- **Delete:** builds the plan for the selected rows and shows it grouped by app, with every step, the torrents removed and the ones that stay, the skipped rows with their reasons, and per torrent whether it keeps its data, lies partly outside the library or is linked by file size only. Nothing runs until you confirm that plan. Building can be cancelled; an execution cannot. The outcome shows every item and step with its result. A plan that expired or can no longer run offers to build it again from the same rows, with a new confirmation. A busy server's answer names the running job and is not retried.
- **History:** every executed deletion, newest first, with its rows, size, outcome and each step, page by page.
- **Settings:** three pages. Connections (`/settings/connections`, where `/settings` leads): the four connections with where each value comes from (saved in the UI or from the environment), secrets that are never shown and only replaced or cleared, and a Test button per app. Counting users (`/settings/counting-users`): the users from Jellyfin. Rules (`/settings/rules`). Each page saves on its own, and only the fields you changed are sent.
- **Theme:** follows the system's light or dark setting. The dark theme is a soft dark gray-blue with off-white text, and every text colour keeps WCAG AA contrast on the surface it sits on.
- **Status:** `GET /api/status` gains `stale`, and each scan records `settingsFingerprint`, a hash of the rules and of each app's URL and user name, never a secret. The order of the counting users, excluded categories and excluded tags does not count.
- **Serving:** the UI is served at every path outside `/api/`, with the UI's page for a path it does not hold, so any page can be reloaded. A missing file under `assets/`, or with an extension, answers 404. A request under a refused host name outside `/api/` gets a short readable page instead of JSON.
- **Framing:** every answer forbids being shown in a frame of another page (`X-Frame-Options: DENY`, `Content-Security-Policy: frame-ancestors 'none'`) and carries `X-Content-Type-Options: nosniff`, so another site cannot lure you into clicking through a deletion.

#### Deployment

- Build the UI before building removarr: `pnpm --dir web install --frozen-lockfile && pnpm --dir web build`, then `go build`. A binary built without it serves a page saying the UI is not built; the API works either way.
- Behind a reverse proxy, the proxy must pass the Host header on unchanged and its name must be listed in `REMOVARR_ALLOWED_HOSTS`; otherwise every change from the UI is refused.
- No database migration. A scan from before the update has no fingerprint and is never reported out of date; scan once to get the notice.

#### Breaking changes

- An unknown path under `/api/` now answers 404 with a JSON error, and a wrong method on a known API path answers 404 where it answered 405.

### Stage 2d: execute plans and record history (issue #5)

removarr can now carry out a plan. This is the first stage that changes your apps. With `REMOVARR_DRY_RUN=true` it does everything except the change itself.

#### Scope

- **Execution:** `POST /api/plans/{id}/execute` runs a plan you built and confirmed, and returns one record per item. A plan is taken once: executing it discards the others, and a second request for it, or one for an expired plan, is refused (404, 410) so it is built again.
- **Order:** within an item, the media manager steps run first and the torrents after. Every unmonitor step of an item comes before its deletions, so Sonarr does not search for what was just deleted, also when one row deletes a file that holds another row's episode.
- **Failures:** every item runs and is recorded. When a step fails, it is recorded with its error and the steps after it in the same item, torrents included, are recorded as skipped and not sent; the next item runs regardless. A step whose app is not configured fails as not configured. An execution has a limit of 30 minutes: once it runs out, the steps still to run fail with that error. Execution stops early only when an item cannot be recorded, so nothing more is deleted than the history holds, or when removarr is stopped.
- **Shared data at execution:** whether a torrent's data stays is decided again at the moment of removal, per torrent, longest path first, from the torrents that are still there. The plan's `keepData` is only the forecast.
- **Dry run:** with `REMOVARR_DRY_RUN=true`, only GET requests reach the apps and every step is recorded as a dry run.
- **History:** `GET /api/actions?before=&limit=` lists the records newest first, with `next` for the following page. Each record keeps its rows, steps and outcome. A torrent step records the keep-data choice that was sent, also when the request failed. An error in the history never repeats the app's URL or address: a request that got no answer names only its method.
- **Stopping removarr:** when removarr is stopped during an execution, it starts no further item and waits up to two minutes for the item it is running to finish and be recorded before it closes the database. Items that did not start stay in the lists. If an item ran but could not be recorded, removarr logs it at error level with its plan, outcome, row keys and step IDs.
- **Stored lists:** executing a plan removes the rows it deleted from the stored candidate lists, so they disappear without a new scan. A row whose steps did not all succeed stays. In a dry run the lists stay as they are.
- **One job at a time:** a scan, building a plan and executing one refuse each other with 409 and name the job that is running.
- **Redirects and hashes:** a redirect that would change a request's method is refused. qBittorrent removal accepts only lower-case hexadecimal hashes, so a malformed ID can never remove other torrents.
- **Sonarr seasons:** unmonitoring a season in Sonarr 4.0.12 also unmonitors its episodes, as read from its source; to be confirmed on the reference stack.

#### Deployment

- Database migration `0004_actions` runs on start, adds the `actions` table and discards plans built by an earlier version: build them again.
- Give removarr more than two minutes to stop before it is killed, so an execution that is running can record its last item: Docker waits 10 seconds by default (`docker stop -t 130`, or `stop_grace_period: 130s` in Compose), systemd 90 seconds (`TimeoutStopSec=130`).
- Start with `REMOVARR_DRY_RUN=true`, execute a plan, and compare the history with the plan before you delete for real.

#### Breaking changes

- Building a plan while a scan runs is now refused with 409. It used to wait for the scan.
- Plan steps now list unmonitoring before deleting, and carry a `rowKey`. A client that read steps by position must read them by content.

### Stage 2c: delete plans from live data (issue #4)

removarr can now say exactly what deleting the selected rows would do. Still read-only towards the apps: a plan is built and stored, never executed.

#### Scope

- **Plans:** `POST /api/plans` with `{"rows":[{"list":"movies","key":"tmdb:603"}]}` reads every app again, the way a scan does, and returns the plan (201). Building one takes about as long as a scan. One plan is built at a time.
- **What a plan holds:** one item per selected row, or per group of tied rows, which are added and marked. Per item: the media manager steps (remove the movie; remove an ended series; or delete the episode files of a season or of a series that is still airing, unmonitor those episodes and the seasons among them that have finished airing), the torrents removed with their seeding flag, data outside the library, whether their data is kept and whether only their file sizes tie them to the item, and the associated torrents that stay, with the reason.
- **Skipped rows:** a row that is ignored, waits, is tied to an ignored row, no longer qualifies, belongs to a list that cannot be produced, is not in the latest scan, waited in the latest scan, or changed since the scan in the torrents it would remove or keep, is listed with the reason. A torrent row whose torrent a selected movie or series removes anyway is skipped and names it.
- **Shared data:** a torrent whose content path equals, contains or lies inside the path of a torrent that stays is removed as an entry only and keeps its data. So does every torrent when a path is unknown.
- **Expiry:** a plan is stored for ten minutes.
- **Ignored view:** a row the scan holds back now says what it waits for, in `waitReason`.

#### Deployment

- Database migration `0003_plans` runs on start and adds the `plans` table.
- Scan again after updating, so that held rows carry `waitReason`. Rows from an older scan say only that they wait.

#### Breaking changes

- None.

### Stage 2b: ignore and un-ignore (issue #3)

Rows you do not want suggested can be hidden and brought back. Still read-only towards the apps: nothing is deleted yet.

#### Scope

- **Ignore:** `POST /api/ignores` with `{"rows":[{"list":"movies","key":"tmdb:603"}]}` hides rows, in bulk. Only rows of the latest scan can be ignored; the request is saved whole or not at all.
- **Un-ignore:** `DELETE /api/ignores` with the same body brings rows back. Only an ignore in the list named is removed; another key is skipped. An ignore whose row is gone can still be removed.
- **Ignored view:** `GET /api/candidates?list=…&ignored=true` lists one entry per ignore, with `ignoredAt`. An ignore that the latest scan has no row for shows its key and title with `notInScan`; a row the scan holds back shows `waiting`.
- **Takes effect at once:** ignoring and un-ignoring change the lists and the counts in `/api/status` and `/api/scans/current` without a new scan. A row tied to an ignored row is not listed and counts as `waiting`, because the torrent or file they share now stays.
- **Series and seasons:** ignoring a series hides its season rows too. Ignoring a season hides only that season's row and has no effect in whole-series mode. In per-season mode, seasons are not merged into one series row while one of them is ignored; un-ignoring that season merges them at the next scan. A series row that a scan merged while you ignored one of its seasons, because the scan was still running, is marked `merged` and counts as `waiting` until the next scan, together with every row tied to it.
- Ignores persist across scans until they are removed.

#### Deployment

- Database migration `0002_ignores` runs on start and adds the `ignores` table.
- Scan again after updating. Scans now also store the rows the whole-torrent rule holds back; a scan from before the update has none, so the ignored view cannot show them until the next scan.

#### Breaking changes

- None. `ignored` on `GET /api/candidates` accepts only `true` and `false`; any other value is a 400.

### Stage 2a: whole torrents, whole files and episode ranges (pull request #9)

Rules that follow from two principles: free disk space without removing anything unwatched, and never delete only part of what a torrent holds. Still read-only: nothing is deleted yet.

#### Scope

- **Whole torrents:** a movie, series or season is listed only when every torrent that backs its files would go with it. A row whose torrent also holds something that stays is not listed, and the scan summary counts it as `waiting`. Examples: a multi-season pack with an unwatched season, a collection pack with a protected movie, a download that is still in a media manager's queue.
- **Whole files:** the same for library files. A file that also holds an episode that stays keeps its row waiting.
- **Tied rows:** rows that share a torrent or a file and can all go are listed with `tiedWith`, naming each other. They are meant to be deleted together.
- **Episode ranges:** a media server item that covers several episodes counts for a further episode only when Sonarr holds it in the same file as the first.
- **Data outside the library:** each torrent on a movie, series or season row states `outsideLibrary`, the bytes it holds that are not in the library, when that is 50 MB or more.
- **List dependencies:** with both Sonarr and Radarr configured, each watched list needs both to answer.
- **API additions:** `tiedWith` on rows, `outsideLibrary` on a row's torrents, `waiting` in each list's summary.
- **Fixed:** a file that spans two seasons was counted twice in the size of a series row.

#### Deployment

- No database migration and no new environment variable.
- Scan again after updating. The stored lists were computed with the old rules.

#### Breaking changes

- With both Sonarr and Radarr configured, neither watched list is produced while one of them cannot be read. Before, the other list was still shown.
- Rows can leave the watched lists. A row that would delete only part of a torrent or of a file now waits, and an episode covered only by a range that Sonarr does not confirm counts as unwatched.
- A row can name more torrents than before: tied rows each name the torrent they share.

### Stage 1: read-only core (pull request #1)

The first working version. It scans Jellyfin, Sonarr, Radarr and qBittorrent on demand and serves three suggestion lists over a JSON API. It changes nothing in those apps: there is no deletion, no ignore and no web UI yet.

#### Scope

- **Three lists:** watched movies, watched series (whole series or per season) and orphan torrents, each row with its size and the torrents a deletion would remove.
- **Rules:** counting users, watched mode "all" or "any", grace period, protection by favourite and by tag, seeding minimums, excluded categories and tags.
- **Linking torrents to library files:** by Sonarr and Radarr history, and where history says nothing, by content. A torrent backs a library file when one of its files has exactly the same size and that size is at least 50 MB.
- **No guessing:** a list is produced only when every app it depends on answered completely. Otherwise it shows the error.
- **API:**
  - `GET /api/health`, `GET /api/status`
  - `GET /api/settings`, `PUT /api/settings`
  - `POST /api/connections/{app}/test`, `GET /api/media-server/users`
  - `POST /api/scans`, `GET /api/scans/current`
  - `GET /api/candidates?list=movies|series|torrents`
- **Settings:** connections and rules are saved through the API and take priority over the environment variables. API keys and the qBittorrent password are never returned.
- **Security:**
  - No login: for a trusted local network only.
  - The server answers only when reached by an IP address, as `localhost`, or by a host name listed in `REMOVARR_ALLOWED_HOSTS`. The health check answers under any name.
  - A request that changes something must be JSON, carry the header `X-Removarr-Request`, and come from the server's own address.
  - Connections to the apps do not follow a redirect to another host or port, or from https to http.

#### Deployment

- **Requirements:** Go 1.27 to build. There is no container image yet.
- **Database:** one SQLite file, `removarr.db`, in the config directory, created on first start and readable by its owner only. Migration `0001_init` creates the tables `settings`, `scans` and `candidates`. Migrations run by themselves at start.
- **Environment variables:**

  | Variable | Default | Purpose |
  |---|---|---|
  | `REMOVARR_PORT` | `7272` | Port the server listens on |
  | `REMOVARR_CONFIG_DIR` | `/config` | Directory of the database |
  | `REMOVARR_DRY_RUN` | `false` | Reserved for deletions, which are not built yet |
  | `REMOVARR_ALLOWED_HOSTS` | none | Host names removarr answers to, besides IP addresses and `localhost`; separated by commas, without scheme or port |
  | `REMOVARR_JELLYFIN_URL`, `REMOVARR_JELLYFIN_API_KEY` | none | Jellyfin connection |
  | `REMOVARR_SONARR_URL`, `REMOVARR_SONARR_API_KEY` | none | Sonarr connection |
  | `REMOVARR_RADARR_URL`, `REMOVARR_RADARR_API_KEY` | none | Radarr connection |
  | `REMOVARR_QBITTORRENT_URL`, `REMOVARR_QBITTORRENT_USERNAME`, `REMOVARR_QBITTORRENT_PASSWORD` | none | qBittorrent connection |

- **After the first start:** select the counting users (`PUT /api/settings`), then start a scan. Without counting users the two watched lists are not produced.
- **Reaching it by name:** to open removarr as `pi.local` or through a reverse proxy, list that name in `REMOVARR_ALLOWED_HOSTS`. A reverse proxy must pass the Host header on unchanged.

#### Breaking changes

None. This is the first version.
