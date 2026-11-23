# Stage 2b: ignore and un-ignore — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The user can hide rows they do not want suggested, review the hidden rows, and bring them back (issue #3).

**Architecture:** A scan stores every qualifying row, also those the whole-torrent rule holds back (marked `waiting`). Ignores live in their own table. Each time a list is read, a pure function in `scan` applies the current ignores to the stored rows: an ignored row is hidden, a row tied to a hidden row waits, and the summary counts follow. So an ignore takes effect at once and gives the same lists as a rescan (spec 6.5). The scan itself reads the ignores only for the per-season merge.

**Tech Stack:** Go 1.27, standard library, `modernc.org/sqlite`.

**Spec:** `docs/superpowers/specs/2026-11-18-removarr-design.md`, sections 5.3, 6.3 (whole torrents), 6.5, 7.5, 7.6, 10. Read 6.5 in full before any task: it was amended for this plan.

## Global Constraints

- Go 1.27 and the standard library; the only third-party module is `modernc.org/sqlite`. No new module.
- No call that changes anything in an app. This stage only reads the apps.
- Every API route goes through `guard` in `internal/api/server.go`. Requests other than GET must carry `X-Removarr-Request`, be `application/json`, and come from this server's origin when they name one.
- The store has one database connection. Inside a transaction use the transaction, never `s.db`.
- Anything read together goes into one transaction (`LEARNINGS.md`, "Storage and concurrency").
- Never print titles, names, URLs or keys from a real library. Tests use invented data.
- Test first; see it fail for the expected reason. Name tests after what must hold.
- The checks: `gofmt -l .` prints nothing, `go vet ./...`, `go test ./...`, `go test -race ./internal/api/ ./internal/config/ ./internal/store/ ./internal/scanner/ ./internal/scan/`, `go build ./...`.
- Commit subjects: `type: what changed`, lower case, `feat|fix|docs|test|chore`, body says why, ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

- A row ignored before a new scan stays hidden after it: the scan stores the row, the read hides it (Task 3 store test, Task 6 API test).
- Ignoring a movie that is tied to a series row makes the series row wait, across lists (Task 2).
- A request from a list that is out of date names a row the latest scan no longer has: 400, and none of the request's rows is saved (Task 6).
- The same row ignored twice, or twice in one request: no error, and the first title and time stay (Tasks 3 and 6).
- A season ignore in whole-series mode has no row, yet shows in the ignored view and can be un-ignored (Tasks 2 and 6).

---

### Task 1: domain types for ignores, and waiting rows kept by the whole-torrent rule

**Files:**
- Create: `internal/domain/ignore.go`, `internal/domain/ignore_test.go`
- Modify: `internal/domain/candidate.go` (struct `Candidate`), `internal/domain/json_test.go`
- Modify: `internal/scan/torrents.go` (struct `ListResult`), `internal/scan/whole.go` (func `WholeTorrents`)
- Test: `internal/scan/whole_test.go`

**Interfaces:**
- Produces:
  - `domain.Candidate` gains `Waiting bool` (`json:"waiting,omitempty"`), `IgnoredAt *time.Time` (`json:"ignoredAt,omitempty"`), `NotInScan bool` (`json:"notInScan,omitempty"`).
  - `domain.IgnoreMovie`, `domain.IgnoreSeries`, `domain.IgnoreSeason`, `domain.IgnoreTorrent` (string constants `"movie"`, `"series"`, `"season"`, `"torrent"`).
  - `type domain.Ignore struct { Kind, Key, Title string; CreatedAt time.Time }`, `func (domain.Ignore) List() domain.ListKind`.
  - `func domain.IgnoreKind(c domain.Candidate) string`.
  - `func domain.ParseSeasonKey(key string) (seriesKey string, season int, ok bool)`.
  - `scan.ListResult` gains `Held []domain.Candidate`: the rows `WholeTorrents` holds back, with `Waiting` set, sorted by key. `Waiting` stays and equals `len(Held)`.

- [ ] **Step 1: Write the failing domain tests**

`internal/domain/ignore_test.go`:

```go
package domain

import "testing"

func TestParseSeasonKeyAcceptsOnlyKeysThatSeasonKeyMakes(t *testing.T) {
	tests := []struct {
		key    string
		series string
		season int
		ok     bool
	}{
		{SeasonKey("100", 2), "tvdb:100", 2, true},
		{SeasonKey("100", 0), "tvdb:100", 0, true},
		{SeasonKey("100", 12), "tvdb:100", 12, true},
		{SeriesKey("100"), "", 0, false},
		{"tvdb:100:s02", "", 0, false},
		{"tvdb:100:s+2", "", 0, false},
		{"tvdb:100:s-1", "", 0, false},
		{"tvdb::s2", "", 0, false},
		{"tmdb:100:s2", "", 0, false},
		{"hash:abc", "", 0, false},
	}
	for _, tt := range tests {
		series, season, ok := ParseSeasonKey(tt.key)
		if series != tt.series || season != tt.season || ok != tt.ok {
			t.Errorf("ParseSeasonKey(%q) = %q, %d, %v; want %q, %d, %v", tt.key, series, season, ok, tt.series, tt.season, tt.ok)
		}
	}
}

func TestIgnoreKindAndListFollowTheRow(t *testing.T) {
	tests := []struct {
		row  Candidate
		kind string
		list ListKind
	}{
		{Candidate{List: ListMovies, Key: "tmdb:603"}, IgnoreMovie, ListMovies},
		{Candidate{List: ListSeries, Key: SeriesKey("100"), Scope: ScopeSeries}, IgnoreSeries, ListSeries},
		{Candidate{List: ListSeries, Key: SeasonKey("100", 2), Scope: ScopeSeason, Season: 2}, IgnoreSeason, ListSeries},
		{Candidate{List: ListTorrents, Key: TorrentKey("abc")}, IgnoreTorrent, ListTorrents},
	}
	for _, tt := range tests {
		kind := IgnoreKind(tt.row)
		if kind != tt.kind {
			t.Errorf("IgnoreKind(%s) = %q, want %q", tt.row.Key, kind, tt.kind)
		}
		if list := (Ignore{Kind: kind, Key: tt.row.Key}).List(); list != tt.list {
			t.Errorf("list of a %s ignore = %q, want %q", kind, list, tt.list)
		}
	}
}
```

Append to `TestJSONNamesOfTheTiedAndOutsideAndWaitingFields` in `internal/domain/json_test.go`, before its closing brace (add `"time"` to its imports):

```go
	at := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	has(keys(Candidate{Waiting: true, IgnoredAt: &at, NotInScan: true}), "waiting", true)
	has(keys(Candidate{Waiting: true, IgnoredAt: &at, NotInScan: true}), "ignoredAt", true)
	has(keys(Candidate{Waiting: true, IgnoredAt: &at, NotInScan: true}), "notInScan", true)
	has(keys(Candidate{}), "waiting", false)
	has(keys(Candidate{}), "ignoredAt", false)
	has(keys(Candidate{}), "notInScan", false)
```

- [ ] **Step 2: Run the domain tests and see them fail**

Run: `go test ./internal/domain/`
Expected: build failure, `undefined: ParseSeasonKey`, `IgnoreKind`, `Ignore`, unknown fields `Waiting`, `IgnoredAt`, `NotInScan`.

- [ ] **Step 3: Implement the domain types**

In `internal/domain/candidate.go`, add to `Candidate` after `TiedWith`:

```go
	Waiting      bool          `json:"waiting,omitempty"`   // held back by the whole-torrent rule (spec 6.3); only the ignored view returns such rows
	IgnoredAt    *time.Time    `json:"ignoredAt,omitempty"` // ignored view: when the user ignored the row
	NotInScan    bool          `json:"notInScan,omitempty"` // ignored view: the latest scan has no such row, so only its key and title are known
```

Create `internal/domain/ignore.go`:

```go
package domain

import (
	"strconv"
	"strings"
	"time"
)

// The kinds of ignore (spec 6.5).
const (
	IgnoreMovie   = "movie"
	IgnoreSeries  = "series"
	IgnoreSeason  = "season"
	IgnoreTorrent = "torrent"
)

// Ignore is a row the user chose to hide (spec 6.5). It is kept by the row's stable key.
type Ignore struct {
	Kind      string // IgnoreMovie, IgnoreSeries, IgnoreSeason or IgnoreTorrent
	Key       string // the row's stable key
	Title     string // the row's title when it was ignored
	CreatedAt time.Time
}

// List returns the list whose rows the ignore can hide.
func (i Ignore) List() ListKind {
	switch i.Kind {
	case IgnoreMovie:
		return ListMovies
	case IgnoreTorrent:
		return ListTorrents
	default:
		return ListSeries
	}
}

// IgnoreKind returns the kind of ignore that hides the row.
func IgnoreKind(c Candidate) string {
	switch {
	case c.List == ListMovies:
		return IgnoreMovie
	case c.List == ListTorrents:
		return IgnoreTorrent
	case c.Scope == ScopeSeason:
		return IgnoreSeason
	default:
		return IgnoreSeries
	}
}

// ParseSeasonKey splits the key of a season row into the key of its series and the season
// number. It accepts exactly the keys SeasonKey makes.
func ParseSeasonKey(key string) (seriesKey string, season int, ok bool) {
	i := strings.LastIndex(key, ":s")
	if i < 0 || !strings.HasPrefix(key, "tvdb:") {
		return "", 0, false
	}
	tvdb := key[len("tvdb:"):i]
	n, err := strconv.Atoi(key[i+2:])
	if err != nil || n < 0 || tvdb == "" || SeasonKey(tvdb, n) != key {
		return "", 0, false
	}
	return SeriesKey(tvdb), n, true
}
```

