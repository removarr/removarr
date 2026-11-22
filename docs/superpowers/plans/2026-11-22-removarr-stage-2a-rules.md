# Stage 2a: the rules from the owner's principles — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: use superpowers:subagent-driven-development to implement this plan task by task. Each task gives exact interfaces, the rule in the spec's words and the tests that must exist. The code is written test-first by the task's implementer; it is not spelled out here.

**Goal:** build the rules that were decided from the owner's two principles: episode ranges, whole torrents with waiting and tied rows, the stricter list dependencies, and data outside the library (GitHub issue #2).

**Architecture:** `internal/scan` keeps every decision. `scan.Movies` and `scan.Series` stop at "which rows qualify" and return rows that say what Delete would touch. A new step, `scan.WholeTorrents`, looks at the rows of both lists together, forms groups through the torrents that back their files, holds back groups that a torrent blocks, ties the rest, and only then decides which torrents each row removes. `internal/scanner` only decides which lists can be produced and calls these functions.

**Tech stack:** Go 1.27, standard library. No new dependency.

**Spec:** `docs/superpowers/specs/2026-11-18-removarr-design.md`. Sections 2 (principles), 5.2, 5.4, 6.2, 6.3 ("Whole torrents"), 6.6, 6.7, 6.8 rule 5, 6.9.

## Global Constraints

- Read-only: no new request of any kind to any app. No new dependency.
- `internal/scan` does no I/O. Every decision about what is a candidate stays there.
- Unsure means unwatched (spec section 2). Never guess from a partial answer (6.8, rule 5).
- A torrent goes as a whole or stays as a whole (spec section 2): no row may be listed while a torrent that backs one of its files would stay.
- The 50 MB limit is `scan.MinContentSize`; do not introduce a second constant for it.
- File IDs and item IDs mean nothing without their media manager. Sonarr and Radarr both number from 1. Download IDs are one namespace.
- A torrent that is not in the download client any more can neither block nor tie anything.
- JSON written before this change must still be readable: new fields are additions.
- `gofmt -l .` prints nothing; `go vet ./...` and `go test ./...` pass after every task.
- Never put URLs, keys, names or titles from a real library into code, tests or documents.

## Review Focus

Inputs the spec implies and that are most likely to go wrong:

1. A pack that backs files of a row and a file that stays (unwatched season, protected movie, specials of a series that is still airing): the row must wait, not be listed with a torrent that stays.
2. Sonarr and Radarr using the same file ID and the same item ID: a movie row and a series row must neither be tied nor block each other because of the numbers alone.
3. A history source that is no longer in the download client: it must not tie two rows or make one wait.
4. A range item whose second episode Sonarr keeps in another file, or has no file for: only the first episode is watched.
5. One media manager configured and failing: neither watched list may be produced, and the error must name the manager that failed.

## File structure

| File | Change |
|---|---|
| `internal/scan/series.go` | Task 1: ranges kept as ranges. Task 3: returns rows |
| `internal/scan/movies.go` | Task 3: returns rows |
| `internal/scan/links.go` | Task 2: manager name, file sizes, backed bytes. Task 3: what the group step needs |
| `internal/scan/library.go` (new) | Task 2: `MovieFiles`, `EpisodeFiles`, moved from the scanner |
| `internal/scan/whole.go` (new) | Task 3: `Row`, `WholeTorrents`. Task 4: data outside the library |
| `internal/scan/torrents.go` | Task 3: `ListResult` gains `Rows` and `Waiting` |
| `internal/domain/candidate.go`, `summary.go` | Task 3: `TiedWith`, `Waiting`. Task 4: `OutsideLibrary` |
| `internal/scanner/scanner.go` | Task 2: glue removed, file-list guard, list dependencies. Task 3: calls `WholeTorrents` |

---

### Task 1: Episode ranges

**Files:** modify `internal/scan/series.go`, `internal/scan/series_test.go`.

**Rule (spec 6.2):** a media server item that covers a range of episodes counts for the first episode of the range. It counts for a further episode of the range only when Sonarr holds that episode in the same file as the first.

**Interfaces:**

- Consumes: `domain.ServerEpisode{SeriesID, Season, Episode, EpisodeEnd, States}`; `domain.Episode{ID, Season, Number, File *MediaFile}`.
- Produces: no exported change. Inside `series.go`, `indexLibrary` stops expanding a range into one entry per episode. It keeps the item's states under its first episode and keeps the range itself (season, first, last, states) per media server series. `episodeStates` gets what it needs to ask "does Sonarr hold episode `first` of this season in the same file as this episode": the file ID of the episode being judged and a lookup from (season, number) to file ID for the managed series.

**Behaviour to pin with tests (table-driven, in `series_test.go`):**

