# Learnings

What this project found out that is not obvious from the code, kept so that nobody has to find it out again. Read the part for the area you are about to touch.

**Adding an entry:** one bullet under the fitting heading, starting with the date and the claim in bold, then why it matters and where it shows in the code or the spec. Keep it to a few lines. If a later finding replaces an entry, change the entry instead of adding a second one.

Most entries about the apps were observed on the reference stack: Sonarr 4.0.12, Radarr 5.18.4, Jellyfin 12.4.0, qBittorrent 5.1.0 (Web API 2.11.4). Check them again when a version differs. Spec section 16 holds the full record with numbers.

## Sonarr and Radarr

- 2026-11-19. **History knows where a file came from only for automatic imports.** A manual import event carries the file ID and no download ID, and a torrent added outside the managers has no history at all. About two thirds of the episode files with an import record were manual imports. This is why torrents are also linked by content (spec 6.6, 16.1).
- 2026-11-19. **Download IDs in history are upper-case hashes; qBittorrent reports lower case.** Lower-case both sides before comparing (`internal/providers/arr`).
- 2026-11-19. **Older import records lack `data.fileId`.** For those, an item's most recent import is taken as the source of its current file.
- 2026-11-19. **Count the records a page returned; do not trust the page size you asked for.** History is returned newest first and the requested size was honoured, but a server that caps it would otherwise cut history short and make tracked torrents look orphaned (`eachPage`).
- 2026-11-19. **Completed downloads can stay in the queue** when their import is blocked or pending, some of them unmatched to any series or movie. Ask the queue with `includeUnknownSeriesItems` or `includeUnknownMovieItems`. A queued torrent is never listed.
- 2026-11-19. **History holds download IDs of other clients too**, such as a usenet client. They never match a torrent and are ignored.
- 2026-11-19. **Sonarr and Radarr both number files and items from 1.** A file ID or item ID means nothing without its manager. Link and count per manager, and add up only by download ID (`linkData`, `NewLinkIndex`).
- 2026-11-22. **Sonarr's file list and episode list are two requests.** An import or upgrade in between leaves an episode pointing at a file the list does not hold. That is an incomplete answer and fails the read; it is not a file of size 0 (`internal/providers/sonarr`).

- 2026-11-22. **Looking an episode up by season and number can be ambiguous.** If Sonarr ever lists the same number twice with different files, the answer depends on the order they arrive in. Where it matters which file an episode is in, such a number counts as unknown (`fileIDsOf` in `internal/scan/series.go`).
- 2026-11-22. **One library file can hold episodes of two seasons, or a special and a regular episode.** A season row used to delete such a file together with an episode that was not watched. Files are kept whole like torrents (spec 6.3).
- 2026-11-24. **Saving a series with a season unmonitored unmonitors that season's episodes in Sonarr 4.0.12** (read from its source; to be confirmed on the reference stack). A series is changed only as a whole, so it is read and sent back with every field, never rebuilt from the fields removarr knows (`internal/providers/sonarr`).

## Jellyfin

- 2026-11-19. **The API key goes in `Authorization: MediaBrowser Token="<key>"`.** The older `X-Emby-Token` header gets a 401.
- 2026-11-19. **Advance paging by the number of items received**, not by the page size requested.
- 2026-11-19. **A played episode can have no last played date.** It still counts as watched; the date shows as unknown (spec 6.1).
- 2026-11-19. **One item can cover a range of episodes** (`IndexNumberEnd`). Jellyfin reads the range from the file name and can read it wrongly. A range counts for a further episode only where Sonarr holds it in the same file as the first (spec 6.2, built in stage 2).
- 2026-11-19. **A favourite can sit on an episode that has no season or episode number.** It still protects its series.

## qBittorrent

- 2026-11-19. **An instance that trusts the local network answers without a login.** The login path has therefore never run against the reference stack.
- 2026-11-19. **`size` of a torrent counts only the files selected for download**, while the file list names every file, each with `progress` and `priority`. removarr compares listed files by size and does not look at `progress` yet (spec 6.6, limits).
- 2026-11-19. **A file list costs one request per torrent.** Ask only where the answer can change a result: 90 of 381 torrents on the reference stack (spec 6.6).
- 2026-11-24. **`hashes=all` removes every torrent, and `|` separates hashes.** Only a lower-case hexadecimal ID is ever sent, and a request names exactly one torrent (`internal/providers/qbittorrent`).

## Linking torrents to library files