- [ ] **Step 4: Run the domain tests and see them pass**

Run: `go test ./internal/domain/`
Expected: PASS.

- [ ] **Step 5: Write the failing whole-torrent test**

Add to `internal/scan/whole_test.go`:

```go
// The rows the rule holds back are kept, so that the lists can apply ignores later without
// a new scan (spec 6.5).
func TestWholeTorrentsKeepsTheRowsItHoldsBack(t *testing.T) {
	x := NewLinkIndex(ManagerFiles{
		Manager: ManagerSeries,
		Links:   link(map[string][]int{"pack": {1, 2}}, map[int]string{11: "pack", 21: "pack"}),
		Files:   libFiles(11, 21),
	})
	series := &ListResult{Rows: []Row{
		seasonRow(1, []int{1}, []int{11}), // the pack also backs file 21, which stays
		seasonRow(3, []int{3}, []int{31}), // backed by nothing
	}}
	WholeTorrents(domain.DefaultRules(), x, []domain.Download{dl("pack")}, series)

	if len(series.Candidates) != 1 || series.Candidates[0].Key != domain.SeasonKey("100", 3) || series.Candidates[0].Waiting {
		t.Fatalf("listed %+v, want season 3 alone and not marked waiting", series.Candidates)
	}
	if series.Waiting != 1 || len(series.Held) != 1 {
		t.Fatalf("Waiting = %d, Held = %+v; want one row held back", series.Waiting, series.Held)
	}
	h := series.Held[0]
	if h.Key != domain.SeasonKey("100", 1) || !h.Waiting || h.Torrents == nil || len(h.Torrents) != 0 || h.TiedWith != nil || h.BelowSeedMin {
		t.Errorf("held row = %+v; want season 1, marked waiting, with no torrents and no ties", h)
	}
}
```

In `runWhole`, inside `check`, after the `res.Waiting` comparison, add:

```go
		if len(res.Held) != res.Waiting {
			t.Errorf("%s: %d rows held, Waiting = %d; want them equal", list, len(res.Held), res.Waiting)
		}
		for _, h := range res.Held {
			if !h.Waiting {
				t.Errorf("%s: held row %s is not marked waiting", list, h.Key)
			}
		}
```

and give `check` the `res.Candidates` loop a check that no listed row has `Waiting` set:

```go
			if c.Waiting {
				t.Errorf("%s: listed row %s is marked waiting", list, c.Key)
			}
```

In the test that calls `WholeTorrents` a second time (search for `second call`), also compare `movies.Held` before and after with `reflect.DeepEqual`, so a second call does not keep the held rows of the first.

- [ ] **Step 6: Run the scan tests and see them fail**

Run: `go test ./internal/scan/`
Expected: build failure, `series.Held undefined`.

- [ ] **Step 7: Keep the held rows**

In `internal/scan/torrents.go`, add to `ListResult` after `Waiting`:

```go
	Held       []domain.Candidate // watched lists: the rows held back by the whole-torrent rule, marked Waiting
```

In `internal/scan/whole.go`, in `WholeTorrents`:

- where the lists are reset, write `l.Candidates, l.Held, l.Waiting = nil, nil, 0`;
- replace the body of `if waits { ... }` with:

```go
		if waits {
			for _, i := range group {
				c := rows[i].row.Candidate
				c.Torrents, c.BelowSeedMin, c.TiedWith, c.Waiting = []domain.TorrentInfo{}, false, nil, true
				rows[i].list.Held = append(rows[i].list.Held, c)
				rows[i].list.Waiting++
			}
			continue
		}
```

- in the final loop, also `sortByKey(l.Held)`.
- in the doc comment of `WholeTorrents`, change "It fills Candidates and Waiting of each list from its Rows." to "It fills Candidates, Held and Waiting of each list from its Rows: Held keeps the rows of groups that wait, marked Waiting, so that ignores can be applied to the stored rows later (spec 6.5)."

- [ ] **Step 8: Run the scan and domain tests and see them pass**

Run: `go test ./internal/scan/ ./internal/domain/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/domain internal/scan
git commit -m "feat: keep the rows the whole-torrent rule holds back, and add ignore types"
```

Body: why the held rows are kept (ignores are applied on read, spec 6.5).

---

### Task 2: apply ignores to the stored rows of a list

**Files:**
- Create: `internal/scan/ignores.go`, `internal/scan/ignores_test.go`
- Modify: `internal/scan/series.go` (doc comment of `SeriesInput.Ignored`)

**Interfaces:**
- Consumes: Task 1's `domain.Ignore`, `domain.ParseSeasonKey`, `Candidate.Waiting/IgnoredAt/NotInScan`; the existing `sortByKey([]domain.Candidate)` in `internal/scan`.
- Produces:
  - `type scan.IgnoreSet map[string]domain.Ignore` and `func scan.NewIgnoreSet([]domain.Ignore) scan.IgnoreSet`.
  - `func (scan.IgnoreSet) Keys() map[string]bool`.
  - `func (scan.IgnoreSet) Hides(list domain.ListKind, key string) bool`.
  - `type scan.ListView struct { Shown, Ignored []domain.Candidate; Waiting int }`.
  - `func scan.View(list domain.ListKind, rows []domain.Candidate, ignores scan.IgnoreSet) scan.ListView`.

The rule this task builds, from spec 6.3 and 6.5: a row is **hidden** when its own key is ignored, or when it is a season row and its series is ignored. A season ignore never hides a series row. A row that is not hidden is **shown** when it was listed by the scan and none of the rows it is tied to (`TiedWith`, any list) is hidden; otherwise it **waits**. Hidden rows count nowhere. The ignored view has one entry per ignore of the list.

- [ ] **Step 1: Write the failing tests**

`internal/scan/ignores_test.go`:

```go
package scan

import (
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

var ignoredAt = time.Date(2026, 3, 11, 9, 0, 0, 0, time.UTC)

func ignore(kind, key string) domain.Ignore {
	return domain.Ignore{Kind: kind, Key: key, Title: "Title of " + key, CreatedAt: ignoredAt}
}

func tie(list domain.ListKind, key string) []domain.RowRef {
	return []domain.RowRef{{List: list, Key: key}}
}

// storedRows is what one invented scan stored: two movies, one of them tied to a series
// row; season rows of another series, one of them held back; a torrent.
func storedRows() []domain.Candidate {
	none := []domain.TorrentInfo{}
	return []domain.Candidate{
		{List: domain.ListMovies, Key: "tmdb:1", Torrents: none},
		{List: domain.ListMovies, Key: "tmdb:2", Torrents: none, TiedWith: tie(domain.ListSeries, "tvdb:100")},
		{List: domain.ListSeries, Key: "tvdb:100", Scope: domain.ScopeSeries, Torrents: none, TiedWith: tie(domain.ListMovies, "tmdb:2")},
		{List: domain.ListSeries, Key: "tvdb:200:s1", Scope: domain.ScopeSeason, Season: 1, Torrents: none},
		{List: domain.ListSeries, Key: "tvdb:200:s2", Scope: domain.ScopeSeason, Season: 2, Torrents: none, Waiting: true},
		{List: domain.ListSeries, Key: "tvdb:2000:s1", Scope: domain.ScopeSeason, Season: 1, Torrents: none},
		{List: domain.ListTorrents, Key: "hash:a", Torrents: none},
	}
}

type viewWant struct {
	shown   []string
	ignored []string
	waiting int
}

func keys(cs []domain.Candidate) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.Key)
	}
	return out
}

func TestViewAppliesIgnores(t *testing.T) {
	tests := []struct {
		name    string
		ignores []domain.Ignore
		want    map[domain.ListKind]viewWant
	}{
		{
			name: "nothing ignored: listed rows are shown and held rows wait",
			want: map[domain.ListKind]viewWant{
				domain.ListMovies:   {shown: []string{"tmdb:1", "tmdb:2"}, ignored: []string{}},
				domain.ListSeries:   {shown: []string{"tvdb:100", "tvdb:200:s1", "tvdb:2000:s1"}, ignored: []string{}, waiting: 1},
				domain.ListTorrents: {shown: []string{"hash:a"}, ignored: []string{}},
			},
		},
		{
			name:    "an ignored row is hidden and counted nowhere",
			ignores: []domain.Ignore{ignore(domain.IgnoreMovie, "tmdb:1"), ignore(domain.IgnoreTorrent, "hash:a")},
			want: map[domain.ListKind]viewWant{
				domain.ListMovies:   {shown: []string{"tmdb:2"}, ignored: []string{"tmdb:1"}},
				domain.ListSeries:   {shown: []string{"tvdb:100", "tvdb:200:s1", "tvdb:2000:s1"}, ignored: []string{}, waiting: 1},
				domain.ListTorrents: {shown: []string{}, ignored: []string{"hash:a"}},
			},
		},
		{
			name:    "a row tied to an ignored row waits, also in the other list",
			ignores: []domain.Ignore{ignore(domain.IgnoreMovie, "tmdb:2")},
			want: map[domain.ListKind]viewWant{
				domain.ListMovies:   {shown: []string{"tmdb:1"}, ignored: []string{"tmdb:2"}},
				domain.ListSeries:   {shown: []string{"tvdb:200:s1", "tvdb:2000:s1"}, ignored: []string{}, waiting: 2},
				domain.ListTorrents: {shown: []string{"hash:a"}, ignored: []string{}},
			},
		},
		{
			name:    "a series ignore hides its season rows, held ones too, and no other series",
			ignores: []domain.Ignore{ignore(domain.IgnoreSeries, "tvdb:200")},
			want: map[domain.ListKind]viewWant{
				domain.ListMovies:   {shown: []string{"tmdb:1", "tmdb:2"}, ignored: []string{}},
				domain.ListSeries:   {shown: []string{"tvdb:100", "tvdb:2000:s1"}, ignored: []string{"tvdb:200"}},
				domain.ListTorrents: {shown: []string{"hash:a"}, ignored: []string{}},
			},
		},
		{
			name:    "a season ignore never hides the series row",
			ignores: []domain.Ignore{ignore(domain.IgnoreSeason, "tvdb:100:s1")},
			want: map[domain.ListKind]viewWant{
				domain.ListMovies:   {shown: []string{"tmdb:1", "tmdb:2"}, ignored: []string{}},
				domain.ListSeries:   {shown: []string{"tvdb:100", "tvdb:200:s1", "tvdb:2000:s1"}, ignored: []string{"tvdb:100:s1"}, waiting: 1},
				domain.ListTorrents: {shown: []string{"hash:a"}, ignored: []string{}},
			},
		},
		{
			name:    "an ignored held row is no longer counted as waiting",
			ignores: []domain.Ignore{ignore(domain.IgnoreSeason, "tvdb:200:s2")},
			want: map[domain.ListKind]viewWant{
				domain.ListMovies:   {shown: []string{"tmdb:1", "tmdb:2"}, ignored: []string{}},
				domain.ListSeries:   {shown: []string{"tvdb:100", "tvdb:200:s1", "tvdb:2000:s1"}, ignored: []string{"tvdb:200:s2"}},
				domain.ListTorrents: {shown: []string{"hash:a"}, ignored: []string{}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := NewIgnoreSet(tt.ignores)
			for list, want := range tt.want {
				v := View(list, storedRows(), set)
				got := viewWant{shown: keys(v.Shown), ignored: keys(v.Ignored), waiting: v.Waiting}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s: got %+v, want %+v", list, got, want)
				}
			}
		})
	}
}

func TestViewIgnoredEntries(t *testing.T) {
	set := NewIgnoreSet([]domain.Ignore{
		ignore(domain.IgnoreSeason, "tvdb:200:s2"), // held back by the scan
		ignore(domain.IgnoreSeries, "tvdb:200"),    // no series row stored: per-season mode
		ignore(domain.IgnoreSeason, "tvdb:100:s3"), // no season row: whole-series mode
	})
	v := View(domain.ListSeries, storedRows(), set)
	if got := keys(v.Ignored); !reflect.DeepEqual(got, []string{"tvdb:100:s3", "tvdb:200", "tvdb:200:s2"}) {
		t.Fatalf("ignored entries = %v", got)
	}
	for _, c := range v.Ignored {
		if c.IgnoredAt == nil || !c.IgnoredAt.Equal(ignoredAt) || c.Torrents == nil || c.List != domain.ListSeries {
			t.Errorf("%s: %+v; want the ignore's time, a non-nil torrent list and the series list", c.Key, c)
		}
	}
	stale, series, held := v.Ignored[0], v.Ignored[1], v.Ignored[2]
	if !stale.NotInScan || stale.Title != "Title of tvdb:100:s3" || stale.Scope != domain.ScopeSeason || stale.Season != 3 {
		t.Errorf("season ignore without a row = %+v; want key, title, scope and season from the ignore", stale)
	}
	if !series.NotInScan || series.Scope != domain.ScopeSeries || series.Season != 0 {
		t.Errorf("series ignore without a row = %+v", series)
	}
	if held.NotInScan || !held.Waiting || held.Season != 2 {
		t.Errorf("ignored held row = %+v; want the stored row, still marked waiting", held)
	}
	if shown := View(domain.ListSeries, storedRows(), set).Shown; len(shown) == 0 || shown[0].IgnoredAt != nil {
		t.Errorf("shown rows = %+v; want rows without an ignore time", shown)
	}
}

func TestIgnoreSetHides(t *testing.T) {
	set := NewIgnoreSet([]domain.Ignore{
		ignore(domain.IgnoreSeries, "tvdb:100"),
		ignore(domain.IgnoreSeason, "tvdb:300:s1"),
		ignore(domain.IgnoreMovie, "tmdb:1"),
	})
	tests := []struct {
		list domain.ListKind
		key  string
		want bool
	}{
		{domain.ListSeries, "tvdb:100", true},
		{domain.ListSeries, "tvdb:100:s4", true},
		{domain.ListSeries, "tvdb:1000:s4", false},
		{domain.ListSeries, "tvdb:300:s1", true},
		{domain.ListSeries, "tvdb:300", false},
		{domain.ListSeries, "tvdb:300:s2", false},
		{domain.ListMovies, "tmdb:1", true},
		{domain.ListTorrents, "tmdb:1", false}, // an ignore hides rows of its own list only
	}
	for _, tt := range tests {
		if got := set.Hides(tt.list, tt.key); got != tt.want {
			t.Errorf("Hides(%s, %s) = %v, want %v", tt.list, tt.key, got, tt.want)
		}
	}
	if got := set.Keys(); !reflect.DeepEqual(got, map[string]bool{"tvdb:100": true, "tvdb:300:s1": true, "tmdb:1": true}) {
		t.Errorf("Keys() = %v", got)
	}
}
```

If `keys` clashes with an existing helper in package `scan` tests, rename it `viewKeys`. (`keysOf` already exists in `series_test.go` and returns nil for no rows; this one returns an empty slice.)

- [ ] **Step 2: Run the tests and see them fail**

Run: `go test ./internal/scan/ -run 'TestView|TestIgnoreSet'`
Expected: build failure, `undefined: NewIgnoreSet`, `View`.

- [ ] **Step 3: Implement**

`internal/scan/ignores.go`:

```go
package scan

import "github.com/removarr/removarr/internal/domain"

// IgnoreSet holds the user's ignores by key (spec 6.5).
type IgnoreSet map[string]domain.Ignore

// NewIgnoreSet indexes ignores by key.
func NewIgnoreSet(ignores []domain.Ignore) IgnoreSet {
	set := make(IgnoreSet, len(ignores))
	for _, i := range ignores {
		set[i.Key] = i
	}
	return set
}

// Keys returns the ignored keys, in the form SeriesInput.Ignored takes.
func (s IgnoreSet) Keys() map[string]bool {
	out := make(map[string]bool, len(s))
	for key := range s {
		out[key] = true
	}
	return out
}

// Hides reports whether an ignore hides the row with key in list: the row's own ignore, or
// for a season row the ignore of its series. A season ignore never hides a series row, so it
// has no effect in whole-series mode (spec 6.5).
func (s IgnoreSet) Hides(list domain.ListKind, key string) bool {
	if i, ok := s[key]; ok && i.List() == list {
		return true
	}
	if list != domain.ListSeries {
		return false
	}
	series, _, ok := domain.ParseSeasonKey(key)
	if !ok {
		return false
	}
	i, ok := s[series]
	return ok && i.Kind == domain.IgnoreSeries
}

// ListView is one list as the user sees it with the current ignores.
type ListView struct {
	Shown   []domain.Candidate // listed rows that are neither hidden nor tied to a hidden row
	Ignored []domain.Candidate // the ignored view: one entry per ignore of the list
	Waiting int                // rows held back by the whole-torrent rule or tied to a hidden row; hidden rows are not counted
}

// View applies the ignores to the rows the latest scan stored for a list, listed and held
// back alike (spec 6.3, 6.5). An ignored row is hidden and counted nowhere. A row tied to a
// hidden row, in any list, waits: its group lost a row, and the torrent or file the two
// shared now backs something that stays. An ignore whose row the scan did not store is
// shown in the ignored view by its key and title. Both slices are sorted by key and never
// nil.
func View(list domain.ListKind, rows []domain.Candidate, ignores IgnoreSet) ListView {
	v := ListView{Shown: []domain.Candidate{}, Ignored: []domain.Candidate{}}
	stored := map[string]domain.Candidate{}
	for _, c := range rows {
		if c.List != list {
			continue
		}
		stored[c.Key] = c
		switch {
		case ignores.Hides(list, c.Key):
		case c.Waiting || tiedToHidden(c, ignores):
			v.Waiting++
		default:
			v.Shown = append(v.Shown, c)
		}
	}
	for key, i := range ignores {
		if i.List() != list {
			continue
		}
		c, ok := stored[key]
		if !ok {
			c = domain.Candidate{List: list, Key: key, Title: i.Title, Torrents: []domain.TorrentInfo{}, NotInScan: true}
			if list == domain.ListSeries {
				c.Scope = domain.ScopeSeries
				if _, season, ok := domain.ParseSeasonKey(key); ok {
					c.Scope, c.Season = domain.ScopeSeason, season
				}
			}
		}
		at := i.CreatedAt
		c.IgnoredAt = &at
		v.Ignored = append(v.Ignored, c)
	}
	sortByKey(v.Shown)
	sortByKey(v.Ignored)
	return v
}

// tiedToHidden reports whether a row the row is tied to is hidden by an ignore.
func tiedToHidden(c domain.Candidate, ignores IgnoreSet) bool {
	for _, ref := range c.TiedWith {
		if ignores.Hides(ref.List, ref.Key) {
			return true
		}
	}
	return false
}
```

In `internal/scan/series.go`, replace the doc comment of `SeriesInput.Ignored` with:

```go
	// Ignored holds the keys the user had ignored when the scan started. Here it only stops
	// the per-season merge into one series row (spec 6.3). Ignored rows are still returned:
	// the lists apply the ignores when they are read (View, spec 6.5).
```

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./internal/scan/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/scan
git commit -m "feat: apply ignores to the stored rows of a list"
```

---

### Task 3: store ignores and read the latest scan with them

**Files:**
- Create: `internal/store/migrations/0002_ignores.sql`, `internal/store/ignores.go`, `internal/store/ignores_test.go`
- Modify: `internal/store/scans.go` (replace `LatestList` with `ReadLatest`), `internal/store/store_test.go` (the `LatestList` tests)

**Interfaces:**
- Consumes: Task 1's `domain.Ignore`, `Candidate.Waiting`.
- Produces:
  - `func (*store.Store) AddIgnores(ctx context.Context, ignores []domain.Ignore) error`
  - `func (*store.Store) RemoveIgnores(ctx context.Context, keys []string) error`
  - `func (*store.Store) Ignores(ctx context.Context) ([]domain.Ignore, error)` — sorted by key, never nil
  - `type store.Latest struct { Scan *store.ScanRecord; Rows []domain.Candidate; Ignores []domain.Ignore }`
  - `func (*store.Store) ReadLatest(ctx context.Context) (store.Latest, error)`
  - `LatestList` stays in this task so that `internal/api` still builds; Task 5 moves the API to `ReadLatest` and removes it.

- [ ] **Step 1: Write the failing tests**

`internal/store/ignores_test.go`:

```go
package store