1. A range item 1 to 2, played; Sonarr holds episodes 1 and 2 in one file: both watched (today's behaviour, must stay).
2. The same item; Sonarr holds episodes 1 and 2 in two different files: only episode 1 is watched, so the season does not qualify.
3. The same item; Sonarr has no file for episode 1 but has one for episode 2: episode 2 is not watched, and it counts as unmatched when no other item covers it.
4. A range item 1 to 3 over one file for 1 and 2 and another file for 3, plus a separate played item for episode 3: all three watched.
5. Two items for the same episode, one of them a range that does not apply: the states of the item that does apply are used.
6. A favourite on a range item still protects the series (spec 6.4), whether or not the range applies to further episodes.
7. A range with an absurd end (for example 1 to 2,000,000,000) costs nothing: no loop over the range, no allocation per episode.
8. An item with `EpisodeEnd` 0 or smaller than `Episode` is a single episode.

- [ ] Step 1: write the failing tests for 2, 3, 4 and 7. Run `go test ./internal/scan/ -run Series` and record the failures.
- [ ] Step 2: implement.
- [ ] Step 3: add the tests for 1, 5, 6 and 8; run the package.
- [ ] Step 4: `gofmt -l .`, `go vet ./...`, `go test ./...`.
- [ ] Step 5: commit `feat: count an episode range only where Sonarr holds it in one file`.

---

### Task 2: Link data with sizes, the moved glue and the list dependencies

**Files:** modify `internal/scan/links.go`, `internal/scan/content.go` (only if a signature needs it), `internal/scanner/scanner.go`, their tests; create `internal/scan/library.go`, `internal/scan/library_test.go`.

No row changes in this task except through the two guards below.

**Interfaces (exact):**

```go
// internal/scan/links.go
const (
	ManagerMovies = "movies" // the movie manager
	ManagerSeries = "series" // the series manager
)

// ManagerFiles is one media manager's links with its current library files.
type ManagerFiles struct {
	Manager string        // ManagerMovies or ManagerSeries
	Links   domain.Links
	Files   []LibraryFile // replaces FileIDs
}

// BackedBytes returns the total size of the current library files the download backs,
// over every media manager.
func (x *LinkIndex) BackedBytes(id string) int64

// internal/scan/library.go
func MovieFiles(s domain.MovieSnapshot) []LibraryFile   // moved from internal/scanner, unchanged behaviour
func EpisodeFiles(s domain.SeriesSnapshot) []LibraryFile // moved from internal/scanner, unchanged behaviour
```

`LinkIndex` keeps `Queued`, `Known`, `Backing` and, until Task 3 replaces it, `Removable`. The scanner's `linkData` stays in the scanner but builds `ManagerFiles` with these functions; `movieFiles`, `episodeFiles` and `fileIDs` leave the scanner.

**Guard 1, file lists (spec 6.8, rule 5):** `scanner.Compute` must not produce a list from history alone when file lists should have been read. When the download client answered, at least one media manager answered, and `Fetched.Files` is nil, the download client counts as not read, with the text "its file lists were not read". `fetch` already sets a non-nil map when it read them.

**Guard 2, list dependencies (spec 6.8, rule 5):** the watched-movies list also needs the series manager when it is configured, and the watched-series list also needs the movie manager when it is configured. The error shown is the failing manager's own ("The series manager could not be read: …"). A manager that is not configured blocks nothing. The list's own manager is reported first.

**Tests:**

1. `MovieFiles`, `EpisodeFiles`: the tests that covered them through the scanner move with them; add one where two episodes share a file and one where an episode has no file.
2. `BackedBytes`: sums over both managers by download ID; a file listed twice counts once; a file with no source counts nowhere.
3. The per-manager sum of `SourcedBytes` in `readFiles` (open review point): a movie file and an episode file with the same file ID, different sizes, different sources. Whether a download is asked for its files must follow from its own manager's file. Check that the test fails when the series links are matched against both managers' files.
4. Guard 1: `Compute` with downloads and a manager answered and `Files` nil gives all three lists the download-client error; with an empty non-nil map it produces the lists.
5. Guard 2: with both managers configured and the series manager failing, the movie list shows the series manager's error; with the series manager not configured, the movie list is produced. The same the other way round.
6. Existing scanner tests keep passing, changed only where the two guards change the expected outcome. Say which and why in the report.

- [ ] Step 1: write the failing tests for 2, 3, 4 and 5; record the failures.
- [ ] Step 2: implement; move the glue.
- [ ] Step 3: `gofmt -l .`, `go vet ./...`, `go test ./...`, `go test -race ./internal/scanner/ ./internal/scan/`.
- [ ] Step 4: commit `feat: require both media managers and the file lists for the watched lists`.

---

### Task 3: Whole torrents, waiting and tied rows

**Files:** create `internal/scan/whole.go`, `internal/scan/whole_test.go`; modify `internal/scan/movies.go`, `series.go`, `torrents.go`, `links.go`, `internal/domain/candidate.go`, `summary.go`, `internal/scanner/scanner.go`, and the tests of each.

**Rule (spec 6.3, "Whole torrents", and 6.7):** read both sections before starting. In short:

- Rows whose files are backed by the same torrent form a group, directly or through a chain of torrents.
- A torrent blocks its group when it is in a media manager's queue, or when it backs a library file that no row of the group would delete.
- A group with a blocking torrent waits: none of its rows is listed, and the summary counts them.
- In a group of several rows that does not wait, the rows are tied; each is listed and names the others.
- Only torrents that are in the download client count for any of this.

**Interfaces (exact):**

```go
// internal/domain/candidate.go
// RowRef names another row.
type RowRef struct {
	List   ListKind `json:"list"`
	Key    string   `json:"key"`
	Title  string   `json:"title"`
	Season int      `json:"season,omitempty"` // season rows
}
// Candidate gains:
	TiedWith []RowRef `json:"tiedWith,omitempty"` // rows that are deleted together with this one (spec 6.3)

// internal/domain/summary.go: ListSummary gains
	Waiting int `json:"waiting"` // rows held back because a torrent they share also holds something that stays

// internal/scan/whole.go
// Row is a movie, series or season that qualifies, with what Delete would touch.
type Row struct {
	Candidate domain.Candidate // everything except Torrents, BelowSeedMin and TiedWith
	Manager   string           // ManagerMovies or ManagerSeries: whose IDs the next two fields hold
	ItemIDs   []int            // the movies or episodes Delete affects
	FileIDs   []int            // the library files Delete removes
}

// WholeTorrents applies the whole-torrent rule to the rows of the watched lists. It fills
// Candidates and Waiting of each list from its Rows.
func WholeTorrents(r domain.Rules, x *LinkIndex, downloads []domain.Download, lists ...*ListResult)

// internal/scan/torrents.go: ListResult gains
	Rows    []Row // watched lists: the qualifying rows before the whole-torrent rule
	Waiting int
```

`scan.Movies` and `scan.Series` return `Rows` and leave `Candidates` empty; they no longer need the link index or the downloads, so `MoviesInput` and `SeriesInput` lose `Index` and `Downloads`. `LinkIndex.Removable` is replaced by what `WholeTorrents` needs and removed; `LinkIndex` learns, per manager, which download backs which file. `scan.Torrents` is unchanged.

**Which torrents a listed row names:** every torrent in the download client that is associated with one of the row's items or backs one of its files, is not in a queue, and backs no library file outside the files its group deletes. A tied row therefore names the torrent it shares, and so does the row it is tied to. Sorted by ID.

**The scanner:** `Compute` calls `Movies` and `Series` for the lists that can be produced, then `WholeTorrents` once with both, then builds the summary (`Waiting` included) and the candidates. The list errors are decided as before.

**Tests in `whole_test.go` (table-driven, hand-built rows and links):**

1. A row whose file no torrent backs: listed, no torrents.
2. A row with its own torrent: listed, names it.
3. Per-season mode, a pack backing seasons 1 and 2, season 2 not qualifying: the season 1 row waits; `Waiting` is 1; no candidate.
4. The same pack with both seasons qualifying as two season rows: both listed, tied to each other, both name the pack.
5. A collection pack with three movies of which one is not a row: the two rows wait.
6. A still-airing series row whose pack also backs a special: waits. The same series ended: listed, names the pack.
7. A queued torrent that backs a row's file: the row waits. A queued torrent that is only associated: the row is listed without it.
8. A chain: row A shares torrent 1 with row B, row B shares torrent 2 with a file that stays: both wait.
9. A movie row and a series row whose file IDs and item IDs are equal numbers but whose torrents differ: neither tied nor waiting.
10. A torrent that backs a movie file and an episode file, both rows qualifying: tied across the two lists, each names it. One of them not qualifying: the other waits.
11. A history source that is not in the download client, shared by two rows: not tied, not waiting, not named.
12. An old download that is associated with a row's item and backs nothing: named by the row. One that backs a file elsewhere: not named, and it does not make the row wait.
13. Candidates come out sorted by key; `TiedWith` sorted by list, then key.

**Tests in the scanner:** one `Compute` test where a pack makes a season row wait (summary: `Waiting` 1, no row), one with tied rows across the two lists, and the existing content-linking tests adapted to the new call. The four faults that a review once found untested must still fail a test: `EpisodeFiles` using the series ID, `EpisodeFiles` dropping items, both managers' files treated as one library, and a shared file's episodes not grouped. Apply each once, record the failing test, revert.

- [ ] Step 1: write `whole_test.go` cases 1 to 5 against the new interface; see them fail to compile or fail.
- [ ] Step 2: implement `Row`, `WholeTorrents`, the changes to `Movies`, `Series`, `LinkIndex`.
- [ ] Step 3: cases 6 to 13.
- [ ] Step 4: wire the scanner; adapt its tests; add the two `Compute` tests; run the four faults.
- [ ] Step 5: `gofmt -l .`, `go vet ./...`, `go test ./...`, `go test -race ./internal/scanner/ ./internal/scan/ ./internal/api/`.
- [ ] Step 6: commit `feat: hold back rows that would delete only part of a torrent, and tie rows that share one`.

---

### Task 3b: Whole files (added after the review of Task 3)

Task 3's implementer and reviewer found a gap that is older than this plan: a season row deletes every file of its season, also a file that holds an episode of another season that does not qualify, or a special of a series that is still airing. That removes an episode that has not been watched.

**Files:** modify `internal/scan/links.go`, `whole.go`, `series.go`, their tests, and `internal/scanner/scanner_test.go`.

**Rule (spec 6.3, last bullet of "Whole torrents"):** rows that would delete the same library file belong to one group, and a file blocks its group when it holds an item that no row of the group would delete. It applies whether or not a torrent backs the file.

**What it needs:** the index keeps, per (manager, file), the items the file holds, from `ManagerFiles.Files[i].Items`. `WholeTorrents` also joins rows on (manager, file). For each group the deleted items are the union of the rows' `ItemIDs`, keyed with the manager; the group waits when an item of one of its files is missing from that set.

**Tests:**

1. Per-season mode, one file holds an episode of season 1 and one of season 2, season 2 qualifies and season 1 does not: the season 2 row waits, with or without a torrent backing the file.
2. Both seasons qualify as two season rows that share the file and no torrent: both listed, tied to each other.
3. A still-airing series whose regular-season file also holds a special: the series row waits. The same series ended: listed.
4. Equal file and item numbers in the two managers neither tie nor block.
5. A series row that deletes both seasons of a file spanning them lists the file once: `FileIDs` holds it once and `Size` counts it once (spec 6.9).
6. `TestComputeAssociatesEveryEpisodeOfASharedFileByContent` changes: its season row now waits. What it pinned about associations stays pinned by the unit test of `EpisodeFiles`.

Commits: `feat: never delete a library file that holds an episode that stays`, `fix: count a file that spans two seasons once in a series row`.

---

### Task 4: Data outside the library

**Files:** modify `internal/domain/candidate.go`, `internal/scan/whole.go`, `whole_test.go`.

**Rule (spec 6.7, 5.2):** a torrent's data outside the library is its size minus the sizes of the library files it backs. The row states it for every torrent where it is 50 MB or more. It is information only.

**Interfaces (exact):**

```go
// internal/domain/candidate.go: TorrentInfo gains
	OutsideLibrary int64 `json:"outsideLibrary,omitempty"` // bytes of the torrent that are not in the library, when 50 MB or more
```

Set by `WholeTorrents` on the torrents of movie, series and season rows, from `LinkIndex.BackedBytes` and `scan.MinContentSize`. Never negative. Not set on the rows of the torrent list: an orphan or unlinked torrent is outside the library as a whole, and its size says so.

**Tests:**

1. A pack of 3,000 MB backing 2,000 MB of library files: 1,000 MB.
2. A difference of 50 MB minus one byte: 0. Exactly 50 MB: 50 MB.
3. A torrent smaller than the files it backs (the library file was replaced by a larger one): 0.
4. An old download that backs nothing: its whole size.
5. A torrent backing a movie file and an episode file: both sizes are subtracted.
6. A row of the torrent list: not set.

- [ ] Step 1: failing tests. Step 2: implement. Step 3: `gofmt -l .`, `go vet ./...`, `go test ./...`.
- [ ] Step 4: commit `feat: show how much of a torrent is not in the library`.

---

### Task 5: Documents and the check on the reference stack (controller and owner)

- `CHANGELOG.md`: an entry under "Unreleased" with scope, deployment and breaking changes.
- `LEARNINGS.md`: what this work found out.
- Spec section 13 and 16.6: the rules are built.
- `README.md`: the new fields of the API answers, if it lists them.
- The owner runs a scan with the new binary. Expected on the reference stack (spec 16.6): the same rows as before, no tied row, `waiting` 0 in both watched lists. The lists are compared with an independent recomputation from the apps' raw answers.
