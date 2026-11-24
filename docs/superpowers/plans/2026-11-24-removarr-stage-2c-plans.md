# Stage 2c: build delete plans from live data — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `POST /api/plans` builds, from live data, the exact plan a deletion of the selected rows would carry out, and stores it for ten minutes (issue #4). Nothing is deleted.

**Architecture:** The planner reads every configured app exactly as a scan does and evaluates the result with the scan's own code (`scanner.Read`, `scanner.Evaluate`), so a plan decides exactly as a scan would. A pure function, `planner.Make`, looks up each selected row in that fresh result, skips what no longer qualifies with the reason, forms one item per group of tied rows, and fills in the media manager steps (spec 6.7), the torrents with their details and the torrents that stay. A torrent is only removed when the latest scan named it too (two reads, spec 6.10). `planner.Service` adds the ID and expiry and saves the plan in the new `plans` table.

**Tech Stack:** Go 1.27, standard library, `modernc.org/sqlite`.

**Spec:** `docs/superpowers/specs/2026-11-18-removarr-design.md`. Read 6.3 (whole torrents), 6.6, 6.7, 6.8 and the new 6.10 in full before any task: 6.5, 6.7, 6.8 and 6.10 were amended for this plan.

## Global Constraints

- Go 1.27 and the standard library; the only third-party module is `modernc.org/sqlite`. No new module.
- No call that changes anything in an app. This stage only reads the apps; plans are built and stored, never executed (that is issue #5).
- `internal/scan` stays pure: no I/O.
- Every API route goes through `guard` in `internal/api/server.go`.
- The store has one database connection. Inside a transaction use the transaction, never `s.db`.
- No guessing from a partial answer (spec 6.8, rule 5): a list that cannot be produced skips its rows.
- Never print titles, names, URLs or keys from a real library. Tests use invented data.
- Test first; see it fail for the expected reason. Name tests after what must hold.
- The checks: `gofmt -l .` prints nothing, `go vet ./...`, `go test ./...`, `go test -race ./internal/api/ ./internal/config/ ./internal/store/ ./internal/scanner/ ./internal/scan/ ./internal/planner/`, `go build ./...`.
- Commit subjects: `type: what changed`, lower case, `feat|fix|docs|test|chore`, body says why, ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

- A torrent that is unlinked (grabbed for a movie, backing nothing) is named by the selected movie row and also selected as a torrent row: it is removed once, and the torrent row is skipped naming the movie (Task 5).
- A torrent whose content path lies inside another torrent's that stays, or equals it, or is unknown, keeps its data; a path that only starts with the same letters (`/dl/one` and `/dl/one-two`) does not (Task 5).
- A selected row tied to an ignored row is skipped and names that row; selecting both rows of a tied group gives one item, not two (Task 4).
- Radarr does not answer while Sonarr is configured: a selected series row is skipped with Radarr's error, because the series list needs both managers (Task 4).
- A plan read at exactly ten minutes after it was built is expired (Task 6).

---

### Task 1: say what a held row waits for, and why the ignores hold a row

**Files:**
- Modify: `internal/domain/candidate.go` (struct `Candidate`), `internal/domain/json_test.go`
- Modify: `internal/scan/whole.go` (func `WholeTorrents`)
- Modify: `internal/scan/ignores.go` (add `Holds`)
- Test: `internal/scan/whole_test.go`, `internal/scan/ignores_test.go`

**Interfaces:**
- Produces:
  - `domain.Candidate` gains `WaitReason string` with tag `json:"waitReason,omitempty"`: set on the rows `WholeTorrents` holds back.
  - `scan.WaitQueued`, `scan.WaitTorrentStays`, `scan.WaitFileStays` (string constants, the texts below).
  - `scan.HoldIgnored`, `scan.HoldWaiting`, `scan.HoldMerged` (string constants).
  - `type scan.Holds`, `func scan.NewHolds(rows []domain.Candidate, ignores scan.IgnoreSet) scan.Holds`, `func (scan.Holds) Reason(c domain.Candidate) string`: why the current ignores or the whole-torrent rule keep `c` from being deleted, `""` when nothing does.

- [ ] **Step 1: Write the failing tests**

Append to `internal/domain/json_test.go`, inside `TestJSONNamesOfTheTiedAndOutsideAndWaitingFields` before its closing brace:

```go
	has(keys(Candidate{Waiting: true, WaitReason: "It waits."}), "waitReason", true)
	has(keys(Candidate{}), "waitReason", false)
```

Append to `internal/scan/whole_test.go`:

```go
func TestWholeTorrentsSaysWhatAHeldRowWaitsFor(t *testing.T) {
	tests := []struct {
		name      string
		series    ManagerFiles
		downloads []string
		rows      []Row
		want      string
	}{
		{
			name:      "its torrent is in a queue",
			series:    ManagerFiles{Links: link(nil, map[int]string{11: "q"}, "q"), Files: libFiles(11)},
			downloads: []string{"q"},
			rows:      []Row{seasonRow(1, []int{11}, []int{11})},
			want:      WaitQueued,
		},
		{
			name:      "its pack backs a file that stays",
			series:    ManagerFiles{Links: link(nil, map[int]string{11: "pack", 21: "pack"}), Files: libFiles(11, 21)},
			downloads: []string{"pack"},
			rows:      []Row{seasonRow(1, []int{11}, []int{11})},
			want:      WaitTorrentStays,
		},
		{
			name:   "its file holds an episode that stays",
			series: ManagerFiles{Links: links(), Files: []LibraryFile{held(30, 12, 21)}},
			rows:   []Row{seasonRow(1, []int{12}, []int{30})},
			want:   WaitFileStays,
		},
		{
			name:      "a queue is named before a pack that stays",
			series:    ManagerFiles{Links: link(nil, map[int]string{11: "q", 12: "pack", 21: "pack"}, "q"), Files: libFiles(11, 12, 21)},
			downloads: []string{"q", "pack"},
			rows:      []Row{seasonRow(1, []int{11, 12}, []int{11, 12})},
			want:      WaitQueued,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.series.Manager = ManagerSeries
			x := NewLinkIndex(tt.series)
			var downloads []domain.Download
			for _, id := range tt.downloads {
				downloads = append(downloads, dl(id))
			}
			res := &ListResult{Rows: tt.rows}
			WholeTorrents(domain.DefaultRules(), x, downloads, res)
			if len(res.Held) != 1 {
				t.Fatalf("held %d rows, want 1", len(res.Held))
			}
			if got := res.Held[0].WaitReason; got != tt.want {
				t.Errorf("WaitReason = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWholeTorrentsGivesAListedRowNoWaitReason(t *testing.T) {
	x := NewLinkIndex(ManagerFiles{Manager: ManagerMovies, Links: link(nil, map[int]string{10: "h1"}), Files: libFiles(10)})
	res := &ListResult{Rows: []Row{movieRow("tmdb:1", 1, 10)}}
	WholeTorrents(domain.DefaultRules(), x, []domain.Download{dl("h1")}, res)
	if len(res.Candidates) != 1 || res.Candidates[0].WaitReason != "" {
		t.Errorf("candidates = %+v, want one listed row without a wait reason", res.Candidates)
	}
}
```

Append to `internal/scan/ignores_test.go`:

```go
func TestHoldsReason(t *testing.T) {
	none := []domain.TorrentInfo{}
	refA := domain.RowRef{List: domain.ListMovies, Key: "tmdb:1", Title: "Movie A"}
	refB := domain.RowRef{List: domain.ListMovies, Key: "tmdb:2", Title: "Movie B"}
	refM := domain.RowRef{List: domain.ListSeries, Key: "tvdb:100", Title: "The Show"}
	rows := []domain.Candidate{
		{List: domain.ListMovies, Key: "tmdb:1", Title: "Movie A", Torrents: none, TiedWith: []domain.RowRef{refB}},
		{List: domain.ListMovies, Key: "tmdb:2", Title: "Movie B", Torrents: none, TiedWith: []domain.RowRef{refA}},
		{List: domain.ListMovies, Key: "tmdb:3", Title: "Movie C", Torrents: none, Waiting: true, WaitReason: WaitQueued},
		{List: domain.ListMovies, Key: "tmdb:4", Title: "Movie D", Torrents: none, Waiting: true},
		{List: domain.ListSeries, Key: "tvdb:100", Title: "The Show", Scope: domain.ScopeSeries, Merged: true, Torrents: none,
			TiedWith: []domain.RowRef{{List: domain.ListMovies, Key: "tmdb:5", Title: "Movie E"}}},
		{List: domain.ListMovies, Key: "tmdb:5", Title: "Movie E", Torrents: none, TiedWith: []domain.RowRef{refM}},
		{List: domain.ListMovies, Key: "tmdb:6", Title: "Movie F", Torrents: none},
	}
	row := func(key string) domain.Candidate {
		for _, c := range rows {
			if c.Key == key {
				return c
			}
		}
		t.Fatalf("no row %s", key)
		return domain.Candidate{}
	}
	tests := []struct {
		name    string
		ignores []domain.Ignore
		key     string
		want    string
	}{
		{"a row nothing holds", nil, "tmdb:6", ""},
		{"an ignored row", []domain.Ignore{ignore(domain.IgnoreMovie, "tmdb:6")}, "tmdb:6", HoldIgnored},
		{"a held row with its reason", nil, "tmdb:3", WaitQueued},
		{"a held row from an older scan", nil, "tmdb:4", HoldWaiting},
		{"a row tied to an ignored row", []domain.Ignore{ignore(domain.IgnoreMovie, "tmdb:2")}, "tmdb:1", `It is tied to "Movie B", which is ignored.`},
		{"a series row merged over a now ignored season", []domain.Ignore{ignore(domain.IgnoreSeason, "tvdb:100:s1")}, "tvdb:100", HoldMerged},
		{"a row tied to such a series row", []domain.Ignore{ignore(domain.IgnoreSeason, "tvdb:100:s1")}, "tmdb:5", `It is tied to "The Show", which waits for the next scan.`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHolds(rows, NewIgnoreSet(tt.ignores))
			if got := h.Reason(row(tt.key)); got != tt.want {
				t.Errorf("Reason(%s) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/domain/ ./internal/scan/`
Expected: build failure: `unknown field WaitReason`, `undefined: WaitQueued`, `undefined: NewHolds`.

- [ ] **Step 3: Add the field**

In `internal/domain/candidate.go`, in struct `Candidate`, after the `Waiting` field:

```go
	WaitReason   string        `json:"waitReason,omitempty"` // what a held row waits for (spec 6.10)
```

(Run `gofmt -w internal/domain/candidate.go` afterwards; the field alignment of the struct may change.)

- [ ] **Step 4: Record the reason in `WholeTorrents`**

In `internal/scan/whole.go`, add above `WholeTorrents`:

```go
// What a held row waits for (spec 6.3, 6.10). A group can wait for several reasons; the
// first of these that applies is given.
const (
	WaitQueued       = "It waits: a torrent that backs its files is in a media manager's queue."
	WaitTorrentStays = "It waits: a torrent that backs its files also backs a library file that stays."
	WaitFileStays    = "It waits: one of its library files also holds an item that stays."
)
```

Replace the block that starts with `waits := false` and ends with the closing brace of `if waits { … continue }` by:

```go
		queued, torrentStays, fileStays := false, false, false
		for _, i := range group {
			manager := rows[i].row.Manager
			for _, file := range rows[i].row.FileIDs {
				if id := x.source[ref{manager, file}]; present(id) && stays(id) {
					if x.queued[id] {
						queued = true
					} else {
						torrentStays = true
					}
				}
				// A file goes as a whole: it may not take an item with it that stays.
				for _, item := range x.holds[ref{manager, file}] {
					if !deletedItems[ref{manager, item}] {
						fileStays = true
					}
				}
			}
		}
		reason := ""
		switch {
		case queued:
			reason = WaitQueued
		case torrentStays:
			reason = WaitTorrentStays
		case fileStays:
			reason = WaitFileStays
		}
		if reason != "" {
			for _, i := range group {
				c := rows[i].row.Candidate
				c.Torrents, c.BelowSeedMin, c.TiedWith, c.Waiting, c.WaitReason = []domain.TorrentInfo{}, false, nil, true, reason
				rows[i].list.Held = append(rows[i].list.Held, c)
				rows[i].list.Waiting++
			}
			continue
		}
```

- [ ] **Step 5: Add `Holds`**

In `internal/scan/ignores.go`, add `"fmt"` to the imports and append:

```go
// Why the current ignores, or the whole-torrent rule, keep a row from being deleted (spec
// 6.10). Rows tied to a row that is out give the texts of Holds.Reason.
const (
	HoldIgnored = "It is ignored. Un-ignore it before deleting it."
	HoldWaiting = "It waits: it shares a torrent or a library file with something that stays."
	HoldMerged  = "It was merged from its seasons while one of them was ignored. Scan again."
)

// Holds answers, for the rows of one scan and the current ignores, why a row cannot be
// deleted. It applies the same effects as View.
type Holds struct{ e effects }

// NewHolds prepares the answers for rows, which are every row of one scan, listed and held.
func NewHolds(rows []domain.Candidate, ignores IgnoreSet) Holds {
	return Holds{newEffects(rows, ignores)}
}

// Reason returns why c cannot be deleted now, or "" when nothing holds it: it is hidden by
// an ignore, held back by the whole-torrent rule or by a season ignore made during the scan,
// or tied to a row that is.
func (h Holds) Reason(c domain.Candidate) string {
	switch {
	case h.e.ignores.Hides(c.List, c.Key):
		return HoldIgnored
	case c.Waiting && c.WaitReason != "":
		return c.WaitReason
	case c.Waiting:
		return HoldWaiting // a scan from before stage 2c stored no reason
	case h.e.heldBack(c):
		return HoldMerged
	}
	for _, ref := range c.TiedWith {
		r, ok := h.e.rows[rowID{ref.List, ref.Key}]
		if !ok {
			r = domain.Candidate{List: ref.List, Key: ref.Key}
		}
		switch {
		case h.e.ignores.Hides(r.List, r.Key):
			return fmt.Sprintf("It is tied to %q, which is ignored.", ref.Title)
		case h.e.heldBack(r):
			return fmt.Sprintf("It is tied to %q, which waits for the next scan.", ref.Title)
		}
	}
	return ""
}
```

- [ ] **Step 6: Run the tests to see them pass**

Run: `gofmt -l . && go test ./internal/domain/ ./internal/scan/ ./internal/scanner/ ./internal/api/`
Expected: no file listed, all PASS. (The scanner and API tests compare stored rows; they must still pass because a held row only gains a field.)

If a scanner or API test compares held rows with `reflect.DeepEqual` and now fails only because of `WaitReason`, add the expected reason to that test's expected row rather than dropping the comparison.

- [ ] **Step 7: Commit**

```bash
git add internal/domain internal/scan
git commit -m "feat: say what a held row waits for and why ignores hold a row" -m "A plan skips a waiting row with what it waits for (spec 6.10). The whole-torrent rule now records the reason on each held row, and Holds gives the reason the current ignores keep a row back, using the same effects as View." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: rows name their item in the manager; associated torrents; finished seasons

**Files:**
- Modify: `internal/scan/whole.go` (struct `Row`), `internal/scan/movies.go`, `internal/scan/series.go` (func `row`)
- Modify: `internal/scan/links.go` (add `Associated`), `internal/scan/torrents.go` (add `TorrentDetails`)
- Create: `internal/scan/airing.go`, `internal/scan/airing_test.go`
- Test: `internal/scan/movies_test.go`, `internal/scan/series_test.go`, `internal/scan/links_test.go`, `internal/scan/torrents_test.go`

**Interfaces:**
- Produces:
  - `scan.Row` gains `ManagerID int`: the movie's ID in the movie manager, or the series' ID in the series manager. Remove-movie and remove-series need it (spec 6.7).
  - `func (x *scan.LinkIndex) Associated(manager string, itemIDs, fileIDs []int) []string`: the downloads the given files were imported from and the downloads grabbed for the given items, sorted, without duplicates and without `""`.
  - `func scan.SeasonFinished(s domain.Series, season int, now time.Time) bool` (spec 6.3, "Finished airing").
  - `func scan.TorrentDetails(r domain.Rules, d domain.Download) domain.TorrentInfo`: what a row shows of a download.

- [ ] **Step 1: Write the failing tests**

Append to `internal/scan/movies_test.go`:

```go
func TestMoviesRowNamesTheMovieInItsManager(t *testing.T) {
	res := Movies(movieInput(func(in *MoviesInput) { in.Snapshot.Movies[0].ID = 42 }))
	if len(res.Rows) != 1 || res.Rows[0].ManagerID != 42 {
		t.Fatalf("rows = %+v, want one row with ManagerID 42", res.Rows)
	}
}
```

Append to `internal/scan/series_test.go`:

```go
func TestSeriesRowsNameTheSeriesInItsManager(t *testing.T) {
	watched := map[string]domain.UserState{"a": played("2026-03-01T20:00:00Z")}
	s := show(true, ep(11, 1, 1, 11), ep(21, 2, 1, 0)) // season 2 has no file
	s.ID = 7
	eps := []domain.ServerEpisode{sep(1, 1, watched)}
	for _, unit := range []string{domain.UnitSeries, domain.UnitSeason} {
		res := Series(seriesInput(s, eps, func(in *SeriesInput) { in.Rules.SeriesUnit = unit }))
		if len(res.Rows) != 1 || res.Rows[0].ManagerID != 7 {
			t.Errorf("%s: rows = %+v, want one row with ManagerID 7", unit, res.Rows)
		}
	}
}
```

Append to `internal/scan/links_test.go`:

```go
func TestAssociatedNamesSourcesAndGrabsOnce(t *testing.T) {
	l := links()
	l.FileSource[10] = "src"
	l.Grabbed["src"] = []int{1}
	l.Grabbed["grab"] = []int{1, 2}
	l.Grabbed[""] = []int{1}
	l.Grabbed["other"] = []int{3}
	x := NewLinkIndex(ManagerFiles{Manager: ManagerMovies, Links: l, Files: []LibraryFile{{ID: 10, Size: 1, Items: []int{1}}}})
	if got := x.Associated(ManagerMovies, []int{1}, []int{10}); !reflect.DeepEqual(got, []string{"grab", "src"}) {
		t.Errorf("Associated = %v, want [grab src]", got)
	}
	if got := x.Associated(ManagerSeries, []int{1}, []int{10}); len(got) != 0 {
		t.Errorf("Associated in the other manager = %v, want none", got)
	}
}
```

(Add `"reflect"` to the imports of `links_test.go` if it is not there.)

Append to `internal/scan/torrents_test.go`:

```go
func TestTorrentDetailsIsWhatATorrentRowShows(t *testing.T) {
	r := domain.DefaultRules()
	r.MinRatio = 2
	ratio := 1.0
	d := dl("abc", func(d *domain.Download) { d.Name = "Some.Torrent"; d.Ratio = &ratio })
	res := Torrents(TorrentsInput{Rules: r, Downloads: []domain.Download{d}, Index: NewLinkIndex()})
	if len(res.Candidates) != 1 || !reflect.DeepEqual(TorrentDetails(r, d), res.Candidates[0].Torrents[0]) {
		t.Errorf("TorrentDetails = %+v, want what the torrent row shows: %+v", TorrentDetails(r, d), res.Candidates)
	}
	if !TorrentDetails(r, d).BelowSeedMin {
		t.Error("a ratio of 1 against a minimum of 2 is not flagged")
	}
}
```

(Add `"reflect"` to the imports of `torrents_test.go` if it is not there.)

Create `internal/scan/airing_test.go`:

```go
package scan

import (
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

func TestSeasonFinished(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	aired := func(id, season int, at *time.Time) domain.Episode {
		return domain.Episode{ID: id, Season: season, Number: id, AirDate: at}
	}
	tests := []struct {
		name   string
		series domain.Series
		season int
		want   bool
	}{
		{"an ended series has finished every season", domain.Series{Ended: true, Episodes: []domain.Episode{aired(1, 1, &future)}}, 1, true},
		{"aired, with a later season", domain.Series{Episodes: []domain.Episode{aired(1, 1, &past), aired(2, 2, &future)}}, 1, true},
		{"the newest season of a running series", domain.Series{Episodes: []domain.Episode{aired(1, 1, &past)}}, 1, false},
		{"an episode that has not aired yet", domain.Series{Episodes: []domain.Episode{aired(1, 1, &past), aired(2, 1, &future), aired(3, 2, nil)}}, 1, false},
		{"an episode without an air date", domain.Series{Episodes: []domain.Episode{aired(1, 1, nil), aired(3, 2, &past)}}, 1, false},
		{"an episode airing right now has not aired", domain.Series{Episodes: []domain.Episode{aired(1, 1, &now), aired(3, 2, &past)}}, 1, false},
		{"a season with no episodes", domain.Series{Episodes: []domain.Episode{aired(3, 2, &past)}}, 1, false},
	}
	for _, tt := range tests {
		if got := SeasonFinished(tt.series, tt.season, now); got != tt.want {
			t.Errorf("%s: SeasonFinished = %v, want %v", tt.name, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/scan/`
Expected: build failure: `unknown field ManagerID`, `x.Associated undefined`, `undefined: TorrentDetails`, `undefined: SeasonFinished`.

- [ ] **Step 3: Implement**

In `internal/scan/whole.go`, struct `Row`, after `Manager`:

```go
	ManagerID int              // the movie or the series in that manager: what remove-movie and remove-series need (spec 6.7)
```

In `internal/scan/movies.go`, in the `Row{…}` literal appended to `res.Rows`, add `ManagerID: m.ID,` after the `Candidate` field.

In `internal/scan/series.go`, func `row`, in the returned `Row{…}`, add `ManagerID: s.ID,` after `Manager: ManagerSeries,`.

In `internal/scan/links.go`, add `"sort"` to the imports and append:

```go
// Associated returns the downloads associated with a row's items and files (spec 6.6): the
// downloads its files were imported from and those grabbed for its items. The result is
// sorted and holds each download once.
func (x *LinkIndex) Associated(manager string, itemIDs, fileIDs []int) []string {
	seen := map[string]bool{}
	for _, file := range fileIDs {
		if id := x.source[ref{manager, file}]; id != "" {
			seen[id] = true
		}
	}
	for _, item := range itemIDs {
		for _, id := range x.grabbed[ref{manager, item}] {
			if id != "" {
				seen[id] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
```

In `internal/scan/torrents.go`, append:

```go
// TorrentDetails returns what a row shows of a download, with the seeding flag of the rules.
func TorrentDetails(r domain.Rules, d domain.Download) domain.TorrentInfo { return torrentInfo(r, d) }
```

Create `internal/scan/airing.go`:

```go
package scan

import (
	"time"

	"github.com/removarr/removarr/internal/domain"
)

// SeasonFinished reports whether a season has finished airing (spec 6.3): the series has
// ended, or a later season exists and every episode of the season aired before now. An
// episode without an air date has not aired, so the newest season of a series that has not
// ended never counts as finished.
func SeasonFinished(s domain.Series, season int, now time.Time) bool {
	if s.Ended {
		return true
	}
	later, episodes := false, 0
	for _, e := range s.Episodes {
		if e.Season > season {
			later = true
		}
		if e.Season != season {
			continue
		}
		episodes++
		if e.AirDate == nil || !e.AirDate.Before(now) {
			return false
		}
	}
	return later && episodes > 0
}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `gofmt -l . && go test ./internal/scan/ ./internal/scanner/`
Expected: no file listed, all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/scan
git commit -m "feat: give the planner the manager IDs, associated torrents and finished seasons" -m "A plan needs the Radarr movie ID or Sonarr series ID of a row, every torrent associated with it to say which ones stay, and whether a season has finished airing to unmonitor it (spec 6.3, 6.7)." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: scanner exposes its read and its evaluation

**Files:**
- Modify: `internal/scanner/scanner.go` (func `fetch` becomes `Read`; func `Compute` delegates to new `Evaluate`)
- Test: `internal/scanner/evaluate_test.go` (create)

**Interfaces:**
- Consumes: `scan.Row.ManagerID`, `(*scan.LinkIndex).Associated` (Task 2).
- Produces:
  - `func scanner.Read(ctx context.Context, p scanner.Providers, rules domain.Rules, phase func(string)) scanner.Fetched`: everything a scan reads; `phase` may be nil.
  - `type scanner.Evaluation struct { Summary domain.ScanSummary; Candidates []domain.Candidate; Rows []scan.Row; Fetched scanner.Fetched; Index *scan.LinkIndex; History *scan.LinkIndex }`
  - `func scanner.Evaluate(rules domain.Rules, now time.Time, f scanner.Fetched) scanner.Evaluation`.
  - `scanner.Compute` keeps its signature and result.

- [ ] **Step 1: Write the failing tests**

Create `internal/scanner/evaluate_test.go`:

```go
package scanner

import (
	"reflect"
	"testing"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
)

// fetched reads the small library of world() the way a scan does.
func fetched(t *testing.T) Fetched {
	t.Helper()
	p, _ := world()
	return Read(ctx, p, withUser(), nil)
}

func TestEvaluateGivesWhatComputeGives(t *testing.T) {
	f := fetched(t)
	summary, candidates := Compute(withUser(), now, f)
	e := Evaluate(withUser(), now, f)
	if !reflect.DeepEqual(e.Summary, summary) || !reflect.DeepEqual(e.Candidates, candidates) {
		t.Errorf("Evaluate = %+v / %+v, want Compute's %+v / %+v", e.Summary, e.Candidates, summary, candidates)
	}
}

func TestEvaluateKeepsTheRowsWithWhatDeleteTouches(t *testing.T) {
	e := Evaluate(withUser(), now, fetched(t))
	byKey := map[string]scan.Row{}
	for _, r := range e.Rows {
		byKey[r.Candidate.Key] = r
	}
	movie, series := byKey["tmdb:603"], byKey["tvdb:100"]
	if movie.Manager != scan.ManagerMovies || movie.ManagerID != 1 || !reflect.DeepEqual(movie.FileIDs, []int{10}) {
		t.Errorf("movie row = %+v", movie)
	}
	if series.Manager != scan.ManagerSeries || series.ManagerID != 1 || !reflect.DeepEqual(series.ItemIDs, []int{11}) {
		t.Errorf("series row = %+v", series)
	}
	if len(e.Fetched.Downloads.Value) != 2 {
		t.Errorf("Fetched holds %d downloads, want the 2 that were read", len(e.Fetched.Downloads.Value))
	}
}

func TestEvaluateKeepsNoRowsOfAListItCannotProduce(t *testing.T) {
	f := fetched(t)
	f.Series.Err = errTest
	e := Evaluate(withUser(), now, f)
	if len(e.Rows) != 0 {
		t.Errorf("rows = %+v, want none: neither watched list can be produced", e.Rows)
	}
}

func TestEvaluateKeepsHistoryApartFromContentLinks(t *testing.T) {
	f := fetched(t)
	// The series file was imported by hand from "orphan1": only its size says so.
	f.Series.Value.Series[0].Episodes[0].File.Size = scan.MinContentSize
	f.Files = map[string][]domain.DownloadFile{"orphan1": {{Name: "ep.mkv", Size: scan.MinContentSize}}}
	e := Evaluate(withUser(), now, f)
	if got := e.Index.Associated(scan.ManagerSeries, []int{11}, []int{11}); !reflect.DeepEqual(got, []string{"orphan1"}) {
		t.Errorf("Index.Associated = %v, want [orphan1] by content", got)
	}
	if got := e.History.Associated(scan.ManagerSeries, []int{11}, []int{11}); len(got) != 0 {
		t.Errorf("History.Associated = %v, want none: history does not know the file", got)
	}
	if got := e.History.Associated(scan.ManagerMovies, []int{1}, []int{10}); !reflect.DeepEqual(got, []string{"h1"}) {
		t.Errorf("History.Associated for the movie = %v, want [h1]", got)
	}
}

func TestReadTellsItsPhases(t *testing.T) {
	p, _ := world()
	var phases []string
	Read(ctx, p, withUser(), func(s string) { phases = append(phases, s) })
	want := []string{"reading the download client", "reading the movie manager", "reading the series manager",
		"matching downloads to library files", "reading the media server"}
	if !reflect.DeepEqual(phases, want) {
		t.Errorf("phases = %v, want %v", phases, want)
	}
}
```

If `errTest` does not exist in the scanner tests, add to `evaluate_test.go`: `var errTest = errors.New("test failure")` and import `"errors"`. Check with `grep -n errTest internal/scanner/*_test.go` first.

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/scanner/`
Expected: build failure: `undefined: Read`, `undefined: Evaluate`.

- [ ] **Step 3: Implement**

In `internal/scanner/scanner.go`:

1. Replace the method `func (s *Service) fetch(ctx context.Context, p Providers, rules domain.Rules) Fetched` by a function with the same body and doc comment (first line changed to "Read reads the apps one after another, as a scan does. phase, when not nil, is told what is being read."), with this signature:

```go
func Read(ctx context.Context, p Providers, rules domain.Rules, phase func(string)) Fetched {
	setPhase := func(string) {}
	if phase != nil {
		setPhase = phase
	}
```

Inside the body replace every `s.setPhase(` by `setPhase(`, and delete the `if f.Downloads.Err == nil { s.Log.Info(...) }` block after `readFiles`.

2. In `func (s *Service) run`, replace `fetched := s.fetch(ctx, p, rules)` by:

```go
	fetched := Read(ctx, p, rules, s.setPhase)
	if fetched.Files != nil {
		s.Log.Info("read file lists for linking by content", "downloads", len(fetched.Files))
	}
```

3. Add above `Compute`:

```go
// Evaluation is what the rules made of one read: the scan's result, and what a plan needs
// besides it (spec 6.10).
type Evaluation struct {
	Summary    domain.ScanSummary
	Candidates []domain.Candidate // every row of the produced lists, listed and held back, as a scan stores them
	Rows       []scan.Row         // the qualifying rows of the produced watched lists, with what Delete touches
	Fetched    Fetched            // what was read, with each media manager's links extended by content
	Index      *scan.LinkIndex    // links by history and by content
	History    *scan.LinkIndex    // links by history alone
}
```

4. Rename `func Compute(rules domain.Rules, now time.Time, f Fetched) (domain.ScanSummary, []domain.Candidate)` to `func Evaluate(rules domain.Rules, now time.Time, f Fetched) Evaluation`, keep its doc comment (start it with "Evaluate applies…"), and change its body:
   - right before the line `index := scan.NewLinkIndex(linkData(&f, f.Files)...)` add `history := scan.NewLinkIndex(linkData(&f, nil)...)`;
   - replace the final `return summary, candidates` by:

```go
	e := Evaluation{Summary: summary, Candidates: candidates, Rows: []scan.Row{}, Fetched: f, Index: index, History: history}
	for _, l := range watched {
		e.Rows = append(e.Rows, l.Rows...)
	}
	return e
```

5. Add a new `Compute` that keeps the old contract:

```go
// Compute applies the rules to what was fetched and returns what a scan stores. A list is
// produced only when everything it depends on was read completely (spec 6.8, rule 5).
func Compute(rules domain.Rules, now time.Time, f Fetched) (domain.ScanSummary, []domain.Candidate) {
	e := Evaluate(rules, now, f)
	return e.Summary, e.Candidates
}
```

`linkData(&f, nil)` must run before `linkData(&f, f.Files)`: the second call replaces `f`'s links by the extended ones, and `ContentLinks` returns new maps, so the history index keeps the links from history alone.

- [ ] **Step 4: Run the tests to see them pass**

Run: `gofmt -l . && go vet ./internal/scanner/ && go test ./internal/scanner/ && go test -race ./internal/scanner/`
Expected: no file listed, all PASS. The existing scanner tests pass unchanged.

- [ ] **Step 5: Break it once**

In a scratch edit, swap the two `linkData` lines so the history index is built after the extension. Run `go test ./internal/scanner/ -run TestEvaluateKeepsHistoryApartFromContentLinks`: it must FAIL. Undo the edit.

- [ ] **Step 6: Commit**

```bash
git add internal/scanner
git commit -m "feat: expose the scan's read and evaluation for planning" -m "A plan must decide exactly as a scan does (spec 6.10), so the planner reads and evaluates through the same code. Evaluate also keeps the rows with what Delete touches and an index of history alone, to tell links by file size." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: plan types, and making a plan from the selected rows

**Files:**
- Create: `internal/domain/plan.go`, `internal/domain/plan_test.go`
- Create: `internal/planner/make.go`, `internal/planner/items.go`, `internal/planner/torrents.go`
- Create: `internal/planner/helpers_test.go`, `internal/planner/make_test.go`, `internal/planner/items_test.go`

**Interfaces:**
- Consumes: `scanner.Evaluation`, `scanner.Evaluate`, `scanner.Fetched`, `scanner.Source` (Task 3); `scan.Holds`, `scan.NewHolds`, `scan.HoldIgnored` (Task 1); `scan.Row.ManagerID`, `scan.SeasonFinished` (Task 2).
- Produces:
  - `domain.Plan`, `domain.PlanItem`, `domain.PlanRow`, `domain.ManagerStep`, `domain.PlanTorrent`, `domain.KeptTorrent`, `domain.SkippedRow`, `domain.PlanLifetime`, `func (domain.Plan) Expired(now time.Time) bool`, the step constants `domain.StepRemoveMovie`, `domain.StepRemoveSeries`, `domain.StepDeleteEpisodeFiles`, `domain.StepUnmonitorEpisodes`, `domain.StepUnmonitorSeasons`, and `domain.KeptQueued`, `domain.KeptBacksLibrary`.
  - `type planner.Input struct { Rules domain.Rules; Now time.Time; Eval scanner.Evaluation; Ignores scan.IgnoreSet; Latest []domain.Candidate; Selected []domain.RowRef }`
  - `func planner.Make(in planner.Input) ([]domain.PlanItem, []domain.SkippedRow)`: both never nil.
  - In package `planner`, unexported, for Task 5: `type maker`, `func (m *maker) torrents(members []domain.Candidate) []domain.PlanTorrent`, `func (m *maker) kept(members []domain.Candidate) []domain.KeptTorrent`, `func (m *maker) finish()`, field `m.owner map[string]int`, `m.downloads map[string]domain.Download`, `m.rows map[rowID]scan.Row`.

- [ ] **Step 1: Write the domain types with their test**

Create `internal/domain/plan_test.go`:

```go
package domain

import (
	"testing"
	"time"
)

func TestPlanExpiresTenMinutesAfterItWasBuilt(t *testing.T) {
	built := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	p := Plan{CreatedAt: built, ExpiresAt: built.Add(PlanLifetime)}
	if PlanLifetime != 10*time.Minute {
		t.Errorf("PlanLifetime = %v, want 10 minutes", PlanLifetime)
	}
	if p.Expired(built.Add(PlanLifetime - time.Nanosecond)) {
		t.Error("expired before ten minutes")
	}
	if !p.Expired(built.Add(PlanLifetime)) {
		t.Error("not expired at exactly ten minutes")
	}
}
```

Run: `go test ./internal/domain/` — Expected: build failure, `undefined: Plan`.

Create `internal/domain/plan.go`:

```go
package domain

import "time"

// PlanLifetime is how long a plan can be executed after it was built (spec 6.8, rule 2).
const PlanLifetime = 10 * time.Minute

// The media manager steps of a plan (spec 6.7).
const (
	StepRemoveMovie        = "remove-movie"         // remove the movie with its files
	StepRemoveSeries       = "remove-series"        // remove the series with its files, specials included
	StepDeleteEpisodeFiles = "delete-episode-files" // delete these episode files; the series stays
	StepUnmonitorEpisodes  = "unmonitor-episodes"   // unmonitor these episodes
	StepUnmonitorSeasons   = "unmonitor-seasons"    // unmonitor these seasons
)

// Why a torrent associated with a plan item stays (spec 6.10).
const (
	KeptQueued       = "queued"        // it is in a media manager's queue
	KeptBacksLibrary = "backs-library" // it backs a library file that stays
)

// Plan is the exact set of operations a deletion will perform, built from live data and
// shown for confirmation (spec 6.7, 6.8, 6.10).
type Plan struct {
	ID        string       `json:"id"`
	CreatedAt time.Time    `json:"createdAt"`
	ExpiresAt time.Time    `json:"expiresAt"`
	Items     []PlanItem   `json:"items"`   // in the order they are executed
	Skipped   []SkippedRow `json:"skipped"` // selected rows that are not deleted, with the reason
}

// Expired reports whether the plan can no longer be executed at now.
func (p Plan) Expired(now time.Time) bool { return !now.Before(p.ExpiresAt) }

// PlanItem is what is deleted together: one row, or rows that are tied (spec 6.3). Its
// media manager steps come first, then its torrents (spec 6.8, rule 3).
type PlanItem struct {
	Rows     []PlanRow     `json:"rows"`
	Steps    []ManagerStep `json:"steps"`    // empty for a torrent row
	Torrents []PlanTorrent `json:"torrents"` // removed after the steps, each once per plan
	Kept     []KeptTorrent `json:"kept"`     // associated with the rows, but they stay
}

// PlanRow is one row of an item, as the planner's own read produced it.
type PlanRow struct {
	Row       Candidate `json:"row"`
	Added     bool      `json:"added,omitempty"`     // not selected: tied to a selected row
	AddedWith *RowRef   `json:"addedWith,omitempty"` // the selected row it was added with
	Shares    []string  `json:"shares,omitempty"`    // IDs of the torrents it shares with other rows of the item; none means a shared library file
}

// ManagerStep is one operation in a media manager. ItemID is the movie or the series in
// that manager.
type ManagerStep struct {
	Manager      string `json:"manager"` // "movies" or "series"
	Action       string `json:"action"`  // one of the Step constants
	ItemID       int    `json:"itemId"`
	FileIDs      []int  `json:"fileIds,omitempty"`      // delete-episode-files
	EpisodeIDs   []int  `json:"episodeIds,omitempty"`   // unmonitor-episodes
	Seasons      []int  `json:"seasons,omitempty"`      // unmonitor-seasons
	AddExclusion bool   `json:"addExclusion,omitempty"` // remove-movie and remove-series: add to the import exclusion list
}

// PlanTorrent is a torrent a plan removes.
type PlanTorrent struct {
	Torrent      TorrentInfo `json:"torrent"`
	KeepData     bool        `json:"keepData,omitempty"`     // remove the entry only: another torrent that stays uses the data, or the path is unknown
	LinkedBySize bool        `json:"linkedBySize,omitempty"` // only its file sizes tie it to the item (spec 6.6)
}

// KeptTorrent is a torrent associated with an item's rows that the plan does not remove.
type KeptTorrent struct {
	Torrent TorrentInfo `json:"torrent"`
	Reason  string      `json:"reason"` // KeptQueued or KeptBacksLibrary
}

// SkippedRow is a selected row the plan does not delete.
type SkippedRow struct {
	Row    RowRef `json:"row"`
	Reason string `json:"reason"`
}
```

Run: `go test ./internal/domain/` — Expected: PASS.

- [ ] **Step 2: Write the test helpers of the planner**

Create `internal/planner/helpers_test.go`:

```go
package planner

import (
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
	"github.com/removarr/removarr/internal/scanner"
)

var now = time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

func played() map[string]domain.UserState {
	return map[string]domain.UserState{"a": {Played: true}}
}

func mfile(id int, size int64) *domain.MediaFile { return &domain.MediaFile{ID: id, Size: size} }

func at(t time.Time) *time.Time { return &t }

// world is a small library that tests change. Everything is watched by user "a":
//   - movie 1 "One" (tmdb:1), file 10, grabbed as and imported from torrent "m1";
//   - movie 2 "Two" (tmdb:2), file 20, from no torrent;
//   - the ended series 1 "Show" (tvdb:100): season 1, episodes 11 and 12 in files 11 and 12,
//     both from the pack "p1";
//   - the torrent "orphan", which no media manager knows.
func world() scanner.Fetched {
	lib := domain.Library{
		Movies: []domain.ServerMovie{
			{ID: "sm1", IDs: domain.ExternalIDs{TMDB: "1"}, States: played()},
			{ID: "sm2", IDs: domain.ExternalIDs{TMDB: "2"}, States: played()},
		},
		Series: []domain.ServerSeries{{ID: "ss1", IDs: domain.ExternalIDs{TVDB: "100"}}},
		Episodes: []domain.ServerEpisode{
			{SeriesID: "ss1", Season: 1, Episode: 1, States: played()},
			{SeriesID: "ss1", Season: 1, Episode: 2, States: played()},
		},
	}
	movies := domain.MovieSnapshot{
		Movies: []domain.Movie{
			{ID: 1, Title: "One", IDs: domain.ExternalIDs{TMDB: "1"}, File: mfile(10, 1000)},
			{ID: 2, Title: "Two", IDs: domain.ExternalIDs{TMDB: "2"}, File: mfile(20, 2000)},
		},
		Links: domain.Links{Grabbed: map[string][]int{"m1": {1}}, FileSource: map[int]string{10: "m1"}, Queue: map[string]bool{}},
	}
	series := domain.SeriesSnapshot{
		Series: []domain.Series{{
			ID: 1, Title: "Show", Ended: true, IDs: domain.ExternalIDs{TVDB: "100"},
			Episodes: []domain.Episode{
				{ID: 11, Season: 1, Number: 1, File: mfile(11, 500)},
				{ID: 12, Season: 1, Number: 2, File: mfile(12, 500)},
			},
		}},
		Links: domain.Links{Grabbed: map[string][]int{"p1": {11, 12}}, FileSource: map[int]string{11: "p1", 12: "p1"}, Queue: map[string]bool{}},
	}
	downloads := []domain.Download{
		{ID: "m1", Name: "One.Torrent", Size: 1000, ContentPath: "/dl/one"},
		{ID: "p1", Name: "Show.S01", Size: 1000, ContentPath: "/dl/show"},
		{ID: "orphan", Name: "Unknown.Torrent", Size: 300, ContentPath: "/dl/unknown"},
	}
	return scanner.Fetched{
		Users:     []domain.User{{ID: "a", Name: "Alice"}},
		Library:   scanner.Source[domain.Library]{Configured: true, Value: lib},
		Movies:    scanner.Source[domain.MovieSnapshot]{Configured: true, Value: movies},
		Series:    scanner.Source[domain.SeriesSnapshot]{Configured: true, Value: series},
		Downloads: scanner.Source[[]domain.Download]{Configured: true, Value: downloads},
		Files:     map[string][]domain.DownloadFile{},
	}
}

func rules() domain.Rules {
	r := domain.DefaultRules()
	r.CountingUsers = []string{"a"}
	return r
}

// scanned is what a scan of f stores: every row, listed and held.
func scanned(r domain.Rules, f scanner.Fetched) []domain.Candidate {
	return scanner.Evaluate(r, now, f).Candidates
}

// plan makes a plan from a read of f, with latest as the latest scan and the given ignores.
func plan(r domain.Rules, f scanner.Fetched, latest []domain.Candidate, ignores []domain.Ignore, selected ...domain.RowRef) ([]domain.PlanItem, []domain.SkippedRow) {
	set := scan.NewIgnoreSet(ignores)
	f.Ignored = set.Keys()
	return Make(Input{
		Rules: r, Now: now, Eval: scanner.Evaluate(r, now, f), Ignores: set, Latest: latest, Selected: selected,
	})
}

// same plans with an unchanged world: the latest scan saw what the planner sees.
func same(t *testing.T, r domain.Rules, f scanner.Fetched, ignores []domain.Ignore, selected ...domain.RowRef) ([]domain.PlanItem, []domain.SkippedRow) {
	t.Helper()
	return plan(r, f, scanned(r, f), ignores, selected...)
}

func sel(list domain.ListKind, key string) domain.RowRef { return domain.RowRef{List: list, Key: key} }

// scannerFetched shortens scanner.Fetched in test tables.
type scannerFetched = scanner.Fetched

var (
	movieOne = sel(domain.ListMovies, "tmdb:1")
	movieTwo = sel(domain.ListMovies, "tmdb:2")
	theShow  = sel(domain.ListSeries, "tvdb:100")
	orphan   = sel(domain.ListTorrents, "hash:orphan")
)

// rowKeys names the rows of an item as "list/key", marking added rows with a "+".
func rowKeys(item domain.PlanItem) []string {
	var out []string
	for _, r := range item.Rows {
		k := string(r.Row.List) + "/" + r.Row.Key
		if r.Added {
			k = "+" + k
		}
		out = append(out, k)
	}
	return out
}

func torrentIDs(item domain.PlanItem) []string {
	var out []string
	for _, t := range item.Torrents {
		out = append(out, t.Torrent.ID)
	}
	return out
}

// skips maps each skipped row's key to its reason.
func skips(skipped []domain.SkippedRow) map[string]string {
	out := map[string]string{}
	for _, s := range skipped {
		out[s.Row.Key] = s.Reason
	}
	return out
}
```

- [ ] **Step 3: Write the failing tests of `Make`**

Create `internal/planner/make_test.go`:

```go
package planner

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
)

func TestMakeReturnsEmptyListsForNoSelection(t *testing.T) {
	items, skipped := same(t, rules(), world(), nil)
	if items == nil || skipped == nil || len(items)+len(skipped) != 0 {
		t.Errorf("items = %v, skipped = %v, want two empty non-nil lists", items, skipped)
	}
}

func TestMakeSkipsEveryRowOfAListThatCannotBeProduced(t *testing.T) {
	f := world()
	latest := scanned(rules(), f)
	f.Movies.Err = errors.New("timeout")
	items, skipped := plan(rules(), f, latest, nil, movieOne, theShow, orphan)
	got := skips(skipped)
	for _, key := range []string{"tmdb:1", "tvdb:100", "hash:orphan"} {
		if !strings.Contains(got[key], "movie manager could not be read") {
			t.Errorf("%s: reason = %q, want the movie manager's error", key, got[key])
		}
	}
	if len(items) != 0 {
		t.Errorf("items = %v, want none", items)
	}
}

func TestMakeSkipsARowThatIsNotInTheLatestScan(t *testing.T) {
	items, skipped := plan(rules(), world(), nil, nil, movieOne)
	if len(items) != 0 || skips(skipped)["tmdb:1"] != ReasonNotInScan {
		t.Errorf("items = %v, skipped = %v, want tmdb:1 skipped as not in the scan", items, skipped)
	}
	if skipped[0].Row.List != domain.ListMovies {
		t.Errorf("skipped row = %+v, want its list kept", skipped[0].Row)
	}
}

func TestMakeSkipsAnIgnoredRow(t *testing.T) {
	ign := []domain.Ignore{{Kind: domain.IgnoreMovie, Key: "tmdb:1", Title: "One"}}
	items, skipped := same(t, rules(), world(), ign, movieOne)
	if len(items) != 0 || skips(skipped)["tmdb:1"] != scan.HoldIgnored {
		t.Errorf("items = %v, skipped = %v, want tmdb:1 skipped as ignored", items, skipped)
	}
}

func TestMakeSkipsAMovieThatNoLongerQualifies(t *testing.T) {
	f := world()
	latest := scanned(rules(), f)
	f.Library.Value.Movies[0].States = nil // nobody has watched it any more
	_, skipped := plan(rules(), f, latest, nil, movieOne)
	if got := skips(skipped)["tmdb:1"]; got != ReasonNoLonger {
		t.Errorf("reason = %q, want %q", got, ReasonNoLonger)
	}
	if skipped[0].Row.Title != "One" {
		t.Errorf("skipped row = %+v, want its title from the latest scan", skipped[0].Row)
	}
}

func TestMakeSkipsATorrentRowWithWhatChanged(t *testing.T) {
	tests := []struct {
		name   string
		change func(f *scannerFetched)
		want   string
	}{
		{"gone from the client", func(f *scannerFetched) { f.Downloads.Value = f.Downloads.Value[:2] }, ReasonGone},
		{"in a queue now", func(f *scannerFetched) { f.Movies.Value.Links.Queue["orphan"] = true }, ReasonQueuedNow},
		{"backs a library file now", func(f *scannerFetched) { f.Movies.Value.Links.FileSource[20] = "orphan" }, ReasonLinkedNow},
		{"excluded now", func(f *scannerFetched) { f.Downloads.Value[2].Category = "music" }, ReasonExcludedNow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := rules()
			r.ExcludedCategories = []string{"music"}
			f := world()
			latest := scanned(r, f)
			tt.change(&f)
			_, skipped := plan(r, f, latest, nil, orphan)
			if got := skips(skipped)["hash:orphan"]; got != tt.want {
				t.Errorf("reason = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMakeSkipsAWaitingRowWithWhatItWaitsFor(t *testing.T) {
	f := world()
	f.Movies.Value.Links.FileSource[20] = "m1" // m1 also backs movie 2's file
	f.Library.Value.Movies[1].States = nil     // and movie 2 is not watched
	_, skipped := same(t, rules(), f, nil, movieOne)
	if got := skips(skipped)["tmdb:1"]; got != scan.WaitTorrentStays {
		t.Errorf("reason = %q, want %q", got, scan.WaitTorrentStays)
	}
}

func TestMakeSkipsARowTiedToAnIgnoredRow(t *testing.T) {
	f := world()
	f.Movies.Value.Links.FileSource[20] = "m1" // m1 backs both movies: they are tied
	ign := []domain.Ignore{{Kind: domain.IgnoreMovie, Key: "tmdb:2", Title: "Two"}}
	items, skipped := same(t, rules(), f, ign, movieOne)
	if len(items) != 0 || skips(skipped)["tmdb:1"] != `It is tied to "Two", which is ignored.` {
		t.Errorf("items = %v, skipped = %v, want tmdb:1 skipped, naming Two", items, skipped)
	}
}

func TestMakeTurnsATorrentRowIntoAnItemOfItsOwn(t *testing.T) {
	items, skipped := same(t, rules(), world(), nil, orphan)
	if len(skipped) != 0 || len(items) != 1 {
		t.Fatalf("items = %v, skipped = %v, want one item", items, skipped)
	}
	item := items[0]
	if !reflect.DeepEqual(rowKeys(item), []string{"torrents/hash:orphan"}) || !reflect.DeepEqual(torrentIDs(item), []string{"orphan"}) {
		t.Errorf("item = %+v, want the torrent row with its torrent", item)
	}
	if item.Steps == nil || len(item.Steps) != 0 || item.Kept == nil {
		t.Errorf("steps = %v, kept = %v, want empty non-nil lists", item.Steps, item.Kept)
	}
}

func TestMakeTakesEachSelectedRowOnceAndOrdersTheItems(t *testing.T) {
	items, _ := same(t, rules(), world(), nil, orphan, theShow, movieTwo, movieOne, movieOne)
	var got []string
	for _, it := range items {
		got = append(got, rowKeys(it)...)
	}
	want := []string{"movies/tmdb:1", "movies/tmdb:2", "series/tvdb:100", "torrents/hash:orphan"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}
```

Create `internal/planner/items_test.go`:

```go
package planner

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
)

// onlyItem checks that a plan has exactly one item and no skipped row, and returns the item.
// Call it as onlyItem(t)(same(…)).
func onlyItem(t *testing.T) func([]domain.PlanItem, []domain.SkippedRow) domain.PlanItem {
	return func(items []domain.PlanItem, skipped []domain.SkippedRow) domain.PlanItem {
		t.Helper()
		if len(items) != 1 || len(skipped) != 0 {
			t.Fatalf("items = %+v, skipped = %+v, want exactly one item", items, skipped)
		}
		return items[0]
	}
}

func TestMakeRemovesAMovieFromTheMovieManager(t *testing.T) {
	for _, exclude := range []bool{false, true} {
		r := rules()
		r.AddExclusionOnDelete = exclude
		item := onlyItem(t)(same(t, r, world(), nil, movieOne))
		want := []domain.ManagerStep{{Manager: scan.ManagerMovies, Action: domain.StepRemoveMovie, ItemID: 1, AddExclusion: exclude}}
		if !reflect.DeepEqual(item.Steps, want) {
			t.Errorf("exclude=%v: steps = %+v, want %+v", exclude, item.Steps, want)
		}
		if !reflect.DeepEqual(torrentIDs(item), []string{"m1"}) {
			t.Errorf("torrents = %v, want [m1]", torrentIDs(item))
		}
	}
}

func TestMakeRemovesAnEndedSeriesFromTheSeriesManager(t *testing.T) {
	item := onlyItem(t)(same(t, rules(), world(), nil, theShow))
	want := []domain.ManagerStep{{Manager: scan.ManagerSeries, Action: domain.StepRemoveSeries, ItemID: 1}}
	if !reflect.DeepEqual(item.Steps, want) || !reflect.DeepEqual(torrentIDs(item), []string{"p1"}) {
		t.Errorf("item = %+v, want remove-series and the pack", item)
	}
}

// running makes the series of world() a running one: season 1 aired in the past, and a
// season 2 with one episode that has no file and airs in the future.
func running(f *scannerFetched, season1Aired bool) {
	s := &f.Series.Value.Series[0]
	s.Ended = false
	for i := range s.Episodes {
		if season1Aired {
			s.Episodes[i].AirDate = at(now.Add(-48 * time.Hour))
		}
	}
	s.Episodes = append(s.Episodes, domain.Episode{ID: 21, Season: 2, Number: 1, AirDate: at(now.Add(48 * time.Hour))})
}

func TestMakeDeletesTheFilesOfARunningSeries(t *testing.T) {
	f := world()
	running(&f, true)
	item := onlyItem(t)(same(t, rules(), f, nil, theShow))
	want := []domain.ManagerStep{
		{Manager: scan.ManagerSeries, Action: domain.StepDeleteEpisodeFiles, ItemID: 1, FileIDs: []int{11, 12}},
		{Manager: scan.ManagerSeries, Action: domain.StepUnmonitorEpisodes, ItemID: 1, EpisodeIDs: []int{11, 12}},
		{Manager: scan.ManagerSeries, Action: domain.StepUnmonitorSeasons, ItemID: 1, Seasons: []int{1}},
	}
	if !reflect.DeepEqual(item.Steps, want) {
		t.Errorf("steps = %+v, want %+v", item.Steps, want)
	}
}

func TestMakeLeavesASeasonMonitoredThatHasNotFinishedAiring(t *testing.T) {
	f := world()
	running(&f, false) // season 1 has no air dates
	item := onlyItem(t)(same(t, rules(), f, nil, theShow))
	for _, s := range item.Steps {
		if s.Action == domain.StepUnmonitorSeasons {
			t.Errorf("steps = %+v, want no season unmonitored", item.Steps)
		}
	}
	if len(item.Steps) != 2 {
		t.Errorf("steps = %+v, want delete and unmonitor episodes", item.Steps)
	}
}

func TestMakeDeletesTheFilesOfASeasonRow(t *testing.T) {
	r := rules()
	r.SeriesUnit = domain.UnitSeason
	f := world()
	running(&f, true)
	// Season 2 gets a file that is not watched, so season 1 is a row of its own.
	s := &f.Series.Value.Series[0]
	s.Episodes[2].File = mfile(21, 500)
	item := onlyItem(t)(same(t, r, f, nil, sel(domain.ListSeries, "tvdb:100:s1")))
	want := []domain.ManagerStep{
		{Manager: scan.ManagerSeries, Action: domain.StepDeleteEpisodeFiles, ItemID: 1, FileIDs: []int{11, 12}},
		{Manager: scan.ManagerSeries, Action: domain.StepUnmonitorEpisodes, ItemID: 1, EpisodeIDs: []int{11, 12}},
		{Manager: scan.ManagerSeries, Action: domain.StepUnmonitorSeasons, ItemID: 1, Seasons: []int{1}},
	}
	if !reflect.DeepEqual(item.Steps, want) {
		t.Errorf("steps = %+v, want %+v", item.Steps, want)
	}
}

func TestMakeDeletesAFileSharedByTwoSeasonRowsOnce(t *testing.T) {
	r := rules()
	r.SeriesUnit = domain.UnitSeason
	f := world()
	s := &f.Series.Value.Series[0]
	s.Ended = false
	// Episode 12 (season 1) and episode 21 (season 2) share file 30; season 3 is not watched,
	// so the seasons are not merged into one series row.
	s.Episodes[1].File = mfile(30, 500)
	s.Episodes = append(s.Episodes,
		domain.Episode{ID: 21, Season: 2, Number: 1, File: mfile(30, 500)},
		domain.Episode{ID: 31, Season: 3, Number: 1, File: mfile(31, 500)})
	f.Series.Value.Links = domain.Links{Grabbed: map[string][]int{}, FileSource: map[int]string{}, Queue: map[string]bool{}}
	f.Library.Value.Episodes = append(f.Library.Value.Episodes, domain.ServerEpisode{SeriesID: "ss1", Season: 2, Episode: 1, States: played()})
	items, skipped := same(t, r, f, nil, sel(domain.ListSeries, "tvdb:100:s1"))
	item := onlyItem(t)(items, skipped)
	if !reflect.DeepEqual(rowKeys(item), []string{"series/tvdb:100:s1", "+series/tvdb:100:s2"}) {
		t.Fatalf("rows = %v, want season 1 and the tied season 2", rowKeys(item))
	}
	deleted := map[int]int{}
	for _, step := range item.Steps {
		if step.Action == domain.StepDeleteEpisodeFiles {
			for _, id := range step.FileIDs {
				deleted[id]++
			}
		}
	}
	if !reflect.DeepEqual(deleted, map[int]int{11: 1, 30: 1}) {
		t.Errorf("files deleted = %v, want 11 and 30 once each", deleted)
	}
}

func TestMakeAddsTiedRowsToTheItem(t *testing.T) {
	f := world()
	f.Movies.Value.Links.FileSource[20] = "m1" // m1 backs both movies: they are tied
	item := onlyItem(t)(same(t, rules(), f, nil, movieOne))
	if !reflect.DeepEqual(rowKeys(item), []string{"movies/tmdb:1", "+movies/tmdb:2"}) {
		t.Fatalf("rows = %v, want One and the added Two", rowKeys(item))
	}
	added := item.Rows[1]
	if added.AddedWith == nil || added.AddedWith.Key != "tmdb:1" || added.AddedWith.Title != "One" {
		t.Errorf("AddedWith = %+v, want One", added.AddedWith)
	}
	if !reflect.DeepEqual(added.Shares, []string{"m1"}) || !reflect.DeepEqual(item.Rows[0].Shares, []string{"m1"}) {
		t.Errorf("shares = %v and %v, want [m1] on both", item.Rows[0].Shares, added.Shares)
	}
	if len(item.Steps) != 2 || item.Steps[0].ItemID != 1 || item.Steps[1].ItemID != 2 {
		t.Errorf("steps = %+v, want remove-movie 1 and 2", item.Steps)
	}
	if !reflect.DeepEqual(torrentIDs(item), []string{"m1"}) {
		t.Errorf("torrents = %v, want m1 once", torrentIDs(item))
	}
}

func TestMakeMakesOneItemOfSeveralSelectedRowsOfAGroup(t *testing.T) {
	f := world()
	f.Movies.Value.Links.FileSource[20] = "m1"
	item := onlyItem(t)(same(t, rules(), f, nil, movieTwo, movieOne))
	if !reflect.DeepEqual(rowKeys(item), []string{"movies/tmdb:1", "movies/tmdb:2"}) {
		t.Errorf("rows = %v, want both, neither added", rowKeys(item))
	}
	for _, r := range item.Rows {
		if r.AddedWith != nil {
			t.Errorf("row %s has AddedWith %+v, want none: it was selected", r.Row.Key, r.AddedWith)
		}
	}
}

func TestMakeSkipsAnItemThatWouldRemoveATorrentTheScanDidNotShow(t *testing.T) {
	f := world()
	latest := scanned(rules(), f)
	// Since the scan, history names a second torrent for movie 1, and it is in the client.
	f.Movies.Value.Links.Grabbed["m2"] = []int{1}
	f.Downloads.Value = append(f.Downloads.Value, domain.Download{ID: "m2", Name: "One.Again"})
	items, skipped := plan(rules(), f, latest, nil, movieOne)
	if len(items) != 0 {
		t.Fatalf("items = %+v, want none", items)
	}
	if got := skips(skipped)["tmdb:1"]; !strings.Contains(got, `"One.Again"`) || !strings.Contains(got, "Scan again") {
		t.Errorf("reason = %q, want it to name One.Again and ask for a scan", got)
	}
}

func TestMakeUsesTheNewReadWhenATorrentIsGoneSinceTheScan(t *testing.T) {
	f := world()
	latest := scanned(rules(), f)
	f.Downloads.Value = f.Downloads.Value[1:] // m1 is gone from the client
	item := onlyItem(t)(plan(rules(), f, latest, nil, movieOne))
	if len(item.Torrents) != 0 {
		t.Errorf("torrents = %v, want none", torrentIDs(item))
	}
}
```

- [ ] **Step 4: Run the tests to see them fail**

Run: `go test ./internal/planner/`
Expected: build failure: `undefined: Make`, `undefined: Input`, `undefined: ReasonNotInScan`.

- [ ] **Step 5: Implement `Make`**

Create `internal/planner/make.go`:

```go
// Package planner builds the plan for a deletion from live data (spec 6.7, 6.8, 6.10). It
// reads and evaluates the apps through scanner, so a plan decides exactly as a scan does.
// Nothing here changes anything in an app.
package planner

import (
	"sort"
	"strings"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
	"github.com/removarr/removarr/internal/scanner"
)

// Input is everything a plan is made from.
type Input struct {
	Rules    domain.Rules
	Now      time.Time
	Eval     scanner.Evaluation // the planner's own read, evaluated as a scan
	Ignores  scan.IgnoreSet     // the current ignores
	Latest   []domain.Candidate // every row of the latest scan, listed and held
	Selected []domain.RowRef    // the rows the user selected, by list and key
}

// Why a selected row is skipped. Other reasons come from the rules: a list's error, what a
// row waits for (scan.Wait…, scan.Hold…), or a change since the scan.
const (
	ReasonNotInScan   = "It is not a row of the latest scan. Scan again."
	ReasonNoLonger    = "It no longer qualifies. Scan again to see why."
	ReasonGone        = "It is no longer in the download client."
	ReasonQueuedNow   = "It is in a media manager's queue now."
	ReasonLinkedNow   = "It backs a library file now."
	ReasonExcludedNow = "Its category or tag is excluded now."
)

// rowID names a row.
type rowID struct {
	list domain.ListKind
	key  string
}

// Make builds the items and the skipped rows of a plan (spec 6.7, 6.10). Both are never nil.
// Items come in the order movies, series, torrents, each by key, so a torrent row is reached
// after any movie or series that removes its torrent anyway.
func Make(in Input) ([]domain.PlanItem, []domain.SkippedRow) {
	m := newMaker(in)
	for _, ref := range selection(in.Selected) {
		m.add(ref)
	}
	m.finish()
	return m.items, m.skipped
}

// maker holds what Make looks things up in, and what it has built so far.
type maker struct {
	in        Input
	latest    map[rowID]domain.Candidate // the latest scan
	fresh     map[rowID]domain.Candidate // the planner's read, listed and held
	rows      map[rowID]scan.Row         // the planner's read: what Delete touches
	series    map[int]domain.Series      // by ID in the series manager
	downloads map[string]domain.Download // the download client's answer
	holds     scan.Holds
	items     []domain.PlanItem
	inItem    map[rowID]int  // row -> its item
	owner     map[string]int // torrent ID -> the item that removes it
	skipped   []domain.SkippedRow
}

func newMaker(in Input) *maker {
	m := &maker{
		in:        in,
		latest:    map[rowID]domain.Candidate{},
		fresh:     map[rowID]domain.Candidate{},
		rows:      map[rowID]scan.Row{},
		series:    map[int]domain.Series{},
		downloads: map[string]domain.Download{},
		holds:     scan.NewHolds(in.Eval.Candidates, in.Ignores),
		items:     []domain.PlanItem{},
		inItem:    map[rowID]int{},
		owner:     map[string]int{},
		skipped:   []domain.SkippedRow{},
	}
	for _, c := range in.Latest {
		m.latest[rowID{c.List, c.Key}] = c
	}
	for _, c := range in.Eval.Candidates {
		m.fresh[rowID{c.List, c.Key}] = c
	}
	for _, r := range in.Eval.Rows {
		m.rows[rowID{r.Candidate.List, r.Candidate.Key}] = r
	}
	for _, s := range in.Eval.Fetched.Series.Value.Series {
		m.series[s.ID] = s
	}
	for _, d := range in.Eval.Fetched.Downloads.Value {
		m.downloads[d.ID] = d
	}
	return m
}

// listRank orders the lists in a plan.
func listRank(l domain.ListKind) int {
	switch l {
	case domain.ListMovies:
		return 0
	case domain.ListSeries:
		return 1
	default:
		return 2
	}
}

// selection returns each selected row once, by list and key.
func selection(refs []domain.RowRef) []domain.RowRef {
	seen := map[rowID]bool{}
	out := []domain.RowRef{}
	for _, r := range refs {
		id := rowID{r.List, r.Key}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, domain.RowRef{List: r.List, Key: r.Key})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := listRank(out[i].List), listRank(out[j].List); a != b {
			return a < b
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func refOf(c domain.Candidate) domain.RowRef {
	return domain.RowRef{List: c.List, Key: c.Key, Title: c.Title, Season: c.Season}
}

// ref names a row with its title, from the planner's read or else from the latest scan.
func (m *maker) ref(id rowID) domain.RowRef {
	if c, ok := m.fresh[id]; ok {
		return refOf(c)
	}
	if c, ok := m.latest[id]; ok {
		return refOf(c)
	}
	return domain.RowRef{List: id.list, Key: id.key}
}

func (m *maker) skip(id rowID, reason string) {
	m.skipped = append(m.skipped, domain.SkippedRow{Row: m.ref(id), Reason: reason})
}

// add plans one selected row, or skips it with the reason (spec 6.10).
func (m *maker) add(ref domain.RowRef) {
	id := rowID{ref.List, ref.Key}
	if i, ok := m.inItem[id]; ok {
		m.markSelected(i, id)
		return
	}
	if problem := m.in.Eval.Summary.Lists[ref.List].Error; problem != "" {
		m.skip(id, problem)
		return
	}
	if _, ok := m.latest[id]; !ok {
		m.skip(id, ReasonNotInScan)
		return
	}
	if m.in.Ignores.Hides(ref.List, ref.Key) {
		m.skip(id, scan.HoldIgnored)
		return
	}
	c, ok := m.fresh[id]
	if !ok {
		m.skip(id, m.goneReason(id))
		return
	}
	if reason := m.holds.Reason(c); reason != "" {
		m.skip(id, reason)
		return
	}
	if ref.List == domain.ListTorrents {
		m.addTorrentRow(id, c)
		return
	}
	m.addGroup(id, c)
}

// goneReason says why the planner's read no longer produces a row of the latest scan. For a
// torrent row the download client and the links tell; for a movie or series row the rules do
// not record why, so a new scan has to show it.
func (m *maker) goneReason(id rowID) string {
	if id.list != domain.ListTorrents {
		return ReasonNoLonger
	}
	hash := strings.TrimPrefix(id.key, "hash:")
	switch {
	case m.downloads[hash].ID == "":
		return ReasonGone
	case m.in.Eval.Index.Queued(hash):
		return ReasonQueuedNow
	case m.in.Eval.Index.Backing(hash) > 0:
		return ReasonLinkedNow
	default:
		// scan.Torrents lists every other download in the client that has an ID.
		return ReasonExcludedNow
	}
}

// markSelected records that a row an earlier selection added to item i was selected too.
func (m *maker) markSelected(i int, id rowID) {
	for j := range m.items[i].Rows {
		r := &m.items[i].Rows[j]
		if r.Row.List == id.list && r.Row.Key == id.key {
			r.Added, r.AddedWith = false, nil
		}
	}
}

// addTorrentRow plans an orphan or unlinked torrent: the torrent alone, no media manager
// step (spec 6.7). A torrent a movie or series of the plan removes anyway is not planned
// twice.
func (m *maker) addTorrentRow(id rowID, c domain.Candidate) {
	if len(c.Torrents) != 1 {
		m.skip(id, ReasonNoLonger)
		return
	}
	t := c.Torrents[0]
	if i, ok := m.owner[t.ID]; ok {
		m.skip(id, removedWith(m.items[i].Rows[0].Row.Title))
		return
	}
	m.push(domain.PlanItem{
		Rows:     []domain.PlanRow{{Row: c}},
		Steps:    []domain.ManagerStep{},
		Torrents: []domain.PlanTorrent{{Torrent: t}},
		Kept:     []domain.KeptTorrent{},
	})
}

// push appends an item and records its rows and torrents.
func (m *maker) push(item domain.PlanItem) {
	i := len(m.items)
	m.items = append(m.items, item)
	for _, r := range item.Rows {
		m.inItem[rowID{r.Row.List, r.Row.Key}] = i
	}
	for _, t := range item.Torrents {
		m.owner[t.Torrent.ID] = i
	}
}
```

Create `internal/planner/items.go`:

```go
package planner

import (
	"fmt"
	"sort"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
)

// changedReason is the reason for an item whose new read would remove a torrent the latest
// scan did not name for its rows (spec 6.10, two reads).
func changedReason(name string) string {
	return fmt.Sprintf("It changed since the scan: it would now remove the torrent %q, which the scan did not show. Scan again.", name)
}

// removedWith is the reason for a torrent row whose torrent another item removes.
func removedWith(title string) string {
	return fmt.Sprintf("Its torrent is removed with %q anyway.", title)
}

// addGroup plans a movie or series row together with the rows it is tied to (spec 6.3).
func (m *maker) addGroup(id rowID, c domain.Candidate) {
	members := []domain.Candidate{c}
	for _, ref := range c.TiedWith {
		o, ok := m.fresh[rowID{ref.List, ref.Key}]
		if !ok || o.Waiting {
			// WholeTorrents ties listed rows only; a plan does not build on anything else.
			m.skip(id, ReasonNoLonger)
			return
		}
		members = append(members, o)
	}
	sort.Slice(members, func(i, j int) bool {
		if a, b := listRank(members[i].List), listRank(members[j].List); a != b {
			return a < b
		}
		return members[i].Key < members[j].Key
	})
	if name, changed := m.changedSinceScan(members); changed {
		m.skip(id, changedReason(name))
		return
	}

	selected := refOf(c)
	item := domain.PlanItem{Rows: []domain.PlanRow{}, Steps: m.steps(members), Torrents: m.torrents(members), Kept: m.kept(members)}
	for _, o := range members {
		r := domain.PlanRow{Row: o, Shares: shares(o, members)}
		if o.List != c.List || o.Key != c.Key {
			with := selected
			r.Added, r.AddedWith = true, &with
		}
		item.Rows = append(item.Rows, r)
	}
	m.push(item)
}

// changedSinceScan returns the name of a torrent the new read would remove for the rows that
// the latest scan did not name for any of them. History is read before the queue, so one
// read alone can take a download that was just imported for one that backs nothing.
func (m *maker) changedSinceScan(members []domain.Candidate) (string, bool) {
	scanned := map[string]bool{}
	for _, o := range members {
		for _, t := range m.latest[rowID{o.List, o.Key}].Torrents {
			scanned[t.ID] = true
		}
	}
	for _, o := range members {
		for _, t := range o.Torrents {
			if !scanned[t.ID] {
				return t.Name, true
			}
		}
	}
	return "", false
}

// shares returns the torrents a row shares with the other rows of its item, sorted.
func shares(o domain.Candidate, members []domain.Candidate) []string {
	mine := map[string]bool{}
	for _, t := range o.Torrents {
		mine[t.ID] = true
	}
	set := map[string]bool{}
	for _, p := range members {
		if p.List == o.List && p.Key == o.Key {
			continue
		}
		for _, t := range p.Torrents {
			if mine[t.ID] {
				set[t.ID] = true
			}
		}
	}
	var out []string
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// steps returns the media manager steps of an item's rows (spec 6.7). A library file that two
// rows of the item delete is deleted once.
func (m *maker) steps(members []domain.Candidate) []domain.ManagerStep {
	steps := []domain.ManagerStep{}
	deleted := map[int]bool{} // episode files already in a step of this item
	for _, c := range members {
		row := m.rows[rowID{c.List, c.Key}]
		switch {
		case c.List == domain.ListMovies:
			steps = append(steps, domain.ManagerStep{
				Manager: scan.ManagerMovies, Action: domain.StepRemoveMovie, ItemID: row.ManagerID,
				AddExclusion: m.in.Rules.AddExclusionOnDelete,
			})
		case c.Scope == domain.ScopeSeries && !c.StillAiring:
			steps = append(steps, domain.ManagerStep{
				Manager: scan.ManagerSeries, Action: domain.StepRemoveSeries, ItemID: row.ManagerID,
				AddExclusion: m.in.Rules.AddExclusionOnDelete,
			})
		default:
			// A season row, or the row of a series that is still airing: the series stays.
			var files []int
			for _, f := range sortedInts(row.FileIDs) {
				if !deleted[f] {
					deleted[f] = true
					files = append(files, f)
				}
			}
			if len(files) > 0 {
				steps = append(steps, domain.ManagerStep{Manager: scan.ManagerSeries, Action: domain.StepDeleteEpisodeFiles, ItemID: row.ManagerID, FileIDs: files})
			}
			if episodes := sortedInts(row.ItemIDs); len(episodes) > 0 {
				steps = append(steps, domain.ManagerStep{Manager: scan.ManagerSeries, Action: domain.StepUnmonitorEpisodes, ItemID: row.ManagerID, EpisodeIDs: episodes})
			}
			if seasons := m.finishedSeasons(row); len(seasons) > 0 {
				steps = append(steps, domain.ManagerStep{Manager: scan.ManagerSeries, Action: domain.StepUnmonitorSeasons, ItemID: row.ManagerID, Seasons: seasons})
			}
		}
	}
	return steps
}

// finishedSeasons returns the regular seasons of a row's episodes that have finished airing:
// only seasons whose files the row deletes are unmonitored (spec 6.7).
func (m *maker) finishedSeasons(row scan.Row) []int {
	s := m.series[row.ManagerID]
	seasonOf := map[int]int{}
	for _, e := range s.Episodes {
		seasonOf[e.ID] = e.Season
	}
	set := map[int]bool{}
	for _, item := range row.ItemIDs {
		if n := seasonOf[item]; n > 0 {
			set[n] = true
		}
	}
	var out []int
	for n := range set {
		if scan.SeasonFinished(s, n, m.in.Now) {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

func sortedInts(xs []int) []int {
	out := append([]int(nil), xs...)
	sort.Ints(out)
	return out
}
```

Create `internal/planner/torrents.go` (Task 5 extends it):

```go
package planner

import (
	"sort"

	"github.com/removarr/removarr/internal/domain"
)

// torrents returns the torrents an item removes: those its rows name, each once, without the
// ones an earlier item of the plan removes already.
func (m *maker) torrents(members []domain.Candidate) []domain.PlanTorrent {
	out := []domain.PlanTorrent{}
	seen := map[string]bool{}
	for _, o := range members {
		for _, t := range o.Torrents {
			if seen[t.ID] {
				continue
			}
			seen[t.ID] = true
			if _, removed := m.owner[t.ID]; removed {
				continue
			}
			out = append(out, domain.PlanTorrent{Torrent: t})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Torrent.ID < out[j].Torrent.ID })
	return out
}

// kept returns the torrents associated with an item's rows that stay. Task 5 fills it.
func (m *maker) kept(members []domain.Candidate) []domain.KeptTorrent {
	return []domain.KeptTorrent{}
}

// finish completes what needs the whole plan. Task 5 fills it.
func (m *maker) finish() {}
```

- [ ] **Step 6: Run the tests to see them pass**

Run: `gofmt -l . && go vet ./internal/planner/ && go test ./internal/planner/ ./internal/domain/`
Expected: no file listed, all PASS.

- [ ] **Step 7: Break it once**

In scratch edits, one at a time: (a) delete the `changedSinceScan` check in `addGroup`; (b) in `steps`, drop the `deleted` check; (c) in `add`, move the `Ignores.Hides` check below the `holds.Reason` check and drop it. Each must make at least one test FAIL. Undo every edit.

- [ ] **Step 8: Commit**

```bash
git add internal/domain internal/planner
git commit -m "feat: make a plan for the selected rows from a fresh read" -m "A plan re-evaluates every selected row with the scan's rules on live data and skips what no longer qualifies, is ignored or waits, with the reason (spec 6.10). Tied rows form one item, each row gets the media manager steps of spec 6.7, and an item is skipped when it would remove a torrent the scan did not show: one read alone can mistake a fresh import for a torrent that backs nothing." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: torrent details: linked by size, the torrents that stay, shared data, once per plan

**Files:**
- Modify: `internal/planner/torrents.go`
- Test: `internal/planner/torrents_test.go` (create)

**Interfaces:**
- Consumes: from Task 4 the unexported `maker` with `owner`, `downloads`, `rows`, `items`; `scanner.Evaluation.Index`, `.History` (Task 3); `(*scan.LinkIndex).Associated`, `Queued`, `scan.TorrentDetails` (Task 2).
- Produces: `PlanTorrent.LinkedBySize`, `PlanTorrent.KeepData` and `PlanItem.Kept` filled in.

- [ ] **Step 1: Write the failing tests**

Create `internal/planner/torrents_test.go`:

```go
package planner

import (
	"reflect"
	"strings"
	"testing"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
)

func TestMakeMarksATorrentLinkedOnlyByFileSize(t *testing.T) {
	f := world()
	// Movie 2's file was imported by hand from c2: only its size ties them.
	size := scan.MinContentSize + 7
	f.Movies.Value.Movies[1].File.Size = size
	f.Downloads.Value = append(f.Downloads.Value, domain.Download{ID: "c2", Name: "Two.Torrent", Size: size, ContentPath: "/dl/two"})
	f.Files = map[string][]domain.DownloadFile{"c2": {{Name: "two.mkv", Size: size}}}
	items, skipped := same(t, rules(), f, nil, movieOne, movieTwo)
	if len(items) != 2 || len(skipped) != 0 {
		t.Fatalf("items = %+v, skipped = %+v", items, skipped)
	}
	if got := items[0].Torrents; len(got) != 1 || got[0].LinkedBySize {
		t.Errorf("One: torrents = %+v, want m1, linked by history", got)
	}
	if got := items[1].Torrents; len(got) != 1 || got[0].Torrent.ID != "c2" || !got[0].LinkedBySize {
		t.Errorf("Two: torrents = %+v, want c2, linked by size", got)
	}
}

func TestMakeListsTheTorrentsThatStay(t *testing.T) {
	f := world()
	f.Library.Value.Movies[1].States = nil // movie 2 is not watched
	// Both were grabbed for movie 1: q is still in a queue, b became movie 2's file.
	f.Movies.Value.Links.Grabbed["q"] = []int{1}
	f.Movies.Value.Links.Queue["q"] = true
	f.Movies.Value.Links.Grabbed["b"] = []int{1}
	f.Movies.Value.Links.FileSource[20] = "b"
	// gone was grabbed for movie 1 and is no longer in the client: it is not mentioned.
	f.Movies.Value.Links.Grabbed["gone"] = []int{1}
	f.Downloads.Value = append(f.Downloads.Value,
		domain.Download{ID: "q", Name: "One.Queued"}, domain.Download{ID: "b", Name: "Two.Torrent"})
	item := onlyItem(t)(same(t, rules(), f, nil, movieOne))
	var got []string
	for _, k := range item.Kept {
		got = append(got, k.Torrent.ID+" "+k.Reason+" "+k.Torrent.Name)
	}
	want := []string{"b " + domain.KeptBacksLibrary + " Two.Torrent", "q " + domain.KeptQueued + " One.Queued"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("kept = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(torrentIDs(item), []string{"m1"}) {
		t.Errorf("torrents = %v, want m1 alone", torrentIDs(item))
	}
}

func TestMakeKeepsTheDataATorrentThatStaysUses(t *testing.T) {
	tests := []struct {
		name  string
		path  string // content path of the torrent "x", which stays
		mine  string // content path of m1
		keep  bool
	}{
		{"the same path", "/dl/one", "/dl/one", true},
		{"the same path with a trailing slash", "/dl/one/", "/dl/one", true},
		{"a path inside it", "/dl/one/extra.mkv", "/dl/one", true},
		{"a path around it", "/dl", "/dl/one", true},
		{"a Windows path inside it", `D:\dl\one\extra.mkv`, `D:\dl\one`, true},
		{"a path that only starts the same", "/dl/one-two", "/dl/one", false},
		{"an unrelated path", "/dl/other", "/dl/one", false},
		{"an unknown path of its own", "/dl/other", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := world()
			f.Downloads.Value[0].ContentPath = tt.mine
			f.Downloads.Value = append(f.Downloads.Value, domain.Download{ID: "x", Name: "Cross.Seed", ContentPath: tt.path})
			item := onlyItem(t)(same(t, rules(), f, nil, movieOne))
			if len(item.Torrents) != 1 || item.Torrents[0].KeepData != tt.keep {
				t.Errorf("torrents = %+v, want m1 with KeepData %v", item.Torrents, tt.keep)
			}
		})
	}
}

func TestMakeDeletesSharedDataWhenEveryTorrentUsingItGoes(t *testing.T) {
	f := world()
	f.Downloads.Value[2].ContentPath = "/dl/one" // the orphan holds the same data as m1
	items, _ := same(t, rules(), f, nil, movieOne, orphan)
	if len(items) != 2 {
		t.Fatalf("items = %+v, want two", items)
	}
	for _, it := range items {
		if it.Torrents[0].KeepData {
			t.Errorf("%s keeps its data, want it deleted: no torrent using it stays", it.Torrents[0].Torrent.ID)
		}
	}
}

func TestMakeRemovesATorrentOnceWhenTwoItemsNameIt(t *testing.T) {
	f := world()
	// g was grabbed for both movies and backs nothing: both rows name it, and it is unlinked.
	f.Movies.Value.Links.Grabbed["g"] = []int{1, 2}
	f.Downloads.Value = append(f.Downloads.Value, domain.Download{ID: "g", Name: "Grabbed.Twice"})
	items, skipped := same(t, rules(), f, nil, movieOne, movieTwo, sel(domain.ListTorrents, "hash:g"))
	if len(items) != 2 {
		t.Fatalf("items = %+v, want the two movies", items)
	}
	if !reflect.DeepEqual(torrentIDs(items[0]), []string{"g", "m1"}) || len(items[1].Torrents) != 0 {
		t.Errorf("torrents = %v and %v, want g with One only", torrentIDs(items[0]), torrentIDs(items[1]))
	}
	if got := skips(skipped)["hash:g"]; !strings.Contains(got, `"One"`) {
		t.Errorf("torrent row reason = %q, want it to name One", got)
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/planner/ -run 'TestMakeMarks|TestMakeLists|TestMakeKeeps|TestMakeDeletesShared|TestMakeRemovesATorrentOnce'`
Expected: FAIL: `LinkedBySize` false for c2, `Kept` empty, `KeepData` false where it should be true. `TestMakeRemovesATorrentOnceWhenTwoItemsNameIt` may already pass (Task 4 dedupes); that is expected.

- [ ] **Step 3: Implement**

Replace `internal/planner/torrents.go` by:

```go
package planner

import (
	"sort"
	"strings"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
)

// torrents returns the torrents an item removes: those its rows name, each once, without the
// ones an earlier item of the plan removes already. A torrent that history does not associate
// with any of the rows is marked as linked by file size (spec 6.7).
func (m *maker) torrents(members []domain.Candidate) []domain.PlanTorrent {
	out := []domain.PlanTorrent{}
	seen := map[string]bool{}
	for _, o := range members {
		for _, t := range o.Torrents {
			if seen[t.ID] {
				continue
			}
			seen[t.ID] = true
			if _, removed := m.owner[t.ID]; removed {
				continue
			}
			out = append(out, domain.PlanTorrent{Torrent: t, LinkedBySize: !m.byHistory(members, t.ID)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Torrent.ID < out[j].Torrent.ID })
	return out
}

// byHistory reports whether history alone associates the download with one of the rows.
func (m *maker) byHistory(members []domain.Candidate, id string) bool {
	for _, o := range members {
		row := m.rows[rowID{o.List, o.Key}]
		for _, h := range m.in.Eval.History.Associated(row.Manager, row.ItemIDs, row.FileIDs) {
			if h == id {
				return true
			}
		}
	}
	return false
}

// kept returns the downloads in the client that are associated with an item's rows and that
// the rows do not name: they are in a queue, or they back a library file that stays (spec
// 6.10).
func (m *maker) kept(members []domain.Candidate) []domain.KeptTorrent {
	named := map[string]bool{}
	for _, o := range members {
		for _, t := range o.Torrents {
			named[t.ID] = true
		}
	}
	out := []domain.KeptTorrent{}
	seen := map[string]bool{}
	for _, o := range members {
		row := m.rows[rowID{o.List, o.Key}]
		for _, id := range m.in.Eval.Index.Associated(row.Manager, row.ItemIDs, row.FileIDs) {
			d, ok := m.downloads[id]
			if !ok || named[id] || seen[id] {
				continue
			}
			seen[id] = true
			reason := domain.KeptBacksLibrary
			if m.in.Eval.Index.Queued(id) {
				reason = domain.KeptQueued
			}
			out = append(out, domain.KeptTorrent{Torrent: scan.TorrentDetails(m.in.Rules, d), Reason: reason})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Torrent.ID < out[j].Torrent.ID })
	return out
}

// finish decides, once every item is known, which torrents keep their data: those whose
// content path another torrent that stays uses too, and those whose path is unknown (spec
// 6.7, shared data).
func (m *maker) finish() {
	removed := map[string]bool{}
	for _, it := range m.items {
		for _, t := range it.Torrents {
			removed[t.Torrent.ID] = true
		}
	}
	for i := range m.items {
		for j := range m.items[i].Torrents {
			t := &m.items[i].Torrents[j]
			t.KeepData = m.sharesData(t.Torrent.ID, removed)
		}
	}
}

// sharesData reports whether a removed download's data must stay.
func (m *maker) sharesData(id string, removed map[string]bool) bool {
	mine := cleanPath(m.downloads[id].ContentPath)
	if mine == "" {
		return true
	}
	for _, d := range m.in.Eval.Fetched.Downloads.Value {
		if removed[d.ID] {
			continue
		}
		if other := cleanPath(d.ContentPath); other != "" && overlaps(mine, other) {
			return true
		}
	}
	return false
}

// cleanPath drops trailing separators. A path of separators only becomes "", unknown.
func cleanPath(p string) string { return strings.TrimRight(p, `/\`) }

// overlaps reports whether two content paths are the same or one lies inside the other.
func overlaps(a, b string) bool { return a == b || within(a, b) || within(b, a) }

// within reports whether p lies inside the directory dir, with either kind of separator.
func within(p, dir string) bool {
	return strings.HasPrefix(p, dir+"/") || strings.HasPrefix(p, dir+`\`)
}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `gofmt -l . && go vet ./internal/planner/ && go test ./internal/planner/ && go test -race ./internal/planner/`
Expected: no file listed, all PASS.

- [ ] **Step 5: Break it once**

In scratch edits, one at a time: (a) in `sharesData` drop `if removed[d.ID] { continue }`; (b) make `within` use `strings.HasPrefix(p, dir)` without a separator; (c) in `kept`, drop the `named[id]` check. Each must make a test FAIL. Undo every edit.

- [ ] **Step 6: Commit**

```bash
git add internal/planner
git commit -m "feat: say how each torrent of a plan is linked, which stay, and whose data is kept" -m "The confirmation is the only guard against two unrelated files of the same size, so a torrent tied to an item only by size is marked. Torrents associated with the rows that stay are listed with the reason, and a torrent whose content path a torrent that stays uses keeps its data (spec 6.7, 6.10)." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: store plans with their expiry

**Files:**
- Create: `internal/store/migrations/0003_plans.sql`, `internal/store/plans.go`, `internal/store/plans_test.go`

**Interfaces:**
- Consumes: `domain.Plan`, `domain.Plan.Expired` (Task 4).
- Produces:
  - `func (*store.Store) SavePlan(ctx context.Context, p domain.Plan) error`: stores a new plan and drops the plans that expired by its creation time.
  - `func (*store.Store) Plan(ctx context.Context, id string, now time.Time) (domain.Plan, error)`.
  - `store.ErrPlanNotFound`, `store.ErrPlanExpired`.

- [ ] **Step 1: Write the failing tests**

Create `internal/store/plans_test.go`:

```go
package store

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

func testPlan(id string, built time.Time) domain.Plan {
	return domain.Plan{
		ID: id, CreatedAt: built, ExpiresAt: built.Add(domain.PlanLifetime),
		Items: []domain.PlanItem{{
			Rows:     []domain.PlanRow{{Row: domain.Candidate{List: domain.ListMovies, Key: "tmdb:1", Title: "One", Torrents: []domain.TorrentInfo{}}}},
			Steps:    []domain.ManagerStep{{Manager: "movies", Action: domain.StepRemoveMovie, ItemID: 1}},
			Torrents: []domain.PlanTorrent{{Torrent: domain.TorrentInfo{ID: "m1", Name: "One.Torrent"}, KeepData: true}},
			Kept:     []domain.KeptTorrent{},
		}},
		Skipped: []domain.SkippedRow{{Row: domain.RowRef{List: domain.ListSeries, Key: "tvdb:1"}, Reason: "It is ignored."}},
	}
}

func TestASavedPlanCanBeReadUntilItExpires(t *testing.T) {
	s := open(t)
	built := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	want := testPlan("abc", built)
	if err := s.SavePlan(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Plan(ctx, "abc", built.Add(domain.PlanLifetime-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("read %+v, want %+v", got, want)
	}
	if _, err := s.Plan(ctx, "abc", built.Add(domain.PlanLifetime)); !errors.Is(err, ErrPlanExpired) {
		t.Errorf("at ten minutes: err = %v, want ErrPlanExpired", err)
	}
}

func TestAnUnknownPlanIsNotFound(t *testing.T) {
	if _, err := open(t).Plan(ctx, "nope", time.Now()); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("err = %v, want ErrPlanNotFound", err)
	}
}

func TestSavingAPlanDropsThePlansThatExpired(t *testing.T) {
	s := open(t)
	built := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	for _, p := range []domain.Plan{
		testPlan("old", built),
		testPlan("recent", built.Add(5*time.Minute)),
		testPlan("new", built.Add(domain.PlanLifetime)),
	} {
		if err := s.SavePlan(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if n := count(t, s, `SELECT COUNT(*) FROM plans`); n != 2 {
		t.Errorf("%d plans stored, want 2: the expired one is dropped", n)
	}
	if _, err := s.Plan(ctx, "old", built); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("old plan: err = %v, want ErrPlanNotFound", err)
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/store/`
Expected: build failure: `s.SavePlan undefined`, `undefined: ErrPlanExpired`.

- [ ] **Step 3: Implement**

Create `internal/store/migrations/0003_plans.sql`:

```sql
CREATE TABLE plans (
  id         TEXT PRIMARY KEY,
  created_at TEXT NOT NULL,
  expires_at INTEGER NOT NULL, -- Unix time in nanoseconds, so that expiry compares as a number
  data       TEXT NOT NULL
);
```

Create `internal/store/plans.go`:

```go
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

var (
	// ErrPlanNotFound is returned for a plan ID that is not stored.
	ErrPlanNotFound = errors.New("no such plan")
	// ErrPlanExpired is returned for a plan that can no longer be executed (spec 6.8, rule 2).
	ErrPlanExpired = errors.New("the plan has expired")
)

// SavePlan stores a new plan. The plans that expired by the time it was created are dropped
// in the same transaction.
func (s *Store) SavePlan(ctx context.Context, p domain.Plan) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM plans WHERE expires_at <= ?`, p.CreatedAt.UnixNano()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO plans (id, created_at, expires_at, data) VALUES (?, ?, ?, ?)`,
		p.ID, formatTime(p.CreatedAt), p.ExpiresAt.UnixNano(), string(data)); err != nil {
		return err
	}
	return tx.Commit()
}

// Plan returns the stored plan with this ID. It returns ErrPlanNotFound when there is none,
// and ErrPlanExpired when the plan expired by now.
func (s *Store) Plan(ctx context.Context, id string, now time.Time) (domain.Plan, error) {
	var data string
	err := s.db.QueryRowContext(ctx, `SELECT data FROM plans WHERE id = ?`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Plan{}, ErrPlanNotFound
	}
	if err != nil {
		return domain.Plan{}, err
	}
	var p domain.Plan
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return domain.Plan{}, fmt.Errorf("plan %s: %w", id, err)
	}
	if p.Expired(now) {
		return domain.Plan{}, ErrPlanExpired
	}
	return p, nil
}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `gofmt -l . && go test ./internal/store/ && go test -race ./internal/store/`
Expected: no file listed, all PASS. If `reflect.DeepEqual` fails only on the time zone of `CreatedAt` after the JSON round trip, build `built` in UTC (as the test does) rather than weakening the comparison.

- [ ] **Step 5: Commit**

```bash
git add internal/store
git commit -m "feat: store plans with their expiry" -m "A plan can be executed for ten minutes after it was built (spec 6.8, rule 2, 7.5). Expiry is stored as a number so that it compares correctly; saving a plan drops the ones that have expired." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: the planner service and `POST /api/plans`

**Files:**
- Create: `internal/planner/service.go`, `internal/planner/service_test.go`
- Create: `internal/api/plans.go`, `internal/api/plans_test.go`
- Modify: `internal/api/server.go` (interface `Planner`, field `Server.Planner`, route), `internal/api/server_test.go` (harness gets a fake planner)
- Modify: `cmd/removarr/main.go` (wiring)
- Modify: `AGENTS.md` (Layout table: add `internal/planner`)

**Interfaces:**
- Consumes: `planner.Make`, `planner.Input` (Task 4); `scanner.Read`, `scanner.Evaluate`, `scanner.Providers` (Task 3); `store.Latest`, `(*store.Store).SavePlan` (Task 6); `readRows` in `internal/api/ignores.go`.
- Produces:
  - `type planner.Store interface { ReadLatest(ctx context.Context) (store.Latest, error); SavePlan(ctx context.Context, p domain.Plan) error }`
  - `type planner.Service struct { Build func(context.Context) (scanner.Providers, error); Rules func(context.Context) (domain.Rules, error); Store planner.Store; Now func() time.Time; NewID func() (string, error) }` (NewID nil means 16 random bytes in hex).
  - `func (*planner.Service) Plan(ctx context.Context, selected []domain.RowRef) (domain.Plan, error)`.
  - `type api.Planner interface { Plan(ctx context.Context, rows []domain.RowRef) (domain.Plan, error) }`, field `api.Server.Planner`.
  - `POST /api/plans` with body `{"rows":[{"list":…,"key":…}]}`: 201 and the plan as JSON.

- [ ] **Step 1: Write the failing service tests**

Create `internal/planner/service_test.go`:

```go
package planner

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scanner"
	"github.com/removarr/removarr/internal/store"
)

// Providers that answer with a fixed read.

type fakeServer struct{ lib domain.Library }

func (f fakeServer) Users(context.Context) ([]domain.User, error) {
	return []domain.User{{ID: "a", Name: "Alice"}}, nil
}
func (f fakeServer) Library(context.Context, []string) (domain.Library, error) { return f.lib, nil }

type fakeMovies struct {
	snap domain.MovieSnapshot
	err  error
}

func (f fakeMovies) Movies(context.Context) (domain.MovieSnapshot, error) { return f.snap, f.err }

type fakeSeries struct{ snap domain.SeriesSnapshot }

func (f fakeSeries) Series(context.Context) (domain.SeriesSnapshot, error) { return f.snap, nil }

type fakeDownloads struct{ list []domain.Download }

func (f fakeDownloads) Downloads(context.Context) ([]domain.Download, error) { return f.list, nil }
func (f fakeDownloads) Files(context.Context, string) ([]domain.DownloadFile, error) {
	return nil, nil
}

func providersOf(f scanner.Fetched) scanner.Providers {
	return scanner.Providers{
		MediaServer: fakeServer{f.Library.Value},
		Movies:      fakeMovies{snap: f.Movies.Value},
		Series:      fakeSeries{f.Series.Value},
		Downloads:   fakeDownloads{f.Downloads.Value},
	}
}

type fakeStore struct {
	latest  store.Latest
	readErr error
	saved   []domain.Plan
}

func (s *fakeStore) ReadLatest(context.Context) (store.Latest, error) { return s.latest, s.readErr }
func (s *fakeStore) SavePlan(_ context.Context, p domain.Plan) error {
	s.saved = append(s.saved, p)
	return nil
}

func service(p scanner.Providers, st *fakeStore) *Service {
	return &Service{
		Build: func(context.Context) (scanner.Providers, error) { return p, nil },
		Rules: func(context.Context) (domain.Rules, error) { return rules(), nil },
		Store: st,
		Now:   func() time.Time { return now },
		NewID: func() (string, error) { return "plan1", nil },
	}
}

func TestPlanReadsTheAppsAndStoresThePlan(t *testing.T) {
	f := world()
	st := &fakeStore{latest: store.Latest{Rows: scanned(rules(), f), Ignores: []domain.Ignore{}}}
	got, err := service(providersOf(f), st).Plan(context.Background(), []domain.RowRef{movieOne})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "plan1" || !got.CreatedAt.Equal(now) || !got.ExpiresAt.Equal(now.Add(domain.PlanLifetime)) {
		t.Errorf("plan = %s, %v, %v; want plan1 built now, expiring in ten minutes", got.ID, got.CreatedAt, got.ExpiresAt)
	}
	if len(got.Items) != 1 || !reflect.DeepEqual(torrentIDs(got.Items[0]), []string{"m1"}) {
		t.Errorf("items = %+v, want movie One with m1", got.Items)
	}
	if len(st.saved) != 1 || !reflect.DeepEqual(st.saved[0], got) {
		t.Errorf("saved = %+v, want the plan returned", st.saved)
	}
}

func TestPlanSkipsTheRowsOfAnAppThatDoesNotAnswer(t *testing.T) {
	f := world()
	st := &fakeStore{latest: store.Latest{Rows: scanned(rules(), f), Ignores: []domain.Ignore{}}}
	p := providersOf(f)
	p.Movies = fakeMovies{err: errors.New("connection refused")}
	got, err := service(p, st).Plan(context.Background(), []domain.RowRef{movieOne, theShow})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 0 || len(got.Skipped) != 2 || !strings.Contains(got.Skipped[0].Reason, "connection refused") {
		t.Errorf("plan = %+v, want both rows skipped with the movie manager's error", got)
	}
}

func TestPlanAppliesTheCurrentIgnores(t *testing.T) {
	f := world()
	st := &fakeStore{latest: store.Latest{
		Rows:    scanned(rules(), f),
		Ignores: []domain.Ignore{{Kind: domain.IgnoreMovie, Key: "tmdb:1", Title: "One"}},
	}}
	got, err := service(providersOf(f), st).Plan(context.Background(), []domain.RowRef{movieOne})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 0 || len(got.Skipped) != 1 {
		t.Errorf("plan = %+v, want tmdb:1 skipped as ignored", got)
	}
}

func TestPlanFailsWhenTheLatestScanCannotBeRead(t *testing.T) {
	st := &fakeStore{readErr: errors.New("disk full")}
	if _, err := service(providersOf(world()), st).Plan(context.Background(), []domain.RowRef{movieOne}); err == nil {
		t.Error("err = nil, want the store's error")
	}
	if len(st.saved) != 0 {
		t.Error("a plan was saved")
	}
}

func TestPlanIDsAreRandom(t *testing.T) {
	a, errA := randomID()
	b, errB := randomID()
	if errA != nil || errB != nil || len(a) != 32 || a == b {
		t.Errorf("randomID = %q, %q (%v, %v); want two different 32-character IDs", a, b, errA, errB)
	}
}
```

Add `"time"` to the imports of `service_test.go`.

Run: `go test ./internal/planner/` — Expected: build failure, `undefined: Service`, `undefined: randomID`.

- [ ] **Step 2: Implement the service**

Create `internal/planner/service.go`:

```go
package planner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
	"github.com/removarr/removarr/internal/scanner"
	"github.com/removarr/removarr/internal/store"
)

// Store is the persistence planning needs. *store.Store satisfies it.
type Store interface {
	ReadLatest(ctx context.Context) (store.Latest, error)
	SavePlan(ctx context.Context, p domain.Plan) error
}

// Service builds plans, one at a time: each reads every app, as a scan does.
type Service struct {
	Build func(ctx context.Context) (scanner.Providers, error) // providers from the current settings
	Rules func(ctx context.Context) (domain.Rules, error)      // the current rules
	Store Store
	Now   func() time.Time
	NewID func() (string, error) // nil means 16 random bytes in hex

	mu sync.Mutex
}

// Plan builds the plan for the selected rows from live data, stores it with its expiry and
// returns it (spec 6.8, 6.10). An app that does not answer skips the rows that need it; only
// a failure to load the settings, read the latest scan or save the plan is an error.
func (s *Service) Plan(ctx context.Context, selected []domain.RowRef) (domain.Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rules, err := s.Rules(ctx)
	if err != nil {
		return domain.Plan{}, fmt.Errorf("load rules: %w", err)
	}
	latest, err := s.Store.ReadLatest(ctx)
	if err != nil {
		return domain.Plan{}, fmt.Errorf("read the latest scan: %w", err)
	}
	p, err := s.Build(ctx)
	if err != nil {
		return domain.Plan{}, fmt.Errorf("load connections: %w", err)
	}
	ignores := scan.NewIgnoreSet(latest.Ignores)
	f := scanner.Read(ctx, p, rules, nil)
	f.Ignored = ignores.Keys()
	now := s.Now()
	items, skipped := Make(Input{
		Rules: rules, Now: now, Eval: scanner.Evaluate(rules, now, f), Ignores: ignores,
		Latest: latest.Rows, Selected: selected,
	})

	newID := s.NewID
	if newID == nil {
		newID = randomID
	}
	id, err := newID()
	if err != nil {
		return domain.Plan{}, fmt.Errorf("make a plan ID: %w", err)
	}
	plan := domain.Plan{ID: id, CreatedAt: now, ExpiresAt: now.Add(domain.PlanLifetime), Items: items, Skipped: skipped}
	if err := s.Store.SavePlan(ctx, plan); err != nil {
		return domain.Plan{}, fmt.Errorf("save plan: %w", err)
	}
	return plan, nil
}

// randomID returns 16 random bytes in hex.
func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
```

Run: `gofmt -l . && go test ./internal/planner/ && go test -race ./internal/planner/` — Expected: PASS.

- [ ] **Step 3: Write the failing API tests**

In `internal/api/server_test.go`, add a fake planner and wire it into the harness:

```go
type fakePlanner struct {
	rows []domain.RowRef
	plan domain.Plan
	err  error
}

func (f *fakePlanner) Plan(_ context.Context, rows []domain.RowRef) (domain.Plan, error) {
	f.rows = rows
	return f.plan, f.err
}
```

Add a field `planner *fakePlanner` to `harness`, set `planner: &fakePlanner{}` in `newHarness`, and pass `Planner: h.planner` in the `Server` literal. Add `"github.com/removarr/removarr/internal/domain"` to the imports if it is not there.

Create `internal/api/plans_test.go`:

```go
package api

import (
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

func TestBuildPlanPassesTheRowsAndReturnsThePlan(t *testing.T) {
	h := newHarness(t, nil)
	built := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	h.planner.plan = domain.Plan{
		ID: "abc", CreatedAt: built, ExpiresAt: built.Add(domain.PlanLifetime),
		Items:   []domain.PlanItem{},
		Skipped: []domain.SkippedRow{{Row: domain.RowRef{List: domain.ListMovies, Key: "tmdb:1"}, Reason: "It is ignored."}},
	}
	rec := h.do(http.MethodPost, "/api/plans", ignoreBody("movies/tmdb:1", "torrents/hash:abc"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d %s, want 201", rec.Code, rec.Body)
	}
	want := []domain.RowRef{{List: domain.ListMovies, Key: "tmdb:1"}, {List: domain.ListTorrents, Key: "hash:abc"}}
	if !reflect.DeepEqual(h.planner.rows, want) {
		t.Errorf("planner got %+v, want %+v", h.planner.rows, want)
	}
	if got := decode[domain.Plan](t, rec); !reflect.DeepEqual(got, h.planner.plan) {
		t.Errorf("response = %+v, want %+v", got, h.planner.plan)
	}
}

func TestBuildPlanRejectsABadBody(t *testing.T) {
	h := newHarness(t, nil)
	for _, body := range []string{`{"rows":[]}`, `{"rows":[{"list":"films","key":"x"}]}`, `not json`} {
		if rec := h.do(http.MethodPost, "/api/plans", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", body, rec.Code)
		}
	}
	if h.planner.rows != nil {
		t.Errorf("the planner was asked for %+v", h.planner.rows)
	}
}

func TestBuildPlanGoesThroughTheGuard(t *testing.T) {
	h := newHarness(t, nil)
	rec := h.do(http.MethodPost, "/api/plans", ignoreBody("movies/tmdb:1"), func(r *http.Request) { r.Header.Del(RequestHeader) })
	if rec.Code != http.StatusForbidden || h.planner.rows != nil {
		t.Errorf("status = %d, planner asked = %v; want 403 and no plan", rec.Code, h.planner.rows != nil)
	}
}

func TestBuildPlanFailureIsAServerError(t *testing.T) {
	h := newHarness(t, nil)
	h.planner.err = errors.New("disk full")
	if rec := h.do(http.MethodPost, "/api/plans", ignoreBody("movies/tmdb:1")); rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}
```

Run: `go test ./internal/api/` — Expected: build failure on `Planner` in the `Server` literal, or 404/405 for `/api/plans`.

- [ ] **Step 4: Implement the route**

In `internal/api/server.go`, after the `Apps` interface:

```go
// Planner builds plans for deletions. *planner.Service satisfies it.
type Planner interface {
	Plan(ctx context.Context, rows []domain.RowRef) (domain.Plan, error)
}
```

Add `Planner Planner` to struct `Server` after `Apps`, and in `Handler` after the ignore routes:

```go
	mux.HandleFunc("POST /api/plans", s.buildPlan)
```

Create `internal/api/plans.go`:

```go
package api

import (
	"net/http"

	"github.com/removarr/removarr/internal/domain"
)

// buildPlan builds the plan for deleting the selected rows from live data and returns it for
// confirmation (spec 6.8, rule 1; 6.10). The body names the rows as an ignore request does.
// Building reads every app, so it takes about as long as a scan.
func (s *Server) buildPlan(w http.ResponseWriter, r *http.Request) {
	rows, err := readRows(w, r)
	if err != nil {
		s.fail(w, err)
		return
	}
	refs := make([]domain.RowRef, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, domain.RowRef{List: row.List, Key: row.Key})
	}
	plan, err := s.Planner.Plan(r.Context(), refs)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, plan)
}
```

In `internal/api/ignores.go`, change the comment of `readRows` from "the body of an ignore request" to "the body of a request that names rows (ignore, un-ignore, plan)".

Run: `gofmt -l . && go test ./internal/api/ && go test -race ./internal/api/` — Expected: PASS.

- [ ] **Step 5: Wire it into the binary**

In `cmd/removarr/main.go`, import `"github.com/removarr/removarr/internal/planner"` and after `scans := &scanner.Service{…}` add:

```go
	plans := &planner.Service{
		Build: apps.Providers,
		Rules: cfg.Rules,
		Store: st,
		Now:   time.Now,
	}
```

and add `Planner: plans` to the `api.Server` literal.

In `AGENTS.md`, in the Layout table, add a row after `internal/scanner`:

```
| `internal/planner` | Builds the plan for a deletion: reads and evaluates through `scanner`, then `Make` decides the items, steps and skips. Executes nothing |
```

And in the Commands section, add `./internal/planner/` to the race test line:

```
go test -race ./internal/api/ ./internal/config/ ./internal/store/ ./internal/scanner/ ./internal/scan/ ./internal/planner/
```

(Also change that line in `.agents/skills/implement-issue/SKILL.md` only if it repeats the list; it refers to `AGENTS.md`, so it normally does not.)

- [ ] **Step 6: Run every check**

Run:

```
gofmt -l .
go vet ./...
go test ./...
go test -race ./internal/api/ ./internal/config/ ./internal/store/ ./internal/scanner/ ./internal/scan/ ./internal/planner/
go build ./...
```

Expected: `gofmt` prints nothing, everything else passes.

- [ ] **Step 7: Break it once**

In a scratch edit, drop `f.Ignored = ignores.Keys()` from `Service.Plan`: no test is expected to fail, because Make applies the ignores through `Holds` anyway; confirm that, then undo. In another scratch edit, drop `Planner: h.planner` from the harness: the API tests must FAIL (nil pointer). Undo.

- [ ] **Step 8: Commit**

```bash
git add internal/planner internal/api cmd/removarr AGENTS.md
git commit -m "feat: build a plan for the selected rows through POST /api/plans" -m "The planner service reads every app, makes the plan with the scan's rules and stores it with its ten-minute expiry; the API returns it for confirmation (spec 6.8, 6.10, 7.6). Plans are built one at a time because each reads the whole library." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
