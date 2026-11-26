# removarr — design

- **Date:** 2026-11-18
- **Status:** approved by the owner on 2026-11-18
- **Amended:** 2026-11-19. Linking by content (6.6), chosen by the owner. Episode ranges (6.2), whole torrents (6.3) and data outside the library (6.7), decided from the owner's two principles in section 2 at the owner's request. 2026-11-22: the Host check and its setting (9.1, 10), after the review of pull request 1; whole files (6.3) and the duplicate episode number (6.2), found while building stage 2a. 2026-11-23: when ignores take effect, what can be ignored and what the ignored view shows (6.5, 7.5, 7.6), decided from the principles while building stage 2b. 2026-11-24: how a plan is built (6.5, 6.7, 6.8, 6.10, 7.1, 7.2), decided from the principles while building stage 2c. 2026-11-24: how a plan is executed and recorded (6.7, 6.8, 6.11, 7.1, 7.2, 7.4, 7.5, 7.6, 8, 16.2), decided from the principles while building stage 2d. 2026-11-26: selection and sorting (5.3), out-of-date lists (5.4, 7.6), testing a connection (5.6), what the status reports (7.6), how the UI is served (7.7), refused host names, reverse proxies and framing (10) and the frontend libraries (15), decided from the principles while building stage 3; navigation, the bulk bar and the settings pages (7.7), at the owner's request, the Scan button's place, the phone header and the panel animation (7.7), at the owner's request.
- **Scope:** version 1

## 1. Purpose

removarr helps the owner of a self-hosted media server reclaim disk space. It looks at what has been watched and at what the download client is still holding, and proposes things to delete. It never deletes on its own: every deletion is selected and confirmed by a person.

It is built for one concrete stack first (Jellyfin, Sonarr, Radarr, qBittorrent on a Raspberry Pi 5 running DietPi) and published as open source so others with a similar stack can use it.

What sets it apart from existing cleanup tools: it combines watched-media cleanup and torrent cleanup in one place, behind a review-and-approve UI, instead of deleting on a schedule.

## 2. Goals and non-goals

### Goals for v1

- Suggest watched movies for deletion.
- Suggest watched series, or watched seasons, for deletion.
- Suggest orphan torrents for deletion.
- Let the user delete or ignore suggestions in bulk, and review ignored items.
- Show enough information per suggestion (size, who watched, seeding state) to decide safely.
- Run as one Docker container, on arm64 and amd64, with a small footprint.
- Keep the media server, media manager and download client behind interfaces so other products can be added later.

### Non-goals for v1

These are deliberately left out. Section 14 lists them as later work.

- Automatic or scheduled deletion.
- Authentication.
- "Never watched" and "abandoned series" lists.
- Detecting orphan files on disk.
- Notifications.
- Jellyseerr integration.
- More than one instance of each app (for example a second, 4K Radarr).
- Plex, Emby, usenet clients, or torrent clients other than qBittorrent.

### Principles

The owner stated two principles on 2026-11-19. They decide the cases that the rules in section 6 would otherwise leave open.

- **Free disk space, but never remove a movie or episode that has not been watched.** Where removarr cannot be sure that it was watched, it counts as unwatched. Specials are the one approved exception (6.3).
- **A torrent goes as a whole or stays as a whole.** Deleting only part of what a torrent holds makes no sense: the torrent keeps the data, so no space is freed. removarr never does it.

## 3. Environment and constraints

- **Reference deployment:** Raspberry Pi 5 (8 GB), DietPi, with Sonarr, Radarr, Prowlarr, qBittorrent and Jellyfin.
- **Prowlarr is not used.** Indexer management plays no part in cleanup, so removarr does not connect to it.
- **Network:** trusted local network only. There is no login in v1.
- **Port:** 7272 by default, chosen to avoid the ports commonly used by media-server applications. It is configurable.
- **API-only:** removarr talks to the other apps over HTTP and never reads or writes the media or download directories. It needs no media mounts and works the same whether the other apps run natively on the host or in Docker.

Consequences of API-only:

- Sizes are what the apps report. removarr does not measure the disk.
- removarr cannot see hardlinks. It infers which torrent backs which library file from Sonarr/Radarr history and, where history says nothing, from file sizes (6.6).

## 4. Terms

| Term | Meaning |
|---|---|
| Media server | Where things are watched. Jellyfin in v1. |
| Media manager | Sonarr (series) and Radarr (movies). |
| Download client | qBittorrent in v1. |
| Scan | One on-demand run that reads all apps and produces the lists. |
| Candidate | One row in a list: something removarr suggests deleting. |
| Counting users | The Jellyfin users whose watch state decides whether something is watched. |
| Protected | Excluded from suggestions by a rule (favourite or tag). |
| Ignored | Hidden from a list by the user's choice. |
| Waiting | Qualifies, but shares a torrent or a file with something that stays, so it is not listed yet (6.3). |
| Tied | Rows that share a torrent or a file and are deleted together (6.3). |
| Linked torrent | A torrent that backs at least one current library file, by history or by content (6.6). |
| Orphan torrent | A torrent whose hash no media manager knows and that backs no library file. |
| Unlinked torrent | A torrent a media manager grabbed, but that backs no current library file. |
| Plan | The exact set of operations a deletion will perform, built from live data and shown for confirmation. |

## 5. Features

### 5.1 Lists

1. **Watched movies.**
2. **Watched series.** A setting chooses the unit: whole series, or per season.
3. **Orphan torrents.** Contains both orphan and unlinked torrents, each labelled with its reason.

### 5.2 Information per row