- 2026-11-19. **Exact byte size with a 50 MB floor is enough to link a torrent to a library file.** It linked 27 torrents (876 GB) that history could not, and an independent recomputation from the apps' raw answers agreed torrent by torrent. History always wins over content; ties go to the download ID that sorts first.
- 2026-11-19. **A torrent that history links to some files can also hold files that were imported by hand.** Ask every torrent that is at least 50 MB larger than the files history links to it, or deleting its linked files would remove a torrent that still holds library files.
- 2026-11-19. **removarr cannot see hard links.** It talks to the apps only, so a size is a size and never "space that will be freed". With hard links, deleting library files while their torrent stays frees nothing, which is the reason for the whole-torrent rule (spec section 2, 6.3).
- 2026-11-19. **What looks like an orphan is often not media at all.** All orphans left on the reference stack were torrents outside the categories the media managers use. Excluded categories and tags exist for them.

- 2026-11-22. **A row cannot be judged on its own once rows share something.** Whether a torrent can go depends on every row that is deleted with it. Rows are grouped through the torrents and files they share, and the group is judged as one: it waits, or its rows are tied (`scan.WholeTorrents`).
- 2026-11-22. **Only a torrent that is in the download client can block or tie rows.** History keeps naming sources that are long gone.

## Ignores

- 2026-11-23. **Ignores are applied when a list is read, not when it is scanned.** A scan stores every qualifying row, also the rows the whole-torrent rule holds back (`waiting`), and `scan.View` applies the current ignores: an ignored row is hidden, a row tied to a hidden row waits. This equals a new scan because a group that loses a row always waits: the torrent or file it shared with that row now backs something that stays (spec 6.5).
- 2026-11-23. **`scan.View` relies on `TiedWith` naming every other row of the group, across chains.** With direct neighbours only, hiding one end of a chain would leave the far end listed (`WholeTorrents`).
- 2026-11-23. **The per-season merge is the one place a scan uses ignores, and it reads them once, at the start.** An ignore saved while the scan runs is not seen, so the scan can merge over a season that is now ignored. Such rows carry `merged` and wait until the next scan.
- 2026-11-23. **Scans from before stage 2b stored no held rows.** Their waiting count reads 0 and the ignored view cannot show held rows until the next scan.

## Plans

- 2026-11-24. **A plan reads everything a scan reads.** Whether a torrent can go depends on everything else it backs, so re-reading only the selected items cannot decide it. The planner reads and evaluates through `scanner.Read` and `scanner.Evaluate`, the code a scan uses (spec 6.10).
- 2026-11-24. **One read is not proof.** History is read before the queue, so a download imported between the two reads looks unlinked for that read. A plan removes a torrent only when the latest scan and the planner's read agree on it, in both directions; a row that waited in the latest scan is skipped (`changedSinceScan`).
- 2026-11-24. **Shared data is decided against everything that stays.** One staying torrent with an unknown content path keeps the data of every torrent a plan removes, and paths are compared without regard to case, so two Linux directories that differ only in case keep their data. Both err toward keeping. A plan's `keepData` assumes every item succeeds; the executor must decide again from the torrents it actually removed.
- 2026-11-24. **A test fixture whose file IDs equal its episode IDs hides swapped IDs.** Unmonitoring files instead of episodes passed every test until the IDs differed. Sonarr's episode IDs are global, so such a swap would unmonitor episodes of other series.
- 2026-11-24. **Shared data is decided per torrent at the moment of removal, longest path first.** A torrent inside another one's folder must go before the outer one, so each decision sees which torrents are still there. This is the second decision the plan's `keepData` forecast promised (`internal/executor`).

## Security

- 2026-11-24. **A `*url.Error` repeats the full request URL, query included, and a dial error the address it dialled** (`Delete "http://host:7878/api/v3/movie/12?deleteFiles=true": dial tcp 10.0.0.5:7878: ...`). Such an error went into the history and `GET /api/actions`. `httpjson.RequestError` unwraps it, leaves out the dialled address and replaces the host before an error is stored or shown; the redirect refusals name no host. `errors.Is` still finds `context.DeadlineExceeded`.
- 2026-11-22. **Comparing Origin with Host does not stop DNS rebinding.** A page that points its own domain name at removarr's address makes the browser send that name as both headers. Only checking the Host header against IP addresses, `localhost` and listed names does (`guard`, `REMOVARR_ALLOWED_HOSTS`, spec section 10).
- 2026-11-24. **Go turns a DELETE, PUT or POST into a GET when it follows a 301, 302 or 303, and the write then reports success.** A proxy that sends http to https on the same host does exactly this; nothing changes in the app while the history says done. `SameHostRedirects` refuses any redirect that changes the method; 307 and 308 keep it and are followed.
- 2026-11-19. **Go's HTTP client forwards a custom header such as `X-Api-Key` when it follows a redirect.** Every client uses `httpjson.SameHostRedirects`, which refuses another host or port, and a step from https to http.
- 2026-11-19. **A base URL can carry a credential** (`http://user:pass@host`), and an error that repeats a bad URL can leak one. Such URLs are rejected, and URL errors are fixed texts.
- 2026-11-19. **`url.Parse` accepts more than a base URL should be.** `http://http://host` parses with host `http`, and a bare `?` survives clearing the query because of `ForceQuery`. `NormalizeURL` checks the host name and the path and clears both.