import (
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

var firstIgnore = time.Date(2026, 3, 11, 9, 0, 0, 0, time.UTC)

func TestIgnoresAreSavedAndRemoved(t *testing.T) {
	s := open(t)
	if got, err := s.Ignores(ctx); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("Ignores on an empty store = %v, %v; want an empty, non-nil slice", got, err)
	}
	movie := domain.Ignore{Kind: domain.IgnoreMovie, Key: "tmdb:603", Title: "The Matrix", CreatedAt: firstIgnore}
	season := domain.Ignore{Kind: domain.IgnoreSeason, Key: "tvdb:100:s2", Title: "The Show", CreatedAt: firstIgnore}
	if err := s.AddIgnores(ctx, []domain.Ignore{season, movie, movie}); err != nil {
		t.Fatal(err)
	}
	// Ignoring again keeps the first title and time.
	again := movie
	again.Title, again.CreatedAt = "Renamed", firstIgnore.Add(time.Hour)
	if err := s.AddIgnores(ctx, []domain.Ignore{again}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Ignores(ctx)
	if err != nil || !reflect.DeepEqual(got, []domain.Ignore{movie, season}) {
		t.Fatalf("Ignores = %+v, %v; want the movie then the season, as first saved", got, err)
	}

	if err := s.RemoveIgnores(ctx, []string{"tmdb:603", "hash:never-ignored"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Ignores(ctx); !reflect.DeepEqual(got, []domain.Ignore{season}) {
		t.Errorf("after removing the movie: %+v, want the season alone", got)
	}
}

func TestIgnoresPersistAcrossScans(t *testing.T) {
	s := open(t)
	ig := domain.Ignore{Kind: domain.IgnoreTorrent, Key: "hash:abc", Title: "Some.Torrent", CreatedAt: firstIgnore}
	if err := s.AddIgnores(ctx, []domain.Ignore{ig}); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		started := firstIgnore.Add(time.Duration(n+1) * time.Hour)
		id, err := s.BeginScan(ctx, started)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.FinishScan(ctx, id, started.Add(time.Minute), domain.ScanSummary{}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := s.Ignores(ctx); !reflect.DeepEqual(got, []domain.Ignore{ig}) {
		t.Errorf("after two scans: %+v, want the ignore unchanged", got)
	}
}
```

Rewrite `TestLatestListReturnsAScanWithItsOwnRows` in `internal/store/store_test.go` as `TestReadLatestReturnsAScanWithItsOwnRowsAndTheIgnores`, against `ReadLatest`:

- On an empty store, `ReadLatest` returns `Scan == nil`, and `Rows` and `Ignores` empty and non-nil.
- Each `finish(n)` stores a movie row `tmdb:<n>`, a torrent row `hash:abc`, and a held series row `{List: domain.ListSeries, Key: "tvdb:100:s1", Scope: domain.ScopeSeason, Season: 1, Waiting: true, Torrents: []domain.TorrentInfo{}}`.
- After `finish(1)`, `finish(2)`, an `AddIgnores` of `tmdb:2` and a running `BeginScan`: `Scan.ID` is the second scan; `Rows` are exactly the three rows of the second scan, ordered by list then key (`movies/tmdb:2`, `series/tvdb:100:s1`, `torrents/hash:abc`), and the series row comes back with `Waiting` true; `Ignores` holds `tmdb:2`.

Rewrite `TestLatestListReleasesTheDatabase` as `TestReadLatestReleasesTheDatabase`: same body, calling `ReadLatest` instead of `LatestList`.

- [ ] **Step 2: Run the store tests and see them fail**

Run: `go test ./internal/store/`
Expected: build failure, `s.Ignores undefined`, `s.AddIgnores undefined`, `s.ReadLatest undefined`.

- [ ] **Step 3: Implement**

`internal/store/migrations/0002_ignores.sql`:

```sql
CREATE TABLE ignores (
  key        TEXT PRIMARY KEY,
  kind       TEXT NOT NULL,
  title      TEXT NOT NULL,
  created_at TEXT NOT NULL
);
```

`internal/store/ignores.go`:

```go
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

// AddIgnores saves ignores in one transaction (spec 6.5). An ignore whose key is saved
// already keeps its first title and time.
func (s *Store) AddIgnores(ctx context.Context, ignores []domain.Ignore) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, i := range ignores {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO ignores (key, kind, title, created_at) VALUES (?, ?, ?, ?)`,
			i.Key, i.Kind, i.Title, formatTime(i.CreatedAt)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RemoveIgnores deletes the ignores with the given keys in one transaction. A key that is
// not ignored is skipped.
func (s *Store) RemoveIgnores(ctx context.Context, keys []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, key := range keys {
		if _, err := tx.ExecContext(ctx, `DELETE FROM ignores WHERE key = ?`, key); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Ignores returns every ignore, sorted by key. The slice is never nil.
func (s *Store) Ignores(ctx context.Context) ([]domain.Ignore, error) {
	return ignores(ctx, s.db)
}

func ignores(ctx context.Context, q queryer) ([]domain.Ignore, error) {
	rows, err := q.QueryContext(ctx, `SELECT key, kind, title, created_at FROM ignores ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Ignore{}
	for rows.Next() {
		var i domain.Ignore
		var created string
		if err := rows.Scan(&i.Key, &i.Kind, &i.Title, &created); err != nil {
			return nil, err
		}
		if i.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, fmt.Errorf("ignore %s: bad time: %w", i.Key, err)
		}
		out = append(out, i)
	}
	return out, rows.Err()
}
```

In `internal/store/scans.go`, add:

```go
// Latest is the latest finished scan, every row it stored and the ignores, read together.
type Latest struct {
	Scan    *ScanRecord        // nil when no scan has finished
	Rows    []domain.Candidate // the rows of every list, listed and held back, by list and key; never nil
	Ignores []domain.Ignore    // sorted by key; never nil
}

// ReadLatest reads the latest finished scan, its rows and the ignores in one transaction.
// FinishScan drops the rows of earlier scans, so a scan that finished between two separate
// reads would leave the older scan with no rows; and the lists are only right with the
// ignores of the same moment (spec 6.5).
func (s *Store) ReadLatest(ctx context.Context) (Latest, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Latest{}, err
	}
	defer tx.Rollback() // nothing was written

	out := Latest{Rows: []domain.Candidate{}}
	if out.Scan, err = latestScan(ctx, tx); err != nil {
		return Latest{}, err
	}
	if out.Scan != nil {
		if out.Rows, err = allCandidates(ctx, tx, out.Scan.ID); err != nil {
			return Latest{}, err
		}
	}
	if out.Ignores, err = ignores(ctx, tx); err != nil {
		return Latest{}, err
	}
	return out, nil
}
```

and refactor the row reading so both helpers share one loop:

```go
func candidates(ctx context.Context, q queryer, scanID int64, list domain.ListKind) ([]domain.Candidate, error) {
	return readCandidates(q.QueryContext(ctx, `SELECT data FROM candidates WHERE scan_id = ? AND list = ? ORDER BY key`, scanID, string(list)))
}

func allCandidates(ctx context.Context, q queryer, scanID int64) ([]domain.Candidate, error) {
	return readCandidates(q.QueryContext(ctx, `SELECT data FROM candidates WHERE scan_id = ? ORDER BY list, key`, scanID))
}

// readCandidates decodes the rows of a query on the data column. The slice is never nil.
func readCandidates(rows *sql.Rows, err error) ([]domain.Candidate, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Candidate{}
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var c domain.Candidate
		if err := json.Unmarshal([]byte(data), &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
```

Keep `LatestList` for now (see Interfaces).

- [ ] **Step 4: Run the store tests, also with the race detector**

Run: `go test ./internal/store/ && go test -race ./internal/store/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store
git commit -m "feat: store ignores and read the latest scan together with them"
```

---

### Task 4: the scan reads ignores for the merge and stores held rows

**Files:**
- Modify: `internal/scanner/scanner.go` (`Store` interface, `Fetched`, `run`, `Compute`)
- Test: `internal/scanner/scanner_test.go`

**Interfaces:**
- Consumes: Task 1's `ListResult.Held`; Task 2's `scan.NewIgnoreSet(...).Keys()`; Task 3's `(*store.Store).Ignores`.
- Produces:
  - `scanner.Store` gains `Ignores(ctx context.Context) ([]domain.Ignore, error)`.
  - `scanner.Fetched` gains `Ignored map[string]bool`.
  - `Compute` returns the held rows (`Waiting` true) after the listed rows of each watched list. Summary counts are unchanged: `Candidates` counts listed rows, `Waiting` held ones.

- [ ] **Step 1: Write the failing tests**

In `internal/scanner/scanner_test.go`:

Give `fakeStore` the fields `ignores []domain.Ignore` and `ignoresErr error`, and:

```go
func (f *fakeStore) Ignores(context.Context) ([]domain.Ignore, error) { return f.ignores, f.ignoresErr }
```

Add:

```go
// twoSeasonWorld is world with a second watched season of the series.
func twoSeasonWorld() Providers {
	p, server := world()
	s := p.Series.(*fakeSeries)
	s.snap.Series[0].Episodes = append(s.snap.Series[0].Episodes,
		domain.Episode{ID: 21, Season: 2, Number: 1, File: &domain.MediaFile{ID: 21, Size: 900}})
	server.lib.Episodes = append(server.lib.Episodes,
		domain.ServerEpisode{SeriesID: "s1", Season: 2, Episode: 1, States: map[string]domain.UserState{"a": {Played: true}}})
	return p
}

func storedKeys(st *fakeStore, list domain.ListKind) []string {
	var out []string
	for _, c := range st.candidates {
		if c.List == list {
			out = append(out, c.Key)
		}
	}
	return out
}

// The per-season merge is the one rule a scan applies ignores to (spec 6.3); everything
// else is applied when the lists are read.
func TestRunDoesNotMergeSeasonsOverASeasonIgnoredBeforeTheScan(t *testing.T) {
	r := withUser()
	r.SeriesUnit = domain.UnitSeason

	st := &fakeStore{}
	if err := service(twoSeasonWorld(), r, st).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := storedKeys(st, domain.ListSeries); !reflect.DeepEqual(got, []string{"tvdb:100"}) {
		t.Fatalf("without an ignore: rows %v, want the merged series row", got)
	}

	st = &fakeStore{ignores: []domain.Ignore{{Kind: domain.IgnoreSeason, Key: domain.SeasonKey("100", 2), Title: "The Show"}}}
	if err := service(twoSeasonWorld(), r, st).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := storedKeys(st, domain.ListSeries); !reflect.DeepEqual(got, []string{"tvdb:100:s1", "tvdb:100:s2"}) {
		t.Errorf("with season 2 ignored: rows %v, want both season rows", got)
	}
}

// Ignored rows are stored like any other: the lists hide them when they are read, so that
// un-ignoring brings them back without a new scan (spec 6.5).
func TestRunStoresIgnoredRows(t *testing.T) {
	p, _ := world()
	st := &fakeStore{ignores: []domain.Ignore{{Kind: domain.IgnoreMovie, Key: "tmdb:603", Title: "The Matrix"}}}
	if err := service(p, withUser(), st).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := storedKeys(st, domain.ListMovies); !reflect.DeepEqual(got, []string{"tmdb:603"}) {
		t.Errorf("movie rows %v, want the ignored movie stored", got)
	}
	if got := st.summary.Lists[domain.ListMovies].Candidates; got != 1 {
		t.Errorf("movie candidates = %d, want 1: the scan counts before ignores", got)
	}
}

func TestRunFailsWhenIgnoresCannotBeRead(t *testing.T) {
	p, _ := world()
	st := &fakeStore{ignoresErr: errors.New("disk on fire")}
	err := service(p, withUser(), st).Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "load ignores") || !strings.Contains(err.Error(), "disk on fire") {
		t.Fatalf("Run = %v, want an error about loading the ignores", err)
	}
	if st.begun != 0 || st.finished != 0 {
		t.Errorf("scan recorded %d starts and %d finishes, want none", st.begun, st.finished)
	}
}
```

Change the helpers so they speak about listed rows, and check that held rows are stored:

- `rowsByKey` skips rows with `Waiting` set (it is used for listed rows).
- `expectWaiting` additionally checks that the row with `key` is among `candidates` with `Waiting` true and `List == domain.ListSeries`:

```go
	held := false
	for _, c := range candidates {
		if c.Key == key && c.Waiting && c.List == domain.ListSeries {
			held = true
		}
	}
	if !held {
		t.Errorf("row %s is not stored as waiting; the lists need it to apply ignores", key)
	}
```

Read every other test that inspects `st.candidates` or the `candidates` returned by `Compute` (`TestRunProducesAllThreeLists`, `TestRunHoldsBackASeasonWhileItsMixedPackBacksAnotherSeason`, the tests from line 700 on) and keep each one asserting what it asserted about listed rows.

- [ ] **Step 2: Run the scanner tests and see them fail**

Run: `go test ./internal/scanner/`
Expected: the three new tests and `expectWaiting` callers fail: the merge ignores the store's ignores, held rows are not stored, ignores are never read.

- [ ] **Step 3: Implement**

In `internal/scanner/scanner.go`:

- Add to `Store`: `Ignores(ctx context.Context) ([]domain.Ignore, error)`.
- Add to `Fetched`, after `Files`:

```go
	// Ignored holds the keys the user had ignored when the scan started. A scan uses them
	// only for the per-season merge (spec 6.3); the lists apply ignores when they are read.
	Ignored map[string]bool
```

- In `run`, after loading the rules and before `s.Build`:

```go
	ignores, err := s.Store.Ignores(ctx)
	if err != nil {
		return fmt.Errorf("load ignores: %w", err)
	}
```

  and after `fetched := s.fetch(ctx, p, rules)`: `fetched.Ignored = scan.NewIgnoreSet(ignores).Keys()`.
- In `Compute`, pass `Ignored: f.Ignored` in the `scan.SeriesInput`, and in `add` append `res.Held` after `res.Candidates`:

```go
		candidates = append(candidates, res.Candidates...)
		candidates = append(candidates, res.Held...)
```

- Extend the doc comment of `Compute`: "The candidates include the rows the whole-torrent rule holds back, marked Waiting: the lists apply ignores to them when they are read (spec 6.5)."

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./internal/scanner/ ./internal/app/ ./cmd/... && go test -race ./internal/scanner/`
Expected: PASS. `*store.Store` satisfies the extended interface since Task 3.

- [ ] **Step 5: Break the glue on purpose**

One at a time, in a scratch edit, and revert each: drop `fetched.Ignored = ...`; drop `Ignored: f.Ignored`; drop the `res.Held` append; ignore the error from `Ignores`. Each must make a test fail. Report which test caught which.

- [ ] **Step 6: Commit**

```bash
git add internal/scanner
git commit -m "feat: read ignores for the per-season merge and store held rows"
```

---

### Task 5: the lists and summaries apply the current ignores

**Files:**
- Create: `internal/api/view.go`, `internal/api/ignores_test.go`
- Modify: `internal/api/server.go` (`Store` interface), `internal/api/scans.go` (`candidates`, `currentScan`, `status`), `internal/api/scans_test.go` (`racingStore`)
- Modify: `internal/store/scans.go`, `internal/store/store_test.go`: remove `LatestList` and anything only it used.

**Interfaces:**
- Consumes: Task 2's `scan.NewIgnoreSet`, `scan.View`, `scan.ListView`; Task 3's `store.Latest`, `(*store.Store).ReadLatest`, `AddIgnores`.
- Produces:
  - `api.Store` becomes `{ ReadLatest(ctx) (store.Latest, error) }` (Task 6 adds two methods).
  - `GET /api/candidates?list=…&ignored=true` returns the ignored view; `ignored` absent or `false` returns the shown rows; any other value is a 400.
  - `GET /api/status` and `GET /api/scans/current` report a summary whose `candidates` and `waiting` count what the lists show with the current ignores.

- [ ] **Step 1: Write the failing tests**

`internal/api/ignores_test.go`:

```go
package api

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/store"
)

// storeTiedScan saves a finished scan in which movie tmdb:603 is tied to series row
// tvdb:100, movie tmdb:604 stands alone, a season row of another series is held back, and
// one torrent is listed.
func storeTiedScan(t *testing.T, h *harness) {
	t.Helper()
	ctx := context.Background()
	started := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	id, err := h.store.BeginScan(ctx, started)
	if err != nil {
		t.Fatal(err)
	}
	none := []domain.TorrentInfo{}
	rows := []domain.Candidate{
		{List: domain.ListMovies, Key: "tmdb:603", Title: "The Matrix", Torrents: none,
			TiedWith: []domain.RowRef{{List: domain.ListSeries, Key: "tvdb:100", Title: "The Show"}}},
		{List: domain.ListMovies, Key: "tmdb:604", Title: "The Matrix Reloaded", Torrents: none},
		{List: domain.ListSeries, Key: "tvdb:100", Title: "The Show", Scope: domain.ScopeSeries, Torrents: none,
			TiedWith: []domain.RowRef{{List: domain.ListMovies, Key: "tmdb:603", Title: "The Matrix"}}},
		{List: domain.ListSeries, Key: "tvdb:200:s1", Title: "Other Show", Scope: domain.ScopeSeason, Season: 1, Torrents: none, Waiting: true},
		{List: domain.ListTorrents, Key: "hash:abc", Title: "Some.Torrent", Torrents: none},
	}
	summary := domain.ScanSummary{Lists: map[domain.ListKind]domain.ListSummary{
		domain.ListMovies:   {Candidates: 2},
		domain.ListSeries:   {Candidates: 1, Waiting: 1, Protected: 3},
		domain.ListTorrents: {Candidates: 1},
	}}
	if err := h.store.FinishScan(ctx, id, started.Add(time.Minute), summary, rows); err != nil {
		t.Fatal(err)
	}
}

type listBody struct {
	Candidates []domain.Candidate `json:"candidates"`
}

func listKeys(t *testing.T, h *harness, path string) []string {
	t.Helper()
	rec := h.do(http.MethodGet, path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d %s", path, rec.Code, rec.Body)
	}
	out := []string{}
	for _, c := range decode[listBody](t, rec).Candidates {
		out = append(out, c.Key)
	}
	return out
}

func lastSummary(t *testing.T, h *harness, path string) map[domain.ListKind]domain.ListSummary {
	t.Helper()
	type body struct {
		Last     *store.ScanRecord `json:"last"`
		LastScan *store.ScanRecord `json:"lastScan"`
	}
	b := decode[body](t, h.do(http.MethodGet, path, ""))
	rec := b.Last
	if rec == nil {
		rec = b.LastScan
	}
	if rec == nil {
		t.Fatalf("%s has no last scan", path)
	}
	return rec.Summary.Lists
}

func TestListsLeaveOutIgnoredRowsAndWhatIsTiedToThem(t *testing.T) {
	h := newHarness(t, nil)
	storeTiedScan(t, h)
	if err := h.store.AddIgnores(context.Background(), []domain.Ignore{
		{Kind: domain.IgnoreMovie, Key: "tmdb:603", Title: "The Matrix", CreatedAt: time.Date(2026, 3, 11, 9, 0, 0, 0, time.UTC)},
	}); err != nil {
		t.Fatal(err)
	}

	if got := listKeys(t, h, "/api/candidates?list=movies"); !reflect.DeepEqual(got, []string{"tmdb:604"}) {
		t.Errorf("movies = %v, want tmdb:604 alone", got)
	}
	if got := listKeys(t, h, "/api/candidates?list=movies&ignored=false"); !reflect.DeepEqual(got, []string{"tmdb:604"}) {
		t.Errorf("movies with ignored=false = %v, want tmdb:604 alone", got)
	}
	if got := listKeys(t, h, "/api/candidates?list=series"); len(got) != 0 {
		t.Errorf("series = %v, want none: tvdb:100 is tied to the ignored movie, tvdb:200:s1 waits", got)
	}
	rec := h.do(http.MethodGet, "/api/candidates?list=movies&ignored=true", "")
	ignored := decode[listBody](t, rec).Candidates
	if len(ignored) != 1 || ignored[0].Key != "tmdb:603" || ignored[0].Title != "The Matrix" || ignored[0].IgnoredAt == nil {
		t.Errorf("ignored movies = %+v, want tmdb:603 with its ignore time", ignored)
	}

	for _, path := range []string{"/api/status", "/api/scans/current"} {
		lists := lastSummary(t, h, path)
		if got := lists[domain.ListMovies]; got.Candidates != 1 || got.Waiting != 0 {
			t.Errorf("%s movies = %+v, want 1 shown and none waiting", path, got)
		}
		if got := lists[domain.ListSeries]; got.Candidates != 0 || got.Waiting != 2 || got.Protected != 3 {
			t.Errorf("%s series = %+v, want none shown, 2 waiting, protected count kept", path, got)
		}
		if got := lists[domain.ListTorrents]; got.Candidates != 1 {
			t.Errorf("%s torrents = %+v, want 1 shown", path, got)
		}
	}
}

func TestCandidatesRejectsABadIgnoredValue(t *testing.T) {
	h := newHarness(t, nil)
	for _, v := range []string{"yes", "1", "TRUE"} {
		if rec := h.do(http.MethodGet, "/api/candidates?list=movies&ignored="+v, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("ignored=%s: status = %d, want 400", v, rec.Code)
		}
	}
}

// A list that could not be produced keeps its error and its count of zero.
func TestSummaryOfAFailedListIsUnchanged(t *testing.T) {
	h := newHarness(t, nil)
	storeScan(t, h) // its series list failed
	if got := lastSummary(t, h, "/api/status")[domain.ListSeries]; got.Error == "" || got.Candidates != 0 {
		t.Errorf("series = %+v, want the error kept", got)
	}
}
```

In `internal/api/scans_test.go`, replace the two `racingStore` methods with one:

```go
func (r *racingStore) ReadLatest(ctx context.Context) (store.Latest, error) {
	l, err := r.Store.ReadLatest(ctx)
	r.afterRead()
	return l, err
}
```

- [ ] **Step 2: Run the API tests and see them fail**

Run: `go test ./internal/api/`
Expected: failures: ignored rows are listed, `ignored=yes` is accepted, the summary counts the scan's numbers.

- [ ] **Step 3: Implement**

`internal/api/view.go`:

```go
package api

import (
	"context"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
	"github.com/removarr/removarr/internal/store"
)

// latestView is the latest scan as the user sees it with the current ignores (spec 6.5).
type latestView struct {
	rec   *store.ScanRecord // nil before the first scan; its summary counts what the lists show
	rows  []domain.Candidate
	lists map[domain.ListKind]scan.ListView
}

// latest reads the latest scan, its rows and the ignores together, and applies the ignores.
// Each produced list's summary counts the rows shown and the rows that wait; a list that
// could not be produced keeps its error.
func (s *Server) latest(ctx context.Context) (latestView, error) {
	l, err := s.Store.ReadLatest(ctx)
	if err != nil {
		return latestView{}, err
	}
	ignores := scan.NewIgnoreSet(l.Ignores)
	v := latestView{rows: l.Rows, lists: map[domain.ListKind]scan.ListView{}}
	for _, list := range []domain.ListKind{domain.ListMovies, domain.ListSeries, domain.ListTorrents} {
		v.lists[list] = scan.View(list, l.Rows, ignores)
	}
	if l.Scan != nil {
		rec := *l.Scan
		lists := make(map[domain.ListKind]domain.ListSummary, len(rec.Summary.Lists))
		for list, sum := range rec.Summary.Lists {
			if sum.Error == "" {
				sum.Candidates, sum.Waiting = len(v.lists[list].Shown), v.lists[list].Waiting
			}
			lists[list] = sum
		}
		rec.Summary.Lists = lists
		v.rec = &rec
	}
	return v, nil
}
```

In `internal/api/server.go`, set the `Store` interface to:

```go
// Store is the part of the store the API uses. *store.Store satisfies it.
type Store interface {
	ReadLatest(ctx context.Context) (store.Latest, error)
}
```

In `internal/api/scans.go`:

- `currentScan`: `v, err := s.latest(r.Context())`; answer `scanResponse{Status: s.Scanner.Status(), Last: v.rec}`.
- `status`: the same, `resp.LastScan = v.rec`.
- `candidates`: after checking `list`, parse `ignored`:

```go
	var showIgnored bool
	switch r.URL.Query().Get("ignored") {
	case "", "false":
	case "true":
		showIgnored = true
	default:
		writeError(w, http.StatusBadRequest, `ignored must be "true" or "false"`)
		return
	}
```

  then read `v, err := s.latest(r.Context())`, and answer with `resp.Candidates = v.lists[list].Shown`, or `v.lists[list].Ignored` when `showIgnored`. `ScannedAt` and `Error` come from `v.rec` when it is not nil. Change the comment above the read to say the scan, its rows and the ignores are read together.

Remove `LatestList` from `internal/store/scans.go` and its remaining tests from `internal/store/store_test.go`. `candidates` stays (used by `Candidates`).

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./... && go test -race ./internal/api/ ./internal/store/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api internal/store
git commit -m "feat: apply the current ignores to the lists and their counts"
```

---

### Task 6: ignore and un-ignore through the API

**Files:**
- Create: `internal/api/ignores.go`
- Modify: `internal/api/server.go` (`Store` interface, `Server`, `Handler`)
- Test: `internal/api/ignores_test.go`
- Modify: `README.md` (the curl examples)

**Interfaces:**
- Consumes: Task 3's `AddIgnores`, `RemoveIgnores`; Task 5's `(*Server).latest` and `latestView.rows`; Task 1's `domain.IgnoreKind`.
- Produces:
  - `api.Store` gains `AddIgnores(ctx, []domain.Ignore) error` and `RemoveIgnores(ctx, []string) error`.
  - `api.Server` gains `Now func() time.Time` (nil means `time.Now`).
  - `POST /api/ignores` and `DELETE /api/ignores`, body `{"rows":[{"list":"movies","key":"tmdb:603"}]}`, answer `204 No Content`.

Rules:
- The body is JSON with exactly the field `rows`, at most 1 MB. `rows` names at least one row; each has a `list` of `movies`, `series` or `torrents` and a non-empty `key`. A bad body is a 400 naming the field, for example `rows[1].list`.
- POST: every row must be a row of that list in the latest scan, listed or held back. Otherwise the answer is a 400 naming the field `rows[i].key`, and nothing is saved. The kind and the title come from the stored row. A row ignored already keeps its first ignore.
- DELETE: removes the ignores with those keys. A key that is not ignored is skipped. It does not look at the scan: an ignore whose row is gone can still be removed.

- [ ] **Step 1: Write the failing tests**

Add to `internal/api/ignores_test.go` (add `"strings"` to the imports):

```go
func ignoreBody(rows ...string) string {
	var parts []string
	for _, r := range rows {
		list, key, _ := strings.Cut(r, "/")
		parts = append(parts, `{"list":"`+list+`","key":"`+key+`"}`)
	}
	return `{"rows":[` + strings.Join(parts, ",") + `]}`
}

func savedIgnores(t *testing.T, h *harness) []string {
	t.Helper()
	all, err := h.store.Ignores(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, i := range all {
		out = append(out, i.Kind+" "+i.Key+" "+i.Title)
	}
	return out
}

func TestIgnoreAndUnignoreRows(t *testing.T) {
	h := newHarness(t, nil)
	storeTiedScan(t, h)

	rec := h.do(http.MethodPost, "/api/ignores", ignoreBody("movies/tmdb:604", "torrents/hash:abc", "series/tvdb:200:s1", "movies/tmdb:604"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST = %d %s, want 204", rec.Code, rec.Body)
	}
	want := []string{"torrent hash:abc Some.Torrent", "movie tmdb:604 The Matrix Reloaded", "season tvdb:200:s1 Other Show"}
	if got := savedIgnores(t, h); !reflect.DeepEqual(got, want) {
		t.Errorf("saved = %v, want %v", got, want)
	}
	if got := listKeys(t, h, "/api/candidates?list=movies"); !reflect.DeepEqual(got, []string{"tmdb:603"}) {
		t.Errorf("movies = %v, want tmdb:603 alone", got)
	}
	if got := listKeys(t, h, "/api/candidates?list=torrents&ignored=true"); !reflect.DeepEqual(got, []string{"hash:abc"}) {
		t.Errorf("ignored torrents = %v", got)
	}

	rec = h.do(http.MethodDelete, "/api/ignores", ignoreBody("movies/tmdb:604", "torrents/hash:abc", "movies/tmdb:999"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d %s, want 204", rec.Code, rec.Body)
	}
	if got := savedIgnores(t, h); !reflect.DeepEqual(got, []string{"season tvdb:200:s1 Other Show"}) {
		t.Errorf("saved after un-ignoring = %v", got)
	}
	if got := listKeys(t, h, "/api/candidates?list=movies"); !reflect.DeepEqual(got, []string{"tmdb:603", "tmdb:604"}) {
		t.Errorf("movies after un-ignoring = %v, want both", got)
	}
}

func TestIgnoreRecordsTheKindOfTheRow(t *testing.T) {
	h := newHarness(t, nil)
	storeTiedScan(t, h)
	if rec := h.do(http.MethodPost, "/api/ignores", ignoreBody("series/tvdb:100")); rec.Code != http.StatusNoContent {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	if got := savedIgnores(t, h); !reflect.DeepEqual(got, []string{"series tvdb:100 The Show"}) {
		t.Errorf("saved = %v", got)
	}
}

func TestIgnoreAcceptsOnlyRowsOfTheLatestScan(t *testing.T) {
	h := newHarness(t, nil)
	storeTiedScan(t, h)
	for _, body := range []string{
		ignoreBody("movies/tmdb:604", "movies/tmdb:999"),  // not in the scan
		ignoreBody("movies/tmdb:604", "series/tmdb:603"),  // in the scan, but in another list
	} {
		rec := h.do(http.MethodPost, "/api/ignores", body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "rows[1].key") {
			t.Errorf("POST %s = %d %s, want 400 naming rows[1].key", body, rec.Code, rec.Body)
		}
	}
	if got := savedIgnores(t, h); len(got) != 0 {
		t.Errorf("saved = %v, want nothing: a request is saved whole or not at all", got)
	}
}

// An ignore whose row the latest scan does not have, such as a season ignore in
// whole-series mode, is shown in the ignored view and can be removed.
func TestUnignoreAnIgnoreWithoutARow(t *testing.T) {
	h := newHarness(t, nil)
	storeTiedScan(t, h)
	if err := h.store.AddIgnores(context.Background(), []domain.Ignore{
		{Kind: domain.IgnoreSeason, Key: "tvdb:100:s3", Title: "The Show", CreatedAt: time.Date(2026, 3, 11, 9, 0, 0, 0, time.UTC)},
	}); err != nil {
		t.Fatal(err)
	}
	rec := h.do(http.MethodGet, "/api/candidates?list=series&ignored=true", "")
	got := decode[listBody](t, rec).Candidates
	if len(got) != 1 || got[0].Key != "tvdb:100:s3" || !got[0].NotInScan || got[0].Season != 3 {
		t.Fatalf("ignored series = %+v, want the season ignore marked notInScan", got)
	}
	if rec := h.do(http.MethodDelete, "/api/ignores", ignoreBody("series/tvdb:100:s3")); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d %s", rec.Code, rec.Body)
	}
	if got := savedIgnores(t, h); len(got) != 0 {
		t.Errorf("saved = %v, want nothing", got)
	}
}

// A row ignored before a new scan stays hidden after it (spec 6.5).
func TestIgnoredRowsStayHiddenAfterANewScan(t *testing.T) {
	h := newHarness(t, nil)
	storeTiedScan(t, h)
	if rec := h.do(http.MethodPost, "/api/ignores", ignoreBody("movies/tmdb:604")); rec.Code != http.StatusNoContent {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	storeTiedScan(t, h)
	if got := listKeys(t, h, "/api/candidates?list=movies"); !reflect.DeepEqual(got, []string{"tmdb:603"}) {
		t.Errorf("movies after a new scan = %v, want tmdb:603 alone", got)
	}
}

func TestIgnoreRequestsAreValidated(t *testing.T) {
	h := newHarness(t, nil)
	storeTiedScan(t, h)
	tests := []struct{ body, field string }{
		{`not json`, ""},
		{`{}`, "rows"},
		{`{"rows":[]}`, "rows"},
		{`{"rows":[{"list":"movies","key":"tmdb:604"}],"extra":1}`, ""},
		{`{"rows":[{"list":"films","key":"tmdb:604"}]}`, "rows[0].list"},
		{`{"rows":[{"list":"movies","key":""}]}`, "rows[0].key"},
		{`{"rows":[{"list":"movies"}]}`, "rows[0].key"},
	}
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		for _, tt := range tests {
			rec := h.do(method, "/api/ignores", tt.body)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tt.field) {
				t.Errorf("%s %s = %d %s, want 400 naming %q", method, tt.body, rec.Code, rec.Body, tt.field)
			}
		}
	}
	if got := savedIgnores(t, h); len(got) != 0 {
		t.Errorf("saved = %v, want nothing", got)
	}
}

func TestIgnoreRequestsGoThroughTheGuard(t *testing.T) {
	tests := []struct {
		name string
		mod  func(*http.Request)
		want int
	}{
		{"no request header", func(r *http.Request) { r.Header.Del(RequestHeader) }, http.StatusForbidden},
		{"form content type", func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") }, http.StatusUnsupportedMediaType},
		{"origin of another site", func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }, http.StatusForbidden},
		{"host name not allowed", func(r *http.Request) { r.Host = "evil.example" }, http.StatusForbidden},
	}
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		for _, tt := range tests {
			t.Run(method+" "+tt.name, func(t *testing.T) {
				h := newHarness(t, nil)
				storeTiedScan(t, h)
				if err := h.store.AddIgnores(context.Background(), []domain.Ignore{{Kind: domain.IgnoreMovie, Key: "tmdb:603", Title: "The Matrix"}}); err != nil {
					t.Fatal(err)
				}
				rec := h.do(method, "/api/ignores", ignoreBody("movies/tmdb:604", "movies/tmdb:603"), tt.mod)
				if rec.Code != tt.want {
					t.Errorf("status = %d, want %d", rec.Code, tt.want)
				}
				if got := savedIgnores(t, h); !reflect.DeepEqual(got, []string{"movie tmdb:603 The Matrix"}) {
					t.Errorf("saved = %v, want the ignores unchanged", got)
				}
			})
		}
	}
}
```

Note on `TestIgnoreAndUnignoreRows`: the saved list is sorted by key, so `hash:abc` comes first. The duplicate `movies/tmdb:604` in one request must not fail.

- [ ] **Step 2: Run the tests and see them fail**

Run: `go test ./internal/api/ -run 'Ignore|Unignore'`
Expected: 404 or 405 for `/api/ignores`.

- [ ] **Step 3: Implement**

In `internal/api/server.go`:

- `Store` interface gains:

```go
	AddIgnores(ctx context.Context, ignores []domain.Ignore) error
	RemoveIgnores(ctx context.Context, keys []string) error
```

- `Server` gains `Now func() time.Time // nil means time.Now` (import `time`), and:

```go
func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
```

- `Handler` gains:

```go
	mux.HandleFunc("POST /api/ignores", s.addIgnores)
	mux.HandleFunc("DELETE /api/ignores", s.removeIgnores)
```

`internal/api/ignores.go`:

```go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/removarr/removarr/internal/domain"
)

// rowRef names one row in an ignore request.
type rowRef struct {
	List domain.ListKind `json:"list"`
	Key  string          `json:"key"`
}

// readRows decodes and checks the body of an ignore request: {"rows":[{"list":…,"key":…}]}.
func readRows(w http.ResponseWriter, r *http.Request) ([]rowRef, error) {
	var req struct {
		Rows []rowRef `json:"rows"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return nil, &domain.FieldError{Field: "", Message: `the body must be {"rows":[{"list":…,"key":…}]}`}
	}
	if len(req.Rows) == 0 {
		return nil, &domain.FieldError{Field: "rows", Message: "must name at least one row"}
	}
	for i, row := range req.Rows {
		switch row.List {
		case domain.ListMovies, domain.ListSeries, domain.ListTorrents:
		default:
			return nil, &domain.FieldError{Field: fmt.Sprintf("rows[%d].list", i), Message: `must be "movies", "series" or "torrents"`}
		}
		if row.Key == "" {
			return nil, &domain.FieldError{Field: fmt.Sprintf("rows[%d].key", i), Message: "must not be empty"}
		}
	}
	return req.Rows, nil
}

// addIgnores ignores rows of the latest scan (spec 6.5). Every row must be in the latest
// scan, in the list named; otherwise nothing is saved. The kind and the title come from the
// stored row.
func (s *Server) addIgnores(w http.ResponseWriter, r *http.Request) {
	rows, err := readRows(w, r)
	if err != nil {
		s.fail(w, err)
		return
	}
	v, err := s.latest(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	stored := map[rowRef]domain.Candidate{}
	for _, c := range v.rows {
		stored[rowRef{c.List, c.Key}] = c
	}
	now := s.now()
	ignores := make([]domain.Ignore, 0, len(rows))
	for i, row := range rows {
		c, ok := stored[row]
		if !ok {
			s.fail(w, &domain.FieldError{Field: fmt.Sprintf("rows[%d].key", i), Message: "is not a row of the latest scan; reload the list"})
			return
		}
		ignores = append(ignores, domain.Ignore{Kind: domain.IgnoreKind(c), Key: c.Key, Title: c.Title, CreatedAt: now})
	}
	if err := s.Store.AddIgnores(r.Context(), ignores); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// removeIgnores un-ignores rows. A key that is not ignored is skipped. The latest scan is
// not consulted: an ignore whose row is gone can still be removed.
func (s *Server) removeIgnores(w http.ResponseWriter, r *http.Request) {
	rows, err := readRows(w, r)
	if err != nil {
		s.fail(w, err)
		return
	}
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.Key)
	}
	if err := s.Store.RemoveIgnores(r.Context(), keys); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

`fail` answers a `*domain.FieldError` with 400 and `{"error":…,"field":…}`; for the undecodable body the field is empty and the error text explains the shape. Check that `fail` does not log FieldErrors as internal errors (it does not today).

In `README.md`, after the `curl 'localhost:7272/api/candidates?list=torrents'` line, add:

```
    curl -X POST -H 'Content-Type: application/json' -H 'X-Removarr-Request: 1' \
      -d '{"rows":[{"list":"movies","key":"tmdb:603"}]}' localhost:7272/api/ignores
    curl 'localhost:7272/api/candidates?list=movies&ignored=true'
    curl -X DELETE -H 'Content-Type: application/json' -H 'X-Removarr-Request: 1' \
      -d '{"rows":[{"list":"movies","key":"tmdb:603"}]}' localhost:7272/api/ignores
```

matching the indentation and prose of the surrounding README section (read it first).

- [ ] **Step 4: Run all checks**

Run:
```
gofmt -l .
go vet ./...
go test ./...
go test -race ./internal/api/ ./internal/config/ ./internal/store/ ./internal/scanner/ ./internal/scan/
go build ./...
```
Expected: no output from gofmt, everything passes.

- [ ] **Step 5: Break the glue on purpose**

One at a time, revert each: register only POST; skip the latest-scan lookup; save before checking every row; drop `DisallowUnknownFields`. Each must make a test fail.

- [ ] **Step 6: Commit**

```bash
git add internal/api README.md
git commit -m "feat: ignore and un-ignore rows through the API"
```
