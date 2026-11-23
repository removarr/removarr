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

## Security

- 2026-11-22. **Comparing Origin with Host does not stop DNS rebinding.** A page that points its own domain name at removarr's address makes the browser send that name as both headers. Only checking the Host header against IP addresses, `localhost` and listed names does (`guard`, `REMOVARR_ALLOWED_HOSTS`, spec section 10).
- 2026-11-19. **Go's HTTP client forwards a custom header such as `X-Api-Key` when it follows a redirect.** Every client uses `httpjson.SameHostRedirects`, which refuses another host or port, and a step from https to http.
- 2026-11-19. **A base URL can carry a credential** (`http://user:pass@host`), and an error that repeats a bad URL can leak one. Such URLs are rejected, and URL errors are fixed texts.
- 2026-11-19. **`url.Parse` accepts more than a base URL should be.** `http://http://host` parses with host `http`, and a bare `?` survives clearing the query because of `ForceQuery`. `NormalizeURL` checks the host name and the path and clears both.

## Storage and concurrency

- 2026-11-22. **The store has one database connection.** Two statements of one request are not atomic: another request can run between them. Anything that must be seen together goes into one transaction (`LatestList`, `ApplySettings`). Inside a transaction, a call on `s.db` waits forever for the connection the transaction holds.
- 2026-11-22. **Finishing a scan drops the rows of the scan before it.** Reading "the latest scan" and then "its rows" in two steps can return the older scan with no rows.
- 2026-11-22. **Read, merge, save needs a lock around all three.** Two settings requests that each changed one rule undid each other, and a reader saw an app's new URL with its old API key. Settings are saved through `config.Service.Apply`, and the API handles one settings request at a time.
- 2026-11-22. **The database path goes into the `file:` DSN unescaped.** A `%`, `?` or `#` in the config directory breaks opening it. Still open, listed in the follow-ups issue.

## Checking against real apps

- 2026-11-19. **Verify a list by computing it a second time from the apps' raw answers**, with a separate script, and compare row by row. This is how the torrents that still held library files were found; no unit test could have shown them.
- 2026-11-22. **To check a change of rules, scan with the old and the new binary against the same apps** and compare row by row. Every difference must be one the raw data explains.
- 2026-11-19. **Print counts and sizes only** when working with real data. Titles, names, URLs and keys stay out of logs, commits and reports.
- 2026-11-19. **On macOS a binary started by an automated agent session can get "no route to host" for addresses on the local network**, while `curl` works. This is the system's local-network privacy control. Run the binary from a normal terminal instead of working around it.
- 2026-11-19. **A full scan of the reference stack takes about 19 seconds** on a Raspberry Pi 5: 277 movies, 134 series, 381 torrents, 90 file lists.

## Tests and reviews

- 2026-11-19. **A test that never asserts the row it is about passes under almost any fault.** Four one-line faults in the linking glue survived a green suite until tests asserted which torrent a series row names. After adding glue code, break it on purpose and check that a test fails.
- 2026-11-22. **Have the reviewer break the code.** Each task review changed one line at a time in a scratch copy and looked for a test that failed. The changes that survived showed tests that passed for the wrong reason: the two ends of an episode range, the list a waiting row is counted in, and the media manager missing from a key.
- 2026-11-22. **An outside review still finds things after two internal ones.** The automated review of pull request 1 raised seven points, all real, most about what happens between two steps: two reads, two writes, two requests.
- 2026-11-22. **A subtest name ends up in the path of its temporary directory.** A name with a `%` in it broke opening a database there. Share one fixture across such cases.