- **Movies and series:** title, year, size, which counting users watched it, the watched date, the number of torrents Delete would remove, and a seeding flag when any of those torrents is below the seeding minimum. A row can be expanded to show its torrents (name, tracker, ratio, seeding time, and how much of the torrent's data is not in the library, see 6.7). A row that is tied to other rows (6.3) names them.
- **Series rows** also show the scope (whole series, or season number) and a "still airing" label where it applies.
- **Torrents:** name, size, state, category, tracker, ratio, seeding time, date added, the reason (orphan or unlinked), and the seeding flag.

### 5.3 Actions

- **Delete** (bulk): builds a plan, shows it for confirmation, then executes it.
- **Ignore** (bulk): hides the rows.
- **Show ignored:** a switch that shows the ignored rows instead. In that view the bulk action is **Un-ignore**. An ignored row must be un-ignored before it can be deleted.
- Lists can be sorted (size, watched date, title), filtered by text, and selected with "select all" over the filtered rows. The bulk bar shows the selected count and total size. The torrent list sorts by date added instead of watched date.
- A bulk action applies only to rows the user can see: changing the filter deselects the rows it hides, and reloading a list deselects the rows it no longer holds. A delete or an ignore clears the selection.

### 5.4 Scan

- A scan runs only when the user starts it. One scan runs at a time.
- Scan results are stored, so the lists load immediately on later visits.
- The header shows when the last scan ran, a summary (candidates per list, protected count, unmatched count, waiting count, errors), and the free space of the library root folders as reported by Sonarr and Radarr.
- **Out of date.** When the rules or a connection's address or user name changed after the last scan started, the header says that the lists may be out of date and suggests a scan. Each scan records a fingerprint of those settings when it starts; a secret is not part of it, and neither is the order of the counting users, excluded categories or excluded tags. Nothing else depends on this: a plan always reads the apps again with the current settings (6.10).

### 5.5 History

A page listing every executed deletion: time, item, size, each step and its result.

### 5.6 Settings

| Setting | Default | Notes |
|---|---|---|
| Connections | none | URL and credentials per app, each with a test button. The test uses the saved values, so it is offered once the changes are saved. See section 9. |
| Counting users | none selected | Watched lists are not produced until at least one user is selected. |
| Watched mode | all | "all": every counting user must have watched. "any": one is enough. |
| Grace period | 0 days | Minimum time since the watched date. |
| Series unit | whole series | Or per season. |
| Protect favourites | on | See 6.4. |
| Protection tag | `keep` | Tag name looked up in Sonarr and Radarr. |
| Minimum ratio | off | Seeding flag, see 6.6. |
| Minimum seeding time | off | In days. Seeding flag, see 6.6. |
| Excluded categories and tags | none | qBittorrent categories and tags left out of orphan detection. |
| Add to exclusion list on delete | off | See 6.7. |

## 6. Behaviour rules

### 6.1 Watched

- A movie or episode is **watched** when Jellyfin marks it played for the counting users: all of them in "all" mode, at least one in "any" mode.
- Its **watched date** is the latest "last played" date among the counting users who played it. If Jellyfin gives no date, the item still counts as watched and the date shows as unknown.
- The **grace period** is satisfied when the watched date is at least that many days ago. An unknown date satisfies it.
- For a season or series, the watched date is the latest watched date among its episodes.
- With no counting users selected, the watched lists are not produced.

### 6.2 Matching between Jellyfin and the media managers

- **Movies:** by TMDB ID, falling back to IMDb ID.
- **Series:** by TVDB ID, falling back to IMDb ID, then TMDB ID.
- **Episodes:** within a matched series, by season number and episode number.
- **Episode ranges:** a Jellyfin item that covers a range of episodes counts for the first episode of the range. It counts for a further episode of the range only when Sonarr holds that episode in the same file as the first. So a range that Jellyfin read wrongly from a file name cannot mark episodes in other files as watched. If Sonarr lists the first episode's number more than once with different files, the range applies to no further episode.
- If several Jellyfin items match one movie or episode, it is watched for a user when any of them is played by that user.
- A movie with no Jellyfin match is never suggested. An episode with no Jellyfin match counts as unwatched. Both are counted as "unmatched" in the scan summary.

### 6.3 Candidate rules

Only episodes and movies that have a file are considered. "Regular seasons" means season 1 and up; season 0 holds specials.

**Movie.** A candidate when it has a file, is matched, is watched, satisfies the grace period, and is not protected.

**Season.** A regular season *qualifies* when it has at least one file and every episode with a file is watched and satisfies the grace period.

**Series, whole-series mode.** A candidate when every regular season that has files qualifies, there is at least one such season, and the series is not protected.

**Series, per-season mode.** Each qualifying regular season is a candidate row. If every regular season that has files qualifies, those rows are replaced by a single series row, the same as in whole-series mode. The rows are not merged while any of those seasons is individually ignored.

**Specials.** Specials never block a series from qualifying and never get their own row. They are deleted only when the series is removed from Sonarr.

**Still airing.** A series row is labelled "still airing" when Sonarr's status for the series is anything other than ended. This changes what Delete does (6.7).

**Finished airing.** A season has finished airing when the series has ended, or when a later regular season exists and every episode in the season has an air date in the past. The newest season of a series that has not ended is never treated as finished.

**Whole torrents.** removarr never deletes only part of the library files that one torrent backs (section 2). So after the rules above have decided which movies, series and seasons qualify, each of them is checked against the torrents that back its files (6.6):

- Rows whose files are backed by the same torrent belong together, and so do rows connected through a chain of such torrents. Such a set of rows is a **group**. Most groups are a single row.
- A torrent **blocks** its group when it is in a media manager's queue, or when it backs a library file that no row of the group would delete. Examples: a multi-season pack with a season that is not watched yet, a collection pack with a movie that is protected, and a pack that holds specials of a series that is still airing.
- A group with a blocking torrent **waits**. None of its rows is a candidate or listed, and the scan summary counts them as waiting.
- In a group of several rows that does not wait, the rows are **tied**: they are deleted together or not at all (6.7). Each is listed and names the rows it is tied to.
- An ignored row (6.5) stays, so the group it belongs to waits.
- A library file goes as a whole too. Rows that would delete the same file belong to one group, and a file blocks its group when it holds an episode that no row of the group would delete. Example: one file holds the last episode of a season and the first of the next, and only one of the two seasons qualifies. Deleting it would remove an episode that has not been watched.
- Only a torrent that is in the download client counts for any of this. A source that history names and that is gone from the client can neither block nor tie.
- In per-season mode, seasons that were merged into one series row wait as one. When one of them is blocked, none is listed, also a season that was listed on its own before the last season qualified. This is accepted for now: nothing is lost, and the summary counts the row as waiting.

The check uses the library files of both media managers. A torrent shared between a movie and a series is therefore never judged from one side only: when a configured media manager did not answer, the groups cannot be formed and neither watched list is produced (6.8, rule 5).

### 6.4 Protection

Protected items are not candidates and do not appear in any list. The scan summary shows how many were protected.

- **Favourites** (when enabled): a movie is protected when any counting user has marked it a favourite. A series is protected when any counting user has marked the series, or any season or episode in it, a favourite.
- **Tag:** a movie or series is protected when it carries the protection tag in Radarr or Sonarr.

A protected series produces no series row and no season rows.

### 6.5 Ignore

- Ignores are stored by stable ID: TMDB ID for a movie, TVDB ID for a series, TVDB ID plus season number for a season, hash for a torrent.
- A series ignore hides the series row and all of its season rows. A season ignore hides only that season's row, and has no effect in whole-series mode.
- Ignored rows are left out of counts and totals.
- Ignores persist across scans until the user removes them.
- Only a row of the latest scan can be ignored. Its title is kept with the ignore, so the ignored view can name it when a later scan no longer has the row.
- Ignoring and un-ignoring take effect at once, without a new scan. A scan stores every row that qualifies, also those that wait (6.3), and the lists apply the current ignores each time they are read: an ignored row is hidden, and a row that is tied to a hidden row waits. This gives the same lists as a new scan would, because a group that loses a row always waits: the torrent or file it shared with that row now backs something that stays. One exception: un-ignoring a season does not merge season rows into a series row (6.3) before the next scan. The reverse is held back instead: a series row that a scan merged while one of its seasons was not yet ignored, because the ignore came during the scan, waits until the next scan, and so does every row tied to it.
- The ignored view of a list shows one entry per ignore: the row as the latest scan has it, or only its key and title when the latest scan has no such row, for example a season ignore in whole-series mode. A season row hidden by an ignore of its series is not a separate entry; un-ignoring the series brings it back.
- A row the latest scan holds back as waiting stays waiting in the ignored view and after it is un-ignored. The ignored view says what it waits for.

### 6.6 Torrents

**Linking.** removarr reads each media manager's history and queue to learn:

- which media items each torrent hash was grabbed for, and
- which torrent each current library file was imported from.

A torrent is **associated** with a movie or episode when that media manager's history records it as grabbed for it. A torrent **backs** a library file when that file was imported from it.

**Linking by content.** History says nothing about a file that was imported by hand, nor about a torrent that was added to the download client directly (16.1). For those, removarr compares file sizes:

- It asks the download client for the files of every torrent that history does not show as backing a library file.
- It also asks a torrent that history does show as backing library files when the torrent is at least 50 MB larger than those files together. Such a torrent may hold a file that was imported by hand as well.
- Torrents in a media manager's queue, in an excluded category or with an excluded tag are never asked about.
- A torrent also **backs** a library file when one of the torrent's files has exactly the same size in bytes as the library file, that size is at least 50 MB, and history names no download as the source of that library file.
- A torrent that backs a library file by content is also **associated** with the movie, or with every episode, that the file belongs to.

History takes precedence: content never replaces a source that history names. When files of several torrents match the same library file, the torrent whose ID sorts first backs it and the others do not.

Limits, accepted for v1:

- Of two torrents that hold the same file, such as cross-seeds, only one is linked. The other stays listed.
- A torrent that history shows as backing library files is asked for its files only when the size test above says it holds more. If history names it as the source of a file it does not hold, the test can fail, and a file imported by hand from the same torrent is then not linked to it. Deleting everything that history links to the torrent would remove it although it still holds that file.
- A file that the torrent lists but has not downloaded is compared like any other.
- Two unrelated files of exactly the same size would be linked. The confirmation of a deletion still shows every torrent it would remove (6.8).

**Classification** of each torrent in the download client:

- **In queue:** the hash is in a media manager's queue. Never listed.
- **Linked:** it backs at least one current library file, by history or by content. Not listed as an orphan; shown under its movie or series.
- **Unlinked:** a media manager's history knows the hash, but it backs no current library file. Typical cause: the release was replaced by an upgrade. Listed.
- **Orphan:** no media manager knows the hash and it backs no library file. Typical cause: the movie or series was deleted from the media manager and the torrent was left behind. Listed.

Torrents in an excluded category or with an excluded tag are left out of orphan detection.

**Seeding flag.** A torrent is flagged "below seeding minimum" when:

- both minimums are set, and its ratio is below the minimum ratio *and* its seeding time is below the minimum time; or
- only one minimum is set, and the torrent is below it.

With both minimums off, nothing is flagged. The flag is information only. It appears on the row and again in the confirmation dialog, and never blocks a deletion.

### 6.7 What Delete does

| Candidate | Media manager step |
|---|---|
| Movie | Remove the movie from Radarr, with its files. |
| Series row, series ended | Remove the series from Sonarr, with its files (specials included). |
| Series row, still airing | Unmonitor the episodes that have files in regular seasons, all of which are watched. Unmonitor each of those seasons that has finished airing. Delete those episode files. The series stays, and upcoming episodes still download. |
| Season row | Unmonitor the season's episodes that have files. Unmonitor the season if it has finished airing. Delete those episode files. The series stays. |

Unmonitoring comes before deleting: if the deletion fails, the files stay and nothing is downloaded again; the other way round, a failed unmonitor would leave deleted episodes monitored, and Sonarr would download them again. Within a plan item, every unmonitor step of every row comes before any other step, since one row can delete a file that holds another row's episode.
| Orphan or unlinked torrent | None. |

**Download client step.** The same rule applies to every movie, series and season row: after the media manager step, removarr removes, with data, each torrent that

- is associated with a deleted movie or episode,
- is not in a media manager's queue, and
- no longer backs any library file.

For an orphan or unlinked torrent row, the torrent itself is removed, with data.

Notes:

- **Whole torrents.** A torrent is removed with everything it holds. removarr never removes part of a torrent's data, and because of the whole-torrent rule (6.3) every torrent that backs a deleted file is removed in the same deletion. A multi-season pack or a collection pack is therefore not touched until everything it backs can go.
- **Tied rows** (6.3) are deleted together. When Delete is asked for one of them, the plan contains all of them and says which rows were added and why. If one of their media manager steps fails, none of the torrents they share is removed.
- **Data outside the library.** A torrent can hold files that are not in the library, because they were never imported or were replaced since. They go with the torrent. A torrent's data outside the library is its size minus the sizes of the library files it backs. The row and the confirmation state this amount for every torrent where it is 50 MB or more. It is information only and never blocks a deletion.
- A torrent that is associated with a movie or episode by content (6.6) is treated the same as one associated by history.
- **Exclusion list:** when the setting is on, removing a movie from Radarr or a series from Sonarr also adds it to that app's import exclusion list, so an import list does not add it back.
- **Shared data:** if a torrent being removed has the same content path as a torrent that is not being removed, or one of the two paths lies inside the other, removarr removes the torrent entry and keeps the data. A torrent whose content path the download client does not report keeps its data too, and so does every torrent being removed while a torrent that stays has no reported content path: unsure means keep. Paths are compared without regard to case or to the kind and number of separators, which only ever keeps more. The plan states this for each torrent; when the plan is executed, removarr decides it again from the torrents that are actually removed, because a failed item keeps its torrents (6.8, rule 4; 6.11).
- **Linked by file size.** The plan marks each torrent that is tied to the deleted items only by content (6.6), so that the case of two unrelated files of the same size can be checked before confirming.
- A torrent appears once in a plan. When a selected torrent row is removed with a selected movie or series anyway, the torrent row is skipped and names that item.
- Deleting an orphan or unlinked torrent never changes anything in Sonarr or Radarr.

### 6.8 Safety rules

1. **Nothing is deleted without a confirmed plan.** Selecting Delete asks the server for a plan. The server builds it from live data, not from the stored scan: the apps are read again and every selected item is re-evaluated with the same rules as the scan (6.10). Items that no longer qualify are listed in the plan as skipped, with the reason. The confirmation dialog shows the plan. Only a confirmed plan is executed.
2. **Plans expire** ten minutes after they are built. An expired plan cannot be executed; the user rebuilds it.
3. **Order per item:** media manager first, then torrents. If the media manager step fails, the torrent steps for that item are skipped. Tied rows (6.3) count as one item: all of their media manager steps come first. An item stops at its first failed step: the steps after it are not sent and are recorded as skipped (6.11).
4. **Failures are contained.** A failed item is recorded with its error and the remaining items continue. Anything left behind, such as a torrent whose media was already removed, appears as an orphan in the next scan.
5. **No guessing when an app is down.** A list is produced only when every app it depends on answered completely:
   - Watched movies: Jellyfin, Radarr, qBittorrent, and Sonarr when it is configured.
   - Watched series: Jellyfin, Sonarr, qBittorrent, and Radarr when it is configured.
   - Orphan torrents: qBittorrent and every configured media manager, of which there must be at least one.

   Otherwise the list shows an error instead of results. In particular, torrents are never called orphans while a configured media manager is unreachable. The download client has answered completely only when the file lists needed for linking by content (6.6) were read too.
6. **Dry run.** With `REMOVARR_DRY_RUN=true`, plans execute as no-ops: no deleting call is sent, and the history records the action as a dry run.

### 6.9 Sizes

- A movie or series row's size is the sum of the library file sizes, as reported by the media manager, for the files Delete would remove.
- A torrent row's size is its size as reported by the download client.
- Sizes are labelled as sizes, not as space that will be freed, because removarr cannot see hard links (section 3). A plan never keeps a torrent that backs the files it deletes (6.3).

### 6.10 Building a plan

Decided from the principles in section 2 while building stage 2c.

- **Live data.** The planner reads every configured app the way a scan does, the file lists for linking by content included, and applies 6.1 to 6.6 with the current ignores. Re-reading only the selected items is not enough: whether a torrent can go depends on everything else it backs (6.3).
- **Selected rows.** Each selected row, named by list and key, is looked up in that result:
  - When its list cannot be produced (6.8, rule 5), the row is skipped with the list's error. A movie or series therefore needs every configured media manager.
  - When the latest scan does not hold the row, or the new read no longer produces it, the row is skipped as no longer qualifying.
  - An ignored row is skipped (5.3).
  - A row that waits is skipped with what it waits for: a shared torrent that is in a queue, a shared torrent that also backs a library file that stays, a shared file that also holds an episode that stays, a tied row that is ignored, or a series row merged over a season that is now ignored (6.5).
- **Items.** A selected row and the rows it is tied to (6.3) form one item. The rows that were not selected are added and marked, naming the selected row and the torrents they share, or a shared file; an added row the latest scan did not hold is marked as such. A group with an ignored row, or with a row that waited in the latest scan, is skipped. Several selected rows of one group make one item.
- **Two reads.** History is read before the queue, so a download imported between the two reads looks unlinked for that read. A torrent is therefore removed only when two reads agree: the latest scan, from which the rows were selected, and the planner's own read. An item is skipped as changed since the scan when the planner would remove a torrent that the latest scan did not name for any of its rows, and when the latest scan named a torrent for its rows that is still in the download client and that the planner would now keep. A row that waited in the latest scan is skipped too: the user has not seen it listed. A torrent row must be a row of the latest scan and still be an orphan or unlinked torrent.
- **Contents of an item:** its rows with their sizes; the media manager steps of 6.7 with the IDs the media manager needs; the torrents it removes, each with the seeding flag, the data outside the library, whether its data is kept (shared data, 6.7) and whether it is linked by file size; and the torrents associated with its rows that stay, with the reason: in a queue, or backing a library file that stays.
- A torrent row becomes an item of its own, with the torrent and no media manager step.
- The plan is stored with its expiry (6.8, rule 2) and returned to the user for confirmation.

### 6.11 Executing a plan

Decided from the principles in section 2 while building stage 2d.

- **One thing at a time.** A scan, the building of a plan and the execution of a plan never run at the same time. A request to start one while another runs is refused with a message that says what is running.
- **A plan runs once.** Executing a plan takes it out of the store, so a second request for it finds no plan. Executing a plan also discards every other pending plan: they were built from data this execution changes, and the user rebuilds them. The expiry stored with the plan decides whether it can still run (6.8, rule 2). A request for a plan that is unknown, already executed or discarded, or expired is refused and changes nothing.
- **Items in plan order.** Each item runs its media manager steps in the order the plan lists them, then its torrents, one torrent at a time (6.8, rule 3). The first step that fails stops the item; the steps after it, torrents included, are not sent and are recorded as skipped. The next item runs regardless (6.8, rule 4).
- **Torrents are decided again.** Right before an item's torrents, removarr reads the download list again. A torrent of the plan that is no longer in the download client is recorded as already gone and needs no call. If the list cannot be read, the item's torrent steps fail with that error.
- **Shared data at execution.** A torrent keeps its data when the plan says so, or when, at the moment it is removed, its content path is unknown or a download that is still in the client has a content path that is unknown, the same, inside it or containing it (6.7, shared data). So a torrent whose data another torrent of the plan also holds keeps it while that torrent is still there, and the last of them to go takes the data with it. Within an item, torrents with longer content paths go first, so a torrent whose directory holds another's goes after it and the directory goes as a whole. A failed or skipped removal leaves its torrent in the client, and every torrent removed after it keeps the data they share. Unsure means keep.
- **Dry run** (6.8, rule 6). Every step is recorded as it would have run, the shared-data decision included, and no call that changes anything is sent. The download list is still read. The stored lists stay as they are.
- **History.** Each item is recorded as soon as it finishes, so an execution that is cut off keeps what it did: the time, the plan, whether it was a dry run, its rows with their sizes, its size, its outcome (done, failed, or dry run), and every step with its result (done, failed with the error, skipped, already gone, or dry run). A torrent step records whether its data was kept: for a removal that was sent, or would have been in a dry run, the choice sent with it, also when the request failed; for a skipped torrent or one already gone, the plan's. Rows the plan skipped are not recorded: nothing was done to them.
- **Stored lists** (7.4, step 5). After each item, unless it was a dry run, removarr removes from the latest stored scan each row whose media manager steps all succeeded, a torrent row whose torrent was removed or was already gone, and the torrent-list row of every torrent the item removed. A row whose steps did not all succeed stays, and the next plan for it reads the apps again (6.10).
- **To the end.** An execution runs to its end even when the user's browser goes away. The request answers with what was recorded.
- **Stopping removarr.** When removarr is stopped during an execution, it starts no further item, waits up to two minutes for the item it is running to finish and be recorded, and then stops. Items that did not start are not recorded and stay in the lists: nothing was done to them.
- **A media manager that no longer knows the item** answers an error, and the item fails: removarr does not guess that it was removed some other way. Its torrents stay, and the next scan shows them (6.8, rule 4).

## 7. Architecture

```
Browser UI (React, TypeScript, shadcn)
        │  JSON over HTTP
   ┌────┴─────────────────────────────┐
   │ Go server                        │
   │   HTTP API ── scan engine ───┐   │
   │            ── planner ───────┤   │
   │            ── executor ──────┼── providers ── Jellyfin, Sonarr, Radarr, qBittorrent
   │            ── store (SQLite) ┘   │
   └──────────────────────────────────┘
```

The UI is compiled to static files and embedded in the Go binary, so the whole app is one process and one container.

### 7.1 Units

| Unit | Responsibility | Depends on |
|---|---|---|
| `domain` | Product-neutral types: media items, files, watch state, downloads, candidates, plans. | nothing |
| `providers` | The three interfaces below. | `domain` |
| `providers/jellyfin`, `sonarr`, `radarr`, `qbittorrent` | One implementation each. Translate an app's API into `domain` types. | `providers`, `domain` |
| `httpjson` | JSON request helper and timestamp parsing shared by the provider implementations. | nothing |
| `scan` | The rules of section 6.1 to 6.6, as pure functions from snapshots to candidates. No I/O. | `domain` |
| `scanner` | Runs one scan: reads every configured app, calls `scan`, and stores the result. | `providers`, `scan`, `store` |
| `planner` | Builds a plan for selected items from live data (6.7, 6.8, 6.10). It reads and evaluates through `scanner`, so a plan applies exactly the rules of a scan. | `scanner`, `providers`, `scan`, `domain` |
| `executor` | Runs a plan step by step and records results (6.11). The only unit that changes anything in an app. | `providers`, `store`, `domain` |
| `gate` | Lets one scan, plan building or execution run at a time (6.11). | nothing |
| `store` | SQLite persistence. | `domain` |
| `config` | Environment variables and the UI-over-env precedence. | `store` |
| `app` | Builds provider implementations from the current settings. | `config`, `providers` |
| `api` | HTTP handlers; serves the embedded UI. | all of the above |
| `web` | The browser UI. | the HTTP API only |
| `webui` | Embeds the built UI in the binary and serves it (7.7). | nothing |

`scan` contains every decision about what is a candidate. Because it does no I/O, each rule can be tested with plain data.

### 7.2 Provider interfaces

Illustrative shape, not final signatures.

- **MediaServer**
  - list users;
  - return a library snapshot for a set of users: movies and episodes with their external IDs, and per user the played flag, last played date and favourite flag.
- **MovieManager** (Radarr) and **SeriesManager** (Sonarr)
  - return a snapshot: items, files and sizes, tags, series status and air dates, torrent links from history, queue hashes, root folder free space;
  - remove a movie or series, delete episode files, unmonitor episodes and seasons.
- **DownloadClient**
  - list downloads: ID, name, size, state, category, tags, content path, date added, and optionally ratio, seeding time and tracker;
  - list the files of one download: name and size;
  - remove a download, with or without its data.

The seeding fields are optional so that a usenet client, which does not seed, can implement the same interface later.

The operations that change something form interfaces of their own, separate from the ones that read, and only the executor is given them. A scan or a plan can therefore not change an app, and the compiler checks it.

Adding Plex, Emby or another download client means adding one implementation of one interface. The scan engine, planner, executor and UI do not change.

### 7.3 Scan flow

1. The user starts a scan. The server refuses if one is already running.
2. The server takes a snapshot from every configured app.
3. The server reads the file lists of the torrents that 6.6 asks about, and `scan` links them by content.
4. For each list whose dependencies all answered (6.8, rule 5), `scan` computes the candidates.
5. The store replaces the previous scan's candidates with the new ones and saves the summary.
6. The UI polls the scan status and reloads the lists when it finishes.

### 7.4 Delete flow

1. The user selects rows and chooses Delete.
2. The server builds a plan from live data and stores it with an expiry.
3. The UI shows the plan grouped by app: what is removed, which torrents are below the seeding minimum, which torrents stay and why, and which items were skipped.
4. The user confirms. The server executes the plan item by item, in order (6.11).
5. The server records each item in the history. It removes the deleted candidates from the stored scan, along with any torrent rows whose torrents were removed, so the lists are correct without a rescan (6.11).
6. The UI shows the outcome, including any failures.

### 7.5 Storage

One SQLite file in the config directory.

| Table | Contents |
|---|---|
| `settings` | Key and JSON value. Holds rules and connection values saved in the UI. |
| `ignores` | Kind, stable key, title at the time of ignoring, created time. |
| `scans` | Start and end time, status, summary. The last ten are kept. |
| `candidates` | The latest scan's rows, also those that wait (6.3): list, stable key, row data as JSON. |
| `plans` | Pending plans with their expiry. The `expires_at` column decides whether a plan can still run. |
| `actions` | History, one record per executed item (6.11): time, plan, dry run, outcome, size, and the rows and per-step results as JSON. |

### 7.6 HTTP API

| Method and path | Purpose |
|---|---|
| `GET /api/health` | Liveness, for the container health check. |
| `GET /api/status` | Which apps are configured, dry run, the scan in progress, the last scan with its summary and free space, and whether the lists are out of date (5.4). It does not contact the apps, because the header polls it: a connection's health shows in its test (5.6) and in the errors of the last scan (8). |
| `POST /api/scans` | Start a scan. |
| `GET /api/scans/current` | Scan progress and result. |
| `GET /api/candidates?list=…&ignored=…` | Rows of one list. |
| `POST /api/ignores`, `DELETE /api/ignores` | Ignore and un-ignore rows, in bulk. The body names each row by list and key (6.5). |
| `POST /api/plans` | Build a plan for the selected rows. |
| `POST /api/plans/{id}/execute` | Execute a confirmed plan and answer with the records of its items (6.11). |
| `GET /api/actions?before=…&limit=…` | History, newest first. `before` is the ID of the oldest record of the previous page; `limit` is 50 by default and at most 200. |
| `GET /api/settings`, `PUT /api/settings` | Read and change settings. |
| `POST /api/connections/{app}/test` | Test one connection. |
| `GET /api/media-server/users` | Users for the counting-users picker. |

### 7.7 Web UI

- **Pages:** Movies, Series, Torrents, History, Settings. Settings has three sub-pages, each with its own heading: Connections (with the dry run notice), Counting users and Rules. `/settings` opens Connections.
- **Navigation:** a side panel on the left, with an icon and a label per page; the label is the link's name. The Settings sub-pages are nested below Settings and always shown with it. The current page is marked, and Settings is marked while one of its sub-pages is shown. A button in the header hides and shows the panel. On a wide screen the panel is shown by default beside the content, and the browser remembers whether it was hidden; a browser that cannot store it shows the panel. On a narrow screen the panel is hidden by default and opens as a drawer over the content, which Escape, a click beside it or a chosen page closes. While the drawer is open the header and the page behind it cannot be reached, by keyboard or by a screen reader, and closing it gives the focus back to the menu button.
- **Header:** the menu button, Scan button, last scan time and summary, free space. The Scan button sits at the right end of the header; on narrow screens the last scan's details take a second row; the side panel opens and closes with a short animation, none when the system asks for reduced motion.
- **Lists:** a table on wide screens and cards on narrow ones. While rows are selected a bulk bar is fixed at the bottom of the window, over the width of the content and never over the side panel, and the list leaves room below its last row so that no row is hidden behind the bar.
- **First run:** with no connections configured, the lists point the user to Settings, Connections.
- **Theme:** follows the system light or dark setting.
- **Serving:** the server serves the UI's files at every path outside `/api/`, and the UI's page for any path it does not hold, so a page of the UI can be reloaded. A path under `assets/`, or one whose last part has a file extension, names a file and never a page of the UI: when the file is missing the answer is "not found". A binary built without the UI answers those paths with a page that says so.

## 8. Error handling

- **App unreachable or returning errors during a scan:** the lists that depend on it show the error; other lists are still produced.
- **App unreachable while planning:** the affected items are skipped in the plan with the reason.
- **Step fails while executing:** see 6.8, rules 3 and 4.
- **Scan already running:** a second start request is refused with a clear message. So is a request to build or execute a plan while a scan, a plan or an execution runs, and the other way round (6.11).
- **Expired plan:** execution is refused and the UI offers to rebuild it. A plan that was already executed or discarded is refused the same way.
- **Invalid settings:** rejected on save with a message naming the field.

## 9. Configuration

### 9.1 Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `REMOVARR_PORT` | `7272` | Port the web server listens on. |
| `REMOVARR_CONFIG_DIR` | `/config` | Directory holding the SQLite file. |
| `REMOVARR_DRY_RUN` | `false` | See 6.8, rule 6. |
| `REMOVARR_ALLOWED_HOSTS` | none | Host names removarr answers to, besides IP addresses and `localhost`. Separated by commas. See section 10. |
| `REMOVARR_JELLYFIN_URL`, `REMOVARR_JELLYFIN_API_KEY` | none | Jellyfin connection. |
| `REMOVARR_SONARR_URL`, `REMOVARR_SONARR_API_KEY` | none | Sonarr connection. |
| `REMOVARR_RADARR_URL`, `REMOVARR_RADARR_API_KEY` | none | Radarr connection. |
| `REMOVARR_QBITTORRENT_URL`, `REMOVARR_QBITTORRENT_USERNAME`, `REMOVARR_QBITTORRENT_PASSWORD` | none | qBittorrent connection. |

### 9.2 Precedence

- For each connection field, a value saved in the UI wins. If none is saved, the environment variable is used. If neither exists, the field is unset.
- Clearing a field in the UI falls back to the environment variable.
- The settings page shows where each value comes from (UI, environment, or unset).
- Port, config directory, dry run and the allowed host names are environment-only. The rules in 5.6 are UI-only.

## 10. Security

- No login in v1. removarr is meant for a trusted local network, and the README says not to expose it to the internet.
- API keys and the qBittorrent password are write-only: the API never returns them, only whether each is set and where it comes from.
- Secrets saved in the UI are stored in the SQLite file, which is created readable by its owner only.
- Requests that change anything must be JSON and carry a custom header, and the server rejects requests whose origin does not match the address it was reached on. This stops another website open in the user's browser from triggering deletions.
- Every answer forbids being shown in a frame of another page, so another site cannot lure the user into clicking through a deletion.
- The server answers a request, read or change, only when its Host header is an IP address, `localhost`, or a host name listed in `REMOVARR_ALLOWED_HOSTS`. Otherwise a website could point its own domain name at removarr's address (DNS rebinding). The browser would then treat removarr as part of that site, and the site could read every answer and change every setting. A name such as `pi.local`, or the name a reverse proxy passes on, has to be listed. The health check is the one exception: it answers under any name, because it tells nothing about the library and a monitor may reach it by a container name. A refused request outside `/api/` gets a short page with the same message, so a browser shows why instead of a blank page; the UI shows the message of a refused API request.
- A reverse proxy in front of removarr must pass the Host header on unchanged, and its name must be listed in `REMOVARR_ALLOWED_HOSTS`: a request that changes something must come from the address the request names.
- removarr contacts only the apps configured in its settings. It does not follow a redirect to another host or port, or from https to http, because the API key would go with it.

## 11. Packaging and deployment

- One container image for `linux/arm64` and `linux/amd64`, published as `ghcr.io/removarr/removarr`.
- Runs as a non-root user. The only volume is the config directory.
- The repository ships a `docker-compose.yml` example and a health check on `/api/health`.
- The README explains how to reach apps that run natively on the host from inside the container.

## 12. Testing

- **`scan`:** table-driven tests over hand-built snapshots, one per rule in sections 6.1 to 6.6, including specials, still-airing series, season merging, ignored seasons, both watched modes, grace period, protection, each torrent classification, linking by content, episode ranges, and whole torrents with waiting and tied rows.
- **Providers:** each implementation tested against a local HTTP server replaying recorded responses from the real app.
- **`planner` and `executor`:** tested with fake providers: re-evaluation and skips, step order, failure containment, the shared-data rule, plan expiry, dry run.
- **`store`:** tested against a temporary SQLite file.
- **`api`:** handler tests, including the write-only secrets and the request checks in section 10.
- **`web`:** component tests for selection and the bulk bar, and one end-to-end test against a server running with fake providers.
- **Before a release:** a manual pass on the reference stack with dry run on, then with it off.

## 13. Build order

v1 is one product, built in four stages. Each stage ends with something that can be checked on the reference stack before the next begins.

1. **Read-only core.** Providers, the scan engine, the store and the scan and candidate endpoints. Check: a scan against the real apps produces three correct lists. The assumptions in section 16 are verified here.
2. **Ignore and delete.** It starts with the rules that were decided after stage 1 was checked and that change the lists: episode ranges (6.2), whole torrents and whole files (6.3) and data outside the library (6.7). These are built (stage 2a). Then ignores, the planner, the executor and the history. Check: plans are correct with dry run on, then real deletions work.
3. **Web UI.** All pages, on top of the finished API.
4. **Packaging.** The multi-architecture image, the compose example and the README.

## 14. Later work

Not part of v1, in no fixed order:

- "Never watched" list (added long ago, never played) and "abandoned series" list.
- Orphan files on disk, which needs access to the media and download directories.
- Scheduled scans and notifications.
- Jellyseerr: clear the request when its media is deleted.
- Grouping cross-seeded torrents with the torrent they duplicate.
- Multiple instances per app.
- Plex and Emby; other torrent clients; usenet clients.
- Authentication.

## 15. Technology choices

Approved by the owner on 2026-11-18. Versions are pinned in the implementation plan to the stable releases current at that time.

| Area | Choice | Reason |
|---|---|---|
| Server language | Go | Small footprint on the Pi; one static binary. |
| HTTP and logging | Go standard library only | About a dozen endpoints; no framework needed. |
| SQLite | `modernc.org/sqlite`, plain SQL, embedded migration files | Pure Go, so arm64 and amd64 builds need no C toolchain. |
| App clients | Thin hand-written clients for Jellyfin, Sonarr, Radarr and qBittorrent | Only a few endpoints each, and exact control over the fields the rules depend on. |
| Frontend base | Vite, React, TypeScript, Tailwind CSS, shadcn/ui | Chosen by the owner. |
| Frontend libraries | TanStack Query, React Router | Lists keep their selection in plain state: TanStack Table's row selection keeps rows the filter hides, which 5.3 forbids. |
| Tooling and tests | pnpm; Go `testing`; Vitest; Playwright for the end-to-end test | Common, well-known defaults. |
| Container | Multi-stage build, distroless base image, non-root user | Small image with nothing extra inside. |
| CI and releases | GitHub Actions; image on `ghcr.io/removarr/removarr` | Free for public repositories and lives with the code. |
| Repository | `github.com/removarr/removarr` | The owner's GitHub organisation. |
| License | MIT | Simplest for others to reuse. |

## 16. Verified API behaviour

The four assumptions this design started from were checked on 2026-11-19 against the reference stack, read-only, at the end of stage 1.

| App | Version |
|---|---|
| Sonarr | 4.0.12 |
| Radarr | 5.18.4 |
| Jellyfin | 12.4.0 |
| qBittorrent | 5.1.0 (Web API 2.11.4) |

Library at the time: 277 movies (226 with a file), 134 series (about 3,950 episode files), 381 torrents, one media server user. A full scan took 18 seconds.

**Result in one line:** assumptions 2, 3 and 4 hold. Assumption 1 holds for downloads the media managers imported themselves and does not hold for manual imports, which changes what the torrent list means. Section 16.1 has the detail and the decision it calls for.

### 16.1 History links downloads to files: holds for automatic imports only

Assumption: Sonarr and Radarr history expose the torrent hash on grab and import events, and make it possible to tell which download a current file was imported from.

Observed:

- Grab events and automatic import events carry the download ID. For a torrent it is the 40-character hash in upper case. qBittorrent reports the same hash in lower case, so removarr lower-cases both sides.
- Import events carry the library file's ID (`data.fileId`). Older records lack it: 599 import records on the reference stack. For those the code takes an item's most recent import as the source of its current file, which linked 98 files.
- History is returned newest first. The requested page size of 1,000 is honoured, smaller sizes too, and consecutive pages do not overlap.
- Download IDs from other download clients appear in history (25 records from a usenet client). They never match a torrent and are ignored.
- Completed downloads whose import is blocked or pending stay in the managers' queues: 25 downloads on the reference stack, 14 of them items the manager cannot match to a series or movie. removarr asks for those too, and a queued torrent is never listed (6.6).

**Deviation.** History cannot tell where a file came from in two cases:

- **Manual import.** The import event has a file ID and no download ID. On the reference stack this is about two thirds of the episode files that have an import record.
- **A torrent added to the download client outside Sonarr and Radarr.** It has no history at all.

With history as the only source of links, a torrent that still holds library files is classed as unlinked or orphan in both cases. Measured on the reference stack, before linking by content existed, by comparing each listed torrent's files with the library files by exact size:

| List | Torrents listed | Hold files that are in the library | Hold none | No large file |
|---|---|---|---|---|
| Orphan | 27 (339 GB) | 14 (262 GB) | 10 (60 GB) | 3 (17 GB) |
| Unlinked | 41 (712 GB) | 13 (614 GB) | 28 (97 GB) | 0 |

So 27 of the 68 listed torrents, 876 of 1,050 GB, still hold library files. With hard links, removing such a torrent with its data frees no space and stops its seeding, while the list presents it as left behind.

**Decision, 2026-11-19.** The owner chose to also link torrents to library files by content. Section 6.6 defines the rule under "Linking by content".

**Measured after the change.** One scan of the reference stack asked for the file lists of the 68 torrents that history does not link:

- 27 of them (876 GB) hold library files and are now linked, through 1,414 episode files and 9 movie files. None of the 27 is listed any more, and no torrent is newly listed.
- 41 torrents (174 GB) stay listed: 28 unlinked and 13 orphan. The list matched an independent recomputation from the apps' raw answers, torrent by torrent.
- The movie and series lists kept the same rows. One series row gained a torrent that a deletion would remove.
- The scan still took 18 seconds.

The same raw answers showed what linking by content does not reach:

- 1,434 library files of at least 50 MB match no file of any torrent in the download client.
- One torrent (15 GB) that history links to some library files also holds 6 files that were imported by hand. This is why 6.6 also asks torrents that are larger than the files history links to them. On the reference stack that is 22 more torrents, 90 file lists in one scan, and it links those 6 files without moving any other link. A scan with that rule read 90 file lists, produced the same three lists row for row, matched the independent recomputation again, and took 19 seconds.
- 2 of the 27 torrents also hold files of at least 50 MB that match no library file, 6 GB together. Under 6.7 those files go with the torrent when its library files are deleted, and the confirmation says how much of the torrent's data is not in the library.
- No file that carries a content link was incompletely downloaded.

### 16.2 Sonarr exposes monitoring per episode and per season: holds

Assumption: Sonarr's API can unmonitor individual episodes and a season. If unmonitoring a season does not also unmonitor its episodes, removarr does both explicitly.

Observed: each season object on a series and each episode carries a `monitored` field. Stage 1 only reads. Every series on the reference stack has a TVDB ID.

Whether unmonitoring a season also unmonitors its episodes was answered from Sonarr's source for version 4.0.12 while building stage 2d: saving a series whose season changed from monitored to unmonitored sets every episode of that season to the same (`SeriesService.UpdateSeries` calls `SetEpisodeMonitoredBySeason`). removarr does both anyway: it unmonitors the episodes whose files it deletes, and then the season. To be confirmed on the reference stack with dry run off.

### 16.3 Jellyfin reports watch state per user: holds

Assumption: Jellyfin reports the played flag, last played date and favourite flag per user, and the external IDs, for movies and episodes.

Observed:

- The played flag, the last played date and the favourite flag are reported per user, with TMDB and IMDb IDs on movies, TVDB, IMDb and TMDB IDs on series, and TVDB and IMDb IDs on episodes.
- The API key is accepted only in the header `Authorization: MediaBrowser Token="<key>"`. This version answers 401 to the older `X-Emby-Token` header.
- The requested page size is honoured and the total is reported.
- Cases the rules anticipated all occur in real data: a played episode with no last played date, items covering a range of episodes, specials, and one movie and one series with no external ID (counted as unmatched).

### 16.4 qBittorrent reports the torrent fields: holds

Assumption: qBittorrent reports content path, ratio, seeding time, tracker, category and tags per torrent.

Observed: every torrent carries hash, name, size, state, category, tags, content path, added date, ratio, seeding time and tracker. Hashes are 40 characters of lower-case hex. The login was not exercised: the reference instance allows the local network without one.

### 16.5 The three lists

The lists of one scan were recomputed independently from the apps' raw answers and matched removarr's output exactly:

| List | Rows | Notes |
|---|---|---|
| Watched movies | 45 | 3 movies with a file had no match on the media server |
| Watched series | 18 | 10 of them still airing; no unmatched episodes |
| Orphan torrents | 41 | 13 orphan and 28 unlinked, after linking by content; 68 before it (16.1) |

All 13 orphans are torrents outside the two categories the media managers use. Unless their categories are excluded in the settings, torrents that have nothing to do with the media library are listed.

### 16.6 The rules decided after the check

On 2026-11-19 the owner stated the two principles of section 2 and asked for the open cases to be decided from them. The rules that follow from them are episode ranges (6.2), whole torrents (6.3) and data outside the library (6.7). They were built in stage 2a, which added one more: a library file goes as a whole too (6.3). Measured on the reference stack before they were built, from the lists of one scan and the apps' raw answers:

- **Whole torrents.** None of the 63 movie and series rows would delete only part of what a torrent backs. For 32 rows every backing torrent goes with the row, and for 31 rows no torrent in the download client holds the files. The rule changes no row today.
- The shapes the rule exists for do occur among the 315 torrents that back library files: 12 back files in two or more seasons of one series, 2 back specials together with regular episodes, and 1 backs two or more movies.
- **Data outside the library.** 23 of those 315 torrents hold 50 MB or more that is not in the library, 66 GB together. The 63 rows would remove 64 torrents. 2 of them hold such data, 14 GB together, and both are old downloads that back no library file any more.
- **Episode ranges.** 20 of the 4,232 episode items on the media server cover a range, each of two episodes. For 18 of them Sonarr holds both episodes in one file. For one, the second episode is in another file or has none, and one belongs to a series without a match. Neither of those two is played, so the rule changes no row today.

**Measured after they were built, 2026-11-22.** Two scans of the reference stack a few seconds apart, one with the binary from before these rules and one with them:

- Both list the same 45 movies, 18 series and 41 torrents, and every movie and series row names the same torrents in both.
- The new scan reports no waiting row and no tied row. An independent computation of both rules from the apps' raw answers, whole torrents and whole files, expects exactly that.
- 2 torrents on movie and series rows state data outside the library, 14 GB together, each with the figure the independent computation gives. No row of the torrent list states one.
- The torrent list matches the independent recomputation torrent by torrent, with 90 file lists read.
- Both scans took 18 seconds.