## Web UI

- 2026-11-26. **`shadcn add` with the new-york-v4 registry installs an unpinned `cn` package and imports `from "cn"`.** That is an npm package nobody chose, at whatever version was newest. Remove it from `web/package.json` and import `cn` from `@/lib/utils` in the added component.
- 2026-11-26. **TanStack Table's row selection keeps rows the filter hides**, so Delete after select all and a narrower filter would name rows the user cannot see, which spec 5.3 forbids. The lists keep the selection in plain state and prune it to the shown rows during render; actions receive only selected rows that are shown (`web/src/lib/selection.ts`, `CandidateList`).
- 2026-11-26. **A scan can finish between two status polls**, so watching `running` go from true to false misses it and the lists stay old. The header also reloads the lists when the last scan's ID changes. Found by the end-to-end test.
- 2026-11-26. **Season rows carry the series title**, so a name built from the title alone reads the same for every season of a series. Name a row with `rowLabel` wherever a row is shown by name: tied rows, plans, outcomes, history.
- 2026-11-26. **A browser password manager fills a user-name-plus-password pair and ignores `autocomplete="off"` on password fields.** The filled values became edits the user never typed, and Save sent them as credentials. Connection inputs use `autocomplete="new-password"` for secrets, a name that is no login pattern, and an edit equal to the shown value is no change (`web/src/pages/settings/Connections.tsx`).
- 2026-11-26. **The bulk bar is fixed to the window, so it knows nothing of the side panel.** The shell sets `--content-left` to the panel's width while the panel is shown beside the content, and the bar's left edge reads it; the list pads itself by the bar's measured height. jsdom has no layout, so only the end-to-end test can tell whether the bar covers the panel; it fails when the bar ignores the variable.
- 2026-11-26. **`pnpm build` type-checks `web/e2e` without the DOM library.** A `page.evaluate(() => window...)` callback in the Playwright test fails the build, not the test; pass the script as a string. The e2e run serves whatever was built last, so a failed build leaves it testing the old UI.
- 2026-11-27. **Vitest turns a CSS import into an empty string, even with `?raw`,** unless the file matches `test.css.include`. The theme's contrast test read no tokens and failed on an undefined colour. `web/vite.config.ts` includes `index.css`.
- 2026-11-27. **shadcn's light `muted-foreground` on `muted` is 4.34:1**, below WCAG AA. Text on a muted surface uses `bg-muted/50` or lighter (4.53:1); `web/src/theme-contrast.test.ts` checks the pairs from the values in `index.css`.
- 2026-11-26. **`fs.Sub` answers `fs.ErrInvalid` for a name with a trailing slash, where `fstest.MapFS` answers `ErrNotExist`.** The embedded UI is an `fs.Sub`, so `/series/` answered 500 from the real binary while every test over a plain `MapFS` passed. Test the UI handler over `fs.Sub` (`internal/webui`).

## Storage and concurrency

- 2026-11-22. **The store has one database connection.** Two statements of one request are not atomic: another request can run between them. Anything that must be seen together goes into one transaction (`LatestList`, `ApplySettings`). Inside a transaction, a call on `s.db` waits forever for the connection the transaction holds.
- 2026-11-22. **Finishing a scan drops the rows of the scan before it.** Reading "the latest scan" and then "its rows" in two steps can return the older scan with no rows.
- 2026-11-22. **Read, merge, save needs a lock around all three.** Two settings requests that each changed one rule undid each other, and a reader saw an app's new URL with its old API key. Settings are saved through `config.Service.Apply`, and the API handles one settings request at a time.
- 2026-11-24. **A deadline on a job must not cover its record.** The store honours the context, so once an execution's time limit passed, `RecordAction` failed at `BeginTx`: an item that had already deleted something was not in the history, and the rest of the plan vanished. The executor records each item on its own bounded context, detached from the request and from the execution's deadline (`internal/executor`).
- 2026-11-22. **The database path goes into the `file:` DSN unescaped.** A `%`, `?` or `#` in the config directory breaks opening it. Still open, listed in the follow-ups issue.

## Container image

- 2026-11-27. **podman builds OCI images by default, and the OCI format has no health check:** `HEALTHCHECK` is dropped with a warning and the container never reports healthy. Build with `podman build --format docker`. Docker's buildx keeps it.
- 2026-11-27. **corepack picks the pnpm version from `packageManager` in the working directory's `package.json`.** `pnpm --dir web` run from the parent directory got a different pnpm; the image's web stage works in `web/` (`Dockerfile`).
- 2026-11-27. **A named volume takes the owner and mode of the image's directory; a bind mount keeps the host's.** `/config` is created in the image as 65532:65532 with mode 0700, so a named volume works at once (observed with podman; that BuildKit keeps the mode is not checked yet), and a root-owned bind mount ends removarr at start with `permission denied` on the database (spec 11).
- 2026-11-27. **An amd64 image runs on an Apple Silicon Mac under podman's emulation**, slowly: about 30 seconds to healthy, against about one second natively. Enough to smoke-test both architectures locally (`scripts/smoke-image.sh`).

## Checking against real apps

- 2026-11-19. **Verify a list by computing it a second time from the apps' raw answers**, with a separate script, and compare row by row. This is how the torrents that still held library files were found; no unit test could have shown them.
- 2026-11-22. **To check a change of rules, scan with the old and the new binary against the same apps** and compare row by row. Every difference must be one the raw data explains.
- 2026-11-19. **Print counts and sizes only** when working with real data. Titles, names, URLs and keys stay out of logs, commits and reports.
- 2026-11-19. **On macOS a binary started by an automated agent session can get "no route to host" for addresses on the local network**, while `curl` works. This is the system's local-network privacy control. Run the binary from a normal terminal instead of working around it.
- 2026-11-24. **A replay gets removarr to the apps from an agent session, read-only.** A local stub in the owner's `~/.claude/removarr-check` answers removarr's requests. It fetches each GET with `/usr/bin/curl`, which may reach the local network, and refuses every other method and every path removarr does not read. Claude Code's auto mode blocks this as a containment escape unless the owner allows the two launch scripts by their exact commands. Together with dry run, nothing that changes an app can reach it; three changing requests sent on purpose were all refused and none was fetched. The first check through it gave the same lists as stage 2a.
- 2026-11-24. **Executing a plan can be checked through the replay too, because dry run sends nothing but GET.** One plan over every listed row (104 selected, 102 items) executed in dry run: every item and all 184 steps recorded as `dry-run`, the stored lists unchanged, a second execute refused with 404, and the replay refused no request. Deleting for real stays the owner's check.
- 2026-11-19. **A full scan of the reference stack takes about 19 seconds** on a Raspberry Pi 5: 277 movies, 134 series, 381 torrents, 90 file lists.
- 2026-11-26. **The web UI can be checked against the real apps through the replay, in a browser driven by the agent.** `run-server.sh` builds removarr with whatever UI is in `internal/webui/dist`, so run `pnpm --dir web build` first. One pass covered settings, a scan, filtering and selecting, ignore and un-ignore, and dry-run deletions of movies and of a torrent row; every count the UI showed matched the API, and the replay refused no request. A scan through a cold replay cache takes about a minute.
- 2026-11-26. **The Playwright browser tool writes page snapshots into `.playwright-mcp/` in the working directory, and on real data they hold titles.** The folder is git-ignored; delete it after a check, and read pages with counting scripts rather than full snapshots.

## Tests and reviews

- 2026-11-19. **A test that never asserts the row it is about passes under almost any fault.** Four one-line faults in the linking glue survived a green suite until tests asserted which torrent a series row names. After adding glue code, break it on purpose and check that a test fails.
- 2026-11-22. **Have the reviewer break the code.** Each task review changed one line at a time in a scratch copy and looked for a test that failed. The changes that survived showed tests that passed for the wrong reason: the two ends of an episode range, the list a waiting row is counted in, and the media manager missing from a key.
- 2026-11-24. **A Copilot review can come back empty because of quota.** The review summary then says the requesting user has insufficient remaining quota, with no threads, and `wait-for-copilot.sh` still ends with 0 because a review exists. Read the summary before treating the review as done; such a refusal counts as no review (pull request #13).
- 2026-11-22. **An outside review still finds things after two internal ones.** The automated review of pull request 1 raised seven points, all real, most about what happens between two steps: two reads, two writes, two requests.
- 2026-11-26. **`gofmt -l .` walks `web/node_modules`.** The nested `web/go.mod` keeps `go vet`, `go build` and `go test` out of `web/`, but gofmt does not look at modules and reports Go files of npm packages. The check is `gofmt -l cmd internal` (`AGENTS.md`).
- 2026-11-22. **A subtest name ends up in the path of its temporary directory.** A name with a `%` in it broke opening a database there. Share one fixture across such cases.
