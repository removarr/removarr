# removarr Stage 1 (Read-Only Core) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Go server that scans Jellyfin, Sonarr, Radarr and qBittorrent on demand and serves the three suggestion lists (watched movies, watched series, orphan torrents) over a JSON API, without changing anything in those apps.

**Architecture:** A pure rule engine (`internal/scan`) works on product-neutral snapshots (`internal/domain`). Thin HTTP clients for each app sit behind interfaces (`internal/providers`). A scanner service fetches snapshots, runs the rules and stores the result in SQLite (`internal/store`). A standard-library HTTP API (`internal/api`) exposes scans, lists and settings.

**Tech Stack:** Go 1.27 (standard library `net/http` and `log/slog`), `modernc.org/sqlite` v1.62.0.

**Spec:** `docs/superpowers/specs/2026-11-18-removarr-design.md`

**Scope:** This is stage 1 of the four stages in spec section 13. It covers providers (read side), the scan engine, the store, and the scan, candidate and settings endpoints. Ignore, delete, history, the web UI and packaging are stages 2 to 4 and get their own plans, written after Task 14 has checked the spec's section 16 assumptions against the real apps.

## Global Constraints

- Go module path is `github.com/removarr/removarr`. `go.mod` declares `go 1.27`.
- The only third-party Go dependency is `modernc.org/sqlite` v1.62.0. HTTP routing and logging use the standard library.
- **Stage 1 is read-only.** No code may send a request that changes Jellyfin, Sonarr, Radarr or qBittorrent. Providers send GET requests only, plus qBittorrent's login POST.
- **API-only.** Never read or write media or download directories. The only file removarr touches is its SQLite database in the config directory.
- Default port is `7272`. Default config directory is `/config`.
- Environment variable names are exactly: `REMOVARR_PORT`, `REMOVARR_CONFIG_DIR`, `REMOVARR_DRY_RUN`, `REMOVARR_JELLYFIN_URL`, `REMOVARR_JELLYFIN_API_KEY`, `REMOVARR_SONARR_URL`, `REMOVARR_SONARR_API_KEY`, `REMOVARR_RADARR_URL`, `REMOVARR_RADARR_API_KEY`, `REMOVARR_QBITTORRENT_URL`, `REMOVARR_QBITTORRENT_USERNAME`, `REMOVARR_QBITTORRENT_PASSWORD`.
- For each connection field, a value saved through the API wins over the environment variable; clearing the saved value falls back to the environment.
- API keys and the qBittorrent password are write-only: never returned by the API, never logged.
- The SQLite file is created readable and writable by its owner only (mode `0600`).
- A list is produced only when every app it depends on answered completely (spec 6.8, rule 5). Torrents are never called orphans while a configured media manager is unreachable.
- License is MIT.

## Review Focus

Conditions the spec implies but does not spell out, most likely first. Each has a test in the task that owns the code.

1. **History longer than one page.** A library with years of Sonarr/Radarr history spans several pages. Every page must be read, or tracked torrents look orphaned. Test: `TestLinksReadsEveryPage` (Task 7).
2. **Hash letter case differs between apps.** Sonarr and Radarr report download IDs in upper case, qBittorrent in lower case. They must compare equal. Tests: `TestLinksLowercasesDownloadIDs` (Task 7), `TestDownloadsMapsFields` (Task 9).
3. **App URL with a base path or trailing slash**, such as `http://pi:8989/sonarr/`. Requests must go to `/sonarr/api/v3/...`. Tests: `TestNormalizeURL` (Task 6), `TestGetKeepsBasePath` (Task 7).
4. **Imports with no download ID** (manual imports). They must not link files to a phantom torrent with an empty ID, and must not make an older torrent look like the source of the current file. Test: `TestLinksIgnoresManualImports` (Task 7).
5. **Missing or oddly formatted timestamps from Jellyfin.** The item counts as watched with an unknown date; the scan must not fail. Tests: `TestParseTime` (Task 7), `TestLibraryToleratesBadDates` (Task 10).

## File Structure

| File | Responsibility |
|---|---|
| `go.mod`, `.gitignore`, `LICENSE`, `README.md`, `.github/workflows/ci.yml` | Project scaffold. |
| `internal/domain/library.go` | Media server types: users, watch state, movies, series, episodes. |
| `internal/domain/managers.go` | Media manager types: movies, series, episodes, files, download links, root folders. |
| `internal/domain/downloads.go` | Download client type. |
| `internal/domain/rules.go` | User-adjustable rules, defaults and validation. |
| `internal/domain/candidate.go` | A list row, its torrent details, and stable keys. |
| `internal/domain/summary.go` | Scan summary types. |
| `internal/scan/watched.go` | The watched rule and grace period. |
| `internal/scan/links.go` | Which torrent backs which file; which torrents a deletion would remove; seeding flag. |
| `internal/scan/torrents.go` | The orphan torrent list. |
| `internal/scan/helpers.go` | Small helpers shared by the movie and series rules. |
| `internal/scan/movies.go` | The watched movie list. |
| `internal/scan/series.go` | The watched series list. |
| `internal/store/store.go`, `internal/store/migrations/0001_init.sql` | Opening SQLite and applying migrations. |
| `internal/store/settings.go` | Key-value settings. |
| `internal/store/scans.go` | Scan records and stored candidates. |
| `internal/config/env.go` | Environment variables and connection field definitions. |
| `internal/config/settings.go` | Connection precedence (saved value over environment) and rules persistence. |
| `internal/httpjson/httpjson.go` | JSON GET helper and lenient timestamp parsing. |
| `internal/providers/providers.go` | The provider interfaces. |
| `internal/providers/arr/arr.go` | What Sonarr and Radarr share: requests, tags, root folders, status, history and queue links. |
| `internal/providers/radarr/radarr.go` | Radarr movies snapshot. |
| `internal/providers/sonarr/sonarr.go` | Sonarr series snapshot. |
| `internal/providers/qbittorrent/qbittorrent.go` | qBittorrent login and torrent list. |
| `internal/providers/jellyfin/jellyfin.go` | Jellyfin users and library snapshot. |
| `internal/scanner/scanner.go` | Runs one scan: fetch, apply rules, store. |
| `internal/app/apps.go` | Builds providers from the current settings. |
| `internal/api/server.go` | Routes, JSON helpers, request guard, health. |
| `internal/api/settings.go` | Settings, connection test and media server users endpoints. |
| `internal/api/scans.go` | Scan, candidates and status endpoints. |
| `cmd/removarr/main.go` | Process entry point. |

Tests live next to the code as `*_test.go`.

---

### Task 1: Scaffold, domain types and the watched rule

**Files:**
- Create: `go.mod`, `.gitignore`, `LICENSE`, `README.md`, `.github/workflows/ci.yml`
- Create: `internal/domain/library.go`, `internal/domain/managers.go`, `internal/domain/downloads.go`, `internal/domain/rules.go`, `internal/domain/candidate.go`
- Create: `internal/scan/watched.go`
- Test: `internal/domain/rules_test.go`, `internal/domain/candidate_test.go`, `internal/scan/watched_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - Package `domain`: `User`, `ExternalIDs`, `UserState`, `ServerMovie`, `ServerSeries`, `ServerEpisode`, `Library`, `MediaFile`, `Movie`, `Episode`, `Series`, `Links`, `RootFolder`, `MovieSnapshot`, `SeriesSnapshot`, `Download`, `Rules`, `DefaultRules() Rules`, `(Rules).Validate() error`, `FieldError`, `ListKind` with `ListMovies`, `ListSeries`, `ListTorrents`, `TorrentInfo`, `Candidate`, `MovieKey(ExternalIDs) string`, `SeriesKey(tvdb string) string`, `SeasonKey(tvdb string, season int) string`, `TorrentKey(hash string) string`, and the constants `WatchedAll`, `WatchedAny`, `UnitSeries`, `UnitSeason`, `ScopeSeries`, `ScopeSeason`, `ReasonOrphan`, `ReasonUnlinked`.
  - Package `scan`: `Verdict{Watched bool; At *time.Time; By []string}`, `Evaluate(r domain.Rules, states map[string]domain.UserState) Verdict`, `GraceOK(r domain.Rules, at *time.Time, now time.Time) bool`, and the unexported helpers `mergeStates(a, b map[string]domain.UserState) map[string]domain.UserState` and `later(a, b *time.Time) *time.Time`.
  - Test helpers in package `scan` (file `watched_test.go`), usable by later test files in the same package: `tp(s string) *time.Time`, `rules(mode string, users ...string) domain.Rules`, `played(at string) domain.UserState`, `sameTime(a, b *time.Time) bool`.

- [ ] **Step 1: Create the module and scaffold files**

Run:

```bash
go mod init github.com/removarr/removarr
go mod edit -go=1.27
mkdir -p internal/domain internal/scan .github/workflows
```

Create `.gitignore`:

```gitignore
/removarr
/dist/
/config/
*.db
*.db-wal
*.db-shm
.env
.remember/
.DS_Store
```

Create `LICENSE`:

```text
MIT License

Copyright (c) 2026 Samet Alemdar

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

Create `README.md`:

```markdown
# removarr

removarr suggests what to delete from a self-hosted media server: watched movies, watched series and orphan torrents. It never deletes anything on its own.

Status: early development. The design is in `docs/superpowers/specs/2026-11-18-removarr-design.md`.

## Development

Requires Go 1.27.

    go test ./...
```

Create `.github/workflows/ci.yml`:

```yaml
name: ci

on:
  push:
    branches: [main]
  pull_request:

jobs:
  go:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version-file: go.mod
      - run: go vet ./...
      - run: go test ./...
```

- [ ] **Step 2: Write the domain types**

Create `internal/domain/library.go`:

```go
// Package domain holds the product-neutral types the rest of removarr works with.
// It imports nothing from the other removarr packages.
package domain

import "time"

// User is an account on the media server.
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ExternalIDs identify a movie or series across products. An empty string means unknown.
type ExternalIDs struct {
	TMDB string
	TVDB string
	IMDB string
}

// UserState is one user's watch state for one item.
type UserState struct {
	Played     bool
	LastPlayed *time.Time // nil when the media server gives no date
	Favorite   bool
}

// ServerMovie is a movie as the media server sees it.
type ServerMovie struct {
	ID     string
	Title  string
	IDs    ExternalIDs
	States map[string]UserState // keyed by user ID
}

// ServerSeries is a series as the media server sees it.
type ServerSeries struct {
	ID         string
	Title      string
	IDs        ExternalIDs
	FavoriteOf map[string]bool // IDs of users who marked the series, or one of its seasons, a favourite
}

// ServerEpisode is an episode item on the media server. One item can cover a range of episodes.
type ServerEpisode struct {
	SeriesID   string // the media server's ID of the series
	Season     int
	Episode    int
	EpisodeEnd int // last episode of the range; 0 for a single episode
	States     map[string]UserState
}

// Library is a snapshot of the media server for a set of users.
type Library struct {
	Movies   []ServerMovie
	Series   []ServerSeries
	Episodes []ServerEpisode
}
```

Create `internal/domain/managers.go`:

```go
package domain

import "time"

// MediaFile is a file in a media manager's library.
type MediaFile struct {
	ID   int
	Size int64
}

// Movie is a movie in the movie manager.
type Movie struct {
	ID    int
	Title string
	Year  int
	IDs   ExternalIDs
	Tags  []string   // tag labels
	File  *MediaFile // nil when the movie has no file
}

// Episode is an episode in the series manager.
type Episode struct {
	ID      int
	Season  int // 0 holds specials
	Number  int
	AirDate *time.Time
	File    *MediaFile // nil when there is no file; the episodes of a multi-episode file share one File.ID
}

// Series is a series in the series manager.
type Series struct {
	ID       int
	Title    string
	Year     int
	IDs      ExternalIDs
	Tags     []string
	Ended    bool
	Episodes []Episode
}

// Links is what a media manager knows about downloads. Download IDs are lower case.
type Links struct {
	// Grabbed maps a download ID to the IDs of the movies or episodes it was grabbed for.
	Grabbed map[string][]int
	// FileSource maps a library file ID to the ID of the download it was imported from.
	FileSource map[int]string
	// Queue holds the IDs of the downloads in the manager's queue.
	Queue map[string]bool
}

// RootFolder is a library folder and its free space, as a media manager reports it.
type RootFolder struct {
	Path      string `json:"path"`
	FreeBytes int64  `json:"freeBytes"`
}

// MovieSnapshot is everything one scan reads from the movie manager.
type MovieSnapshot struct {
	Movies      []Movie
	Links       Links
	RootFolders []RootFolder
}

// SeriesSnapshot is everything one scan reads from the series manager.
type SeriesSnapshot struct {
	Series      []Series
	Links       Links
	RootFolders []RootFolder
}
```

Create `internal/domain/downloads.go`:

```go
package domain

import "time"

// Download is one item in the download client.
type Download struct {
	ID          string // lower-case identifier; the info hash for torrents
	Name        string
	Size        int64
	State       string // the client's own state name
	Category    string
	Tags        []string
	ContentPath string
	AddedAt     time.Time
	Ratio       *float64       // nil for clients that do not seed
	SeedingTime *time.Duration // nil for clients that do not seed
	Tracker     string         // tracker host name; empty when unknown
}
```

Create `internal/domain/candidate.go`:

```go
package domain

import (
	"fmt"
	"time"
)

// ListKind names one of the three suggestion lists.
type ListKind string

const (
	ListMovies   ListKind = "movies"
	ListSeries   ListKind = "series"
	ListTorrents ListKind = "torrents"
)

const (
	ScopeSeries = "series"
	ScopeSeason = "season"

	ReasonOrphan   = "orphan"
	ReasonUnlinked = "unlinked"
)

// TorrentInfo is what a row shows about one torrent.
type TorrentInfo struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Size         int64     `json:"size"`
	State        string    `json:"state"`
	Category     string    `json:"category"`
	Tracker      string    `json:"tracker"`
	Ratio        *float64  `json:"ratio"`
	SeedingDays  *float64  `json:"seedingDays"`
	AddedAt      time.Time `json:"addedAt"`
	BelowSeedMin bool      `json:"belowSeedMin"`
}

// Candidate is one row in a list: something removarr suggests deleting.
type Candidate struct {
	List         ListKind      `json:"list"`
	Key          string        `json:"key"`
	Title        string        `json:"title"`
	Year         int           `json:"year,omitempty"`
	Size         int64         `json:"size"`
	Scope        string        `json:"scope,omitempty"`       // series list: ScopeSeries or ScopeSeason
	Season       int           `json:"season,omitempty"`      // season rows
	StillAiring  bool          `json:"stillAiring,omitempty"` // series rows
	WatchedBy    []string      `json:"watchedBy,omitempty"`   // user names
	WatchedAt    *time.Time    `json:"watchedAt,omitempty"`
	Reason       string        `json:"reason,omitempty"` // torrent list: ReasonOrphan or ReasonUnlinked
	Torrents     []TorrentInfo `json:"torrents"`         // media rows: torrents Delete would remove; torrent rows: the torrent itself
	BelowSeedMin bool          `json:"belowSeedMin"`     // true when any of Torrents is below the seeding minimum
}

// MovieKey is the stable key of a movie row: its TMDB ID, or its IMDb ID when there is none.
func MovieKey(ids ExternalIDs) string {
	if ids.TMDB != "" {
		return "tmdb:" + ids.TMDB
	}
	return "imdb:" + ids.IMDB
}

// SeriesKey is the stable key of a whole-series row.
func SeriesKey(tvdb string) string { return "tvdb:" + tvdb }

// SeasonKey is the stable key of a season row.
func SeasonKey(tvdb string, season int) string { return fmt.Sprintf("tvdb:%s:s%d", tvdb, season) }

// TorrentKey is the stable key of a torrent row.
func TorrentKey(hash string) string { return "hash:" + hash }
```

- [ ] **Step 3: Write the failing tests for rules and keys**

Create `internal/domain/rules_test.go`:

```go
package domain

import (
	"errors"
	"testing"
)

func TestDefaultRulesAreValid(t *testing.T) {
	if err := DefaultRules().Validate(); err != nil {
		t.Fatalf("default rules must validate: %v", err)
	}
}

func TestDefaultRulesMatchTheSpec(t *testing.T) {
	r := DefaultRules()
	if r.WatchedMode != WatchedAll || r.SeriesUnit != UnitSeries || r.GraceDays != 0 {
		t.Errorf("unexpected defaults: %+v", r)
	}
	if !r.ProtectFavorites || r.ProtectionTag != "keep" {
		t.Errorf("protection defaults wrong: %+v", r)
	}
	if r.MinRatio != 0 || r.MinSeedDays != 0 || r.AddExclusionOnDelete {
		t.Errorf("seeding and exclusion must default to off: %+v", r)
	}
	if r.CountingUsers == nil || r.ExcludedCategories == nil || r.ExcludedTags == nil {
		t.Errorf("list defaults must be empty, not nil, so they serialise as []: %+v", r)
	}
}

func TestRulesValidateNamesTheField(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Rules)
		field  string
	}{
		{"watched mode", func(r *Rules) { r.WatchedMode = "most" }, "watchedMode"},
		{"series unit", func(r *Rules) { r.SeriesUnit = "episode" }, "seriesUnit"},
		{"grace days", func(r *Rules) { r.GraceDays = -1 }, "graceDays"},
		{"min ratio", func(r *Rules) { r.MinRatio = -0.5 }, "minRatio"},
		{"min seed days", func(r *Rules) { r.MinSeedDays = -1 }, "minSeedDays"},
		{"empty user", func(r *Rules) { r.CountingUsers = []string{"u1", " "} }, "countingUsers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := DefaultRules()
			tt.change(&r)
			var fe *FieldError
			if err := r.Validate(); !errors.As(err, &fe) {
				t.Fatalf("want *FieldError, got %v", err)
			}
			if fe.Field != tt.field {
				t.Errorf("field = %q, want %q", fe.Field, tt.field)
			}
		})
	}
}
```

Create `internal/domain/candidate_test.go`:

```go
package domain

import "testing"

func TestKeys(t *testing.T) {
	tests := []struct{ got, want string }{
		{MovieKey(ExternalIDs{TMDB: "603", IMDB: "tt0133093"}), "tmdb:603"},
		{MovieKey(ExternalIDs{IMDB: "tt0133093"}), "imdb:tt0133093"},
		{SeriesKey("81189"), "tvdb:81189"},
		{SeasonKey("81189", 2), "tvdb:81189:s2"},
		{TorrentKey("abc"), "hash:abc"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("got %q, want %q", tt.got, tt.want)
		}
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./internal/domain/`
Expected: FAIL to compile with `undefined: DefaultRules` (and `Rules`, `FieldError`, `WatchedAll`, `UnitSeries`).

- [ ] **Step 5: Write the rules**

Create `internal/domain/rules.go`:

```go
package domain

import "strings"

const (
	WatchedAll = "all"
	WatchedAny = "any"

	UnitSeries = "series"
	UnitSeason = "season"
)

// Rules are the user-adjustable settings that decide what is suggested (spec 5.6).
type Rules struct {
	CountingUsers        []string `json:"countingUsers"` // media server user IDs
	WatchedMode          string   `json:"watchedMode"`   // WatchedAll or WatchedAny
	GraceDays            int      `json:"graceDays"`
	SeriesUnit           string   `json:"seriesUnit"` // UnitSeries or UnitSeason
	ProtectFavorites     bool     `json:"protectFavorites"`
	ProtectionTag        string   `json:"protectionTag"`
	MinRatio             float64  `json:"minRatio"`    // 0 means off
	MinSeedDays          float64  `json:"minSeedDays"` // 0 means off
	ExcludedCategories   []string `json:"excludedCategories"`
	ExcludedTags         []string `json:"excludedTags"`
	AddExclusionOnDelete bool     `json:"addExclusionOnDelete"`
}

// DefaultRules returns the rules a fresh install starts with.
func DefaultRules() Rules {
	return Rules{
		CountingUsers:      []string{},
		WatchedMode:        WatchedAll,
		SeriesUnit:         UnitSeries,
		ProtectFavorites:   true,
		ProtectionTag:      "keep",
		ExcludedCategories: []string{},
		ExcludedTags:       []string{},
	}
}

// FieldError reports an invalid value for one named setting.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// Validate checks the rules. It returns a *FieldError naming the first invalid field.
func (r Rules) Validate() error {
	if r.WatchedMode != WatchedAll && r.WatchedMode != WatchedAny {
		return &FieldError{Field: "watchedMode", Message: `must be "all" or "any"`}
	}
	if r.SeriesUnit != UnitSeries && r.SeriesUnit != UnitSeason {
		return &FieldError{Field: "seriesUnit", Message: `must be "series" or "season"`}
	}
	if r.GraceDays < 0 {
		return &FieldError{Field: "graceDays", Message: "must be 0 or more"}
	}
	if r.MinRatio < 0 {
		return &FieldError{Field: "minRatio", Message: "must be 0 or more"}
	}
	if r.MinSeedDays < 0 {
		return &FieldError{Field: "minSeedDays", Message: "must be 0 or more"}
	}
	for _, u := range r.CountingUsers {
		if strings.TrimSpace(u) == "" {
			return &FieldError{Field: "countingUsers", Message: "must not contain empty user IDs"}
		}
	}
	return nil
}
```

- [ ] **Step 6: Run the domain tests to verify they pass**

Run: `go test ./internal/domain/`
Expected: `ok  	github.com/removarr/removarr/internal/domain`

- [ ] **Step 7: Write the failing tests for the watched rule**

Create `internal/scan/watched_test.go`:

```go
package scan

import (
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

// tp parses an RFC 3339 timestamp for tests.
func tp(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return &t
}

// rules returns the default rules with the given watched mode and counting users.
func rules(mode string, users ...string) domain.Rules {
	r := domain.DefaultRules()
	r.WatchedMode = mode
	r.CountingUsers = users
	return r
}

// played returns a played state; at may be empty for an unknown date.
func played(at string) domain.UserState {
	st := domain.UserState{Played: true}
	if at != "" {
		st.LastPlayed = tp(at)
	}
	return st
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func TestEvaluate(t *testing.T) {
	jan, feb := "2026-01-10T20:00:00Z", "2026-02-10T20:00:00Z"
	type states = map[string]domain.UserState
	tests := []struct {
		name    string
		rules   domain.Rules
		states  states
		watched bool
		at      *time.Time
		by      []string
	}{
		{"all: everyone played", rules("all", "a", "b"), states{"a": played(jan), "b": played(feb)}, true, tp(feb), []string{"a", "b"}},
		{"all: one has not played", rules("all", "a", "b"), states{"a": played(jan), "b": {}}, false, tp(jan), []string{"a"}},
		{"all: one has no state", rules("all", "a", "b"), states{"a": played(jan)}, false, tp(jan), []string{"a"}},
		{"any: one played", rules("any", "a", "b"), states{"b": played(feb)}, true, tp(feb), []string{"b"}},
		{"any: nobody played", rules("any", "a", "b"), states{"a": {}}, false, nil, nil},
		{"no counting users", rules("all"), states{"a": played(jan)}, false, nil, nil},
		{"other users are ignored", rules("all", "a"), states{"a": played(jan), "z": played(feb)}, true, tp(jan), []string{"a"}},
		{"played without a date", rules("all", "a"), states{"a": played("")}, true, nil, []string{"a"}},
		{"nil states", rules("all", "a"), nil, false, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Evaluate(tt.rules, tt.states)
			if v.Watched != tt.watched {
				t.Errorf("Watched = %v, want %v", v.Watched, tt.watched)
			}
			if !sameTime(v.At, tt.at) {
				t.Errorf("At = %v, want %v", v.At, tt.at)
			}
			if !reflect.DeepEqual(v.By, tt.by) {
				t.Errorf("By = %v, want %v", v.By, tt.by)
			}
		})
	}
}

func TestGraceOK(t *testing.T) {
	now := *tp("2026-03-10T12:00:00Z")
	r := domain.DefaultRules()
	if !GraceOK(r, tp("2026-03-10T11:59:00Z"), now) {
		t.Error("a grace period of 0 days must always be satisfied")
	}
	r.GraceDays = 7
	if GraceOK(r, tp("2026-03-03T12:00:01Z"), now) {
		t.Error("one second short of 7 days must not satisfy a 7 day grace period")
	}
	if !GraceOK(r, tp("2026-03-03T12:00:00Z"), now) {
		t.Error("exactly 7 days must satisfy a 7 day grace period")
	}
	if !GraceOK(r, nil, now) {
		t.Error("an unknown watched date must satisfy the grace period")
	}
}

func TestMergeStates(t *testing.T) {
	a := map[string]domain.UserState{"u": {Favorite: true}}
	b := map[string]domain.UserState{"u": played("2026-01-01T00:00:00Z"), "v": {}}
	got := mergeStates(a, b)
	if !got["u"].Played || !got["u"].Favorite || got["u"].LastPlayed == nil {
		t.Errorf("merged state for u = %+v, want played, favourite and dated", got["u"])
	}
	if _, ok := got["v"]; !ok {
		t.Error("user v must be kept")
	}
	if a["u"].Played {
		t.Error("mergeStates must not change its arguments")
	}
	if mergeStates(nil, nil) != nil {
		t.Error("merging nothing must give nil")
	}
}
```

- [ ] **Step 8: Run the tests to verify they fail**

Run: `go test ./internal/scan/`
Expected: FAIL to compile with `undefined: Evaluate`, `undefined: GraceOK`, `undefined: mergeStates`.

- [ ] **Step 9: Write the watched rule**

Create `internal/scan/watched.go`:

```go
// Package scan holds the rules that turn snapshots of the apps into suggestion lists
// (spec section 6). It does no I/O.
package scan

import (
	"time"

	"github.com/removarr/removarr/internal/domain"
)

// Verdict is the result of applying the watched rule (spec 6.1) to one movie or episode.
type Verdict struct {
	Watched bool
	At      *time.Time // latest known play date among the counting users who played; nil when unknown
	By      []string   // IDs of the counting users who played, in CountingUsers order
}

// Evaluate applies the watched rule to the per-user states of one item.
func Evaluate(r domain.Rules, states map[string]domain.UserState) Verdict {
	var v Verdict
	for _, id := range r.CountingUsers {
		st, ok := states[id]
		if !ok || !st.Played {
			continue
		}
		v.By = append(v.By, id)
		v.At = later(v.At, st.LastPlayed)
	}
	if r.WatchedMode == domain.WatchedAny {
		v.Watched = len(v.By) > 0
	} else {
		v.Watched = len(r.CountingUsers) > 0 && len(v.By) == len(r.CountingUsers)
	}
	return v
}

// GraceOK reports whether the grace period has passed since the watched date.
// An unknown date satisfies it.
func GraceOK(r domain.Rules, at *time.Time, now time.Time) bool {
	if at == nil || r.GraceDays <= 0 {
		return true
	}
	return now.Sub(*at) >= time.Duration(r.GraceDays)*24*time.Hour
}

// mergeStates combines the states of several media server items that stand for the same
// movie or episode: a user has played or favourited it when that holds for any of the items.
func mergeStates(a, b map[string]domain.UserState) map[string]domain.UserState {
	if a == nil && b == nil {
		return nil
	}
	out := make(map[string]domain.UserState, len(a)+len(b))
	for id, st := range a {
		out[id] = st
	}
	for id, st := range b {
		cur, ok := out[id]
		if !ok {
			out[id] = st
			continue
		}
		cur.Played = cur.Played || st.Played
		cur.Favorite = cur.Favorite || st.Favorite
		cur.LastPlayed = later(cur.LastPlayed, st.LastPlayed)
		out[id] = cur
	}
	return out
}

// later returns the later of two optional times.
func later(a, b *time.Time) *time.Time {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	case b.After(*a):
		return b
	default:
		return a
	}
}
```

- [ ] **Step 10: Run all tests to verify they pass**

Run: `go vet ./... && go test ./...`
Expected: `ok` for `internal/domain` and `internal/scan`, no vet output.

- [ ] **Step 11: Commit**

```bash
git add go.mod .gitignore LICENSE README.md .github internal
git commit -m "feat: scaffold project, domain types and watched rule"
```

---

### Task 2: Torrent links, seeding flag and the orphan torrent list

**Files:**
- Create: `internal/scan/links.go`, `internal/scan/torrents.go`
- Test: `internal/scan/links_test.go`, `internal/scan/torrents_test.go`

**Interfaces:**
- Consumes: package `domain` from Task 1 (`Links`, `Download`, `Rules`, `Candidate`, `TorrentInfo`, `ListTorrents`, `TorrentKey`, `ReasonOrphan`, `ReasonUnlinked`).
- Produces, in package `scan`:
  - `ManagerFiles{Links domain.Links; FileIDs []int}` — one media manager's links and the IDs of its current library files.
  - `NewLinkIndex(managers ...ManagerFiles) *LinkIndex`
  - `(*LinkIndex).Queued(id string) bool`, `(*LinkIndex).Known(id string) bool`, `(*LinkIndex).Backing(id string) int`
  - `(*LinkIndex).Removable(links domain.Links, itemIDs, deletedFileIDs []int) []string` — sorted download IDs; `nil` when none.
  - `SeedBelowMin(r domain.Rules, d domain.Download) bool`
  - `ListResult{Candidates []domain.Candidate; Protected int; Unmatched int}`
  - `TorrentsInput{Rules domain.Rules; Downloads []domain.Download; Index *LinkIndex}`, `Torrents(in TorrentsInput) ListResult`
  - Unexported helpers used by Tasks 3 and 4: `torrentInfo(r domain.Rules, d domain.Download) domain.TorrentInfo`, `downloadsByID(ds []domain.Download) map[string]domain.Download`, `sortByKey(cs []domain.Candidate)`.
  - Test helpers in `links_test.go`: `links() domain.Links` (empty, non-nil maps) and, in `torrents_test.go`, `dl(id string, mods ...func(*domain.Download)) domain.Download`.

- [ ] **Step 1: Write the failing tests for the link index**

Create `internal/scan/links_test.go`:

```go
package scan

import (
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

// links returns empty links with non-nil maps.
func links() domain.Links {
	return domain.Links{Grabbed: map[string][]int{}, FileSource: map[int]string{}, Queue: map[string]bool{}}
}

func TestLinkIndex(t *testing.T) {
	movies := links()
	movies.Grabbed["aaa"] = []int{1}
	movies.Grabbed["old"] = []int{1}
	movies.FileSource[10] = "aaa"
	movies.Queue["qqq"] = true

	series := links()
	series.Grabbed["pack"] = []int{101, 102}
	series.FileSource[20] = "pack"
	series.FileSource[21] = "pack"
	series.FileSource[99] = "gone" // file 99 is not a current file

	x := NewLinkIndex(
		ManagerFiles{Links: movies, FileIDs: []int{10}},
		ManagerFiles{Links: series, FileIDs: []int{20, 21, 21}},
	)

	backing := map[string]int{"aaa": 1, "pack": 2, "old": 0, "gone": 0, "zzz": 0}
	for id, want := range backing {
		if got := x.Backing(id); got != want {
			t.Errorf("Backing(%q) = %d, want %d", id, got, want)
		}
	}
	known := map[string]bool{"aaa": true, "old": true, "pack": true, "qqq": true, "zzz": false}
	for id, want := range known {
		if got := x.Known(id); got != want {
			t.Errorf("Known(%q) = %v, want %v", id, got, want)
		}
	}
	if !x.Queued("qqq") || x.Queued("aaa") {
		t.Error("only qqq is queued")
	}
}

func TestRemovable(t *testing.T) {
	l := links()
	l.Grabbed["new"] = []int{1}     // current release of movie 1
	l.Grabbed["old"] = []int{1}     // replaced by an upgrade
	l.Grabbed["pack"] = []int{2, 3} // collection pack behind movies 2 and 3
	l.Grabbed["queued"] = []int{1}
	l.Grabbed["other"] = []int{4}
	l.FileSource[10] = "new"
	l.FileSource[20] = "pack"
	l.FileSource[30] = "pack"
	l.FileSource[40] = "other"
	l.Queue["queued"] = true
	x := NewLinkIndex(ManagerFiles{Links: l, FileIDs: []int{10, 20, 30, 40}})

	tests := []struct {
		name         string
		items, files []int
		want         []string
	}{
		{"movie with an old and a new release", []int{1}, []int{10}, []string{"new", "old"}},
		{"one movie of a pack keeps the pack", []int{2}, []int{20}, nil},
		{"both movies of a pack free the pack", []int{2, 3}, []int{20, 30}, []string{"pack"}},
		{"a file listed twice is counted once", []int{2}, []int{20, 20}, nil},
		{"nothing associated", []int{99}, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := x.Removable(l, tt.items, tt.files); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Removable = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRemovableCountsFilesInOtherManagers(t *testing.T) {
	a := links()
	a.Grabbed["shared"] = []int{1}
	a.FileSource[10] = "shared"
	b := links()
	b.Grabbed["shared"] = []int{7}
	b.FileSource[70] = "shared"
	x := NewLinkIndex(ManagerFiles{Links: a, FileIDs: []int{10}}, ManagerFiles{Links: b, FileIDs: []int{70}})

	if got := x.Removable(a, []int{1}, []int{10}); got != nil {
		t.Errorf("a download that still backs a file in another manager must stay, got %v", got)
	}
}

func TestSeedBelowMin(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	d := func(days float64) *time.Duration {
		x := time.Duration(days * 24 * float64(time.Hour))
		return &x
	}
	tests := []struct {
		name              string
		minRatio, minDays float64
		ratio             *float64
		seeded            *time.Duration
		want              bool
	}{
		{"both off", 0, 0, f(0.1), d(1), false},
		{"ratio only, below", 1, 0, f(0.5), d(100), true},
		{"ratio only, met", 1, 0, f(1.0), d(0), false},
		{"time only, below", 0, 14, f(5), d(13.9), true},
		{"time only, met", 0, 14, f(0), d(14), false},
		{"both set, both below", 1, 14, f(0.5), d(3), true},
		{"both set, ratio met", 1, 14, f(1.2), d(3), false},
		{"both set, time met", 1, 14, f(0.2), d(20), false},
		{"client does not seed", 1, 14, nil, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := domain.DefaultRules()
			r.MinRatio, r.MinSeedDays = tt.minRatio, tt.minDays
			got := SeedBelowMin(r, domain.Download{Ratio: tt.ratio, SeedingTime: tt.seeded})
			if got != tt.want {
				t.Errorf("SeedBelowMin = %v, want %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/scan/`
Expected: FAIL to compile with `undefined: NewLinkIndex`, `undefined: ManagerFiles`, `undefined: SeedBelowMin`.

- [ ] **Step 3: Write the link index**

Create `internal/scan/links.go`:

```go
package scan

import (
	"sort"

	"github.com/removarr/removarr/internal/domain"
)

// ManagerFiles pairs one media manager's links with the IDs of its current library files.
type ManagerFiles struct {
	Links   domain.Links
	FileIDs []int
}

// LinkIndex answers questions about downloads across all media managers (spec 6.6).
type LinkIndex struct {
	queued  map[string]bool
	known   map[string]bool
	backing map[string]int // download ID -> number of current library files imported from it
}

// NewLinkIndex builds the index from every media manager that answered.
func NewLinkIndex(managers ...ManagerFiles) *LinkIndex {
	x := &LinkIndex{queued: map[string]bool{}, known: map[string]bool{}, backing: map[string]int{}}
	for _, m := range managers {
		for id := range m.Links.Queue {
			if id != "" {
				x.queued[id] = true
				x.known[id] = true
			}
		}
		for id := range m.Links.Grabbed {
			if id != "" {
				x.known[id] = true
			}
		}
		seen := map[int]bool{}
		for _, file := range m.FileIDs {
			if seen[file] {
				continue
			}
			seen[file] = true
			if id := m.Links.FileSource[file]; id != "" {
				x.backing[id]++
				x.known[id] = true
			}
		}
	}
	return x
}

// Queued reports whether the download is in a media manager's queue.
func (x *LinkIndex) Queued(id string) bool { return x.queued[id] }

// Known reports whether any media manager's history or queue mentions the download.
func (x *LinkIndex) Known(id string) bool { return x.known[id] }

// Backing returns how many current library files were imported from the download.
func (x *LinkIndex) Backing(id string) int { return x.backing[id] }

// Removable returns the downloads a deletion would leave with nothing to back (spec 6.7):
// those associated in links with one of itemIDs, not queued, and backing no library file
// once deletedFileIDs are gone. links must belong to the manager the deletion happens in.
// The result is sorted, and nil when empty.
func (x *LinkIndex) Removable(links domain.Links, itemIDs, deletedFileIDs []int) []string {
	items := make(map[int]bool, len(itemIDs))
	for _, id := range itemIDs {
		items[id] = true
	}
	freed := map[string]int{}
	seen := map[int]bool{}
	for _, file := range deletedFileIDs {
		if seen[file] {
			continue
		}
		seen[file] = true
		if id := links.FileSource[file]; id != "" {
			freed[id]++
		}
	}
	var out []string
	for id, grabbedFor := range links.Grabbed {
		if id == "" || x.queued[id] {
			continue
		}
		associated := false
		for _, item := range grabbedFor {
			if items[item] {
				associated = true
				break
			}
		}
		if !associated || x.backing[id]-freed[id] > 0 {
			continue
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// SeedBelowMin reports whether a download is below the seeding minimum (spec 6.6).
// With both minimums set, it is below only when it is below both.
func SeedBelowMin(r domain.Rules, d domain.Download) bool {
	ratioSet, timeSet := r.MinRatio > 0, r.MinSeedDays > 0
	belowRatio := ratioSet && d.Ratio != nil && *d.Ratio < r.MinRatio
	belowTime := timeSet && d.SeedingTime != nil && d.SeedingTime.Hours() < r.MinSeedDays*24
	switch {
	case ratioSet && timeSet:
		return belowRatio && belowTime
	case ratioSet:
		return belowRatio
	case timeSet:
		return belowTime
	default:
		return false
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/scan/`
Expected: `ok  	github.com/removarr/removarr/internal/scan`

- [ ] **Step 5: Write the failing test for the orphan torrent list**

Create `internal/scan/torrents_test.go`:

```go
package scan

import (
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

// dl returns a healthy, long-seeded download; mods adjust it.
func dl(id string, mods ...func(*domain.Download)) domain.Download {
	ratio := 2.0
	seeded := 30 * 24 * time.Hour
	d := domain.Download{ID: id, Name: "name-" + id, Size: 100, State: "uploading", Ratio: &ratio, SeedingTime: &seeded}
	for _, m := range mods {
		m(&d)
	}
	return d
}

func TestTorrents(t *testing.T) {
	l := links()
	l.Grabbed["linked"] = []int{1}
	l.FileSource[10] = "linked"
	l.Grabbed["unlinked"] = []int{1}
	l.Grabbed["queued"] = []int{2}
	l.Queue["queued"] = true
	x := NewLinkIndex(ManagerFiles{Links: l, FileIDs: []int{10}})

	r := domain.DefaultRules()
	r.ExcludedCategories = []string{"manual"}
	r.ExcludedTags = []string{"cross-seed"}
	r.MinRatio = 1
	low := 0.2

	res := Torrents(TorrentsInput{Rules: r, Index: x, Downloads: []domain.Download{
		dl("linked"),
		dl("unlinked"),
		dl("queued"),
		dl("orphan", func(d *domain.Download) { d.Ratio = &low }),
		dl("cat", func(d *domain.Download) { d.Category = "manual" }),
		dl("tag", func(d *domain.Download) { d.Tags = []string{"x", "cross-seed"} }),
		dl(""),
	}})

	if len(res.Candidates) != 2 {
		t.Fatalf("got %d candidates, want 2 (orphan and unlinked): %+v", len(res.Candidates), res.Candidates)
	}
	orphan, unlinked := res.Candidates[0], res.Candidates[1]
	if orphan.Key != "hash:orphan" || orphan.Reason != domain.ReasonOrphan {
		t.Errorf("first row = %+v, want the orphan", orphan)
	}
	if unlinked.Key != "hash:unlinked" || unlinked.Reason != domain.ReasonUnlinked {
		t.Errorf("second row = %+v, want the unlinked torrent", unlinked)
	}
	if orphan.List != domain.ListTorrents || orphan.Title != "name-orphan" || orphan.Size != 100 {
		t.Errorf("orphan row fields wrong: %+v", orphan)
	}
	if !orphan.BelowSeedMin || len(orphan.Torrents) != 1 || !orphan.Torrents[0].BelowSeedMin {
		t.Errorf("orphan must be flagged below the seeding minimum: %+v", orphan)
	}
	if unlinked.BelowSeedMin {
		t.Errorf("unlinked torrent has ratio 2 and must not be flagged: %+v", unlinked)
	}
	if got := unlinked.Torrents[0].SeedingDays; got == nil || *got != 30 {
		t.Errorf("SeedingDays = %v, want 30", got)
	}
}
```

- [ ] **Step 6: Run the test to verify it fails**

Run: `go test ./internal/scan/ -run TestTorrents`
Expected: FAIL to compile with `undefined: Torrents`, `undefined: TorrentsInput`.

- [ ] **Step 7: Write the orphan torrent list**

Create `internal/scan/torrents.go`:

```go
package scan

import (
	"sort"

	"github.com/removarr/removarr/internal/domain"
)

// ListResult is one computed list plus the counts shown in the scan summary.
type ListResult struct {
	Candidates []domain.Candidate
	Protected  int // items held back by a protection rule
	Unmatched  int // items that could not be matched to the media server
}

// TorrentsInput is everything the orphan torrent list is computed from.
type TorrentsInput struct {
	Rules     domain.Rules
	Downloads []domain.Download
	Index     *LinkIndex
}

// Torrents returns the orphan and unlinked torrents (spec 6.6), sorted by key.
func Torrents(in TorrentsInput) ListResult {
	var res ListResult
	for _, d := range in.Downloads {
		if d.ID == "" || excluded(in.Rules, d) || in.Index.Queued(d.ID) || in.Index.Backing(d.ID) > 0 {
			continue
		}
		reason := domain.ReasonOrphan
		if in.Index.Known(d.ID) {
			reason = domain.ReasonUnlinked
		}
		info := torrentInfo(in.Rules, d)
		res.Candidates = append(res.Candidates, domain.Candidate{
			List:         domain.ListTorrents,
			Key:          domain.TorrentKey(d.ID),
			Title:        d.Name,
			Size:         d.Size,
			Reason:       reason,
			Torrents:     []domain.TorrentInfo{info},
			BelowSeedMin: info.BelowSeedMin,
		})
	}
	sortByKey(res.Candidates)
	return res
}

// excluded reports whether the rules leave a download out of orphan detection.
func excluded(r domain.Rules, d domain.Download) bool {
	for _, c := range r.ExcludedCategories {
		if c == d.Category {
			return true
		}
	}
	for _, want := range r.ExcludedTags {
		for _, have := range d.Tags {
			if want == have {
				return true
			}
		}
	}
	return false
}

// torrentInfo turns a download into the details a row shows.
func torrentInfo(r domain.Rules, d domain.Download) domain.TorrentInfo {
	info := domain.TorrentInfo{
		ID:           d.ID,
		Name:         d.Name,
		Size:         d.Size,
		State:        d.State,
		Category:     d.Category,
		Tracker:      d.Tracker,
		Ratio:        d.Ratio,
		AddedAt:      d.AddedAt,
		BelowSeedMin: SeedBelowMin(r, d),
	}
	if d.SeedingTime != nil {
		days := d.SeedingTime.Hours() / 24
		info.SeedingDays = &days
	}
	return info
}

// downloadsByID indexes downloads by ID.
func downloadsByID(ds []domain.Download) map[string]domain.Download {
	out := make(map[string]domain.Download, len(ds))
	for _, d := range ds {
		out[d.ID] = d
	}
	return out
}

// sortByKey orders candidates by key so results are deterministic.
func sortByKey(cs []domain.Candidate) {
	sort.Slice(cs, func(i, j int) bool { return cs[i].Key < cs[j].Key })
}
```

- [ ] **Step 8: Run all tests to verify they pass**

Run: `go vet ./... && go test ./...`
Expected: `ok` for both packages, no vet output.

- [ ] **Step 9: Commit**

```bash
git add internal/scan
git commit -m "feat: torrent links, seeding flag and orphan torrent list"
```

---

### Task 3: The watched movie list

**Files:**
- Create: `internal/scan/helpers.go`, `internal/scan/movies.go`
- Test: `internal/scan/movies_test.go`

**Interfaces:**
- Consumes:
  - Task 1: `domain` types; `Evaluate`, `GraceOK`, `mergeStates`; test helpers `tp`, `rules`, `played`, `sameTime`.
  - Task 2: `LinkIndex`, `NewLinkIndex`, `ManagerFiles`, `(*LinkIndex).Removable`, `ListResult`, `torrentInfo`, `downloadsByID`, `sortByKey`; test helpers `links()`, `dl(...)`.
- Produces, in package `scan`:
  - `MoviesInput{Rules domain.Rules; Now time.Time; Users []domain.User; Library domain.Library; Snapshot domain.MovieSnapshot; Downloads []domain.Download; Index *LinkIndex}`
  - `Movies(in MoviesInput) ListResult`
  - Unexported helpers used by Task 4: `userNames(users []domain.User) map[string]string`, `namesOf(names map[string]string, ids []string) []string`, `hasTag(tags []string, tag string) bool`, `anyFavorite(r domain.Rules, states map[string]domain.UserState) bool`, `torrentInfos(r domain.Rules, byID map[string]domain.Download, ids []string) ([]domain.TorrentInfo, bool)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/scan/movies_test.go`:

```go
package scan

import (
	"reflect"
	"testing"

	"github.com/removarr/removarr/internal/domain"
)

// movieInput returns a scan input with one movie, The Matrix, watched by Alice nine days ago
// and backed by torrent h1. mods adjust the input before the link index is built.
func movieInput(mods ...func(*MoviesInput)) MoviesInput {
	l := links()
	l.Grabbed["h1"] = []int{1}
	l.FileSource[10] = "h1"
	in := MoviesInput{
		Rules: rules("all", "a"),
		Now:   *tp("2026-03-10T12:00:00Z"),
		Users: []domain.User{{ID: "a", Name: "Alice"}, {ID: "b", Name: "Bob"}},
		Library: domain.Library{Movies: []domain.ServerMovie{{
			ID: "j1", Title: "The Matrix",
			IDs:    domain.ExternalIDs{TMDB: "603", IMDB: "tt0133093"},
			States: map[string]domain.UserState{"a": played("2026-03-01T20:00:00Z")},
		}}},
		Snapshot: domain.MovieSnapshot{
			Movies: []domain.Movie{{
				ID: 1, Title: "The Matrix", Year: 1999,
				IDs:  domain.ExternalIDs{TMDB: "603", IMDB: "tt0133093"},
				File: &domain.MediaFile{ID: 10, Size: 5000},
			}},
			Links: l,
		},
		Downloads: []domain.Download{dl("h1")},
	}
	for _, m := range mods {
		m(&in)
	}
	var fileIDs []int
	for _, m := range in.Snapshot.Movies {
		if m.File != nil {
			fileIDs = append(fileIDs, m.File.ID)
		}
	}
	in.Index = NewLinkIndex(ManagerFiles{Links: in.Snapshot.Links, FileIDs: fileIDs})
	return in
}

func TestMoviesSuggestsAWatchedMovie(t *testing.T) {
	res := Movies(movieInput())
	if len(res.Candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(res.Candidates))
	}
	c := res.Candidates[0]
	if c.List != domain.ListMovies || c.Key != "tmdb:603" || c.Title != "The Matrix" || c.Year != 1999 || c.Size != 5000 {
		t.Errorf("row fields wrong: %+v", c)
	}
	if !reflect.DeepEqual(c.WatchedBy, []string{"Alice"}) {
		t.Errorf("WatchedBy = %v, want [Alice]", c.WatchedBy)
	}
	if !sameTime(c.WatchedAt, tp("2026-03-01T20:00:00Z")) {
		t.Errorf("WatchedAt = %v", c.WatchedAt)
	}
	if len(c.Torrents) != 1 || c.Torrents[0].ID != "h1" || c.BelowSeedMin {
		t.Errorf("torrents wrong: %+v", c.Torrents)
	}
	if res.Protected != 0 || res.Unmatched != 0 {
		t.Errorf("counts = %d protected, %d unmatched, want 0 and 0", res.Protected, res.Unmatched)
	}
}

func TestMoviesSkips(t *testing.T) {
	tests := []struct {
		name      string
		mod       func(*MoviesInput)
		protected int
		unmatched int
	}{
		{"not watched", func(in *MoviesInput) {
			in.Library.Movies[0].States = map[string]domain.UserState{"a": {}}
		}, 0, 0},
		{"grace period not over", func(in *MoviesInput) { in.Rules.GraceDays = 30 }, 0, 0},
		{"no file", func(in *MoviesInput) { in.Snapshot.Movies[0].File = nil }, 0, 0},
		{"no match on the media server", func(in *MoviesInput) { in.Library.Movies = nil }, 0, 1},
		{"protected by tag", func(in *MoviesInput) { in.Snapshot.Movies[0].Tags = []string{"Keep"} }, 1, 0},
		{"protected by favourite", func(in *MoviesInput) {
			st := played("2026-03-01T20:00:00Z")
			st.Favorite = true
			in.Library.Movies[0].States = map[string]domain.UserState{"a": st}
		}, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Movies(movieInput(tt.mod))
			if len(res.Candidates) != 0 {
				t.Errorf("got %d candidates, want 0", len(res.Candidates))
			}
			if res.Protected != tt.protected || res.Unmatched != tt.unmatched {
				t.Errorf("counts = %d protected, %d unmatched, want %d and %d",
					res.Protected, res.Unmatched, tt.protected, tt.unmatched)
			}
		})
	}
}

func TestMoviesUnwatchedProtectedMovieIsNotCounted(t *testing.T) {
	res := Movies(movieInput(func(in *MoviesInput) {
		in.Snapshot.Movies[0].Tags = []string{"keep"}
		in.Library.Movies[0].States = map[string]domain.UserState{"a": {}}
	}))
	if res.Protected != 0 {
		t.Errorf("Protected = %d, want 0: only movies that would otherwise be suggested count", res.Protected)
	}
}

func TestMoviesFavouriteProtectionCanBeTurnedOff(t *testing.T) {
	res := Movies(movieInput(func(in *MoviesInput) {
		in.Rules.ProtectFavorites = false
		st := played("2026-03-01T20:00:00Z")
		st.Favorite = true
		in.Library.Movies[0].States = map[string]domain.UserState{"a": st}
	}))
	if len(res.Candidates) != 1 {
		t.Errorf("got %d candidates, want 1", len(res.Candidates))
	}
}

func TestMoviesFallsBackToIMDb(t *testing.T) {
	res := Movies(movieInput(func(in *MoviesInput) {
		in.Library.Movies[0].IDs = domain.ExternalIDs{IMDB: "tt0133093"}
	}))
	if len(res.Candidates) != 1 {
		t.Errorf("got %d candidates, want 1 (matched by IMDb ID)", len(res.Candidates))
	}
}

func TestMoviesMergesSeveralServerItems(t *testing.T) {
	res := Movies(movieInput(func(in *MoviesInput) {
		in.Rules.CountingUsers = []string{"a", "b"}
		in.Library.Movies = append(in.Library.Movies, domain.ServerMovie{
			ID: "j2", Title: "The Matrix (4K)",
			IDs:    domain.ExternalIDs{TMDB: "603"},
			States: map[string]domain.UserState{"b": played("2026-03-05T20:00:00Z")},
		})
	}))
	if len(res.Candidates) != 1 {
		t.Fatalf("got %d candidates, want 1: Alice and Bob each watched one copy", len(res.Candidates))
	}
	c := res.Candidates[0]
	if !reflect.DeepEqual(c.WatchedBy, []string{"Alice", "Bob"}) || !sameTime(c.WatchedAt, tp("2026-03-05T20:00:00Z")) {
		t.Errorf("WatchedBy = %v, WatchedAt = %v", c.WatchedBy, c.WatchedAt)
	}
}

func TestMoviesFlagsSeedingAndToleratesMissingTorrents(t *testing.T) {
	res := Movies(movieInput(func(in *MoviesInput) { in.Rules.MinRatio = 3 }))
	if !res.Candidates[0].BelowSeedMin {
		t.Error("ratio 2 is below a minimum of 3, the row must be flagged")
	}

	res = Movies(movieInput(func(in *MoviesInput) { in.Downloads = nil }))
	if got := res.Candidates[0].Torrents; got == nil || len(got) != 0 {
		t.Errorf("Torrents = %v, want an empty, non-nil list when the torrent is gone from the client", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/scan/ -run TestMovies`
Expected: FAIL to compile with `undefined: MoviesInput`, `undefined: Movies`.

- [ ] **Step 3: Write the shared helpers**

Create `internal/scan/helpers.go`:

```go
package scan

import (
	"strings"

	"github.com/removarr/removarr/internal/domain"
)

// userNames maps user IDs to display names.
func userNames(users []domain.User) map[string]string {
	out := make(map[string]string, len(users))
	for _, u := range users {
		out[u.ID] = u.Name
	}
	return out
}

// namesOf returns the display names of the given user IDs, falling back to the ID itself.
func namesOf(names map[string]string, ids []string) []string {
	var out []string
	for _, id := range ids {
		if n := names[id]; n != "" {
			out = append(out, n)
		} else {
			out = append(out, id)
		}
	}
	return out
}

// hasTag reports whether tags contains tag, ignoring case. An empty tag matches nothing.
func hasTag(tags []string, tag string) bool {
	if tag == "" {
		return false
	}
	for _, t := range tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

// anyFavorite reports whether a counting user marked the item a favourite.
func anyFavorite(r domain.Rules, states map[string]domain.UserState) bool {
	for _, id := range r.CountingUsers {
		if states[id].Favorite {
			return true
		}
	}
	return false
}

// torrentInfos returns the details of those downloads that exist in the client, and whether
// any of them is below the seeding minimum. The returned slice is never nil.
func torrentInfos(r domain.Rules, byID map[string]domain.Download, ids []string) ([]domain.TorrentInfo, bool) {
	infos := []domain.TorrentInfo{}
	below := false
	for _, id := range ids {
		d, ok := byID[id]
		if !ok {
			continue
		}
		info := torrentInfo(r, d)
		infos = append(infos, info)
		below = below || info.BelowSeedMin
	}
	return infos, below
}
```

- [ ] **Step 4: Write the movie list**

Create `internal/scan/movies.go`:

```go
package scan

import (
	"time"

	"github.com/removarr/removarr/internal/domain"
)

// MoviesInput is everything the watched movie list is computed from.
type MoviesInput struct {
	Rules     domain.Rules
	Now       time.Time
	Users     []domain.User
	Library   domain.Library
	Snapshot  domain.MovieSnapshot
	Downloads []domain.Download
	Index     *LinkIndex
}

// Movies returns the watched movie list (spec 6.2 to 6.4), sorted by key.
func Movies(in MoviesInput) ListResult {
	var res ListResult
	names := userNames(in.Users)
	downloads := downloadsByID(in.Downloads)

	byTMDB := map[string][]domain.ServerMovie{}
	byIMDB := map[string][]domain.ServerMovie{}
	for _, m := range in.Library.Movies {
		if m.IDs.TMDB != "" {
			byTMDB[m.IDs.TMDB] = append(byTMDB[m.IDs.TMDB], m)
		}
		if m.IDs.IMDB != "" {
			byIMDB[m.IDs.IMDB] = append(byIMDB[m.IDs.IMDB], m)
		}
	}

	for _, m := range in.Snapshot.Movies {
		if m.File == nil {
			continue
		}
		var matches []domain.ServerMovie
		if m.IDs.TMDB != "" {
			matches = byTMDB[m.IDs.TMDB]
		}
		if len(matches) == 0 && m.IDs.IMDB != "" {
			matches = byIMDB[m.IDs.IMDB]
		}
		if len(matches) == 0 {
			res.Unmatched++
			continue
		}
		var states map[string]domain.UserState
		for _, match := range matches {
			states = mergeStates(states, match.States)
		}
		v := Evaluate(in.Rules, states)
		if !v.Watched || !GraceOK(in.Rules, v.At, in.Now) {
			continue
		}
		if hasTag(m.Tags, in.Rules.ProtectionTag) || (in.Rules.ProtectFavorites && anyFavorite(in.Rules, states)) {
			res.Protected++
			continue
		}
		removable := in.Index.Removable(in.Snapshot.Links, []int{m.ID}, []int{m.File.ID})
		torrents, below := torrentInfos(in.Rules, downloads, removable)
		res.Candidates = append(res.Candidates, domain.Candidate{
			List:         domain.ListMovies,
			Key:          domain.MovieKey(m.IDs),
			Title:        m.Title,
			Year:         m.Year,
			Size:         m.File.Size,
			WatchedBy:    namesOf(names, v.By),
			WatchedAt:    v.At,
			Torrents:     torrents,
			BelowSeedMin: below,
		})
	}
	sortByKey(res.Candidates)
	return res
}
```

- [ ] **Step 5: Run all tests to verify they pass**

Run: `go vet ./... && go test ./...`
Expected: `ok` for both packages, no vet output.

- [ ] **Step 6: Commit**

```bash
git add internal/scan
git commit -m "feat: watched movie list"
```

---

### Task 4: The watched series list

**Files:**
- Create: `internal/scan/series.go`
- Test: `internal/scan/series_test.go`

**Interfaces:**
- Consumes:
  - Task 1: `domain` types; `Evaluate`, `GraceOK`, `mergeStates`, `later`; test helpers `tp`, `rules`, `played`.
  - Task 2: `LinkIndex`, `NewLinkIndex`, `ManagerFiles`, `(*LinkIndex).Removable`, `ListResult`, `downloadsByID`, `sortByKey`; test helpers `links()`, `dl(...)`.
  - Task 3: `userNames`, `namesOf`, `hasTag`, `anyFavorite`, `torrentInfos`.
- Produces, in package `scan`:
  - `SeriesInput{Rules domain.Rules; Now time.Time; Users []domain.User; Library domain.Library; Snapshot domain.SeriesSnapshot; Downloads []domain.Download; Index *LinkIndex; Ignored map[string]bool}` — `Ignored` holds the candidate keys the user has ignored; stage 1 always passes `nil`.
  - `Series(in SeriesInput) ListResult`

The rules this task implements, from spec 6.3 and 6.7:

- Only episodes with a file count. Regular seasons are season 1 and up; season 0 holds specials.
- A regular season qualifies when it has a file and every episode with a file is watched and past the grace period.
- Whole-series mode: one row when every regular season with files qualifies.
- Per-season mode: one row per qualifying regular season. When every regular season with files qualifies, the rows merge into one series row, unless one of those seasons is individually ignored.
- Specials never block a series and never get a row.
- A series row for an ended series covers every file, specials included, and every episode of the series. A series row for a series that has not ended covers the regular-season files only.
- An episode with no counterpart on the media server counts as unwatched and is counted as unmatched (regular seasons only).
- A protected series produces no rows and is counted once, but only if it would otherwise have produced a row.

- [ ] **Step 1: Write the failing tests**

Create `internal/scan/series_test.go`:

```go
package scan

import (
	"reflect"
	"testing"

	"github.com/removarr/removarr/internal/domain"
)

var (
	seen   = map[string]domain.UserState{"a": played("2026-03-01T20:00:00Z")}
	unseen = map[string]domain.UserState{"a": {}}
)

// ep builds a managed episode. A fileID of 0 means the episode has no file; files are 100 bytes.
func ep(id, season, number, fileID int) domain.Episode {
	e := domain.Episode{ID: id, Season: season, Number: number}
	if fileID != 0 {
		e.File = &domain.MediaFile{ID: fileID, Size: 100}
	}
	return e
}

// show builds a managed series with TVDB ID 100.
func show(ended bool, eps ...domain.Episode) domain.Series {
	return domain.Series{
		ID: 1, Title: "The Show", Year: 2020, Ended: ended,
		IDs:      domain.ExternalIDs{TVDB: "100", IMDB: "tt100"},
		Episodes: eps,
	}
}

// sep builds a media server episode of the series "js1".
func sep(season, number int, states map[string]domain.UserState) domain.ServerEpisode {
	return domain.ServerEpisode{SeriesID: "js1", Season: season, Episode: number, States: states}
}

// seriesInput returns a scan input for one series. The media server knows it as "js1" by its
// TVDB ID. mods adjust the input before the link index is built.
func seriesInput(s domain.Series, eps []domain.ServerEpisode, mods ...func(*SeriesInput)) SeriesInput {
	in := SeriesInput{
		Rules: rules("all", "a"),
		Now:   *tp("2026-03-10T12:00:00Z"),
		Users: []domain.User{{ID: "a", Name: "Alice"}, {ID: "b", Name: "Bob"}},
		Library: domain.Library{
			Series:   []domain.ServerSeries{{ID: "js1", Title: s.Title, IDs: domain.ExternalIDs{TVDB: "100"}}},
			Episodes: eps,
		},
		Snapshot: domain.SeriesSnapshot{Series: []domain.Series{s}, Links: links()},
	}
	for _, m := range mods {
		m(&in)
	}
	var fileIDs []int
	for _, sr := range in.Snapshot.Series {
		for _, e := range sr.Episodes {
			if e.File != nil {
				fileIDs = append(fileIDs, e.File.ID)
			}
		}
	}
	in.Index = NewLinkIndex(ManagerFiles{Links: in.Snapshot.Links, FileIDs: fileIDs})
	return in
}

func perSeason(in *SeriesInput) { in.Rules.SeriesUnit = domain.UnitSeason }

func keysOf(cs []domain.Candidate) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Key)
	}
	return out
}

func torrentIDs(c domain.Candidate) []string {
	var out []string
	for _, t := range c.Torrents {
		out = append(out, t.ID)
	}
	return out
}

// twoSeasons is a series with a special, two episodes in season 1 and one in season 2.
func twoSeasons(ended bool) domain.Series {
	return show(ended, ep(1, 0, 1, 1), ep(11, 1, 1, 11), ep(12, 1, 2, 12), ep(21, 2, 1, 21))
}

func allSeen() []domain.ServerEpisode {
	return []domain.ServerEpisode{sep(1, 1, seen), sep(1, 2, seen), sep(2, 1, seen)}
}

func TestSeriesWholeSeriesEnded(t *testing.T) {
	res := Series(seriesInput(twoSeasons(true), allSeen()))
	if len(res.Candidates) != 1 {
		t.Fatalf("got %d rows, want 1", len(res.Candidates))
	}
	c := res.Candidates[0]
	if c.List != domain.ListSeries || c.Key != "tvdb:100" || c.Scope != domain.ScopeSeries || c.Title != "The Show" || c.Year != 2020 {
		t.Errorf("row fields wrong: %+v", c)
	}
	if c.StillAiring {
		t.Error("an ended series must not be labelled still airing")
	}
	if c.Size != 400 {
		t.Errorf("Size = %d, want 400: an ended series is removed with its specials", c.Size)
	}
	if !reflect.DeepEqual(c.WatchedBy, []string{"Alice"}) || !sameTime(c.WatchedAt, tp("2026-03-01T20:00:00Z")) {
		t.Errorf("WatchedBy = %v, WatchedAt = %v", c.WatchedBy, c.WatchedAt)
	}
	if res.Unmatched != 0 {
		t.Errorf("Unmatched = %d, want 0: specials are not counted", res.Unmatched)
	}
}

func TestSeriesWholeSeriesStillAiring(t *testing.T) {
	res := Series(seriesInput(twoSeasons(false), allSeen()))
	if len(res.Candidates) != 1 {
		t.Fatalf("got %d rows, want 1", len(res.Candidates))
	}
	c := res.Candidates[0]
	if !c.StillAiring {
		t.Error("a series that has not ended must be labelled still airing")
	}
	if c.Size != 300 {
		t.Errorf("Size = %d, want 300: specials stay while the series stays", c.Size)
	}
}

func TestSeriesWholeSeriesNeedsEverythingWatched(t *testing.T) {
	eps := []domain.ServerEpisode{sep(1, 1, seen), sep(1, 2, seen), sep(2, 1, unseen)}
	res := Series(seriesInput(twoSeasons(true), eps))
	if len(res.Candidates) != 0 {
		t.Errorf("got rows %v, want none", keysOf(res.Candidates))
	}
}

func TestSeriesPerSeason(t *testing.T) {
	eps := []domain.ServerEpisode{sep(1, 1, seen), sep(1, 2, seen), sep(2, 1, unseen)}
	res := Series(seriesInput(twoSeasons(true), eps, perSeason))
	if len(res.Candidates) != 1 {
		t.Fatalf("got rows %v, want only season 1", keysOf(res.Candidates))
	}
	c := res.Candidates[0]
	if c.Key != "tvdb:100:s1" || c.Scope != domain.ScopeSeason || c.Season != 1 || c.Size != 200 {
		t.Errorf("season row wrong: %+v", c)
	}
	if c.StillAiring {
		t.Error("season rows are never labelled still airing")
	}
}

func TestSeriesPerSeasonMergesWhenEverythingIsWatched(t *testing.T) {
	res := Series(seriesInput(twoSeasons(true), allSeen(), perSeason))
	if got := keysOf(res.Candidates); !reflect.DeepEqual(got, []string{"tvdb:100"}) {
		t.Errorf("rows = %v, want one merged series row", got)
	}
}

func TestSeriesPerSeasonDoesNotMergeOverAnIgnoredSeason(t *testing.T) {
	res := Series(seriesInput(twoSeasons(true), allSeen(), perSeason, func(in *SeriesInput) {
		in.Ignored = map[string]bool{"tvdb:100:s2": true}
	}))
	if got := keysOf(res.Candidates); !reflect.DeepEqual(got, []string{"tvdb:100:s1", "tvdb:100:s2"}) {
		t.Errorf("rows = %v, want two season rows", got)
	}
}

func TestSeriesIgnoredSeasonHasNoEffectInWholeSeriesMode(t *testing.T) {
	res := Series(seriesInput(twoSeasons(true), allSeen(), func(in *SeriesInput) {
		in.Ignored = map[string]bool{"tvdb:100:s2": true}
	}))
	if got := keysOf(res.Candidates); !reflect.DeepEqual(got, []string{"tvdb:100"}) {
		t.Errorf("rows = %v, want the series row", got)
	}
}

func TestSeriesGracePeriod(t *testing.T) {
	res := Series(seriesInput(twoSeasons(true), allSeen(), func(in *SeriesInput) { in.Rules.GraceDays = 30 }))
	if len(res.Candidates) != 0 {
		t.Errorf("got rows %v, want none: watched nine days ago, grace is thirty", keysOf(res.Candidates))
	}
}

func TestSeriesUnmatchedEpisodeCountsAsUnwatched(t *testing.T) {
	eps := []domain.ServerEpisode{sep(1, 1, seen), sep(2, 1, seen)} // season 1 episode 2 is missing
	res := Series(seriesInput(twoSeasons(true), eps, perSeason))
	if got := keysOf(res.Candidates); !reflect.DeepEqual(got, []string{"tvdb:100:s2"}) {
		t.Errorf("rows = %v, want only season 2", got)
	}
	if res.Unmatched != 1 {
		t.Errorf("Unmatched = %d, want 1", res.Unmatched)
	}
}

func TestSeriesUnknownToTheMediaServer(t *testing.T) {
	res := Series(seriesInput(twoSeasons(true), allSeen(), func(in *SeriesInput) { in.Library.Series = nil }))
	if len(res.Candidates) != 0 {
		t.Errorf("got rows %v, want none", keysOf(res.Candidates))
	}
	if res.Unmatched != 3 {
		t.Errorf("Unmatched = %d, want 3 regular-season episodes", res.Unmatched)
	}
}

func TestSeriesEpisodeRange(t *testing.T) {
	double := domain.ServerEpisode{SeriesID: "js1", Season: 1, Episode: 1, EpisodeEnd: 2, States: seen}
	eps := []domain.ServerEpisode{double, sep(2, 1, seen)}
	res := Series(seriesInput(twoSeasons(true), eps))
	if len(res.Candidates) != 1 {
		t.Errorf("got %d rows, want 1: one media server item covers episodes 1 and 2", len(res.Candidates))
	}
}

func TestSeriesProtection(t *testing.T) {
	fav := played("2026-03-01T20:00:00Z")
	fav.Favorite = true
	tests := []struct {
		name string
		eps  []domain.ServerEpisode
		mod  func(*SeriesInput)
	}{
		{"tag", allSeen(), func(in *SeriesInput) { in.Snapshot.Series[0].Tags = []string{"keep"} }},
		{"series favourite", allSeen(), func(in *SeriesInput) {
			in.Library.Series[0].FavoriteOf = map[string]bool{"a": true}
		}},
		{"episode favourite", []domain.ServerEpisode{
			sep(1, 1, map[string]domain.UserState{"a": fav}), sep(1, 2, seen), sep(2, 1, seen),
		}, func(*SeriesInput) {}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Series(seriesInput(twoSeasons(true), tt.eps, perSeason, tt.mod))
			if len(res.Candidates) != 0 || res.Protected != 1 {
				t.Errorf("rows = %v, Protected = %d; want no rows and 1 protected series",
					keysOf(res.Candidates), res.Protected)
			}
		})
	}
}

func TestSeriesUnwatchedProtectedSeriesIsNotCounted(t *testing.T) {
	eps := []domain.ServerEpisode{sep(1, 1, unseen), sep(1, 2, unseen), sep(2, 1, unseen)}
	res := Series(seriesInput(twoSeasons(true), eps, func(in *SeriesInput) {
		in.Snapshot.Series[0].Tags = []string{"keep"}
	}))
	if res.Protected != 0 {
		t.Errorf("Protected = %d, want 0: only series that would otherwise be suggested count", res.Protected)
	}
}

func TestSeriesFallsBackToIMDb(t *testing.T) {
	res := Series(seriesInput(twoSeasons(true), allSeen(), func(in *SeriesInput) {
		in.Library.Series[0].IDs = domain.ExternalIDs{IMDB: "tt100"}
	}))
	if len(res.Candidates) != 1 {
		t.Errorf("got %d rows, want 1 (matched by IMDb ID)", len(res.Candidates))
	}
}

func TestSeriesWithoutFilesIsSkipped(t *testing.T) {
	res := Series(seriesInput(show(true, ep(11, 1, 1, 0)), []domain.ServerEpisode{sep(1, 1, seen)}))
	if len(res.Candidates) != 0 || res.Unmatched != 0 || res.Protected != 0 {
		t.Errorf("a series with no files must produce nothing: %+v", res)
	}
}

func TestSeriesTorrents(t *testing.T) {
	threeSeasons := show(false, ep(11, 1, 1, 11), ep(12, 1, 2, 12), ep(21, 2, 1, 21), ep(31, 3, 1, 31))
	withLinks := func(in *SeriesInput) {
		l := in.Snapshot.Links
		l.Grabbed["s1pack"] = []int{11, 12} // season 1 pack
		l.FileSource[11], l.FileSource[12] = "s1pack", "s1pack"
		l.Grabbed["bigpack"] = []int{21, 31} // pack covering seasons 2 and 3
		l.FileSource[21], l.FileSource[31] = "bigpack", "bigpack"
		l.Grabbed["old"] = []int{11} // replaced by s1pack
		in.Downloads = []domain.Download{dl("s1pack"), dl("bigpack"), dl("old")}
	}

	// Seasons 1 and 2 watched, season 3 not: the big pack still backs season 3.
	eps := []domain.ServerEpisode{sep(1, 1, seen), sep(1, 2, seen), sep(2, 1, seen), sep(3, 1, unseen)}
	res := Series(seriesInput(threeSeasons, eps, perSeason, withLinks))
	if got := keysOf(res.Candidates); !reflect.DeepEqual(got, []string{"tvdb:100:s1", "tvdb:100:s2"}) {
		t.Fatalf("rows = %v, want seasons 1 and 2", got)
	}
	if got := torrentIDs(res.Candidates[0]); !reflect.DeepEqual(got, []string{"old", "s1pack"}) {
		t.Errorf("season 1 torrents = %v, want [old s1pack]", got)
	}
	if got := res.Candidates[1].Torrents; got == nil || len(got) != 0 {
		t.Errorf("season 2 torrents = %v, want empty: the big pack stays", got)
	}

	// Everything watched: one series row that frees every torrent.
	eps[3] = sep(3, 1, seen)
	res = Series(seriesInput(threeSeasons, eps, perSeason, withLinks))
	if got := torrentIDs(res.Candidates[0]); !reflect.DeepEqual(got, []string{"bigpack", "old", "s1pack"}) {
		t.Errorf("series torrents = %v, want [bigpack old s1pack]", got)
	}
}

func TestSeriesEndedRowTakesTorrentsOfEpisodesWithoutFiles(t *testing.T) {
	withStale := func(in *SeriesInput) {
		in.Snapshot.Links.Grabbed["stale"] = []int{13} // grabbed for an episode whose file is gone
		in.Downloads = []domain.Download{dl("stale")}
	}
	eps := []domain.ServerEpisode{sep(1, 1, seen)}

	ended := show(true, ep(11, 1, 1, 11), ep(13, 1, 3, 0))
	res := Series(seriesInput(ended, eps, withStale))
	if got := torrentIDs(res.Candidates[0]); !reflect.DeepEqual(got, []string{"stale"}) {
		t.Errorf("ended series torrents = %v, want [stale]: the whole series goes", got)
	}

	airing := show(false, ep(11, 1, 1, 11), ep(13, 1, 3, 0))
	res = Series(seriesInput(airing, eps, withStale))
	if got := res.Candidates[0].Torrents; len(got) != 0 {
		t.Errorf("still-airing series torrents = %v, want none: only deleted episodes count", got)
	}
}

func TestSeriesWatchedByInAnyMode(t *testing.T) {
	byA := map[string]domain.UserState{"a": played("2026-03-01T20:00:00Z")}
	byB := map[string]domain.UserState{"b": played("2026-03-02T20:00:00Z")}
	anyMode := func(in *SeriesInput) {
		in.Rules.WatchedMode = domain.WatchedAny
		in.Rules.CountingUsers = []string{"a", "b"}
	}
	s := show(true, ep(11, 1, 1, 11), ep(12, 1, 2, 12))

	res := Series(seriesInput(s, []domain.ServerEpisode{sep(1, 1, byA), sep(1, 2, byB)}, anyMode))
	if len(res.Candidates) != 1 {
		t.Fatalf("got %d rows, want 1: every episode was watched by someone", len(res.Candidates))
	}
	if got := res.Candidates[0].WatchedBy; len(got) != 0 {
		t.Errorf("WatchedBy = %v, want empty: nobody watched every episode", got)
	}

	res = Series(seriesInput(s, []domain.ServerEpisode{sep(1, 1, byA), sep(1, 2, byA)}, anyMode))
	if got := res.Candidates[0].WatchedBy; !reflect.DeepEqual(got, []string{"Alice"}) {
		t.Errorf("WatchedBy = %v, want [Alice]", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/scan/ -run TestSeries`
Expected: FAIL to compile with `undefined: SeriesInput`, `undefined: Series`.

- [ ] **Step 3: Write the series list**

Create `internal/scan/series.go`:

```go
package scan

import (
	"sort"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

// SeriesInput is everything the watched series list is computed from.
type SeriesInput struct {
	Rules     domain.Rules
	Now       time.Time
	Users     []domain.User
	Library   domain.Library
	Snapshot  domain.SeriesSnapshot
	Downloads []domain.Download
	Index     *LinkIndex
	Ignored   map[string]bool // candidate keys the user has ignored
}

type epKey struct{ season, number int }

// seasonAgg collects what the rules need to know about one season of one series.
type seasonAgg struct {
	number   int
	files    map[int]int64  // library file ID -> size
	itemIDs  []int          // IDs of the episodes that have a file
	watched  int            // how many of those episodes are watched
	at       *time.Time     // latest watched date among them
	playedBy map[string]int // user ID -> how many of those episodes the user played
}

// qualifies reports whether every episode with a file is watched and past the grace period.
func (a *seasonAgg) qualifies(r domain.Rules, now time.Time) bool {
	return len(a.itemIDs) > 0 && a.watched == len(a.itemIDs) && GraceOK(r, a.at, now)
}

// Series returns the watched series list (spec 6.2 to 6.4), sorted by key.
func Series(in SeriesInput) ListResult {
	var res ListResult
	lib := indexLibrary(in.Library)
	b := rowBuilder{in: in, names: userNames(in.Users), downloads: downloadsByID(in.Downloads)}

	for _, s := range in.Snapshot.Series {
		if s.IDs.TVDB == "" {
			continue
		}
		serverIDs := lib.match(s.IDs)
		seasons := map[int]*seasonAgg{}
		for _, e := range s.Episodes {
			if e.File == nil {
				continue
			}
			a := seasons[e.Season]
			if a == nil {
				a = &seasonAgg{number: e.Season, files: map[int]int64{}, playedBy: map[string]int{}}
				seasons[e.Season] = a
			}
			a.files[e.File.ID] = e.File.Size
			a.itemIDs = append(a.itemIDs, e.ID)

			states, found := lib.episodeStates(serverIDs, e.Season, e.Number)
			if !found {
				if e.Season > 0 {
					res.Unmatched++
				}
				continue
			}
			v := Evaluate(in.Rules, states)
			if !v.Watched {
				continue
			}
			a.watched++
			a.at = later(a.at, v.At)
			for _, u := range v.By {
				a.playedBy[u]++
			}
		}

		var regular []*seasonAgg
		for number, a := range seasons {
			if number > 0 {
				regular = append(regular, a)
			}
		}
		if len(regular) == 0 {
			continue
		}
		sort.Slice(regular, func(i, j int) bool { return regular[i].number < regular[j].number })

		var qualifying []*seasonAgg
		for _, a := range regular {
			if a.qualifies(in.Rules, in.Now) {
				qualifying = append(qualifying, a)
			}
		}
		all := len(qualifying) == len(regular)
		perSeason := in.Rules.SeriesUnit == domain.UnitSeason

		var rows []domain.Candidate
		switch {
		case all && !(perSeason && anySeasonIgnored(in.Ignored, s, regular)):
			rows = append(rows, b.seriesRow(s, regular, seasons))
		case perSeason:
			for _, a := range qualifying {
				rows = append(rows, b.seasonRow(s, a))
			}
		}
		if len(rows) == 0 {
			continue
		}
		if hasTag(s.Tags, in.Rules.ProtectionTag) || (in.Rules.ProtectFavorites && lib.favorite(in.Rules, serverIDs)) {
			res.Protected++
			continue
		}
		res.Candidates = append(res.Candidates, rows...)
	}
	sortByKey(res.Candidates)
	return res
}

func anySeasonIgnored(ignored map[string]bool, s domain.Series, regular []*seasonAgg) bool {
	for _, a := range regular {
		if ignored[domain.SeasonKey(s.IDs.TVDB, a.number)] {
			return true
		}
	}
	return false
}

// rowBuilder turns seasons into rows.
type rowBuilder struct {
	in        SeriesInput
	names     map[string]string
	downloads map[string]domain.Download
}

// seriesRow builds a whole-series row. regular are the regular seasons with files, all of
// which qualify; seasons also holds the specials.
func (b rowBuilder) seriesRow(s domain.Series, regular []*seasonAgg, seasons map[int]*seasonAgg) domain.Candidate {
	deleted := regular
	var itemIDs []int
	if s.Ended {
		// An ended series is removed from the series manager, specials included.
		deleted = nil
		for _, a := range seasons {
			deleted = append(deleted, a)
		}
		for _, e := range s.Episodes {
			itemIDs = append(itemIDs, e.ID)
		}
	} else {
		// The series stays: only the watched regular-season files go.
		for _, a := range regular {
			itemIDs = append(itemIDs, a.itemIDs...)
		}
	}
	c := b.row(s, regular, deleted, itemIDs)
	c.Key = domain.SeriesKey(s.IDs.TVDB)
	c.Scope = domain.ScopeSeries
	c.StillAiring = !s.Ended
	return c
}

// seasonRow builds the row of one qualifying season.
func (b rowBuilder) seasonRow(s domain.Series, a *seasonAgg) domain.Candidate {
	one := []*seasonAgg{a}
	c := b.row(s, one, one, a.itemIDs)
	c.Key = domain.SeasonKey(s.IDs.TVDB, a.number)
	c.Scope = domain.ScopeSeason
	c.Season = a.number
	return c
}

// row builds what series and season rows share. watchedOver are the seasons the watched date
// and watchers come from; deleted are the seasons whose files Delete would remove; itemIDs
// are the episodes Delete affects.
func (b rowBuilder) row(s domain.Series, watchedOver, deleted []*seasonAgg, itemIDs []int) domain.Candidate {
	var size int64
	var fileIDs []int
	for _, a := range deleted {
		for id, bytes := range a.files {
			size += bytes
			fileIDs = append(fileIDs, id)
		}
	}

	var at *time.Time
	episodes := 0
	played := map[string]int{}
	for _, a := range watchedOver {
		at = later(at, a.at)
		episodes += len(a.itemIDs)
		for u, n := range a.playedBy {
			played[u] += n
		}
	}
	// A user is listed when they played every episode with a file. In "any" mode this can be nobody.
	var by []string
	for _, u := range b.in.Rules.CountingUsers {
		if played[u] == episodes {
			by = append(by, u)
		}
	}

	removable := b.in.Index.Removable(b.in.Snapshot.Links, itemIDs, fileIDs)
	torrents, below := torrentInfos(b.in.Rules, b.downloads, removable)
	return domain.Candidate{
		List:         domain.ListSeries,
		Title:        s.Title,
		Year:         s.Year,
		Size:         size,
		WatchedBy:    namesOf(b.names, by),
		WatchedAt:    at,
		Torrents:     torrents,
		BelowSeedMin: below,
	}
}

// libraryIndex looks up the media server's view of a managed series.
type libraryIndex struct {
	byTVDB, byIMDB, byTMDB map[string][]string // external ID -> media server series IDs
	favoriteOf             map[string]map[string]bool
	episodes               map[string]map[epKey]map[string]domain.UserState
}

func indexLibrary(lib domain.Library) *libraryIndex {
	x := &libraryIndex{
		byTVDB:     map[string][]string{},
		byIMDB:     map[string][]string{},
		byTMDB:     map[string][]string{},
		favoriteOf: map[string]map[string]bool{},
		episodes:   map[string]map[epKey]map[string]domain.UserState{},
	}
	for _, s := range lib.Series {
		if s.IDs.TVDB != "" {
			x.byTVDB[s.IDs.TVDB] = append(x.byTVDB[s.IDs.TVDB], s.ID)
		}
		if s.IDs.IMDB != "" {
			x.byIMDB[s.IDs.IMDB] = append(x.byIMDB[s.IDs.IMDB], s.ID)
		}
		if s.IDs.TMDB != "" {
			x.byTMDB[s.IDs.TMDB] = append(x.byTMDB[s.IDs.TMDB], s.ID)
		}
		x.favoriteOf[s.ID] = s.FavoriteOf
	}
	for _, e := range lib.Episodes {
		last := e.EpisodeEnd
		if last < e.Episode {
			last = e.Episode
		}
		byEpisode := x.episodes[e.SeriesID]
		if byEpisode == nil {
			byEpisode = map[epKey]map[string]domain.UserState{}
			x.episodes[e.SeriesID] = byEpisode
		}
		for number := e.Episode; number <= last; number++ {
			k := epKey{e.Season, number}
			byEpisode[k] = mergeStates(byEpisode[k], e.States)
		}
	}
	return x
}

// match returns the media server series that correspond to a managed series:
// by TVDB ID, falling back to IMDb ID, then TMDB ID (spec 6.2).
func (x *libraryIndex) match(ids domain.ExternalIDs) []string {
	if ids.TVDB != "" && len(x.byTVDB[ids.TVDB]) > 0 {
		return x.byTVDB[ids.TVDB]
	}
	if ids.IMDB != "" && len(x.byIMDB[ids.IMDB]) > 0 {
		return x.byIMDB[ids.IMDB]
	}
	if ids.TMDB != "" && len(x.byTMDB[ids.TMDB]) > 0 {
		return x.byTMDB[ids.TMDB]
	}
	return nil
}

// episodeStates returns the merged watch states of one episode across the matched series,
// and whether the media server has the episode at all.
func (x *libraryIndex) episodeStates(serverIDs []string, season, number int) (map[string]domain.UserState, bool) {
	var states map[string]domain.UserState
	found := false
	for _, id := range serverIDs {
		if st, ok := x.episodes[id][epKey{season, number}]; ok {
			states = mergeStates(states, st)
			found = true
		}
	}
	return states, found
}

// favorite reports whether a counting user marked the series, one of its seasons or one of
// its episodes a favourite (spec 6.4).
func (x *libraryIndex) favorite(r domain.Rules, serverIDs []string) bool {
	for _, id := range serverIDs {
		for _, u := range r.CountingUsers {
			if x.favoriteOf[id][u] {
				return true
			}
		}
		for _, states := range x.episodes[id] {
			if anyFavorite(r, states) {
				return true
			}
		}
	}
	return false
}
```

- [ ] **Step 4: Run all tests to verify they pass**

Run: `go vet ./... && go test ./...`
Expected: `ok` for both packages, no vet output.

- [ ] **Step 5: Commit**

```bash
git add internal/scan
git commit -m "feat: watched series list"
```

---

### Task 5: SQLite store

**Files:**
- Create: `internal/domain/summary.go`
- Create: `internal/store/store.go`, `internal/store/migrations/0001_init.sql`, `internal/store/settings.go`, `internal/store/scans.go`
- Modify: `go.mod`, `go.sum` (adds `modernc.org/sqlite`)
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: Task 1: `domain.Candidate`, `domain.ListKind`, `domain.RootFolder`.
- Produces:
  - Package `domain`: `ListSummary{Candidates, Protected, Unmatched int; Error string}`, `ScanSummary{Lists map[ListKind]ListSummary; RootFolders []RootFolder}`.
  - Package `store`:
    - `Open(path string) (*Store, error)`, `(*Store).Close() error`
    - `(*Store).GetSetting(ctx context.Context, key string) (value string, ok bool, err error)`
    - `(*Store).SetSetting(ctx context.Context, key, value string) error`
    - `(*Store).DeleteSetting(ctx context.Context, key string) error`
    - `ScanRecord{ID int64; StartedAt time.Time; FinishedAt *time.Time; Status string; Summary domain.ScanSummary}`, constants `ScanRunning = "running"`, `ScanDone = "done"`
    - `(*Store).BeginScan(ctx context.Context, startedAt time.Time) (int64, error)`
    - `(*Store).FinishScan(ctx context.Context, id int64, finishedAt time.Time, summary domain.ScanSummary, candidates []domain.Candidate) error`
    - `(*Store).LatestScan(ctx context.Context) (*ScanRecord, error)` — the most recent finished scan; `nil, nil` when there is none.
    - `(*Store).Candidates(ctx context.Context, scanID int64, list domain.ListKind) ([]domain.Candidate, error)` — sorted by key; empty, non-nil slice when there are none.

- [ ] **Step 1: Add the SQLite driver**

Run:

```bash
go get modernc.org/sqlite@v1.62.0
mkdir -p internal/store/migrations
```

- [ ] **Step 2: Write the summary types and the schema**

Create `internal/domain/summary.go`:

```go
package domain

// ListSummary is the outcome of one list in a scan.
type ListSummary struct {
	Candidates int    `json:"candidates"`
	Protected  int    `json:"protected"`
	Unmatched  int    `json:"unmatched"`
	Error      string `json:"error,omitempty"` // set when the list could not be produced
}

// ScanSummary is what the header shows about a scan (spec 5.4).
type ScanSummary struct {
	Lists       map[ListKind]ListSummary `json:"lists"`
	RootFolders []RootFolder             `json:"rootFolders"`
}
```

Create `internal/store/migrations/0001_init.sql`:

```sql
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE scans (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  started_at  TEXT NOT NULL,
  finished_at TEXT,
  status      TEXT NOT NULL,
  summary     TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE candidates (
  scan_id INTEGER NOT NULL REFERENCES scans(id) ON DELETE CASCADE,
  list    TEXT NOT NULL,
  key     TEXT NOT NULL,
  data    TEXT NOT NULL,
  PRIMARY KEY (scan_id, list, key)
);
```

- [ ] **Step 3: Write the failing tests**

Create `internal/store/store_test.go`:

```go
package store

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

var ctx = context.Background()

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "removarr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func count(t *testing.T, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestOpenCreatesAPrivateFileAndCanBeReopened(t *testing.T) {
	path := filepath.Join(t.TempDir(), "removarr.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, "k", "v"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("database file mode = %o, want 600", perm)
	}

	s, err = Open(path) // migrations must not run a second time
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer s.Close()
	if v, ok, _ := s.GetSetting(ctx, "k"); !ok || v != "v" {
		t.Errorf("setting after reopen = %q, %v; want v, true", v, ok)
	}
}

func TestSettings(t *testing.T) {
	s := open(t)
	if _, ok, err := s.GetSetting(ctx, "missing"); ok || err != nil {
		t.Errorf("missing key: ok = %v, err = %v; want false, nil", ok, err)
	}
	if err := s.SetSetting(ctx, "a", "1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, "a", "2"); err != nil {
		t.Fatalf("overwriting: %v", err)
	}
	if v, ok, _ := s.GetSetting(ctx, "a"); !ok || v != "2" {
		t.Errorf("a = %q, %v; want 2, true", v, ok)
	}
	if err := s.DeleteSetting(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetSetting(ctx, "a"); ok {
		t.Error("a must be gone after delete")
	}
	if err := s.DeleteSetting(ctx, "a"); err != nil {
		t.Errorf("deleting a missing key must not fail: %v", err)
	}
}

func TestScanLifecycle(t *testing.T) {
	s := open(t)
	if rec, err := s.LatestScan(ctx); rec != nil || err != nil {
		t.Fatalf("LatestScan on an empty store = %v, %v; want nil, nil", rec, err)
	}

	started := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	finished := started.Add(42 * time.Second)
	id, err := s.BeginScan(ctx, started)
	if err != nil {
		t.Fatal(err)
	}
	if rec, _ := s.LatestScan(ctx); rec != nil {
		t.Error("a running scan must not be returned as the latest scan")
	}

	summary := domain.ScanSummary{
		Lists: map[domain.ListKind]domain.ListSummary{
			domain.ListMovies:   {Candidates: 2, Protected: 1},
			domain.ListSeries:   {Error: "The series manager could not be read: timeout"},
			domain.ListTorrents: {Candidates: 1},
		},
		RootFolders: []domain.RootFolder{{Path: "/media/movies", FreeBytes: 1 << 30}},
	}
	candidates := []domain.Candidate{
		{List: domain.ListMovies, Key: "tmdb:603", Title: "The Matrix", Size: 5000, Torrents: []domain.TorrentInfo{}},
		{List: domain.ListMovies, Key: "tmdb:27205", Title: "Inception", Size: 7000, Torrents: []domain.TorrentInfo{}},
		{List: domain.ListTorrents, Key: "hash:abc", Title: "some.torrent", Reason: domain.ReasonOrphan, Torrents: []domain.TorrentInfo{{ID: "abc"}}},
	}
	if err := s.FinishScan(ctx, id, finished, summary, candidates); err != nil {
		t.Fatal(err)
	}

	rec, err := s.LatestScan(ctx)
	if err != nil || rec == nil {
		t.Fatalf("LatestScan = %v, %v", rec, err)
	}
	if rec.ID != id || rec.Status != ScanDone || !rec.StartedAt.Equal(started) || rec.FinishedAt == nil || !rec.FinishedAt.Equal(finished) {
		t.Errorf("scan record wrong: %+v", rec)
	}
	if !reflect.DeepEqual(rec.Summary, summary) {
		t.Errorf("summary = %+v, want %+v", rec.Summary, summary)
	}

	movies, err := s.Candidates(ctx, id, domain.ListMovies)
	if err != nil {
		t.Fatal(err)
	}
	if len(movies) != 2 || movies[0].Key != "tmdb:27205" || movies[1].Key != "tmdb:603" {
		t.Errorf("movies = %+v, want two rows sorted by key", movies)
	}
	if movies[1].Title != "The Matrix" || movies[1].Size != 5000 {
		t.Errorf("movie row did not round-trip: %+v", movies[1])
	}
	torrents, _ := s.Candidates(ctx, id, domain.ListTorrents)
	if len(torrents) != 1 || torrents[0].Reason != domain.ReasonOrphan {
		t.Errorf("torrents = %+v", torrents)
	}
	series, err := s.Candidates(ctx, id, domain.ListSeries)
	if err != nil || series == nil || len(series) != 0 {
		t.Errorf("series = %v, %v; want an empty, non-nil slice", series, err)
	}
}

func TestANewScanReplacesTheOldCandidates(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

	first, _ := s.BeginScan(ctx, now)
	old := []domain.Candidate{{List: domain.ListMovies, Key: "tmdb:1"}}
	if err := s.FinishScan(ctx, first, now, domain.ScanSummary{}, old); err != nil {
		t.Fatal(err)
	}
	second, _ := s.BeginScan(ctx, now)
	fresh := []domain.Candidate{{List: domain.ListMovies, Key: "tmdb:2"}}
	if err := s.FinishScan(ctx, second, now, domain.ScanSummary{}, fresh); err != nil {
		t.Fatal(err)
	}

	if n := count(t, s, `SELECT COUNT(*) FROM candidates`); n != 1 {
		t.Errorf("%d candidate rows stored, want 1: only the latest scan keeps its rows", n)
	}
	got, _ := s.Candidates(ctx, second, domain.ListMovies)
	if len(got) != 1 || got[0].Key != "tmdb:2" {
		t.Errorf("latest candidates = %+v", got)
	}
}

func TestOnlyTheLastTenScansAreKept(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 12; i++ {
		id, err := s.BeginScan(ctx, now)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.FinishScan(ctx, id, now, domain.ScanSummary{}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if n := count(t, s, `SELECT COUNT(*) FROM scans`); n != 10 {
		t.Errorf("%d scans kept, want 10", n)
	}
}

func TestBeginScanDropsAScanLeftRunning(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	if _, err := s.BeginScan(ctx, now); err != nil { // never finished, as after a crash
		t.Fatal(err)
	}
	if _, err := s.BeginScan(ctx, now); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT COUNT(*) FROM scans WHERE status = ?`, ScanRunning); n != 1 {
		t.Errorf("%d running scans, want 1", n)
	}
}

func TestFinishScanRejectsAnUnknownScan(t *testing.T) {
	s := open(t)
	if err := s.FinishScan(ctx, 999, time.Now(), domain.ScanSummary{}, nil); err == nil {
		t.Error("finishing a scan that does not exist must fail")
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./internal/store/`
Expected: FAIL to compile with `undefined: Store`, `undefined: Open`, `undefined: ScanDone`.

- [ ] **Step 5: Write opening and migrations**

Create `internal/store/store.go`:

```go
// Package store is removarr's SQLite persistence.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Store wraps the database.
type Store struct{ db *sql.DB }

// Open opens the database file at path, creating it readable by its owner only,
// and applies any migrations that have not run yet.
func Open(path string) (*Store, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create database file: %w", err)
	}
	f.Close()

	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection keeps writes serialised; removarr's load is tiny.
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// migrate applies the embedded migration files in name order. Each file name starts with
// its version number, for example 0001_init.sql.
func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		base := strings.TrimPrefix(name, "migrations/")
		version, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: the file name must start with a number", base)
		}
		var applied int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		body, err := migrationFiles.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", base, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 6: Write the settings table access**

Create `internal/store/settings.go`:

```go
package store

import (
	"context"
	"database/sql"
	"errors"
)

// GetSetting returns the value stored under key. ok is false when there is none.
func (s *Store) GetSetting(ctx context.Context, key string) (value string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// SetSetting stores value under key, replacing any earlier value.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}

// DeleteSetting removes key. Removing a key that does not exist is not an error.
func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key)
	return err
}
```

- [ ] **Step 7: Write the scan tables access**

Create `internal/store/scans.go`:

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

const (
	ScanRunning = "running"
	ScanDone    = "done"

	keepScans = 10
)

// ScanRecord is one stored scan.
type ScanRecord struct {
	ID         int64              `json:"id"`
	StartedAt  time.Time          `json:"startedAt"`
	FinishedAt *time.Time         `json:"finishedAt"`
	Status     string             `json:"status"`
	Summary    domain.ScanSummary `json:"summary"`
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// BeginScan records the start of a scan and returns its ID. A scan left running by an
// earlier crash is dropped first; the scanner guarantees only one scan runs at a time.
func (s *Store) BeginScan(ctx context.Context, startedAt time.Time) (int64, error) {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM scans WHERE status = ?`, ScanRunning); err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO scans (started_at, status) VALUES (?, ?)`, formatTime(startedAt), ScanRunning)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishScan stores a scan's summary and candidates in one transaction. The candidates of
// earlier scans are dropped, and only the last ten scans are kept.
func (s *Store) FinishScan(ctx context.Context, id int64, finishedAt time.Time, summary domain.ScanSummary, candidates []domain.Candidate) error {
	rawSummary, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `UPDATE scans SET finished_at = ?, status = ?, summary = ? WHERE id = ?`,
		formatTime(finishedAt), ScanDone, string(rawSummary), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("scan %d does not exist", id)
	}
	for _, c := range candidates {
		data, err := json.Marshal(c)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO candidates (scan_id, list, key, data) VALUES (?, ?, ?, ?)`,
			id, string(c.List), c.Key, string(data)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM candidates WHERE scan_id <> ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM scans WHERE id NOT IN (SELECT id FROM scans ORDER BY id DESC LIMIT ?)`, keepScans); err != nil {
		return err
	}
	return tx.Commit()
}

// LatestScan returns the most recent finished scan, or nil when there is none.
func (s *Store) LatestScan(ctx context.Context) (*ScanRecord, error) {
	var (
		rec      ScanRecord
		started  string
		finished sql.NullString
		summary  string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, started_at, finished_at, status, summary FROM scans WHERE status = ? ORDER BY id DESC LIMIT 1`, ScanDone).
		Scan(&rec.ID, &started, &finished, &rec.Status, &summary)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if rec.StartedAt, err = time.Parse(time.RFC3339Nano, started); err != nil {
		return nil, fmt.Errorf("scan %d: bad start time: %w", rec.ID, err)
	}
	if finished.Valid {
		t, err := time.Parse(time.RFC3339Nano, finished.String)
		if err != nil {
			return nil, fmt.Errorf("scan %d: bad finish time: %w", rec.ID, err)
		}
		rec.FinishedAt = &t
	}
	if err := json.Unmarshal([]byte(summary), &rec.Summary); err != nil {
		return nil, fmt.Errorf("scan %d: bad summary: %w", rec.ID, err)
	}
	return &rec, nil
}

// Candidates returns the rows of one list of one scan, sorted by key.
func (s *Store) Candidates(ctx context.Context, scanID int64, list domain.ListKind) ([]domain.Candidate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT data FROM candidates WHERE scan_id = ? AND list = ? ORDER BY key`, scanID, string(list))
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

- [ ] **Step 8: Run all tests to verify they pass**

Run: `go mod tidy && go vet ./... && go test ./...`
Expected: `ok` for `internal/domain`, `internal/scan` and `internal/store`, no vet output. `go mod tidy` records `modernc.org/sqlite` as a direct dependency now that the code imports it.

- [ ] **Step 9: Commit**

```bash
git add go.mod go.sum internal/domain/summary.go internal/store
git commit -m "feat: SQLite store for settings, scans and candidates"
```

---

### Task 6: Configuration

**Files:**
- Create: `internal/config/env.go`, `internal/config/settings.go`
- Test: `internal/config/env_test.go`, `internal/config/settings_test.go`

**Interfaces:**
- Consumes: Task 1: `domain.Rules`, `domain.DefaultRules`, `domain.FieldError`. Task 5's `*store.Store` satisfies `SettingsStore` but this package does not import `store`.
- Produces, in package `config`:
  - Constants `AppJellyfin = "jellyfin"`, `AppSonarr = "sonarr"`, `AppRadarr = "radarr"`, `AppQBittorrent = "qbittorrent"`; `Apps []string` (those four, in that order).
  - `Field{Key, App, Name, Env string; Secret, URL bool}`, `Fields []Field`, `FieldFor(app, name string) (Field, bool)`. Field names are `url`, `apiKey`, `username`, `password`.
  - `Env{Port int; ConfigDir string; DryRun bool; Conn map[string]string}`, `LoadEnv(getenv func(string) string) (Env, error)`
  - `NormalizeURL(raw string) (string, error)`
  - `SettingsStore` interface with `GetSetting`, `SetSetting`, `DeleteSetting` (same signatures as Task 5).
  - `Source` with `SourceUI = "ui"`, `SourceEnv = "env"`, `SourceNone = "none"`.
  - `NewService(env Env, store SettingsStore) *Service`
  - `(*Service).Env() Env`
  - `(*Service).Conn(ctx context.Context, key string) (value string, source Source, err error)`
  - `CheckConn(f Field, value string) (string, error)` — validates without saving; returns the value to store. A bad URL gives a `*domain.FieldError` with field `connections.<app>.<name>`.
  - `(*Service).SetConn(ctx context.Context, f Field, value string) error` — an empty value clears the saved value.
  - `FieldView{Value string; Set bool; Source Source}`, `(*Service).ConnView(ctx context.Context) (map[string]map[string]FieldView, error)` — keyed by app, then field name; `Value` is never set for secrets.
  - `AppConn{URL, APIKey, Username, Password string}`, `(*Service).App(ctx context.Context, app string) (conn AppConn, configured bool, err error)`
  - `(*Service).Rules(ctx context.Context) (domain.Rules, error)`, `(*Service).SetRules(ctx context.Context, r domain.Rules) error`

- [ ] **Step 1: Write the failing tests for the environment**

Create `internal/config/env_test.go`:

```go
package config

import (
	"strings"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestLoadEnvDefaults(t *testing.T) {
	e, err := LoadEnv(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if e.Port != 7272 || e.ConfigDir != "/config" || e.DryRun || len(e.Conn) != 0 {
		t.Errorf("defaults wrong: %+v", e)
	}
}

func TestLoadEnvReadsEverything(t *testing.T) {
	e, err := LoadEnv(env(map[string]string{
		"REMOVARR_PORT":                 "9000",
		"REMOVARR_CONFIG_DIR":           "/data",
		"REMOVARR_DRY_RUN":              "true",
		"REMOVARR_SONARR_URL":           " http://pi:8989/sonarr/ ",
		"REMOVARR_SONARR_API_KEY":       "abc",
		"REMOVARR_QBITTORRENT_USERNAME": " admin ",
		"REMOVARR_QBITTORRENT_PASSWORD": " secret ",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if e.Port != 9000 || e.ConfigDir != "/data" || !e.DryRun {
		t.Errorf("basic values wrong: %+v", e)
	}
	want := map[string]string{
		"sonarr.url":           "http://pi:8989/sonarr",
		"sonarr.api_key":       "abc",
		"qbittorrent.username": "admin",
		"qbittorrent.password": " secret ", // secrets are kept exactly as given
	}
	for k, v := range want {
		if e.Conn[k] != v {
			t.Errorf("Conn[%q] = %q, want %q", k, e.Conn[k], v)
		}
	}
	if len(e.Conn) != len(want) {
		t.Errorf("Conn = %v, want only the fields that were set", e.Conn)
	}
}

func TestLoadEnvRejectsBadValues(t *testing.T) {
	tests := []struct{ name, value string }{
		{"REMOVARR_PORT", "abc"},
		{"REMOVARR_PORT", "0"},
		{"REMOVARR_PORT", "70000"},
		{"REMOVARR_DRY_RUN", "maybe"},
		{"REMOVARR_RADARR_URL", "pi:7878"},
		{"REMOVARR_JELLYFIN_URL", "ftp://pi"},
	}
	for _, tt := range tests {
		t.Run(tt.name+"="+tt.value, func(t *testing.T) {
			_, err := LoadEnv(env(map[string]string{tt.name: tt.value}))
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), tt.name) {
				t.Errorf("error %q must name the variable %s", err, tt.name)
			}
		})
	}
}

func TestNormalizeURL(t *testing.T) {
	good := map[string]string{
		"http://pi:8989":                 "http://pi:8989",
		"http://pi:8989/":                "http://pi:8989",
		"http://pi:8989/sonarr/":         "http://pi:8989/sonarr",
		"  https://media.example/jf  ":   "https://media.example/jf",
		"http://pi:8989/sonarr?x=1#frag": "http://pi:8989/sonarr",
	}
	for in, want := range good {
		got, err := NormalizeURL(in)
		if err != nil || got != want {
			t.Errorf("NormalizeURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "pi:8989", "ftp://pi", "http://", "sonarr"} {
		if got, err := NormalizeURL(in); err == nil {
			t.Errorf("NormalizeURL(%q) = %q, want an error", in, got)
		}
	}
}

func TestFieldsAreConsistent(t *testing.T) {
	keys, envs := map[string]bool{}, map[string]bool{}
	apps := map[string]bool{}
	for _, f := range Fields {
		if keys[f.Key] || envs[f.Env] {
			t.Errorf("field %+v repeats a key or environment variable", f)
		}
		keys[f.Key], envs[f.Env], apps[f.App] = true, true, true
		if !strings.HasPrefix(f.Env, "REMOVARR_") {
			t.Errorf("environment variable %q must start with REMOVARR_", f.Env)
		}
		if got, ok := FieldFor(f.App, f.Name); !ok || got.Key != f.Key {
			t.Errorf("FieldFor(%q, %q) = %+v, %v", f.App, f.Name, got, ok)
		}
	}
	if len(Fields) != 9 || len(apps) != 4 {
		t.Errorf("%d fields over %d apps, want 9 over 4", len(Fields), len(apps))
	}
	if _, ok := FieldFor("sonarr", "password"); ok {
		t.Error("sonarr has no password field")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config/`
Expected: FAIL to compile with `undefined: LoadEnv`, `undefined: NormalizeURL`, `undefined: Fields`, `undefined: FieldFor`.

- [ ] **Step 3: Write the environment loading**

Create `internal/config/env.go`:

```go
// Package config resolves removarr's configuration: environment variables, and the
// settings saved through the API, which take priority (spec section 9).
package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	AppJellyfin    = "jellyfin"
	AppSonarr      = "sonarr"
	AppRadarr      = "radarr"
	AppQBittorrent = "qbittorrent"
)

// Apps lists the apps removarr connects to.
var Apps = []string{AppJellyfin, AppSonarr, AppRadarr, AppQBittorrent}

// Field describes one connection setting.
type Field struct {
	Key    string // settings key, for example "jellyfin.api_key"
	App    string // one of Apps
	Name   string // name within the app in API payloads: "url", "apiKey", "username" or "password"
	Env    string // environment variable
	Secret bool   // write-only: never returned by the API
	URL    bool   // validated and normalised as a base URL
}

// Fields lists every connection setting (spec 9.1).
var Fields = []Field{
	{Key: "jellyfin.url", App: AppJellyfin, Name: "url", Env: "REMOVARR_JELLYFIN_URL", URL: true},
	{Key: "jellyfin.api_key", App: AppJellyfin, Name: "apiKey", Env: "REMOVARR_JELLYFIN_API_KEY", Secret: true},
	{Key: "sonarr.url", App: AppSonarr, Name: "url", Env: "REMOVARR_SONARR_URL", URL: true},
	{Key: "sonarr.api_key", App: AppSonarr, Name: "apiKey", Env: "REMOVARR_SONARR_API_KEY", Secret: true},
	{Key: "radarr.url", App: AppRadarr, Name: "url", Env: "REMOVARR_RADARR_URL", URL: true},
	{Key: "radarr.api_key", App: AppRadarr, Name: "apiKey", Env: "REMOVARR_RADARR_API_KEY", Secret: true},
	{Key: "qbittorrent.url", App: AppQBittorrent, Name: "url", Env: "REMOVARR_QBITTORRENT_URL", URL: true},
	{Key: "qbittorrent.username", App: AppQBittorrent, Name: "username", Env: "REMOVARR_QBITTORRENT_USERNAME"},
	{Key: "qbittorrent.password", App: AppQBittorrent, Name: "password", Env: "REMOVARR_QBITTORRENT_PASSWORD", Secret: true},
}

// FieldFor finds a connection field by app and name.
func FieldFor(app, name string) (Field, bool) {
	for _, f := range Fields {
		if f.App == app && f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// Env is removarr's configuration from the environment.
type Env struct {
	Port      int
	ConfigDir string
	DryRun    bool
	Conn      map[string]string // connection field key -> value, for the fields that are set
}

// LoadEnv reads the environment through getenv. Errors name the offending variable.
func LoadEnv(getenv func(string) string) (Env, error) {
	env := Env{Port: 7272, ConfigDir: "/config", Conn: map[string]string{}}

	if v := strings.TrimSpace(getenv("REMOVARR_PORT")); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil || port < 1 || port > 65535 {
			return Env{}, fmt.Errorf("REMOVARR_PORT: %q is not a port number", v)
		}
		env.Port = port
	}
	if v := strings.TrimSpace(getenv("REMOVARR_CONFIG_DIR")); v != "" {
		env.ConfigDir = v
	}
	if v := strings.TrimSpace(getenv("REMOVARR_DRY_RUN")); v != "" {
		dry, err := strconv.ParseBool(v)
		if err != nil {
			return Env{}, fmt.Errorf("REMOVARR_DRY_RUN: %q is not true or false", v)
		}
		env.DryRun = dry
	}
	for _, f := range Fields {
		v := getenv(f.Env)
		if !f.Secret {
			v = strings.TrimSpace(v)
		}
		if v == "" {
			continue
		}
		if f.URL {
			normalized, err := NormalizeURL(v)
			if err != nil {
				return Env{}, fmt.Errorf("%s: %w", f.Env, err)
			}
			v = normalized
		}
		env.Conn[f.Key] = v
	}
	return env, nil
}

// NormalizeURL validates an app's base URL and removes trailing slashes, the query and the
// fragment. A path prefix such as /sonarr is kept.
func NormalizeURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("%q is not an http or https URL", raw)
	}
	u.RawQuery, u.Fragment = "", ""
	return strings.TrimRight(u.String(), "/"), nil
}
```

- [ ] **Step 4: Run the environment tests to verify they pass**

Run: `go test ./internal/config/`
Expected: `ok  	github.com/removarr/removarr/internal/config`

- [ ] **Step 5: Write the failing tests for saved settings**

Create `internal/config/settings_test.go`:

```go
package config

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/removarr/removarr/internal/domain"
)

var ctx = context.Background()

// memStore is an in-memory SettingsStore.
type memStore map[string]string

func (m memStore) GetSetting(_ context.Context, k string) (string, bool, error) {
	v, ok := m[k]
	return v, ok, nil
}
func (m memStore) SetSetting(_ context.Context, k, v string) error { m[k] = v; return nil }
func (m memStore) DeleteSetting(_ context.Context, k string) error { delete(m, k); return nil }

func field(t *testing.T, app, name string) Field {
	t.Helper()
	f, ok := FieldFor(app, name)
	if !ok {
		t.Fatalf("no field %s.%s", app, name)
	}
	return f
}

func service(envConn map[string]string) (*Service, memStore) {
	if envConn == nil {
		envConn = map[string]string{}
	}
	st := memStore{}
	return NewService(Env{Port: 7272, ConfigDir: "/config", Conn: envConn}, st), st
}

func TestConnPrecedence(t *testing.T) {
	s, _ := service(map[string]string{"sonarr.url": "http://env:8989"})
	url := field(t, AppSonarr, "url")

	check := func(step, wantValue string, wantSource Source) {
		t.Helper()
		v, src, err := s.Conn(ctx, url.Key)
		if err != nil || v != wantValue || src != wantSource {
			t.Errorf("%s: Conn = %q, %q, %v; want %q, %q", step, v, src, err, wantValue, wantSource)
		}
	}

	check("environment only", "http://env:8989", SourceEnv)

	if err := s.SetConn(ctx, url, "http://ui:8989/"); err != nil {
		t.Fatal(err)
	}
	check("saved value wins", "http://ui:8989", SourceUI)

	if err := s.SetConn(ctx, url, ""); err != nil {
		t.Fatal(err)
	}
	check("cleared, back to the environment", "http://env:8989", SourceEnv)

	if v, src, _ := s.Conn(ctx, field(t, AppRadarr, "url").Key); v != "" || src != SourceNone {
		t.Errorf("unset field: Conn = %q, %q; want empty, none", v, src)
	}
}

func TestSetConnRejectsABadURL(t *testing.T) {
	s, st := service(nil)
	err := s.SetConn(ctx, field(t, AppSonarr, "url"), "pi:8989")
	var fe *domain.FieldError
	if !errors.As(err, &fe) || fe.Field != "connections.sonarr.url" {
		t.Fatalf("err = %v, want a FieldError for connections.sonarr.url", err)
	}
	if len(st) != 0 {
		t.Errorf("nothing must be saved on error, store = %v", st)
	}
}

func TestConnViewHidesSecrets(t *testing.T) {
	s, _ := service(map[string]string{"sonarr.api_key": "env-key"})
	if err := s.SetConn(ctx, field(t, AppRadarr, "url"), "http://radarr:7878"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetConn(ctx, field(t, AppQBittorrent, "password"), "hunter2"); err != nil {
		t.Fatal(err)
	}

	view, err := s.ConnView(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]FieldView{
		AppJellyfin: {"url": {Source: SourceNone}, "apiKey": {Source: SourceNone}},
		AppSonarr:   {"url": {Source: SourceNone}, "apiKey": {Set: true, Source: SourceEnv}},
		AppRadarr:   {"url": {Value: "http://radarr:7878", Set: true, Source: SourceUI}, "apiKey": {Source: SourceNone}},
		AppQBittorrent: {
			"url":      {Source: SourceNone},
			"username": {Source: SourceNone},
			"password": {Set: true, Source: SourceUI},
		},
	}
	if !reflect.DeepEqual(view, want) {
		t.Errorf("view =\n%+v\nwant\n%+v", view, want)
	}
}

func TestApp(t *testing.T) {
	s, _ := service(map[string]string{
		"sonarr.url":      "http://sonarr:8989",
		"radarr.url":      "http://radarr:7878",
		"radarr.api_key":  "rk",
		"qbittorrent.url": "http://qbit:8080",
	})

	if _, configured, _ := s.App(ctx, AppSonarr); configured {
		t.Error("sonarr has a URL but no API key: not configured")
	}
	conn, configured, err := s.App(ctx, AppRadarr)
	if err != nil || !configured || conn.URL != "http://radarr:7878" || conn.APIKey != "rk" {
		t.Errorf("radarr = %+v, %v, %v", conn, configured, err)
	}
	if _, configured, _ := s.App(ctx, AppQBittorrent); !configured {
		t.Error("qBittorrent needs only a URL: it may allow access without a login")
	}
	if _, configured, _ := s.App(ctx, AppJellyfin); configured {
		t.Error("jellyfin has nothing set: not configured")
	}
}

func TestRules(t *testing.T) {
	s, st := service(nil)

	got, err := s.Rules(ctx)
	if err != nil || !reflect.DeepEqual(got, domain.DefaultRules()) {
		t.Fatalf("Rules on a fresh store = %+v, %v; want the defaults", got, err)
	}

	want := domain.DefaultRules()
	want.CountingUsers = []string{"u1"}
	want.SeriesUnit = domain.UnitSeason
	want.GraceDays = 7
	if err := s.SetRules(ctx, want); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Rules(ctx); !reflect.DeepEqual(got, want) {
		t.Errorf("Rules = %+v, want %+v", got, want)
	}

	bad := want
	bad.GraceDays = -1
	var fe *domain.FieldError
	if err := s.SetRules(ctx, bad); !errors.As(err, &fe) || fe.Field != "graceDays" {
		t.Errorf("err = %v, want a FieldError for graceDays", err)
	}
	if got, _ := s.Rules(ctx); got.GraceDays != 7 {
		t.Errorf("invalid rules must not be saved, GraceDays = %d", got.GraceDays)
	}

	// Rules saved by an older version may lack fields, or hold null lists.
	st["rules"] = `{"graceDays":5,"countingUsers":null}`
	got, err = s.Rules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.GraceDays != 5 || got.WatchedMode != domain.WatchedAll || got.ProtectionTag != "keep" {
		t.Errorf("missing fields must take their defaults: %+v", got)
	}
	if got.CountingUsers == nil || got.ExcludedTags == nil {
		t.Errorf("lists must never be nil: %+v", got)
	}
}
```

- [ ] **Step 6: Run the tests to verify they fail**

Run: `go test ./internal/config/`
Expected: FAIL to compile with `undefined: Service`, `undefined: NewService`, `undefined: Source`, `undefined: FieldView`.

- [ ] **Step 7: Write the settings service**

Create `internal/config/settings.go`:

```go
package config

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/removarr/removarr/internal/domain"
)

const (
	connPrefix = "conn."
	rulesKey   = "rules"
)

// SettingsStore is the persistence the service needs. *store.Store satisfies it.
type SettingsStore interface {
	GetSetting(ctx context.Context, key string) (value string, ok bool, err error)
	SetSetting(ctx context.Context, key, value string) error
	DeleteSetting(ctx context.Context, key string) error
}

// Source says where a connection value comes from.
type Source string

const (
	SourceUI   Source = "ui"
	SourceEnv  Source = "env"
	SourceNone Source = "none"
)

// Service resolves connection settings and rules.
type Service struct {
	env   Env
	store SettingsStore
}

// NewService returns a Service over the environment and the saved settings.
func NewService(env Env, store SettingsStore) *Service {
	return &Service{env: env, store: store}
}

// Env returns the configuration read from the environment.
func (s *Service) Env() Env { return s.env }

// Conn resolves one connection field: a saved value wins, then the environment (spec 9.2).
func (s *Service) Conn(ctx context.Context, key string) (string, Source, error) {
	saved, ok, err := s.store.GetSetting(ctx, connPrefix+key)
	if err != nil {
		return "", SourceNone, err
	}
	if ok && saved != "" {
		return saved, SourceUI, nil
	}
	if v := s.env.Conn[key]; v != "" {
		return v, SourceEnv, nil
	}
	return "", SourceNone, nil
}

// CheckConn validates a value for a connection field without saving it, and returns the
// value to store. An invalid URL gives a *domain.FieldError.
func CheckConn(f Field, value string) (string, error) {
	if !f.Secret {
		value = strings.TrimSpace(value)
	}
	if value == "" || !f.URL {
		return value, nil
	}
	normalized, err := NormalizeURL(value)
	if err != nil {
		return "", &domain.FieldError{Field: "connections." + f.App + "." + f.Name, Message: err.Error()}
	}
	return normalized, nil
}

// SetConn saves a connection field. An empty value clears the saved value, so the
// environment variable applies again.
func (s *Service) SetConn(ctx context.Context, f Field, value string) error {
	v, err := CheckConn(f, value)
	if err != nil {
		return err
	}
	if v == "" {
		return s.store.DeleteSetting(ctx, connPrefix+f.Key)
	}
	return s.store.SetSetting(ctx, connPrefix+f.Key, v)
}

// FieldView describes one connection field for the settings page.
type FieldView struct {
	Value  string `json:"value,omitempty"` // never set for secrets
	Set    bool   `json:"set"`
	Source Source `json:"source"`
}

// ConnView describes every connection field, keyed by app and field name, without
// revealing secrets.
func (s *Service) ConnView(ctx context.Context) (map[string]map[string]FieldView, error) {
	out := map[string]map[string]FieldView{}
	for _, f := range Fields {
		v, source, err := s.Conn(ctx, f.Key)
		if err != nil {
			return nil, err
		}
		view := FieldView{Set: v != "", Source: source}
		if !f.Secret {
			view.Value = v
		}
		if out[f.App] == nil {
			out[f.App] = map[string]FieldView{}
		}
		out[f.App][f.Name] = view
	}
	return out, nil
}

// AppConn is the resolved connection of one app.
type AppConn struct {
	URL      string
	APIKey   string
	Username string
	Password string
}

// App returns the resolved connection of one app. configured is false while a required
// field is missing: the URL, and the API key for every app except qBittorrent, which can
// allow access without a login.
func (s *Service) App(ctx context.Context, app string) (AppConn, bool, error) {
	var c AppConn
	for _, f := range Fields {
		if f.App != app {
			continue
		}
		v, _, err := s.Conn(ctx, f.Key)
		if err != nil {
			return AppConn{}, false, err
		}
		switch f.Name {
		case "url":
			c.URL = v
		case "apiKey":
			c.APIKey = v
		case "username":
			c.Username = v
		case "password":
			c.Password = v
		}
	}
	configured := c.URL != "" && (app == AppQBittorrent || c.APIKey != "")
	return c, configured, nil
}

// Rules returns the saved rules. Fields that were never saved take their defaults.
func (s *Service) Rules(ctx context.Context) (domain.Rules, error) {
	r := domain.DefaultRules()
	raw, ok, err := s.store.GetSetting(ctx, rulesKey)
	if err != nil || !ok {
		return r, err
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return domain.DefaultRules(), fmt.Errorf("the saved rules are not valid JSON: %w", err)
	}
	return withLists(r), nil
}

// SetRules validates and saves the rules.
func (s *Service) SetRules(ctx context.Context, r domain.Rules) error {
	r = withLists(r)
	if err := r.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return s.store.SetSetting(ctx, rulesKey, string(raw))
}

// withLists replaces nil lists with empty ones so they serialise as [] and not null.
func withLists(r domain.Rules) domain.Rules {
	if r.CountingUsers == nil {
		r.CountingUsers = []string{}
	}
	if r.ExcludedCategories == nil {
		r.ExcludedCategories = []string{}
	}
	if r.ExcludedTags == nil {
		r.ExcludedTags = []string{}
	}
	return r
}
```

- [ ] **Step 8: Run all tests to verify they pass**

Run: `go vet ./... && go test ./...`
Expected: `ok` for `internal/config`, `internal/domain`, `internal/scan` and `internal/store`, no vet output.

- [ ] **Step 9: Commit**

```bash
git add internal/config
git commit -m "feat: configuration with saved settings taking priority over the environment"
```

---

### Task 7: Provider interfaces, JSON helper and the shared Sonarr/Radarr client

Sonarr and Radarr expose the same v3 API shape for tags, root folders, status, history and the queue. This task builds that shared part, including the logic that works out which download each library file came from.

How the linking works (spec 6.6, and assumption 1 in spec section 16):

- `GET /api/v3/history?sortKey=date&sortDirection=descending` is read page by page, newest first. Every record with a `downloadId` associates that download with the record's movie or episode.
- A record with `eventType` `downloadFolderImported` is an import. Its `data.fileId` names the library file it created, so `FileSource[fileId] = downloadId`.
- If an app version does not report `data.fileId`, the most recent import of an item is taken as the source of the item's current file.
- A manual import has no `downloadId`. It must not link the file to anything, and must not let an older torrent be mistaken for the source of the current file.
- `GET /api/v3/queue` gives the downloads that are still being handled.

**Files:**
- Create: `internal/providers/providers.go`, `internal/httpjson/httpjson.go`, `internal/providers/arr/arr.go`
- Test: `internal/httpjson/httpjson_test.go`, `internal/providers/arr/arr_test.go`

**Interfaces:**
- Consumes: Task 1: `domain.Links`, `domain.RootFolder`, `domain.User`, `domain.Library`, `domain.MovieSnapshot`, `domain.SeriesSnapshot`, `domain.Download`.
- Produces:
  - Package `providers`: `ErrNotConfigured`, and the interfaces `MediaServer{Users(ctx) ([]domain.User, error); Library(ctx, userIDs []string) (domain.Library, error)}`, `MovieManager{Movies(ctx) (domain.MovieSnapshot, error)}`, `SeriesManager{Series(ctx) (domain.SeriesSnapshot, error)}`, `DownloadClient{Downloads(ctx) ([]domain.Download, error)}`, `Tester{Test(ctx) (detail string, err error)}`. Every `ctx` is a `context.Context`.
  - Package `httpjson`: `StatusError{Status int; Body string}`, `Get(ctx context.Context, c *http.Client, rawURL string, header http.Header, out any) error`, `ParseTime(s string) *time.Time`.
  - Package `arr`: `Client{BaseURL, APIKey string; HTTP *http.Client; PageSize int}`, `(*Client).Get(ctx, path string, q url.Values, out any) error`, `(*Client).Tags(ctx) (map[int]string, error)`, `TagLabels(byID map[int]string, ids []int) []string`, `IDString(n int) string`, `(*Client).RootFolders(ctx) ([]domain.RootFolder, error)`, `(*Client).Status(ctx) (string, error)`, `LinkOptions{Episodes bool; CurrentFiles map[int]int}`, `(*Client).Links(ctx, opt LinkOptions) (domain.Links, error)`.

- [ ] **Step 1: Write the provider interfaces**

Create `internal/providers/providers.go`:

```go
// Package providers defines what removarr needs from the apps it talks to. Adding support
// for another product means adding one implementation of one of these interfaces.
package providers

import (
	"context"
	"errors"

	"github.com/removarr/removarr/internal/domain"
)

// ErrNotConfigured is returned when an app's connection settings are incomplete.
var ErrNotConfigured = errors.New("not configured")

// MediaServer is where things are watched (Jellyfin in v1).
type MediaServer interface {
	Users(ctx context.Context) ([]domain.User, error)
	// Library returns the watch state of every movie and episode for the given users.
	Library(ctx context.Context, userIDs []string) (domain.Library, error)
}

// MovieManager manages movies (Radarr in v1).
type MovieManager interface {
	Movies(ctx context.Context) (domain.MovieSnapshot, error)
}

// SeriesManager manages series (Sonarr in v1).
type SeriesManager interface {
	Series(ctx context.Context) (domain.SeriesSnapshot, error)
}

// DownloadClient holds the downloads (qBittorrent in v1).
type DownloadClient interface {
	Downloads(ctx context.Context) ([]domain.Download, error)
}

// Tester checks that an app can be reached with the configured credentials.
// detail describes the app, for example "Sonarr 4.0.9".
type Tester interface {
	Test(ctx context.Context) (detail string, err error)
}
```

- [ ] **Step 2: Write the failing tests for the JSON helper**

Create `internal/httpjson/httpjson_test.go`:

```go
package httpjson

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var ctx = context.Background()

func serve(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func TestGetDecodesJSONAndSendsHeaders(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "yes" || r.Header.Get("Accept") != "application/json" {
			http.Error(w, "bad headers", http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"name":"ok"}`))
	})
	var out struct {
		Name string `json:"name"`
	}
	if err := Get(ctx, srv.Client(), srv.URL, http.Header{"X-Test": {"yes"}}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Name != "ok" {
		t.Errorf("Name = %q, want ok", out.Name)
	}
}

func TestGetReturnsAStatusError(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
	err := Get(ctx, srv.Client(), srv.URL, nil, nil)
	var se *StatusError
	if !errors.As(err, &se) || se.Status != http.StatusUnauthorized || !strings.Contains(se.Body, "Unauthorized") {
		t.Errorf("err = %v, want a StatusError for 401 carrying the body", err)
	}
}

func TestGetReportsBadJSON(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>login</html>")) })
	var out map[string]any
	err := Get(ctx, srv.Client(), srv.URL, nil, &out)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Errorf("err = %v, want a decode error", err)
	}
}

func TestParseTime(t *testing.T) {
	good := map[string]string{
		"2026-03-01T20:15:30Z":         "2026-03-01T20:15:30Z",
		"2026-03-01T20:15:30.1234567Z": "2026-03-01T20:15:30.1234567Z",
		"2026-03-01T21:15:30+01:00":    "2026-03-01T20:15:30Z",
		"2026-03-01T20:15:30":          "2026-03-01T20:15:30Z", // no zone: taken as UTC
		"2026-03-01T20:15:30.5":        "2026-03-01T20:15:30.5Z",
	}
	for in, want := range good {
		wantTime, err := time.Parse(time.RFC3339Nano, want)
		if err != nil {
			t.Fatal(err)
		}
		got := ParseTime(in)
		if got == nil || !got.Equal(wantTime) {
			t.Errorf("ParseTime(%q) = %v, want %v", in, got, wantTime)
		}
	}
	for _, in := range []string{"", "yesterday", "0001-01-01T00:00:00Z", "2026-13-45T00:00:00Z"} {
		if got := ParseTime(in); got != nil {
			t.Errorf("ParseTime(%q) = %v, want nil", in, got)
		}
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/httpjson/`
Expected: FAIL to compile with `undefined: Get`, `undefined: StatusError`, `undefined: ParseTime`.

- [ ] **Step 4: Write the JSON helper**

Create `internal/httpjson/httpjson.go`:

```go
// Package httpjson holds the small HTTP helpers the provider clients share.
package httpjson

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// StatusError is returned when a server answers with a status outside 200 to 299.
type StatusError struct {
	Status int
	Body   string // the first 200 bytes of the response, for diagnostics
}

func (e *StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("unexpected status %d", e.Status)
	}
	return fmt.Sprintf("unexpected status %d: %s", e.Status, e.Body)
}

// Get sends a GET request and decodes the JSON response into out. A nil out skips decoding.
// Credentials belong in header, never in rawURL, so they cannot end up in error messages.
func Get(ctx context.Context, c *http.Client, rawURL string, header http.Header, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	for name, values := range header {
		req.Header[name] = values
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return &StatusError{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// ParseTime parses the timestamp formats the supported apps use. It returns nil when s is
// empty, is not a timestamp, or is a placeholder date such as year 1.
func ParseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05"} {
		t, err := time.Parse(layout, s)
		if err != nil {
			continue
		}
		if t.Year() < 1900 {
			return nil
		}
		u := t.UTC()
		return &u
	}
	return nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/httpjson/`
Expected: `ok  	github.com/removarr/removarr/internal/httpjson`

- [ ] **Step 6: Write the failing tests for the shared client**

Create `internal/providers/arr/arr_test.go`:

```go
package arr

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

var ctx = context.Background()

// newClient starts a test server with the given routes and returns a client for it.
func newClient(t *testing.T, routes map[string]http.HandlerFunc) *Client {
	t.Helper()
	mux := http.NewServeMux()
	for path, h := range routes {
		mux.HandleFunc(path, h)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, APIKey: "k", HTTP: srv.Client()}
}

func body(s string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, s) }
}

// paged serves records as a paged resource and records each request's query.
func paged(records []string, queries *[]url.Values) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if queries != nil {
			*queries = append(*queries, q)
		}
		page, _ := strconv.Atoi(q.Get("page"))
		size, _ := strconv.Atoi(q.Get("pageSize"))
		start := (page - 1) * size
		var slice []string
		if start < len(records) {
			slice = records[start:min(start+size, len(records))]
		}
		fmt.Fprintf(w, `{"page":%d,"pageSize":%d,"totalRecords":%d,"records":[%s]}`,
			page, size, len(records), strings.Join(slice, ","))
	}
}

func linksClient(t *testing.T, history, queue []string) *Client {
	return newClient(t, map[string]http.HandlerFunc{
		"/api/v3/history": paged(history, nil),
		"/api/v3/queue":   paged(queue, nil),
	})
}

func TestGetSendsTheAPIKey(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/v3/tag": func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Api-Key") != "k" {
				http.Error(w, "no key", http.StatusUnauthorized)
				return
			}
			fmt.Fprint(w, `[]`)
		},
	})
	if _, err := c.Tags(ctx); err != nil {
		t.Errorf("Tags: %v", err)
	}
}

func TestGetKeepsBasePath(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/sonarr/api/v3/tag": body(`[{"id":1,"label":"keep"}]`),
	})
	c.BaseURL += "/sonarr"
	tags, err := c.Tags(ctx)
	if err != nil || tags[1] != "keep" {
		t.Errorf("Tags behind a base path = %v, %v", tags, err)
	}
}

func TestTagsRootFoldersAndStatus(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/v3/tag":           body(`[{"id":1,"label":"keep"},{"id":2,"label":"4k"}]`),
		"/api/v3/rootfolder":    body(`[{"id":1,"path":"/media/tv","freeSpace":123,"accessible":true}]`),
		"/api/v3/system/status": body(`{"appName":"Sonarr","version":"4.0.9.2244"}`),
	})

	tags, err := c.Tags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := TagLabels(tags, []int{2, 9, 1}); !reflect.DeepEqual(got, []string{"4k", "keep"}) {
		t.Errorf("TagLabels = %v, want [4k keep] (unknown IDs are skipped)", got)
	}

	folders, err := c.RootFolders(ctx)
	if err != nil || len(folders) != 1 || folders[0].Path != "/media/tv" || folders[0].FreeBytes != 123 {
		t.Errorf("RootFolders = %+v, %v", folders, err)
	}

	status, err := c.Status(ctx)
	if err != nil || status != "Sonarr 4.0.9.2244" {
		t.Errorf("Status = %q, %v", status, err)
	}

	if IDString(0) != "" || IDString(603) != "603" {
		t.Error("IDString must give an empty string for 0")
	}
}

func TestLinksLowercasesDownloadIDs(t *testing.T) {
	c := linksClient(t,
		[]string{`{"eventType":"grabbed","downloadId":"ABCDEF","episodeId":5}`},
		[]string{`{"downloadId":"FFEE"}`, `{"downloadId":""}`},
	)
	links, err := c.Links(ctx, LinkOptions{Episodes: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(links.Grabbed, map[string][]int{"abcdef": {5}}) {
		t.Errorf("Grabbed = %v, want abcdef -> [5]", links.Grabbed)
	}
	if !reflect.DeepEqual(links.Queue, map[string]bool{"ffee": true}) {
		t.Errorf("Queue = %v, want only ffee", links.Queue)
	}
}

func TestLinksFindsTheSourceOfEachFile(t *testing.T) {
	// Newest first: the episode was upgraded from OLD to NEW.
	c := linksClient(t, []string{
		`{"eventType":"downloadFolderImported","downloadId":"NEW","episodeId":5,"data":{"fileId":"30"}}`,
		`{"eventType":"grabbed","downloadId":"NEW","episodeId":5}`,
		`{"eventType":"episodeFileDeleted","downloadId":"","episodeId":5,"data":{"reason":"Upgrade"}}`,
		`{"eventType":"downloadFolderImported","downloadId":"OLD","episodeId":5,"data":{"FileId":20}}`,
		`{"eventType":"grabbed","downloadId":"OLD","episodeId":5}`,
	}, nil)
	links, err := c.Links(ctx, LinkOptions{Episodes: true, CurrentFiles: map[int]int{5: 30}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(links.FileSource, map[int]string{30: "new", 20: "old"}) {
		t.Errorf("FileSource = %v, want 30 -> new and 20 -> old", links.FileSource)
	}
	if !reflect.DeepEqual(links.Grabbed, map[string][]int{"new": {5}, "old": {5}}) {
		t.Errorf("Grabbed = %v, want each download listed once for episode 5", links.Grabbed)
	}
}

func TestLinksFallsBackWhenImportsCarryNoFileID(t *testing.T) {
	c := linksClient(t, []string{
		`{"eventType":"downloadFolderImported","downloadId":"NEW","episodeId":5,"data":{}}`,
		`{"eventType":"downloadFolderImported","downloadId":"OLD","episodeId":5,"data":{}}`,
	}, nil)
	links, err := c.Links(ctx, LinkOptions{Episodes: true, CurrentFiles: map[int]int{5: 30}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(links.FileSource, map[int]string{30: "new"}) {
		t.Errorf("FileSource = %v, want the current file linked to the most recent import", links.FileSource)
	}
}

func TestLinksIgnoresManualImports(t *testing.T) {
	tests := []struct {
		name    string
		history []string
		want    map[int]string
	}{
		{"file IDs reported", []string{
			`{"eventType":"downloadFolderImported","downloadId":"","episodeId":5,"data":{"fileId":"40"}}`,
			`{"eventType":"downloadFolderImported","downloadId":"OLD","episodeId":5,"data":{"fileId":"30"}}`,
		}, map[int]string{30: "old"}},
		{"file IDs not reported", []string{
			`{"eventType":"downloadFolderImported","downloadId":"","episodeId":5,"data":{}}`,
			`{"eventType":"downloadFolderImported","downloadId":"OLD","episodeId":5,"data":{}}`,
		}, map[int]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := linksClient(t, tt.history, nil)
			links, err := c.Links(ctx, LinkOptions{Episodes: true, CurrentFiles: map[int]int{5: 40}})
			if err != nil {
				t.Fatal(err)
			}
			if _, linked := links.FileSource[40]; linked {
				t.Error("the manually imported file 40 must not be linked to any download")
			}
			if !reflect.DeepEqual(links.FileSource, tt.want) {
				t.Errorf("FileSource = %v, want %v", links.FileSource, tt.want)
			}
			if _, phantom := links.Grabbed[""]; phantom {
				t.Error("an empty download ID must never become a key")
			}
		})
	}
}

func TestLinksReadsEveryPage(t *testing.T) {
	var history, queue []string
	for i := 1; i <= 5; i++ {
		history = append(history, fmt.Sprintf(`{"eventType":"grabbed","downloadId":"H%d","movieId":%d}`, i, i))
	}
	for i := 1; i <= 3; i++ {
		queue = append(queue, fmt.Sprintf(`{"downloadId":"Q%d"}`, i))
	}
	var historyQueries, queueQueries []url.Values
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/v3/history": paged(history, &historyQueries),
		"/api/v3/queue":   paged(queue, &queueQueries),
	})
	c.PageSize = 2

	links, err := c.Links(ctx, LinkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(links.Grabbed) != 5 || len(links.Queue) != 3 {
		t.Errorf("read %d history downloads and %d queued, want 5 and 3", len(links.Grabbed), len(links.Queue))
	}
	if len(historyQueries) != 3 || len(queueQueries) != 2 {
		t.Errorf("made %d history and %d queue requests, want 3 and 2", len(historyQueries), len(queueQueries))
	}
	first := historyQueries[0]
	if first.Get("sortKey") != "date" || first.Get("sortDirection") != "descending" {
		t.Errorf("history must be requested newest first, query = %v", first)
	}
}

func TestLinksUsesTheRightItemAndQueueParameter(t *testing.T) {
	record := `{"eventType":"grabbed","downloadId":"AA","movieId":7,"episodeId":9}`
	for _, episodes := range []bool{false, true} {
		var queueQueries []url.Values
		c := newClient(t, map[string]http.HandlerFunc{
			"/api/v3/history": paged([]string{record}, nil),
			"/api/v3/queue":   paged(nil, &queueQueries),
		})
		links, err := c.Links(ctx, LinkOptions{Episodes: episodes})
		if err != nil {
			t.Fatal(err)
		}
		wantItem, wantParam := 7, "includeUnknownMovieItems"
		if episodes {
			wantItem, wantParam = 9, "includeUnknownSeriesItems"
		}
		if !reflect.DeepEqual(links.Grabbed["aa"], []int{wantItem}) {
			t.Errorf("episodes=%v: Grabbed[aa] = %v, want [%d]", episodes, links.Grabbed["aa"], wantItem)
		}
		if queueQueries[0].Get(wantParam) != "true" {
			t.Errorf("episodes=%v: queue query = %v, want %s=true", episodes, queueQueries[0], wantParam)
		}
	}
}

func TestLinksFailsWhenHistoryFails(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/v3/history": func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) },
	})
	if _, err := c.Links(ctx, LinkOptions{}); err == nil || !strings.Contains(err.Error(), "history") {
		t.Errorf("err = %v, want an error that names the history", err)
	}
}
```

- [ ] **Step 7: Run the tests to verify they fail**

Run: `go test ./internal/providers/arr/`
Expected: FAIL to compile with `undefined: Client`, `undefined: TagLabels`, `undefined: LinkOptions`, `undefined: IDString`.

- [ ] **Step 8: Write the shared client**

Create `internal/providers/arr/arr.go`:

```go
// Package arr holds what the Sonarr and Radarr clients share. Both apps expose the same
// v3 API shape for tags, root folders, status, history and the queue.
package arr

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/httpjson"
)

const (
	defaultPageSize = 1000
	eventImported   = "downloadFolderImported"
)

// Client talks to one Sonarr or Radarr instance.
type Client struct {
	BaseURL  string // no trailing slash; may include a path prefix such as /sonarr
	APIKey   string
	HTTP     *http.Client
	PageSize int // records per page for history and queue; 0 means 1000
}

// Get requests path and decodes the JSON response into out. It does not change q.
func (c *Client) Get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return httpjson.Get(ctx, c.HTTP, u, http.Header{"X-Api-Key": {c.APIKey}}, out)
}

// Tags returns the tag labels by tag ID.
func (c *Client) Tags(ctx context.Context) (map[int]string, error) {
	var raw []struct {
		ID    int    `json:"id"`
		Label string `json:"label"`
	}
	if err := c.Get(ctx, "/api/v3/tag", nil, &raw); err != nil {
		return nil, err
	}
	out := make(map[int]string, len(raw))
	for _, t := range raw {
		out[t.ID] = t.Label
	}
	return out, nil
}

// TagLabels turns tag IDs into labels, skipping IDs that are not known.
func TagLabels(byID map[int]string, ids []int) []string {
	var out []string
	for _, id := range ids {
		if label, ok := byID[id]; ok {
			out = append(out, label)
		}
	}
	return out
}

// IDString formats a numeric external ID. Zero means unknown and gives an empty string.
func IDString(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// RootFolders returns the library folders with their free space.
func (c *Client) RootFolders(ctx context.Context) ([]domain.RootFolder, error) {
	var raw []struct {
		Path      string `json:"path"`
		FreeSpace int64  `json:"freeSpace"`
	}
	if err := c.Get(ctx, "/api/v3/rootfolder", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]domain.RootFolder, 0, len(raw))
	for _, f := range raw {
		out = append(out, domain.RootFolder{Path: f.Path, FreeBytes: f.FreeSpace})
	}
	return out, nil
}

// Status returns the app's name and version, for example "Sonarr 4.0.9.2244".
func (c *Client) Status(ctx context.Context) (string, error) {
	var raw struct {
		AppName string `json:"appName"`
		Version string `json:"version"`
	}
	if err := c.Get(ctx, "/api/v3/system/status", nil, &raw); err != nil {
		return "", err
	}
	return strings.TrimSpace(raw.AppName + " " + raw.Version), nil
}

// page is one page of a paged resource.
type page[T any] struct {
	TotalRecords int `json:"totalRecords"`
	Records      []T `json:"records"`
}

// eachPage reads every page of a paged resource and calls fn for each record, in order.
// It sets the page and pageSize parameters on q.
func eachPage[T any](ctx context.Context, c *Client, path string, q url.Values, fn func(T)) error {
	size := c.PageSize
	if size <= 0 {
		size = defaultPageSize
	}
	q.Set("pageSize", strconv.Itoa(size))
	for pageNo := 1; ; pageNo++ {
		q.Set("page", strconv.Itoa(pageNo))
		var p page[T]
		if err := c.Get(ctx, path, q, &p); err != nil {
			return err
		}
		for _, r := range p.Records {
			fn(r)
		}
		if len(p.Records) == 0 || pageNo*size >= p.TotalRecords {
			return nil
		}
	}
}

type historyRecord struct {
	EventType  string         `json:"eventType"`
	DownloadID string         `json:"downloadId"`
	MovieID    int            `json:"movieId"`
	EpisodeID  int            `json:"episodeId"`
	Data       map[string]any `json:"data"`
}

type queueRecord struct {
	DownloadID string `json:"downloadId"`
}

// LinkOptions adapts Links to Sonarr or Radarr.
type LinkOptions struct {
	// Episodes is true for Sonarr, whose history items are episodes, and false for Radarr,
	// whose history items are movies.
	Episodes bool
	// CurrentFiles maps an item ID to the ID of its current library file. It is used only
	// when import events do not report the file ID.
	CurrentFiles map[int]int
}

// Links reads the whole history and the queue and works out which download each library
// file came from. Download IDs are lower-cased so they compare equal to the download
// client's IDs.
func (c *Client) Links(ctx context.Context, opt LinkOptions) (domain.Links, error) {
	links := domain.Links{Grabbed: map[string][]int{}, FileSource: map[int]string{}, Queue: map[string]bool{}}

	type importInfo struct {
		download  string
		hasFileID bool
	}
	newestImport := map[int]importInfo{} // item ID -> its most recent import event
	grabbed := map[string]map[int]bool{}

	history := url.Values{"sortKey": {"date"}, "sortDirection": {"descending"}}
	err := eachPage(ctx, c, "/api/v3/history", history, func(r historyRecord) {
		item := r.MovieID
		if opt.Episodes {
			item = r.EpisodeID
		}
		if item == 0 {
			return
		}
		download := normalizeID(r.DownloadID)
		if download != "" {
			if grabbed[download] == nil {
				grabbed[download] = map[int]bool{}
			}
			if !grabbed[download][item] {
				grabbed[download][item] = true
				links.Grabbed[download] = append(links.Grabbed[download], item)
			}
		}
		if r.EventType != eventImported {
			return
		}
		fileID, hasFileID := intFrom(r.Data, "fileId")
		if _, seen := newestImport[item]; !seen {
			newestImport[item] = importInfo{download: download, hasFileID: hasFileID}
		}
		if hasFileID && download != "" {
			if _, set := links.FileSource[fileID]; !set {
				links.FileSource[fileID] = download
			}
		}
	})
	if err != nil {
		return domain.Links{}, fmt.Errorf("history: %w", err)
	}

	// Some versions do not report the file ID on import events. For those, the most recent
	// import of an item is taken as the source of the item's current file. A manual import
	// has no download ID, so it links nothing.
	for item, file := range opt.CurrentFiles {
		if _, ok := links.FileSource[file]; ok {
			continue
		}
		if imp, ok := newestImport[item]; ok && !imp.hasFileID && imp.download != "" {
			links.FileSource[file] = imp.download
		}
	}

	unknown := "includeUnknownMovieItems"
	if opt.Episodes {
		unknown = "includeUnknownSeriesItems"
	}
	err = eachPage(ctx, c, "/api/v3/queue", url.Values{unknown: {"true"}}, func(r queueRecord) {
		if download := normalizeID(r.DownloadID); download != "" {
			links.Queue[download] = true
		}
	})
	if err != nil {
		return domain.Links{}, fmt.Errorf("queue: %w", err)
	}
	return links, nil
}

func normalizeID(id string) string { return strings.ToLower(strings.TrimSpace(id)) }

// intFrom reads an integer stored under key, ignoring the key's letter case. The apps send
// the value as a string; a JSON number is accepted too.
func intFrom(data map[string]any, key string) (int, bool) {
	for k, v := range data {
		if !strings.EqualFold(k, key) {
			continue
		}
		switch x := v.(type) {
		case string:
			n, err := strconv.Atoi(strings.TrimSpace(x))
			return n, err == nil
		case float64:
			return int(x), true
		}
	}
	return 0, false
}
```

- [ ] **Step 9: Run all tests to verify they pass**

Run: `go vet ./... && go test ./...`
Expected: `ok` for every package including `internal/httpjson` and `internal/providers/arr`; `internal/providers` reports `[no test files]`; no vet output.

- [ ] **Step 10: Commit**

```bash
git add internal/providers internal/httpjson
git commit -m "feat: provider interfaces and shared Sonarr/Radarr client with download links"
```

---

### Task 8: Radarr and Sonarr providers

**Files:**
- Create: `internal/providers/radarr/radarr.go`, `internal/providers/sonarr/sonarr.go`
- Test: `internal/providers/radarr/radarr_test.go`, `internal/providers/sonarr/sonarr_test.go`

**Interfaces:**
- Consumes:
  - Task 1: `domain.Movie`, `domain.Series`, `domain.Episode`, `domain.MediaFile`, `domain.ExternalIDs`, `domain.MovieSnapshot`, `domain.SeriesSnapshot`.
  - Task 7: `providers.MovieManager`, `providers.SeriesManager`, `providers.Tester`; `arr.Client`, `(*arr.Client).Get`, `Tags`, `RootFolders`, `Status`, `Links`, `arr.LinkOptions`, `arr.TagLabels`, `arr.IDString`; `httpjson.ParseTime`.
- Produces:
  - Package `radarr`: `New(baseURL, apiKey string, hc *http.Client) *Client`; `*Client` implements `providers.MovieManager` and `providers.Tester`.
  - Package `sonarr`: `New(baseURL, apiKey string, hc *http.Client) *Client`; `*Client` implements `providers.SeriesManager` and `providers.Tester`.

API endpoints used, all GET:

| App | Endpoint | Fields read |
|---|---|---|
| Radarr | `/api/v3/movie` | `id`, `title`, `year`, `tmdbId`, `imdbId`, `tags`, `hasFile`, `movieFile.id`, `movieFile.size` |
| Sonarr | `/api/v3/series` | `id`, `title`, `year`, `tvdbId`, `tmdbId`, `imdbId`, `status`, `tags`, `statistics.episodeFileCount` |
| Sonarr | `/api/v3/episode?seriesId=N` | `id`, `seasonNumber`, `episodeNumber`, `airDateUtc`, `hasFile`, `episodeFileId` |
| Sonarr | `/api/v3/episodefile?seriesId=N` | `id`, `size` |

A Sonarr series counts as ended only when its `status` is `ended`. Episodes are not fetched for a series whose `statistics.episodeFileCount` is 0, because a series without files can never produce a row. If Sonarr sends no statistics at all, the episodes are fetched, so a series is never skipped by mistake.

- [ ] **Step 1: Write the failing test for Radarr**

Create `internal/providers/radarr/radarr_test.go`:

```go
package radarr

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/removarr/removarr/internal/domain"
)

func body(s string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, s) }
}

func TestMovies(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/movie", body(`[
		{"id":1,"title":"The Matrix","year":1999,"tmdbId":603,"imdbId":"tt0133093","hasFile":true,"tags":[1],
		 "movieFile":{"id":10,"size":5000,"relativePath":"The Matrix (1999).mkv"}},
		{"id":2,"title":"Not Downloaded","year":2020,"tmdbId":0,"imdbId":"","hasFile":false,"tags":[]}
	]`))
	mux.HandleFunc("/api/v3/tag", body(`[{"id":1,"label":"keep"}]`))
	mux.HandleFunc("/api/v3/history", body(`{"page":1,"pageSize":1000,"totalRecords":2,"records":[
		{"eventType":"downloadFolderImported","downloadId":"ABC","movieId":1,"data":{"fileId":"10"}},
		{"eventType":"grabbed","downloadId":"ABC","movieId":1}
	]}`))
	mux.HandleFunc("/api/v3/queue", body(`{"page":1,"pageSize":1000,"totalRecords":1,"records":[{"downloadId":"QQQ"}]}`))
	mux.HandleFunc("/api/v3/rootfolder", body(`[{"id":1,"path":"/media/movies","freeSpace":999}]`))
	mux.HandleFunc("/api/v3/system/status", body(`{"appName":"Radarr","version":"5.14.0"}`))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL, "k", srv.Client())
	snap, err := c.Movies(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	want := []domain.Movie{
		{ID: 1, Title: "The Matrix", Year: 1999, IDs: domain.ExternalIDs{TMDB: "603", IMDB: "tt0133093"},
			Tags: []string{"keep"}, File: &domain.MediaFile{ID: 10, Size: 5000}},
		{ID: 2, Title: "Not Downloaded", Year: 2020},
	}
	if !reflect.DeepEqual(snap.Movies, want) {
		t.Errorf("Movies =\n%+v\nwant\n%+v", snap.Movies, want)
	}
	if snap.Links.FileSource[10] != "abc" || !reflect.DeepEqual(snap.Links.Grabbed["abc"], []int{1}) || !snap.Links.Queue["qqq"] {
		t.Errorf("Links = %+v", snap.Links)
	}
	if len(snap.RootFolders) != 1 || snap.RootFolders[0].FreeBytes != 999 {
		t.Errorf("RootFolders = %+v", snap.RootFolders)
	}

	detail, err := c.Test(context.Background())
	if err != nil || detail != "Radarr 5.14.0" {
		t.Errorf("Test = %q, %v", detail, err)
	}
}

func TestMoviesReportsWhichRequestFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := New(srv.URL, "wrong", srv.Client()).Movies(context.Background())
	if err == nil {
		t.Fatal("want an error when the API key is rejected")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/providers/radarr/`
Expected: FAIL to compile with `undefined: New`.

- [ ] **Step 3: Write the Radarr provider**

Create `internal/providers/radarr/radarr.go`:

```go
// Package radarr reads movies from Radarr.
package radarr

import (
	"context"
	"fmt"
	"net/http"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/providers"
	"github.com/removarr/removarr/internal/providers/arr"
)

var (
	_ providers.MovieManager = (*Client)(nil)
	_ providers.Tester       = (*Client)(nil)
)

// Client talks to one Radarr instance.
type Client struct{ arr *arr.Client }

// New returns a client. baseURL has no trailing slash.
func New(baseURL, apiKey string, hc *http.Client) *Client {
	return &Client{arr: &arr.Client{BaseURL: baseURL, APIKey: apiKey, HTTP: hc}}
}

type movieJSON struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Year    int    `json:"year"`
	TMDBID  int    `json:"tmdbId"`
	IMDBID  string `json:"imdbId"`
	Tags    []int  `json:"tags"`
	HasFile bool   `json:"hasFile"`
	File    *struct {
		ID   int   `json:"id"`
		Size int64 `json:"size"`
	} `json:"movieFile"`
}

// Movies returns every movie with its file, tags and download links.
func (c *Client) Movies(ctx context.Context) (domain.MovieSnapshot, error) {
	var raw []movieJSON
	if err := c.arr.Get(ctx, "/api/v3/movie", nil, &raw); err != nil {
		return domain.MovieSnapshot{}, fmt.Errorf("movies: %w", err)
	}
	tags, err := c.arr.Tags(ctx)
	if err != nil {
		return domain.MovieSnapshot{}, fmt.Errorf("tags: %w", err)
	}

	var snap domain.MovieSnapshot
	current := map[int]int{} // movie ID -> current file ID
	for _, m := range raw {
		movie := domain.Movie{
			ID:    m.ID,
			Title: m.Title,
			Year:  m.Year,
			IDs:   domain.ExternalIDs{TMDB: arr.IDString(m.TMDBID), IMDB: m.IMDBID},
			Tags:  arr.TagLabels(tags, m.Tags),
		}
		if m.HasFile && m.File != nil {
			movie.File = &domain.MediaFile{ID: m.File.ID, Size: m.File.Size}
			current[m.ID] = m.File.ID
		}
		snap.Movies = append(snap.Movies, movie)
	}

	if snap.Links, err = c.arr.Links(ctx, arr.LinkOptions{CurrentFiles: current}); err != nil {
		return domain.MovieSnapshot{}, err
	}
	if snap.RootFolders, err = c.arr.RootFolders(ctx); err != nil {
		return domain.MovieSnapshot{}, fmt.Errorf("root folders: %w", err)
	}
	return snap, nil
}

// Test checks the connection and returns the app name and version.
func (c *Client) Test(ctx context.Context) (string, error) { return c.arr.Status(ctx) }
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/providers/radarr/`
Expected: `ok  	github.com/removarr/removarr/internal/providers/radarr`

- [ ] **Step 5: Write the failing test for Sonarr**

Create `internal/providers/sonarr/sonarr_test.go`:

```go
package sonarr

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/removarr/removarr/internal/domain"
)

func body(s string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, s) }
}

// forSeries serves s for series 1 and fails the test for any other series.
func forSeries(t *testing.T, s string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if id := r.URL.Query().Get("seriesId"); id != "1" {
			t.Errorf("%s requested for series %q; only series 1 has files", r.URL.Path, id)
		}
		fmt.Fprint(w, s)
	}
}

func TestSeries(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/series", body(`[
		{"id":1,"title":"The Show","year":2020,"tvdbId":100,"tmdbId":200,"imdbId":"tt100","status":"ended","tags":[1],
		 "statistics":{"episodeFileCount":2}},
		{"id":2,"title":"Nothing Yet","year":2021,"tvdbId":101,"status":"continuing","tags":[],
		 "statistics":{"episodeFileCount":0}}
	]`))
	mux.HandleFunc("/api/v3/episode", forSeries(t, `[
		{"id":11,"seasonNumber":1,"episodeNumber":1,"airDateUtc":"2020-01-01T01:00:00Z","hasFile":true,"episodeFileId":111},
		{"id":12,"seasonNumber":1,"episodeNumber":2,"airDateUtc":"2020-01-08T01:00:00Z","hasFile":true,"episodeFileId":111},
		{"id":13,"seasonNumber":1,"episodeNumber":3,"hasFile":false,"episodeFileId":0}
	]`))
	mux.HandleFunc("/api/v3/episodefile", forSeries(t, `[{"id":111,"size":900}]`))
	mux.HandleFunc("/api/v3/tag", body(`[{"id":1,"label":"keep"}]`))
	mux.HandleFunc("/api/v3/history", body(`{"page":1,"pageSize":1000,"totalRecords":2,"records":[
		{"eventType":"downloadFolderImported","downloadId":"PACK","episodeId":11,"data":{"fileId":"111"}},
		{"eventType":"downloadFolderImported","downloadId":"PACK","episodeId":12,"data":{"fileId":"111"}}
	]}`))
	mux.HandleFunc("/api/v3/queue", body(`{"page":1,"pageSize":1000,"totalRecords":0,"records":[]}`))
	mux.HandleFunc("/api/v3/rootfolder", body(`[{"id":1,"path":"/media/tv","freeSpace":777}]`))
	mux.HandleFunc("/api/v3/system/status", body(`{"appName":"Sonarr","version":"4.0.9"}`))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL, "k", srv.Client())
	snap, err := c.Series(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Series) != 2 {
		t.Fatalf("got %d series, want 2", len(snap.Series))
	}

	show := snap.Series[0]
	if show.ID != 1 || show.Title != "The Show" || show.Year != 2020 || !show.Ended {
		t.Errorf("series fields wrong: %+v", show)
	}
	if show.IDs != (domain.ExternalIDs{TVDB: "100", TMDB: "200", IMDB: "tt100"}) {
		t.Errorf("IDs = %+v", show.IDs)
	}
	if !reflect.DeepEqual(show.Tags, []string{"keep"}) {
		t.Errorf("Tags = %v", show.Tags)
	}
	if len(show.Episodes) != 3 {
		t.Fatalf("got %d episodes, want 3", len(show.Episodes))
	}
	first, second, third := show.Episodes[0], show.Episodes[1], show.Episodes[2]
	if first.ID != 11 || first.Season != 1 || first.Number != 1 || first.AirDate == nil {
		t.Errorf("first episode wrong: %+v", first)
	}
	wantFile := domain.MediaFile{ID: 111, Size: 900}
	if first.File == nil || *first.File != wantFile || second.File == nil || *second.File != wantFile {
		t.Errorf("both episodes of the double-episode file must carry file 111 with its size: %+v, %+v", first.File, second.File)
	}
	if third.File != nil || third.AirDate != nil {
		t.Errorf("third episode has no file and no air date: %+v", third)
	}

	empty := snap.Series[1]
	if empty.Ended || len(empty.Episodes) != 0 || empty.IDs.TVDB != "101" {
		t.Errorf("series without files wrong: %+v", empty)
	}

	if snap.Links.FileSource[111] != "pack" || !reflect.DeepEqual(snap.Links.Grabbed["pack"], []int{11, 12}) {
		t.Errorf("Links = %+v", snap.Links)
	}
	if len(snap.RootFolders) != 1 || snap.RootFolders[0].Path != "/media/tv" {
		t.Errorf("RootFolders = %+v", snap.RootFolders)
	}

	detail, err := c.Test(context.Background())
	if err != nil || detail != "Sonarr 4.0.9" {
		t.Errorf("Test = %q, %v", detail, err)
	}
}
```

- [ ] **Step 6: Run the test to verify it fails**

Run: `go test ./internal/providers/sonarr/`
Expected: FAIL to compile with `undefined: New`.

- [ ] **Step 7: Write the Sonarr provider**

Create `internal/providers/sonarr/sonarr.go`:

```go
// Package sonarr reads series from Sonarr.
package sonarr

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/httpjson"
	"github.com/removarr/removarr/internal/providers"
	"github.com/removarr/removarr/internal/providers/arr"
)

var (
	_ providers.SeriesManager = (*Client)(nil)
	_ providers.Tester        = (*Client)(nil)
)

// Client talks to one Sonarr instance.
type Client struct{ arr *arr.Client }

// New returns a client. baseURL has no trailing slash.
func New(baseURL, apiKey string, hc *http.Client) *Client {
	return &Client{arr: &arr.Client{BaseURL: baseURL, APIKey: apiKey, HTTP: hc}}
}

type seriesJSON struct {
	ID         int    `json:"id"`
	Title      string `json:"title"`
	Year       int    `json:"year"`
	TVDBID     int    `json:"tvdbId"`
	TMDBID     int    `json:"tmdbId"`
	IMDBID     string `json:"imdbId"`
	Status     string `json:"status"`
	Tags       []int  `json:"tags"`
	Statistics *struct {
		EpisodeFileCount int `json:"episodeFileCount"`
	} `json:"statistics"`
}

// hasFiles reports whether the series may have files. When Sonarr sends no statistics the
// answer is yes, so a series is never skipped by mistake.
func (s seriesJSON) hasFiles() bool {
	return s.Statistics == nil || s.Statistics.EpisodeFileCount > 0
}

type episodeJSON struct {
	ID            int    `json:"id"`
	SeasonNumber  int    `json:"seasonNumber"`
	EpisodeNumber int    `json:"episodeNumber"`
	AirDateUTC    string `json:"airDateUtc"`
	HasFile       bool   `json:"hasFile"`
	EpisodeFileID int    `json:"episodeFileId"`
}

type fileJSON struct {
	ID   int   `json:"id"`
	Size int64 `json:"size"`
}

// Series returns every series with its tags and download links. Episodes are included
// for the series that have at least one file.
func (c *Client) Series(ctx context.Context) (domain.SeriesSnapshot, error) {
	var raw []seriesJSON
	if err := c.arr.Get(ctx, "/api/v3/series", nil, &raw); err != nil {
		return domain.SeriesSnapshot{}, fmt.Errorf("series: %w", err)
	}
	tags, err := c.arr.Tags(ctx)
	if err != nil {
		return domain.SeriesSnapshot{}, fmt.Errorf("tags: %w", err)
	}

	var snap domain.SeriesSnapshot
	current := map[int]int{} // episode ID -> current file ID
	for _, s := range raw {
		series := domain.Series{
			ID:    s.ID,
			Title: s.Title,
			Year:  s.Year,
			IDs:   domain.ExternalIDs{TVDB: arr.IDString(s.TVDBID), TMDB: arr.IDString(s.TMDBID), IMDB: s.IMDBID},
			Tags:  arr.TagLabels(tags, s.Tags),
			Ended: s.Status == "ended",
		}
		if s.hasFiles() {
			episodes, err := c.episodes(ctx, s.ID)
			if err != nil {
				return domain.SeriesSnapshot{}, fmt.Errorf("episodes of %q: %w", s.Title, err)
			}
			series.Episodes = episodes
			for _, e := range episodes {
				if e.File != nil {
					current[e.ID] = e.File.ID
				}
			}
		}
		snap.Series = append(snap.Series, series)
	}

	if snap.Links, err = c.arr.Links(ctx, arr.LinkOptions{Episodes: true, CurrentFiles: current}); err != nil {
		return domain.SeriesSnapshot{}, err
	}
	if snap.RootFolders, err = c.arr.RootFolders(ctx); err != nil {
		return domain.SeriesSnapshot{}, fmt.Errorf("root folders: %w", err)
	}
	return snap, nil
}

// episodes returns the episodes of one series, each with its file when it has one.
func (c *Client) episodes(ctx context.Context, seriesID int) ([]domain.Episode, error) {
	q := url.Values{"seriesId": {strconv.Itoa(seriesID)}}

	var files []fileJSON
	if err := c.arr.Get(ctx, "/api/v3/episodefile", q, &files); err != nil {
		return nil, err
	}
	sizes := make(map[int]int64, len(files))
	for _, f := range files {
		sizes[f.ID] = f.Size
	}

	var raw []episodeJSON
	if err := c.arr.Get(ctx, "/api/v3/episode", q, &raw); err != nil {
		return nil, err
	}
	out := make([]domain.Episode, 0, len(raw))
	for _, e := range raw {
		episode := domain.Episode{
			ID:      e.ID,
			Season:  e.SeasonNumber,
			Number:  e.EpisodeNumber,
			AirDate: httpjson.ParseTime(e.AirDateUTC),
		}
		if e.HasFile && e.EpisodeFileID != 0 {
			episode.File = &domain.MediaFile{ID: e.EpisodeFileID, Size: sizes[e.EpisodeFileID]}
		}
		out = append(out, episode)
	}
	return out, nil
}

// Test checks the connection and returns the app name and version.
func (c *Client) Test(ctx context.Context) (string, error) { return c.arr.Status(ctx) }
```

- [ ] **Step 8: Run all tests to verify they pass**

Run: `go vet ./... && go test ./...`
Expected: `ok` for every package with tests, no vet output.

- [ ] **Step 9: Commit**

```bash
git add internal/providers/radarr internal/providers/sonarr
git commit -m "feat: Radarr and Sonarr providers"
```

---

### Task 9: qBittorrent provider

**Files:**
- Create: `internal/providers/qbittorrent/qbittorrent.go`
- Test: `internal/providers/qbittorrent/qbittorrent_test.go`

**Interfaces:**
- Consumes: Task 1: `domain.Download`. Task 7: `providers.DownloadClient`, `providers.Tester`, `httpjson.StatusError`.
- Produces, in package `qbittorrent`:
  - `New(baseURL, username, password string, timeout time.Duration) (*Client, error)`; `*Client` implements `providers.DownloadClient` and `providers.Tester`.
  - `ErrLogin` (username or password rejected) and `ErrLoginRequired` (qBittorrent asks for a login and none is configured).

How qBittorrent's Web API (v2) behaves:

- `POST /api/v2/auth/login` with form fields `username` and `password` answers `Ok.` and sets a session cookie, or answers `Fails.` with status 200. This is the only non-GET request removarr sends in stage 1; it changes nothing in qBittorrent.
- Every other endpoint answers 403 without a valid session, unless qBittorrent is set to skip authentication for the caller's address.
- `GET /api/v2/torrents/info` returns every torrent. Fields read: `hash`, `name`, `size`, `state`, `category`, `tags` (one comma-separated string), `content_path`, `added_on` (Unix seconds), `ratio`, `seeding_time` (seconds), `tracker` (URL of the working tracker; empty when none works).
- `GET /api/v2/torrents/trackers?hash=H` lists a torrent's trackers. Entries such as `** [DHT] **` are not real trackers.
- `GET /api/v2/app/version` returns plain text such as `v5.0.3`.

Tracker URLs can contain a private passkey. Only the host name is kept.

- [ ] **Step 1: Write the failing tests**

Create `internal/providers/qbittorrent/qbittorrent_test.go`:

```go
package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

var ctx = context.Background()

// fakeQbit imitates the parts of qBittorrent's Web API the client uses.
type fakeQbit struct {
	auth     bool              // require a session cookie
	logins   int               // login attempts seen
	torrents string            // body of /torrents/info
	trackers map[string]string // hash -> body of /torrents/trackers
}

func (f *fakeQbit) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/auth/login", func(w http.ResponseWriter, r *http.Request) {
		f.logins++
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.FormValue("username") != "admin" || r.FormValue("password") != "secret" {
			fmt.Fprint(w, "Fails.")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "session", Path: "/"})
		fmt.Fprint(w, "Ok.")
	})
	guarded := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if f.auth {
				if c, err := r.Cookie("SID"); err != nil || c.Value != "session" {
					http.Error(w, "Forbidden", http.StatusForbidden)
					return
				}
			}
			h(w, r)
		}
	}
	mux.HandleFunc("/api/v2/torrents/info", guarded(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, f.torrents)
	}))
	mux.HandleFunc("/api/v2/torrents/trackers", guarded(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, f.trackers[r.URL.Query().Get("hash")])
	}))
	mux.HandleFunc("/api/v2/app/version", guarded(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "v5.0.3")
	}))
	return mux
}

func start(t *testing.T, f *fakeQbit, username, password string) *Client {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, username, password, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const oneTorrent = `[
	{"hash":"ABCDEF0123","name":"The.Matrix.1999","size":5000,"state":"stalledUP","category":"radarr",
	 "tags":"keep, cross-seed","content_path":"/downloads/The.Matrix.1999","added_on":1767225600,
	 "ratio":1.5,"seeding_time":172800,"tracker":"https://tracker.example.org:443/announce?passkey=secret"},
	{"hash":"","name":"broken entry"}
]`

func TestDownloadsMapsFields(t *testing.T) {
	c := start(t, &fakeQbit{torrents: oneTorrent}, "", "")
	got, err := c.Downloads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d downloads, want 1: an entry without a hash is skipped", len(got))
	}
	d := got[0]
	if d.ID != "abcdef0123" {
		t.Errorf("ID = %q, want the hash in lower case", d.ID)
	}
	if d.Name != "The.Matrix.1999" || d.Size != 5000 || d.State != "stalledUP" || d.Category != "radarr" || d.ContentPath != "/downloads/The.Matrix.1999" {
		t.Errorf("plain fields wrong: %+v", d)
	}
	if !reflect.DeepEqual(d.Tags, []string{"keep", "cross-seed"}) {
		t.Errorf("Tags = %q, want [keep cross-seed]", d.Tags)
	}
	if want := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC); !d.AddedAt.Equal(want) {
		t.Errorf("AddedAt = %v, want %v", d.AddedAt, want)
	}
	if d.Ratio == nil || *d.Ratio != 1.5 {
		t.Errorf("Ratio = %v, want 1.5", d.Ratio)
	}
	if d.SeedingTime == nil || *d.SeedingTime != 48*time.Hour {
		t.Errorf("SeedingTime = %v, want 48h", d.SeedingTime)
	}
	if d.Tracker != "tracker.example.org" {
		t.Errorf("Tracker = %q, want only the host name, never the passkey", d.Tracker)
	}
}

func TestDownloadsLogsInOnce(t *testing.T) {
	f := &fakeQbit{auth: true, torrents: oneTorrent}
	c := start(t, f, "admin", "secret")
	for i := 0; i < 2; i++ {
		if _, err := c.Downloads(ctx); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	if f.logins != 1 {
		t.Errorf("logged in %d times, want 1: the session is reused", f.logins)
	}
}

func TestDownloadsWithAWrongPassword(t *testing.T) {
	c := start(t, &fakeQbit{auth: true, torrents: oneTorrent}, "admin", "wrong")
	if _, err := c.Downloads(ctx); !errors.Is(err, ErrLogin) {
		t.Errorf("err = %v, want ErrLogin", err)
	}
}

func TestDownloadsWhenALoginIsNeededButNotConfigured(t *testing.T) {
	f := &fakeQbit{auth: true, torrents: oneTorrent}
	c := start(t, f, "", "")
	if _, err := c.Downloads(ctx); !errors.Is(err, ErrLoginRequired) {
		t.Errorf("err = %v, want ErrLoginRequired", err)
	}
	if f.logins != 0 {
		t.Errorf("logged in %d times, want 0: there are no credentials to send", f.logins)
	}
}

func TestDownloadsFallsBackToTheTrackerList(t *testing.T) {
	f := &fakeQbit{
		torrents: `[{"hash":"aa11","name":"x","tracker":""}]`,
		trackers: map[string]string{
			"aa11": `[{"url":"** [DHT] **"},{"url":"** [PeX] **"},{"url":"udp://open.tracker.example:1337/announce"}]`,
		},
	}
	got, err := start(t, f, "", "").Downloads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Tracker != "open.tracker.example" {
		t.Errorf("Tracker = %q, want open.tracker.example", got[0].Tracker)
	}
}

func TestTestReportsTheVersion(t *testing.T) {
	detail, err := start(t, &fakeQbit{auth: true}, "admin", "secret").Test(ctx)
	if err != nil || detail != "qBittorrent v5.0.3" {
		t.Errorf("Test = %q, %v", detail, err)
	}
}

func TestBasePathIsKept(t *testing.T) {
	f := &fakeQbit{auth: true, torrents: oneTorrent}
	srv := httptest.NewServer(http.StripPrefix("/qbt", f.handler()))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL+"/qbt", "admin", "secret", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.Downloads(ctx); err != nil || len(got) != 1 {
		t.Errorf("behind a base path: %d downloads, %v", len(got), err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/providers/qbittorrent/`
Expected: FAIL to compile with `undefined: New`, `undefined: Client`, `undefined: ErrLogin`, `undefined: ErrLoginRequired`.

- [ ] **Step 3: Write the qBittorrent provider**

Create `internal/providers/qbittorrent/qbittorrent.go`:

```go
// Package qbittorrent reads torrents from qBittorrent's Web API (v2).
package qbittorrent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/httpjson"
	"github.com/removarr/removarr/internal/providers"
)

var (
	_ providers.DownloadClient = (*Client)(nil)
	_ providers.Tester         = (*Client)(nil)
)

var (
	// ErrLogin is returned when qBittorrent rejects the username or password.
	ErrLogin = errors.New("qBittorrent rejected the username or password")
	// ErrLoginRequired is returned when qBittorrent asks for a login and none is configured.
	ErrLoginRequired = errors.New("qBittorrent requires a username and password")
)

// Client talks to one qBittorrent instance and keeps its session cookie.
type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client
}

// New returns a client. baseURL has no trailing slash. An empty username means qBittorrent
// is expected to allow access without a login.
func New(baseURL, username, password string, timeout time.Duration) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &Client{
		baseURL:  baseURL,
		username: username,
		password: password,
		http:     &http.Client{Jar: jar, Timeout: timeout},
	}, nil
}

type torrentJSON struct {
	Hash        string  `json:"hash"`
	Name        string  `json:"name"`
	Size        int64   `json:"size"`
	State       string  `json:"state"`
	Category    string  `json:"category"`
	Tags        string  `json:"tags"`
	ContentPath string  `json:"content_path"`
	AddedOn     int64   `json:"added_on"`
	Ratio       float64 `json:"ratio"`
	SeedingTime int64   `json:"seeding_time"`
	Tracker     string  `json:"tracker"`
}

// Downloads returns every torrent.
func (c *Client) Downloads(ctx context.Context) ([]domain.Download, error) {
	body, err := c.get(ctx, "/api/v2/torrents/info", nil)
	if err != nil {
		return nil, fmt.Errorf("torrents: %w", err)
	}
	var raw []torrentJSON
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("torrents: decode response: %w", err)
	}

	out := make([]domain.Download, 0, len(raw))
	for _, t := range raw {
		hash := strings.ToLower(strings.TrimSpace(t.Hash))
		if hash == "" {
			continue
		}
		ratio := t.Ratio
		seeded := time.Duration(t.SeedingTime) * time.Second
		tracker := trackerHost(t.Tracker)
		if tracker == "" {
			tracker = c.firstTracker(ctx, hash)
		}
		out = append(out, domain.Download{
			ID:          hash,
			Name:        t.Name,
			Size:        t.Size,
			State:       t.State,
			Category:    t.Category,
			Tags:        splitTags(t.Tags),
			ContentPath: t.ContentPath,
			AddedAt:     time.Unix(t.AddedOn, 0).UTC(),
			Ratio:       &ratio,
			SeedingTime: &seeded,
			Tracker:     tracker,
		})
	}
	return out, nil
}

// Test checks the connection and returns the app name and version.
func (c *Client) Test(ctx context.Context) (string, error) {
	body, err := c.get(ctx, "/api/v2/app/version", nil)
	if err != nil {
		return "", err
	}
	return "qBittorrent " + strings.TrimSpace(string(body)), nil
}

// firstTracker asks for a torrent's tracker list and returns the host of its first real
// tracker. It is used when qBittorrent reports no working tracker. Any failure gives "".
func (c *Client) firstTracker(ctx context.Context, hash string) string {
	body, err := c.get(ctx, "/api/v2/torrents/trackers", url.Values{"hash": {hash}})
	if err != nil {
		return ""
	}
	var raw []struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return ""
	}
	for _, t := range raw {
		if host := trackerHost(t.URL); host != "" {
			return host
		}
	}
	return ""
}

// get requests path and returns the response body. When qBittorrent answers 403 the client
// logs in and tries once more.
func (c *Client) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	body, status, err := c.once(ctx, path, q)
	if err != nil {
		return nil, err
	}
	if status == http.StatusForbidden {
		if c.username == "" {
			return nil, ErrLoginRequired
		}
		if err := c.login(ctx); err != nil {
			return nil, err
		}
		if body, status, err = c.once(ctx, path, q); err != nil {
			return nil, err
		}
	}
	if status < 200 || status > 299 {
		return nil, &httpjson.StatusError{Status: status, Body: snippet(body)}
	}
	return body, nil
}

func (c *Client) once(ctx context.Context, path string, q url.Values) ([]byte, int, error) {
	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Referer", c.baseURL)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	return body, resp.StatusCode, nil
}

// login starts a session. The cookie jar keeps the session cookie for later requests.
func (c *Client) login(ctx context.Context) error {
	form := url.Values{"username": {c.username}, "password": {c.password}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", c.baseURL)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("qBittorrent login: %w", &httpjson.StatusError{Status: resp.StatusCode, Body: snippet(body)})
	}
	if strings.HasPrefix(strings.TrimSpace(string(body)), "Fails") {
		return ErrLogin
	}
	return nil
}

// trackerHost returns the host name of a tracker URL, or "" when raw is not a URL.
func trackerHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Hostname()
}

// splitTags splits qBittorrent's comma-separated tag string.
func splitTags(s string) []string {
	var out []string
	for _, tag := range strings.Split(s, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			out = append(out, tag)
		}
	}
	return out
}

func snippet(body []byte) string {
	if len(body) > 200 {
		body = body[:200]
	}
	return strings.TrimSpace(string(body))
}
```

- [ ] **Step 4: Run all tests to verify they pass**

Run: `go vet ./... && go test ./...`
Expected: `ok` for every package with tests, no vet output.

- [ ] **Step 5: Commit**

```bash
git add internal/providers/qbittorrent
git commit -m "feat: qBittorrent provider"
```

---

### Task 10: Jellyfin provider

**Files:**
- Create: `internal/providers/jellyfin/jellyfin.go`
- Test: `internal/providers/jellyfin/jellyfin_test.go`

**Interfaces:**
- Consumes: Task 1: `domain.User`, `domain.Library`, `domain.ServerMovie`, `domain.ServerSeries`, `domain.ServerEpisode`, `domain.UserState`, `domain.ExternalIDs`. Task 7: `providers.MediaServer`, `providers.Tester`, `httpjson.Get`, `httpjson.ParseTime`.
- Produces, in package `jellyfin`: `New(baseURL, apiKey string, hc *http.Client) *Client`; `*Client` implements `providers.MediaServer` and `providers.Tester`; exported field `PageSize int` (items per request; 0 means 500).

How Jellyfin's API is used, all GET, authenticated with the header `Authorization: MediaBrowser Token="<api key>"`:

- `/Users` returns `[{"Id", "Name"}]`.
- `/Items?userId=U&IncludeItemTypes=T&Recursive=true&Fields=ProviderIds&EnableUserData=true&StartIndex=N&Limit=M` returns `{"Items": [...], "TotalRecordCount": n}`. It is called for the types `Movie`, `Series`, `Season` and `Episode`, once per user, page by page.
- Item fields read: `Id`, `Name`, `SeriesId`, `ParentIndexNumber` (season), `IndexNumber` (episode), `IndexNumberEnd` (last episode of a range), `ProviderIds` (keys `Tmdb`, `Tvdb`, `Imdb`, matched ignoring case), `UserData.Played`, `UserData.LastPlayedDate`, `UserData.IsFavorite`.
- `/System/Info` returns `{"ProductName", "Version"}` and is used for the connection test because it requires a valid key.

A season marked as a favourite counts as a favourite on its series. Episodes without a season or episode number cannot be matched and are skipped. A date that cannot be parsed means "unknown date", never a failed scan.

- [ ] **Step 1: Write the failing tests**

Create `internal/providers/jellyfin/jellyfin_test.go`:

```go
package jellyfin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

var ctx = context.Background()

// fakeJellyfin serves items by "userID/ItemType".
type fakeJellyfin struct {
	items    map[string][]string
	requests map[string]int // "userID/ItemType" -> number of /Items requests
}

func (f *fakeJellyfin) handler(t *testing.T) http.Handler {
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != `MediaBrowser Token="k"` {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/Users", auth(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"Id":"a","Name":"Alice"},{"Id":"b","Name":"Bob"}]`)
	}))
	mux.HandleFunc("/System/Info", auth(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ProductName":"Jellyfin Server","Version":"10.10.3"}`)
	}))
	mux.HandleFunc("/Items", auth(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("Recursive") != "true" || q.Get("Fields") != "ProviderIds" || q.Get("EnableUserData") != "true" {
			t.Errorf("unexpected /Items query: %v", q)
		}
		key := q.Get("userId") + "/" + q.Get("IncludeItemTypes")
		if f.requests == nil {
			f.requests = map[string]int{}
		}
		f.requests[key]++
		all := f.items[key]
		start, _ := strconv.Atoi(q.Get("StartIndex"))
		limit, _ := strconv.Atoi(q.Get("Limit"))
		var slice []string
		if start < len(all) {
			slice = all[start:min(start+limit, len(all))]
		}
		fmt.Fprintf(w, `{"Items":[%s],"TotalRecordCount":%d}`, strings.Join(slice, ","), len(all))
	}))
	return mux
}

func start(t *testing.T, f *fakeJellyfin, apiKey string) *Client {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	return New(srv.URL, apiKey, srv.Client())
}

func TestUsers(t *testing.T) {
	users, err := start(t, &fakeJellyfin{}, "k").Users(ctx)
	want := []domain.User{{ID: "a", Name: "Alice"}, {ID: "b", Name: "Bob"}}
	if err != nil || !reflect.DeepEqual(users, want) {
		t.Errorf("Users = %+v, %v", users, err)
	}
}

func TestAWrongKeyIsAnError(t *testing.T) {
	if _, err := start(t, &fakeJellyfin{}, "wrong").Users(ctx); err == nil {
		t.Error("want an error when the API key is rejected")
	}
}

func TestTestReportsTheVersion(t *testing.T) {
	detail, err := start(t, &fakeJellyfin{}, "k").Test(ctx)
	if err != nil || detail != "Jellyfin Server 10.10.3" {
		t.Errorf("Test = %q, %v", detail, err)
	}
}

func TestLibraryMergesUsers(t *testing.T) {
	f := &fakeJellyfin{items: map[string][]string{
		"a/Movie": {`{"Id":"m1","Name":"The Matrix","ProviderIds":{"Tmdb":"603","Imdb":"tt0133093"},
			"UserData":{"Played":true,"LastPlayedDate":"2026-03-01T20:00:00.0000000Z","IsFavorite":false}}`},
		"b/Movie": {`{"Id":"m1","Name":"The Matrix","ProviderIds":{"Tmdb":"603","Imdb":"tt0133093"},
			"UserData":{"Played":false,"IsFavorite":true}}`},
		"a/Series": {`{"Id":"s1","Name":"The Show","ProviderIds":{"tvdb":"100","IMDB":"tt100","Tmdb":"200"},"UserData":{"IsFavorite":false}}`},
		"b/Series": {`{"Id":"s1","Name":"The Show","ProviderIds":{"tvdb":"100","IMDB":"tt100","Tmdb":"200"},"UserData":{"IsFavorite":false}}`},
		"a/Season": {`{"Id":"se1","SeriesId":"s1","IndexNumber":1,"UserData":{"IsFavorite":true}}`},
		"b/Season": {`{"Id":"se1","SeriesId":"s1","IndexNumber":1,"UserData":{"IsFavorite":false}}`},
		"a/Episode": {
			`{"Id":"e1","SeriesId":"s1","ParentIndexNumber":1,"IndexNumber":1,"IndexNumberEnd":2,
			  "UserData":{"Played":true,"LastPlayedDate":"2026-03-02T20:00:00Z"}}`,
			`{"Id":"e9","SeriesId":"s1","UserData":{"Played":true}}`,
		},
		"b/Episode": {`{"Id":"e1","SeriesId":"s1","ParentIndexNumber":1,"IndexNumber":1,"IndexNumberEnd":2,"UserData":{"Played":false}}`},
	}}
	lib, err := start(t, f, "k").Library(ctx, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}

	if len(lib.Movies) != 1 {
		t.Fatalf("got %d movies, want 1", len(lib.Movies))
	}
	m := lib.Movies[0]
	if m.ID != "m1" || m.Title != "The Matrix" || m.IDs != (domain.ExternalIDs{TMDB: "603", IMDB: "tt0133093"}) {
		t.Errorf("movie = %+v", m)
	}
	alice, bob := m.States["a"], m.States["b"]
	wantPlayed := time.Date(2026, 3, 1, 20, 0, 0, 0, time.UTC)
	if !alice.Played || alice.LastPlayed == nil || !alice.LastPlayed.Equal(wantPlayed) || alice.Favorite {
		t.Errorf("Alice's state = %+v", alice)
	}
	if bob.Played || bob.LastPlayed != nil || !bob.Favorite {
		t.Errorf("Bob's state = %+v", bob)
	}

	if len(lib.Series) != 1 {
		t.Fatalf("got %d series, want 1", len(lib.Series))
	}
	s := lib.Series[0]
	if s.ID != "s1" || s.IDs != (domain.ExternalIDs{TVDB: "100", IMDB: "tt100", TMDB: "200"}) {
		t.Errorf("series = %+v; provider ID keys must be matched ignoring case", s)
	}
	if !reflect.DeepEqual(s.FavoriteOf, map[string]bool{"a": true}) {
		t.Errorf("FavoriteOf = %v, want Alice only: she marked season 1 a favourite", s.FavoriteOf)
	}

	if len(lib.Episodes) != 1 {
		t.Fatalf("got %d episodes, want 1: an episode without numbers is skipped", len(lib.Episodes))
	}
	e := lib.Episodes[0]
	if e.SeriesID != "s1" || e.Season != 1 || e.Episode != 1 || e.EpisodeEnd != 2 {
		t.Errorf("episode = %+v", e)
	}
	if !e.States["a"].Played || e.States["b"].Played {
		t.Errorf("episode states = %+v", e.States)
	}
}

func TestLibraryReadsEveryPage(t *testing.T) {
	var movies []string
	for i := 1; i <= 5; i++ {
		movies = append(movies, fmt.Sprintf(`{"Id":"m%d","Name":"Movie %d","UserData":{"Played":true}}`, i, i))
	}
	f := &fakeJellyfin{items: map[string][]string{"a/Movie": movies}}
	c := start(t, f, "k")
	c.PageSize = 2

	lib, err := c.Library(ctx, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Movies) != 5 {
		t.Errorf("got %d movies, want 5", len(lib.Movies))
	}
	if f.requests["a/Movie"] != 3 {
		t.Errorf("made %d movie requests, want 3 pages of 2", f.requests["a/Movie"])
	}
}

func TestLibraryToleratesBadDates(t *testing.T) {
	f := &fakeJellyfin{items: map[string][]string{"a/Movie": {
		`{"Id":"m1","Name":"Odd","UserData":{"Played":true,"LastPlayedDate":"not-a-date"}}`,
		`{"Id":"m2","Name":"Placeholder","UserData":{"Played":true,"LastPlayedDate":"0001-01-01T00:00:00.0000000Z"}}`,
		`{"Id":"m3","Name":"Missing","UserData":{"Played":true}}`,
	}}}
	lib, err := start(t, f, "k").Library(ctx, []string{"a"})
	if err != nil {
		t.Fatalf("a bad date must not fail the scan: %v", err)
	}
	if len(lib.Movies) != 3 {
		t.Fatalf("got %d movies, want 3", len(lib.Movies))
	}
	for _, m := range lib.Movies {
		if st := m.States["a"]; !st.Played || st.LastPlayed != nil {
			t.Errorf("%s: state = %+v, want played with an unknown date", m.Title, st)
		}
	}
}

func TestLibraryWithoutUsersAsksNothing(t *testing.T) {
	f := &fakeJellyfin{}
	lib, err := start(t, f, "k").Library(ctx, nil)
	if err != nil || len(lib.Movies)+len(lib.Series)+len(lib.Episodes) != 0 || len(f.requests) != 0 {
		t.Errorf("Library(nil) = %+v, %v with %d requests; want empty and no requests", lib, err, len(f.requests))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/providers/jellyfin/`
Expected: FAIL to compile with `undefined: New`, `undefined: Client`.

- [ ] **Step 3: Write the Jellyfin provider**

Create `internal/providers/jellyfin/jellyfin.go`:

```go
// Package jellyfin reads users and watch state from Jellyfin.
package jellyfin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/httpjson"
	"github.com/removarr/removarr/internal/providers"
)

var (
	_ providers.MediaServer = (*Client)(nil)
	_ providers.Tester      = (*Client)(nil)
)

const defaultPageSize = 500

// Client talks to one Jellyfin server.
type Client struct {
	baseURL  string
	apiKey   string
	http     *http.Client
	PageSize int // items per request; 0 means 500
}

// New returns a client. baseURL has no trailing slash.
func New(baseURL, apiKey string, hc *http.Client) *Client {
	return &Client{baseURL: baseURL, apiKey: apiKey, http: hc}
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	header := http.Header{"Authorization": {`MediaBrowser Token="` + c.apiKey + `"`}}
	return httpjson.Get(ctx, c.http, u, header, out)
}

// Users returns the server's user accounts.
func (c *Client) Users(ctx context.Context) ([]domain.User, error) {
	var raw []struct {
		ID   string `json:"Id"`
		Name string `json:"Name"`
	}
	if err := c.get(ctx, "/Users", nil, &raw); err != nil {
		return nil, fmt.Errorf("users: %w", err)
	}
	out := make([]domain.User, 0, len(raw))
	for _, u := range raw {
		out = append(out, domain.User{ID: u.ID, Name: u.Name})
	}
	return out, nil
}

// Test checks the connection and returns the server's name and version.
func (c *Client) Test(ctx context.Context) (string, error) {
	var raw struct {
		ProductName string `json:"ProductName"`
		Version     string `json:"Version"`
	}
	if err := c.get(ctx, "/System/Info", nil, &raw); err != nil {
		return "", err
	}
	if raw.ProductName == "" {
		raw.ProductName = "Jellyfin"
	}
	return strings.TrimSpace(raw.ProductName + " " + raw.Version), nil
}

type itemJSON struct {
	ID                string            `json:"Id"`
	Name              string            `json:"Name"`
	SeriesID          string            `json:"SeriesId"`
	ParentIndexNumber *int              `json:"ParentIndexNumber"`
	IndexNumber       *int              `json:"IndexNumber"`
	IndexNumberEnd    *int              `json:"IndexNumberEnd"`
	ProviderIDs       map[string]string `json:"ProviderIds"`
	UserData          struct {
		Played         bool   `json:"Played"`
		LastPlayedDate string `json:"LastPlayedDate"`
		IsFavorite     bool   `json:"IsFavorite"`
	} `json:"UserData"`
}

func (it itemJSON) state() domain.UserState {
	return domain.UserState{
		Played:     it.UserData.Played,
		LastPlayed: httpjson.ParseTime(it.UserData.LastPlayedDate),
		Favorite:   it.UserData.IsFavorite,
	}
}

func (it itemJSON) externalIDs() domain.ExternalIDs {
	var ids domain.ExternalIDs
	for key, value := range it.ProviderIDs {
		switch strings.ToLower(key) {
		case "tmdb":
			ids.TMDB = value
		case "tvdb":
			ids.TVDB = value
		case "imdb":
			ids.IMDB = value
		}
	}
	return ids
}

// items returns every item of one type as one user sees it, reading page by page.
func (c *Client) items(ctx context.Context, userID, itemType string) ([]itemJSON, error) {
	size := c.PageSize
	if size <= 0 {
		size = defaultPageSize
	}
	var all []itemJSON
	for start := 0; ; start += size {
		q := url.Values{
			"userId":           {userID},
			"IncludeItemTypes": {itemType},
			"Recursive":        {"true"},
			"Fields":           {"ProviderIds"},
			"EnableUserData":   {"true"},
			"StartIndex":       {strconv.Itoa(start)},
			"Limit":            {strconv.Itoa(size)},
		}
		var page struct {
			Items []itemJSON `json:"Items"`
			Total int        `json:"TotalRecordCount"`
		}
		if err := c.get(ctx, "/Items", q, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		switch {
		case len(page.Items) == 0:
			return all, nil
		case page.Total > 0 && len(all) >= page.Total:
			return all, nil
		case page.Total == 0 && len(page.Items) < size:
			// The server sent no total: a short page is the last one.
			return all, nil
		}
	}
}

// Library returns the watch state of every movie and episode for the given users.
func (c *Client) Library(ctx context.Context, userIDs []string) (domain.Library, error) {
	movies := map[string]*domain.ServerMovie{}
	series := map[string]*domain.ServerSeries{}
	episodes := map[string]*domain.ServerEpisode{}
	var movieOrder, seriesOrder, episodeOrder []string // first-seen order, for stable output

	for _, user := range userIDs {
		items, err := c.items(ctx, user, "Movie")
		if err != nil {
			return domain.Library{}, fmt.Errorf("movies: %w", err)
		}
		for _, it := range items {
			m := movies[it.ID]
			if m == nil {
				m = &domain.ServerMovie{ID: it.ID, Title: it.Name, IDs: it.externalIDs(), States: map[string]domain.UserState{}}
				movies[it.ID] = m
				movieOrder = append(movieOrder, it.ID)
			}
			m.States[user] = it.state()
		}

		if items, err = c.items(ctx, user, "Series"); err != nil {
			return domain.Library{}, fmt.Errorf("series: %w", err)
		}
		for _, it := range items {
			s := series[it.ID]
			if s == nil {
				s = &domain.ServerSeries{ID: it.ID, Title: it.Name, IDs: it.externalIDs(), FavoriteOf: map[string]bool{}}
				series[it.ID] = s
				seriesOrder = append(seriesOrder, it.ID)
			}
			if it.UserData.IsFavorite {
				s.FavoriteOf[user] = true
			}
		}

		// A favourite season counts as a favourite on its series.
		if items, err = c.items(ctx, user, "Season"); err != nil {
			return domain.Library{}, fmt.Errorf("seasons: %w", err)
		}
		for _, it := range items {
			if s := series[it.SeriesID]; s != nil && it.UserData.IsFavorite {
				s.FavoriteOf[user] = true
			}
		}

		if items, err = c.items(ctx, user, "Episode"); err != nil {
			return domain.Library{}, fmt.Errorf("episodes: %w", err)
		}
		for _, it := range items {
			if it.SeriesID == "" || it.ParentIndexNumber == nil || it.IndexNumber == nil {
				continue // cannot be matched to a managed episode
			}
			e := episodes[it.ID]
			if e == nil {
				e = &domain.ServerEpisode{
					SeriesID: it.SeriesID,
					Season:   *it.ParentIndexNumber,
					Episode:  *it.IndexNumber,
					States:   map[string]domain.UserState{},
				}
				if it.IndexNumberEnd != nil {
					e.EpisodeEnd = *it.IndexNumberEnd
				}
				episodes[it.ID] = e
				episodeOrder = append(episodeOrder, it.ID)
			}
			e.States[user] = it.state()
		}
	}

	var lib domain.Library
	for _, id := range movieOrder {
		lib.Movies = append(lib.Movies, *movies[id])
	}
	for _, id := range seriesOrder {
		lib.Series = append(lib.Series, *series[id])
	}
	for _, id := range episodeOrder {
		lib.Episodes = append(lib.Episodes, *episodes[id])
	}
	return lib, nil
}
```

- [ ] **Step 4: Run all tests to verify they pass**

Run: `go vet ./... && go test ./...`
Expected: `ok` for every package with tests, no vet output.

- [ ] **Step 5: Commit**

```bash
git add internal/providers/jellyfin
git commit -m "feat: Jellyfin provider"
```

---

### Task 11: Scanner service

**Files:**
- Create: `internal/scanner/scanner.go`
- Test: `internal/scanner/scanner_test.go`

**Interfaces:**
- Consumes:
  - Task 1: `domain` types. Task 5: `domain.ScanSummary`, `domain.ListSummary`.
  - Tasks 2 to 4: `scan.ManagerFiles`, `scan.NewLinkIndex`, `scan.ListResult`, `scan.Movies`, `scan.MoviesInput`, `scan.Series`, `scan.SeriesInput`, `scan.Torrents`, `scan.TorrentsInput`.
  - Task 7: `providers.MediaServer`, `providers.MovieManager`, `providers.SeriesManager`, `providers.DownloadClient`.
- Produces, in package `scanner`:
  - `Providers{MediaServer providers.MediaServer; Movies providers.MovieManager; Series providers.SeriesManager; Downloads providers.DownloadClient}` — a nil field means the app is not configured.
  - `Store` interface: `BeginScan(ctx context.Context, startedAt time.Time) (int64, error)` and `FinishScan(ctx context.Context, id int64, finishedAt time.Time, summary domain.ScanSummary, candidates []domain.Candidate) error`. `*store.Store` satisfies it.
  - `Service{Build func(context.Context) (Providers, error); Rules func(context.Context) (domain.Rules, error); Store Store; Now func() time.Time; Log *slog.Logger; Timeout time.Duration}`
  - `(*Service).Start() error` — starts a scan in the background; `ErrRunning` while one is in progress.
  - `(*Service).Run(ctx context.Context) error` — runs a scan and waits for it; `ErrRunning` while one is in progress.
  - `(*Service).Status() Status`, `Status{Running bool; Phase string; StartedAt *time.Time; LastError string}`
  - `ErrRunning`
  - `Source[T]{Configured bool; Err error; Value T}`, `Fetched{Users []domain.User; Library Source[domain.Library]; Movies Source[domain.MovieSnapshot]; Series Source[domain.SeriesSnapshot]; Downloads Source[[]domain.Download]}`, `Compute(rules domain.Rules, now time.Time, f Fetched) (domain.ScanSummary, []domain.Candidate)`.

Which list needs which app (spec 6.8, rule 5):

| List | Needs |
|---|---|
| Watched movies | movie manager, download client, media server, at least one counting user |
| Watched series | series manager, download client, media server, at least one counting user |
| Orphan torrents | download client, at least one configured media manager, and every configured media manager answering |

When a need is not met, the list's summary carries an error text and the list has no rows. The other lists are still produced. In stage 1 no ignores exist yet, so `scan.SeriesInput.Ignored` is left nil.

- [ ] **Step 1: Write the failing tests**

Create `internal/scanner/scanner_test.go`:

```go
package scanner

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

var (
	ctx = context.Background()
	now = time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
)

type fakeServer struct {
	lib   domain.Library
	err   error
	calls int
}

func (f *fakeServer) Users(context.Context) ([]domain.User, error) {
	return []domain.User{{ID: "a", Name: "Alice"}}, f.err
}

func (f *fakeServer) Library(context.Context, []string) (domain.Library, error) {
	f.calls++
	return f.lib, f.err
}

type fakeMovies struct {
	snap domain.MovieSnapshot
	err  error
}

func (f *fakeMovies) Movies(context.Context) (domain.MovieSnapshot, error) { return f.snap, f.err }

type fakeSeries struct {
	snap domain.SeriesSnapshot
	err  error
}

func (f *fakeSeries) Series(context.Context) (domain.SeriesSnapshot, error) { return f.snap, f.err }

type fakeDownloads struct {
	list    []domain.Download
	err     error
	entered chan struct{} // when set, receives once Downloads has been called
	release chan struct{} // when set, Downloads waits until it is closed
}

func (f *fakeDownloads) Downloads(context.Context) ([]domain.Download, error) {
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.release != nil {
		<-f.release
	}
	return f.list, f.err
}

type fakeStore struct {
	begun, finished int
	summary         domain.ScanSummary
	candidates      []domain.Candidate
}

func (f *fakeStore) BeginScan(context.Context, time.Time) (int64, error) {
	f.begun++
	return int64(f.begun), nil
}

func (f *fakeStore) FinishScan(_ context.Context, _ int64, _ time.Time, s domain.ScanSummary, c []domain.Candidate) error {
	f.finished++
	f.summary, f.candidates = s, c
	return nil
}

// world returns providers for a small library: one watched movie backed by torrent h1, one
// watched ended series, and one torrent no media manager knows.
func world() (Providers, *fakeServer) {
	seen := map[string]domain.UserState{"a": {Played: true}}
	server := &fakeServer{lib: domain.Library{
		Movies:   []domain.ServerMovie{{ID: "m1", IDs: domain.ExternalIDs{TMDB: "603"}, States: seen}},
		Series:   []domain.ServerSeries{{ID: "s1", IDs: domain.ExternalIDs{TVDB: "100"}}},
		Episodes: []domain.ServerEpisode{{SeriesID: "s1", Season: 1, Episode: 1, States: seen}},
	}}
	movies := &fakeMovies{snap: domain.MovieSnapshot{
		Movies: []domain.Movie{{
			ID: 1, Title: "The Matrix", IDs: domain.ExternalIDs{TMDB: "603"},
			File: &domain.MediaFile{ID: 10, Size: 5000},
		}},
		Links: domain.Links{
			Grabbed:    map[string][]int{"h1": {1}},
			FileSource: map[int]string{10: "h1"},
			Queue:      map[string]bool{},
		},
		RootFolders: []domain.RootFolder{{Path: "/media/movies", FreeBytes: 100}},
	}}
	series := &fakeSeries{snap: domain.SeriesSnapshot{
		Series: []domain.Series{{
			ID: 1, Title: "The Show", Ended: true, IDs: domain.ExternalIDs{TVDB: "100"},
			Episodes: []domain.Episode{{ID: 11, Season: 1, Number: 1, File: &domain.MediaFile{ID: 11, Size: 900}}},
		}},
		Links:       domain.Links{Grabbed: map[string][]int{}, FileSource: map[int]string{}, Queue: map[string]bool{}},
		RootFolders: []domain.RootFolder{{Path: "/media/tv", FreeBytes: 200}, {Path: "/media/movies", FreeBytes: 100}},
	}}
	downloads := &fakeDownloads{list: []domain.Download{{ID: "h1", Name: "The.Matrix"}, {ID: "orphan1", Name: "Unknown"}}}
	return Providers{MediaServer: server, Movies: movies, Series: series, Downloads: downloads}, server
}

func withUser() domain.Rules {
	r := domain.DefaultRules()
	r.CountingUsers = []string{"a"}
	return r
}

func service(p Providers, r domain.Rules, st *fakeStore) *Service {
	return &Service{
		Build: func(context.Context) (Providers, error) { return p, nil },
		Rules: func(context.Context) (domain.Rules, error) { return r, nil },
		Store: st,
		Now:   func() time.Time { return now },
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// run performs one scan and returns what was stored.
func run(t *testing.T, p Providers, r domain.Rules) *fakeStore {
	t.Helper()
	st := &fakeStore{}
	if err := service(p, r, st).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if st.begun != 1 || st.finished != 1 {
		t.Fatalf("scan recorded %d starts and %d finishes, want 1 and 1", st.begun, st.finished)
	}
	return st
}

// expect checks one list's summary: its row count when it was produced, or that its error
// mentions wantError when it was not.
func expect(t *testing.T, st *fakeStore, list domain.ListKind, rows int, wantError string) {
	t.Helper()
	got := st.summary.Lists[list]
	if wantError == "" {
		if got.Error != "" || got.Candidates != rows {
			t.Errorf("%s: %d rows, error %q; want %d rows and no error", list, got.Candidates, got.Error, rows)
		}
		return
	}
	if !strings.Contains(got.Error, wantError) || got.Candidates != 0 {
		t.Errorf("%s: %d rows, error %q; want no rows and an error mentioning %q", list, got.Candidates, got.Error, wantError)
	}
	for _, c := range st.candidates {
		if c.List == list {
			t.Errorf("%s: row %s stored although the list failed", list, c.Key)
		}
	}
}

func TestRunProducesAllThreeLists(t *testing.T) {
	p, _ := world()
	st := run(t, p, withUser())

	expect(t, st, domain.ListMovies, 1, "")
	expect(t, st, domain.ListSeries, 1, "")
	expect(t, st, domain.ListTorrents, 1, "")

	keys := map[string]bool{}
	for _, c := range st.candidates {
		keys[c.Key] = true
	}
	for _, want := range []string{"tmdb:603", "tvdb:100", "hash:orphan1"} {
		if !keys[want] {
			t.Errorf("row %s missing; stored rows: %v", want, keys)
		}
	}
	if len(st.candidates) != 3 {
		t.Errorf("%d rows stored, want 3", len(st.candidates))
	}
	if len(st.summary.RootFolders) != 2 {
		t.Errorf("RootFolders = %+v, want /media/movies and /media/tv once each", st.summary.RootFolders)
	}
}

func TestRunWithoutCountingUsers(t *testing.T) {
	p, server := world()
	st := run(t, p, domain.DefaultRules())

	expect(t, st, domain.ListMovies, 0, "No counting users")
	expect(t, st, domain.ListSeries, 0, "No counting users")
	expect(t, st, domain.ListTorrents, 1, "")
	if server.calls != 0 {
		t.Errorf("the media server library was read %d times, want 0", server.calls)
	}
}

func TestRunWhenAMediaManagerFails(t *testing.T) {
	p, _ := world()
	p.Movies = &fakeMovies{err: errors.New("timeout")}
	st := run(t, p, withUser())

	expect(t, st, domain.ListMovies, 0, "movie manager could not be read: timeout")
	expect(t, st, domain.ListSeries, 1, "")
	// h1 belongs to a movie. With the movie manager down it would look like an orphan.
	expect(t, st, domain.ListTorrents, 0, "movie manager could not be read")
}

func TestRunWithOnlyOneMediaManagerConfigured(t *testing.T) {
	p, _ := world()
	p.Movies = nil
	st := run(t, p, withUser())

	expect(t, st, domain.ListMovies, 0, "movie manager is not configured")
	expect(t, st, domain.ListSeries, 1, "")
	expect(t, st, domain.ListTorrents, 2, "") // nothing manages h1 any more
}

func TestRunWithoutAnyMediaManager(t *testing.T) {
	p, _ := world()
	p.Movies, p.Series = nil, nil
	st := run(t, p, withUser())

	expect(t, st, domain.ListTorrents, 0, "No media manager is configured")
}

func TestRunWhenTheDownloadClientFails(t *testing.T) {
	p, _ := world()
	p.Downloads = &fakeDownloads{err: errors.New("connection refused")}
	st := run(t, p, withUser())

	for _, list := range []domain.ListKind{domain.ListMovies, domain.ListSeries, domain.ListTorrents} {
		expect(t, st, list, 0, "download client could not be read: connection refused")
	}
}

func TestRunWithoutAMediaServer(t *testing.T) {
	p, _ := world()
	p.MediaServer = nil
	st := run(t, p, withUser())

	expect(t, st, domain.ListMovies, 0, "media server is not configured")
	expect(t, st, domain.ListSeries, 0, "media server is not configured")
	expect(t, st, domain.ListTorrents, 1, "")
}

func TestRunWhenTheMediaServerFails(t *testing.T) {
	p, server := world()
	server.err = errors.New("401 Unauthorized")
	st := run(t, p, withUser())

	expect(t, st, domain.ListMovies, 0, "media server could not be read: 401 Unauthorized")
	expect(t, st, domain.ListTorrents, 1, "")
}

func waitIdle(t *testing.T, s *Service) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for s.Status().Running {
		if time.Now().After(deadline) {
			t.Fatal("the scan did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestOnlyOneScanAtATime(t *testing.T) {
	p, _ := world()
	dl := &fakeDownloads{entered: make(chan struct{}), release: make(chan struct{})}
	p.Downloads = dl
	st := &fakeStore{}
	svc := service(p, withUser(), st)

	if err := svc.Start(); err != nil {
		t.Fatal(err)
	}
	<-dl.entered // the scan is now waiting inside the download client

	if status := svc.Status(); !status.Running || status.Phase == "" || status.StartedAt == nil {
		t.Errorf("status during a scan = %+v", status)
	}
	if err := svc.Start(); !errors.Is(err, ErrRunning) {
		t.Errorf("second Start = %v, want ErrRunning", err)
	}
	if err := svc.Run(ctx); !errors.Is(err, ErrRunning) {
		t.Errorf("Run during a scan = %v, want ErrRunning", err)
	}

	close(dl.release)
	waitIdle(t, svc)
	if st.finished != 1 {
		t.Errorf("%d scans finished, want 1", st.finished)
	}
	if status := svc.Status(); status.Running || status.LastError != "" {
		t.Errorf("status after a scan = %+v", status)
	}

	dl.entered, dl.release = nil, nil
	if err := svc.Run(ctx); err != nil {
		t.Errorf("a new scan after the first finished: %v", err)
	}
}

func TestAScanThatCannotStartIsReported(t *testing.T) {
	p, _ := world()
	st := &fakeStore{}
	svc := service(p, withUser(), st)
	svc.Rules = func(context.Context) (domain.Rules, error) { return domain.Rules{}, errors.New("database is locked") }

	if err := svc.Run(ctx); err == nil {
		t.Fatal("want an error")
	}
	if st.begun != 0 {
		t.Error("no scan must be recorded when the rules cannot be loaded")
	}
	status := svc.Status()
	if status.Running || !strings.Contains(status.LastError, "database is locked") {
		t.Errorf("status = %+v, want idle with the error kept", status)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/scanner/`
Expected: FAIL to compile with `undefined: Providers`, `undefined: Service`, `undefined: ErrRunning`.

- [ ] **Step 3: Write the scanner**

Create `internal/scanner/scanner.go`:

```go
// Package scanner runs one scan: it reads every configured app, applies the rules and
// stores the result.
package scanner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/providers"
	"github.com/removarr/removarr/internal/scan"
)

// Providers are the apps a scan reads. A nil field means the app is not configured.
type Providers struct {
	MediaServer providers.MediaServer
	Movies      providers.MovieManager
	Series      providers.SeriesManager
	Downloads   providers.DownloadClient
}

// Store is the persistence a scan needs. *store.Store satisfies it.
type Store interface {
	BeginScan(ctx context.Context, startedAt time.Time) (int64, error)
	FinishScan(ctx context.Context, id int64, finishedAt time.Time, summary domain.ScanSummary, candidates []domain.Candidate) error
}

// ErrRunning is returned while a scan is in progress.
var ErrRunning = errors.New("a scan is already running")

const defaultTimeout = 30 * time.Minute

// Status describes the scan in progress, if any.
type Status struct {
	Running   bool       `json:"running"`
	Phase     string     `json:"phase,omitempty"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	LastError string     `json:"lastError,omitempty"` // why the most recent scan could not be completed
}

// Service runs scans, one at a time.
type Service struct {
	Build   func(ctx context.Context) (Providers, error)    // builds providers from the current settings
	Rules   func(ctx context.Context) (domain.Rules, error) // loads the current rules
	Store   Store
	Now     func() time.Time
	Log     *slog.Logger
	Timeout time.Duration // limit for one background scan; 0 means 30 minutes

	mu     sync.Mutex
	status Status
}

// Start begins a scan in the background. It returns ErrRunning when one is in progress.
func (s *Service) Start() error {
	if err := s.begin(); err != nil {
		return err
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		s.finish(s.run(ctx))
	}()
	return nil
}

// Run performs a scan and waits for it. It returns ErrRunning when one is in progress.
func (s *Service) Run(ctx context.Context) error {
	if err := s.begin(); err != nil {
		return err
	}
	err := s.run(ctx)
	s.finish(err)
	return err
}

// Status returns what the scanner is doing.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Service) begin() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.Running {
		return ErrRunning
	}
	started := s.Now()
	s.status = Status{Running: true, Phase: "starting", StartedAt: &started}
	return nil
}

func (s *Service) finish(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = Status{}
	if err != nil {
		s.status.LastError = err.Error()
		s.Log.Error("scan failed", "error", err)
	}
}

func (s *Service) setPhase(phase string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Phase = phase
}

func (s *Service) run(ctx context.Context) error {
	started := s.Now()
	rules, err := s.Rules(ctx)
	if err != nil {
		return fmt.Errorf("load rules: %w", err)
	}
	p, err := s.Build(ctx)
	if err != nil {
		return fmt.Errorf("load connections: %w", err)
	}
	id, err := s.Store.BeginScan(ctx, started)
	if err != nil {
		return fmt.Errorf("record scan: %w", err)
	}

	fetched := s.fetch(ctx, p, rules)
	s.setPhase("applying the rules")
	summary, candidates := Compute(rules, s.Now(), fetched)
	s.setPhase("saving")
	if err := s.Store.FinishScan(ctx, id, s.Now(), summary, candidates); err != nil {
		return fmt.Errorf("save scan: %w", err)
	}
	s.Log.Info("scan finished",
		"movies", summary.Lists[domain.ListMovies].Candidates,
		"series", summary.Lists[domain.ListSeries].Candidates,
		"torrents", summary.Lists[domain.ListTorrents].Candidates)
	return nil
}

// Source is what a scan got from one app.
type Source[T any] struct {
	Configured bool
	Err        error
	Value      T
}

func (s Source[T]) ok() bool { return s.Configured && s.Err == nil }

// problem explains why the app's data cannot be used, or returns "" when it can.
func (s Source[T]) problem(role string) string {
	switch {
	case !s.Configured:
		return "The " + role + " is not configured."
	case s.Err != nil:
		return "The " + role + " could not be read: " + s.Err.Error()
	default:
		return ""
	}
}

// Fetched is everything one scan read from the apps.
type Fetched struct {
	Users     []domain.User
	Library   Source[domain.Library]
	Movies    Source[domain.MovieSnapshot]
	Series    Source[domain.SeriesSnapshot]
	Downloads Source[[]domain.Download]
}

// fetch reads the apps one after another. The media server's library is read only when
// counting users are selected, because without them no watched list can be produced.
func (s *Service) fetch(ctx context.Context, p Providers, rules domain.Rules) Fetched {
	var f Fetched
	if p.Downloads != nil {
		s.setPhase("reading the download client")
		f.Downloads.Configured = true
		f.Downloads.Value, f.Downloads.Err = p.Downloads.Downloads(ctx)
	}
	if p.Movies != nil {
		s.setPhase("reading the movie manager")
		f.Movies.Configured = true
		f.Movies.Value, f.Movies.Err = p.Movies.Movies(ctx)
	}
	if p.Series != nil {
		s.setPhase("reading the series manager")
		f.Series.Configured = true
		f.Series.Value, f.Series.Err = p.Series.Series(ctx)
	}
	if p.MediaServer != nil {
		f.Library.Configured = true
		if len(rules.CountingUsers) > 0 {
			s.setPhase("reading the media server")
			f.Users, f.Library.Err = p.MediaServer.Users(ctx)
			if f.Library.Err == nil {
				f.Library.Value, f.Library.Err = p.MediaServer.Library(ctx, rules.CountingUsers)
			}
		}
	}
	return f
}

const (
	roleMediaServer = "media server"
	roleMovies      = "movie manager"
	roleSeries      = "series manager"
	roleDownloads   = "download client"
)

// Compute applies the rules to what was fetched. A list is produced only when everything
// it depends on was read completely (spec 6.8, rule 5); otherwise its summary carries the
// reason and it has no rows.
func Compute(rules domain.Rules, now time.Time, f Fetched) (domain.ScanSummary, []domain.Candidate) {
	summary := domain.ScanSummary{Lists: map[domain.ListKind]domain.ListSummary{}, RootFolders: []domain.RootFolder{}}
	candidates := []domain.Candidate{}

	var managers []scan.ManagerFiles
	seenFolder := map[string]bool{}
	addFolders := func(folders []domain.RootFolder) {
		for _, folder := range folders {
			if !seenFolder[folder.Path] {
				seenFolder[folder.Path] = true
				summary.RootFolders = append(summary.RootFolders, folder)
			}
		}
	}
	if f.Movies.ok() {
		var fileIDs []int
		for _, m := range f.Movies.Value.Movies {
			if m.File != nil {
				fileIDs = append(fileIDs, m.File.ID)
			}
		}
		managers = append(managers, scan.ManagerFiles{Links: f.Movies.Value.Links, FileIDs: fileIDs})
		addFolders(f.Movies.Value.RootFolders)
	}
	if f.Series.ok() {
		var fileIDs []int
		for _, s := range f.Series.Value.Series {
			for _, e := range s.Episodes {
				if e.File != nil {
					fileIDs = append(fileIDs, e.File.ID)
				}
			}
		}
		managers = append(managers, scan.ManagerFiles{Links: f.Series.Value.Links, FileIDs: fileIDs})
		addFolders(f.Series.Value.RootFolders)
	}
	index := scan.NewLinkIndex(managers...)

	add := func(list domain.ListKind, problem string, compute func() scan.ListResult) {
		if problem != "" {
			summary.Lists[list] = domain.ListSummary{Error: problem}
			return
		}
		res := compute()
		summary.Lists[list] = domain.ListSummary{Candidates: len(res.Candidates), Protected: res.Protected, Unmatched: res.Unmatched}
		candidates = append(candidates, res.Candidates...)
	}

	noUsers := ""
	if len(rules.CountingUsers) == 0 {
		noUsers = "No counting users are selected."
	}
	watchedProblem := func(manager string) string {
		return first(manager, f.Downloads.problem(roleDownloads), f.Library.problem(roleMediaServer), noUsers)
	}

	add(domain.ListMovies, watchedProblem(f.Movies.problem(roleMovies)), func() scan.ListResult {
		return scan.Movies(scan.MoviesInput{
			Rules: rules, Now: now, Users: f.Users, Library: f.Library.Value,
			Snapshot: f.Movies.Value, Downloads: f.Downloads.Value, Index: index,
		})
	})
	add(domain.ListSeries, watchedProblem(f.Series.problem(roleSeries)), func() scan.ListResult {
		return scan.Series(scan.SeriesInput{
			Rules: rules, Now: now, Users: f.Users, Library: f.Library.Value,
			Snapshot: f.Series.Value, Downloads: f.Downloads.Value, Index: index,
		})
	})
	add(domain.ListTorrents, torrentsProblem(f), func() scan.ListResult {
		return scan.Torrents(scan.TorrentsInput{Rules: rules, Downloads: f.Downloads.Value, Index: index})
	})
	return summary, candidates
}

// torrentsProblem explains why the orphan torrent list cannot be produced, or returns "".
// A configured media manager that could not be read blocks the list, because all of its
// torrents would look like orphans.
func torrentsProblem(f Fetched) string {
	if p := f.Downloads.problem(roleDownloads); p != "" {
		return p
	}
	if !f.Movies.Configured && !f.Series.Configured {
		return "No media manager is configured."
	}
	if f.Movies.Configured && f.Movies.Err != nil {
		return f.Movies.problem(roleMovies)
	}
	if f.Series.Configured && f.Series.Err != nil {
		return f.Series.problem(roleSeries)
	}
	return ""
}

// first returns the first non-empty text.
func first(texts ...string) string {
	for _, t := range texts {
		if t != "" {
			return t
		}
	}
	return ""
}
```

- [ ] **Step 4: Run all tests to verify they pass**

Run: `go vet ./... && go test ./...`
Expected: `ok` for every package with tests, no vet output.

- [ ] **Step 5: Commit**

```bash
git add internal/scanner
git commit -m "feat: scanner service that reads the apps, applies the rules and stores the result"
```

---

### Task 12: Provider wiring and the settings API

**Files:**
- Create: `internal/app/apps.go`, `internal/api/server.go`, `internal/api/settings.go`
- Test: `internal/app/apps_test.go`, `internal/api/server_test.go`, `internal/api/settings_test.go`

**Interfaces:**
- Consumes:
  - Task 1: `domain.Rules`, `domain.FieldError`, `domain.User`, `domain.Candidate`, `domain.ListKind`.
  - Task 5: `store.Open`, `store.ScanRecord`, `(*store.Store).LatestScan`, `(*store.Store).Candidates`.
  - Task 6: `config.Service` and its methods, `config.Env`, `config.Apps`, `config.FieldFor`, `config.CheckConn`, `config.FieldView`, the `config.App…` constants.
  - Task 7: `providers.Tester`, `providers.MediaServer`, `providers.ErrNotConfigured`.
  - Tasks 8 to 10: `radarr.New`, `sonarr.New`, `qbittorrent.New`, `jellyfin.New`.
  - Task 11: `scanner.Providers`, `scanner.Status`, `scanner.ErrRunning`.
- Produces:
  - Package `app`: `Apps{Config *config.Service; Timeout time.Duration}` with `(*Apps).Providers(ctx context.Context) (scanner.Providers, error)`, `(*Apps).Tester(ctx context.Context, app string) (providers.Tester, error)`, `(*Apps).MediaServer(ctx context.Context) (providers.MediaServer, error)`. The last two return `providers.ErrNotConfigured` when the app's settings are incomplete.
  - Package `api`:
    - `RequestHeader = "X-Removarr-Request"`
    - Interfaces `Scanner{Start() error; Status() scanner.Status}`, `Store{LatestScan(ctx) (*store.ScanRecord, error); Candidates(ctx, scanID int64, list domain.ListKind) ([]domain.Candidate, error)}`, `Apps{Tester(ctx, app string) (providers.Tester, error); MediaServer(ctx) (providers.MediaServer, error)}`
    - `Server{Config *config.Service; Store Store; Scanner Scanner; Apps Apps; Log *slog.Logger}`, `(*Server).Handler() http.Handler`
    - Unexported helpers used by Task 13: `writeJSON(w http.ResponseWriter, status int, v any)`, `writeError(w http.ResponseWriter, status int, message string)`, `(*Server).fail(w http.ResponseWriter, err error)`, `errorBody{Error, Field string}`.
    - Test helpers in `server_test.go`, used by Task 13's tests: `harness`, `newHarness(t *testing.T, envConn map[string]string) *harness`, `(*harness).do(method, path, body string, mods ...func(*http.Request)) *httptest.ResponseRecorder`, `decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T`, `fakeScanner`, `fakeApps`, `testerFunc`.

Endpoints in this task (spec 7.6):

| Method and path | Success | Failure |
|---|---|---|
| `GET /api/health` | 200 `{"status":"ok"}` | |
| `GET /api/settings` | 200 `{"rules": {...}, "connections": {app: {field: {"value","set","source"}}}, "dryRun": bool}` | |
| `PUT /api/settings` | 200, same body as GET | 400 `{"error","field"}`; nothing is saved |
| `POST /api/connections/{app}/test` | 200 `{"ok":true,"detail":"Sonarr 4.0.9"}` or `{"ok":false,"error":"..."}` | 404 for an unknown app |
| `GET /api/media-server/users` | 200 `[{"id","name"}]` | 409 when not configured, 502 when it cannot be read |

`PUT /api/settings` takes `{"rules": {...}, "connections": {"sonarr": {"url": "...", "apiKey": "..."}}}`. Both parts are optional. `rules` is merged over the saved rules, so a partial object changes only the fields it names. In `connections`, only the fields present change; an empty string or `null` clears the saved value so the environment variable applies again. Everything is validated before anything is saved.

The request guard (spec section 10) applies to every request that is not GET, HEAD or OPTIONS: it must carry the header `X-Removarr-Request`, have the content type `application/json`, and, when it has an `Origin` header, that origin's host must equal the request's `Host`. A browser will not let another website send such a request without removarr's permission, and removarr never grants it.

- [ ] **Step 1: Write the failing test for the provider wiring**

Create `internal/app/apps_test.go`:

```go
package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/providers"
)

var ctx = context.Background()

type memStore map[string]string

func (m memStore) GetSetting(_ context.Context, k string) (string, bool, error) {
	v, ok := m[k]
	return v, ok, nil
}
func (m memStore) SetSetting(_ context.Context, k, v string) error { m[k] = v; return nil }
func (m memStore) DeleteSetting(_ context.Context, k string) error { delete(m, k); return nil }

func apps(conn map[string]string) *Apps {
	env := config.Env{Port: 7272, ConfigDir: "/config", Conn: conn}
	return &Apps{Config: config.NewService(env, memStore{}), Timeout: time.Second}
}

func TestProvidersAreNilUntilConfigured(t *testing.T) {
	p, err := apps(map[string]string{}).Providers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.MediaServer != nil || p.Movies != nil || p.Series != nil || p.Downloads != nil {
		t.Errorf("nothing is configured, every provider must be nil: %+v", p)
	}
}

func TestProvidersForConfiguredApps(t *testing.T) {
	p, err := apps(map[string]string{
		"radarr.url":      "http://radarr:7878",
		"radarr.api_key":  "rk",
		"qbittorrent.url": "http://qbit:8080",
		"sonarr.url":      "http://sonarr:8989", // no API key: not configured
	}).Providers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Movies == nil || p.Downloads == nil {
		t.Errorf("radarr and qbittorrent are configured: %+v", p)
	}
	if p.Series != nil || p.MediaServer != nil {
		t.Errorf("sonarr and jellyfin are not configured: %+v", p)
	}
}

func TestTesterAndMediaServer(t *testing.T) {
	a := apps(map[string]string{
		"jellyfin.url":     "http://jellyfin:8096",
		"jellyfin.api_key": "jk",
		"qbittorrent.url":  "http://qbit:8080",
	})
	for _, name := range []string{config.AppJellyfin, config.AppQBittorrent} {
		if tester, err := a.Tester(ctx, name); err != nil || tester == nil {
			t.Errorf("Tester(%s) = %v, %v; want a tester", name, tester, err)
		}
	}
	for _, name := range []string{config.AppSonarr, config.AppRadarr, "plex"} {
		if _, err := a.Tester(ctx, name); !errors.Is(err, providers.ErrNotConfigured) {
			t.Errorf("Tester(%s) error = %v, want ErrNotConfigured", name, err)
		}
	}
	if ms, err := a.MediaServer(ctx); err != nil || ms == nil {
		t.Errorf("MediaServer = %v, %v", ms, err)
	}
	if _, err := apps(map[string]string{}).MediaServer(ctx); !errors.Is(err, providers.ErrNotConfigured) {
		t.Errorf("MediaServer without settings: error = %v, want ErrNotConfigured", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/app/`
Expected: FAIL to compile with `undefined: Apps`.

- [ ] **Step 3: Write the provider wiring**

Create `internal/app/apps.go`:

```go
// Package app builds provider clients from the current settings.
package app

import (
	"context"
	"net/http"
	"time"

	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/providers"
	"github.com/removarr/removarr/internal/providers/jellyfin"
	"github.com/removarr/removarr/internal/providers/qbittorrent"
	"github.com/removarr/removarr/internal/providers/radarr"
	"github.com/removarr/removarr/internal/providers/sonarr"
	"github.com/removarr/removarr/internal/scanner"
)

// Apps builds clients for the apps removarr talks to. Settings are read on every call, so
// a change made through the API takes effect on the next scan.
type Apps struct {
	Config  *config.Service
	Timeout time.Duration // limit for one request to an app
}

func (a *Apps) client() *http.Client { return &http.Client{Timeout: a.Timeout} }

// Providers returns a client for every configured app. The others stay nil.
func (a *Apps) Providers(ctx context.Context) (scanner.Providers, error) {
	var p scanner.Providers

	c, ok, err := a.Config.App(ctx, config.AppJellyfin)
	if err != nil {
		return scanner.Providers{}, err
	}
	if ok {
		p.MediaServer = jellyfin.New(c.URL, c.APIKey, a.client())
	}

	if c, ok, err = a.Config.App(ctx, config.AppRadarr); err != nil {
		return scanner.Providers{}, err
	}
	if ok {
		p.Movies = radarr.New(c.URL, c.APIKey, a.client())
	}

	if c, ok, err = a.Config.App(ctx, config.AppSonarr); err != nil {
		return scanner.Providers{}, err
	}
	if ok {
		p.Series = sonarr.New(c.URL, c.APIKey, a.client())
	}

	if c, ok, err = a.Config.App(ctx, config.AppQBittorrent); err != nil {
		return scanner.Providers{}, err
	}
	if ok {
		client, err := qbittorrent.New(c.URL, c.Username, c.Password, a.Timeout)
		if err != nil {
			return scanner.Providers{}, err
		}
		p.Downloads = client
	}
	return p, nil
}

// Tester returns the connection tester of one app. It returns providers.ErrNotConfigured
// when the app is unknown or its settings are incomplete.
func (a *Apps) Tester(ctx context.Context, app string) (providers.Tester, error) {
	c, ok, err := a.Config.App(ctx, app)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, providers.ErrNotConfigured
	}
	switch app {
	case config.AppJellyfin:
		return jellyfin.New(c.URL, c.APIKey, a.client()), nil
	case config.AppRadarr:
		return radarr.New(c.URL, c.APIKey, a.client()), nil
	case config.AppSonarr:
		return sonarr.New(c.URL, c.APIKey, a.client()), nil
	case config.AppQBittorrent:
		client, err := qbittorrent.New(c.URL, c.Username, c.Password, a.Timeout)
		if err != nil {
			return nil, err
		}
		return client, nil
	default:
		return nil, providers.ErrNotConfigured
	}
}

// MediaServer returns the media server client, or providers.ErrNotConfigured.
func (a *Apps) MediaServer(ctx context.Context) (providers.MediaServer, error) {
	c, ok, err := a.Config.App(ctx, config.AppJellyfin)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, providers.ErrNotConfigured
	}
	return jellyfin.New(c.URL, c.APIKey, a.client()), nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/app/`
Expected: `ok  	github.com/removarr/removarr/internal/app`

- [ ] **Step 5: Write the failing tests for the server, the guard and health**

Create `internal/api/server_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/providers"
	"github.com/removarr/removarr/internal/scanner"
	"github.com/removarr/removarr/internal/store"
)

type fakeScanner struct {
	status  scanner.Status
	err     error
	started int
}

func (f *fakeScanner) Start() error {
	if f.err != nil {
		return f.err
	}
	f.started++
	return nil
}

func (f *fakeScanner) Status() scanner.Status { return f.status }

type testerFunc func(context.Context) (string, error)

func (f testerFunc) Test(ctx context.Context) (string, error) { return f(ctx) }

type fakeApps struct {
	testers map[string]providers.Tester
	server  providers.MediaServer
}

func (f *fakeApps) Tester(_ context.Context, app string) (providers.Tester, error) {
	if t, ok := f.testers[app]; ok {
		return t, nil
	}
	return nil, providers.ErrNotConfigured
}

func (f *fakeApps) MediaServer(context.Context) (providers.MediaServer, error) {
	if f.server == nil {
		return nil, providers.ErrNotConfigured
	}
	return f.server, nil
}

// harness is a Server over a real store in a temporary directory, with fake apps and a
// fake scanner.
type harness struct {
	t       *testing.T
	store   *store.Store
	config  *config.Service
	scanner *fakeScanner
	apps    *fakeApps
	handler http.Handler
}

func newHarness(t *testing.T, envConn map[string]string) *harness {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "removarr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if envConn == nil {
		envConn = map[string]string{}
	}
	h := &harness{
		t:       t,
		store:   st,
		config:  config.NewService(config.Env{Port: 7272, ConfigDir: "/config", DryRun: true, Conn: envConn}, st),
		scanner: &fakeScanner{},
		apps:    &fakeApps{testers: map[string]providers.Tester{}},
	}
	srv := &Server{
		Config:  h.config,
		Store:   st,
		Scanner: h.scanner,
		Apps:    h.apps,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	h.handler = srv.Handler()
	return h
}

// do sends a request. Requests other than GET get the headers the guard asks for; mods can
// change or remove them.
func (h *harness) do(method, path, body string, mods ...func(*http.Request)) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "removarr.local:7272"
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(RequestHeader, "1")
	}
	for _, m := range mods {
		m(req)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("response is not the expected JSON: %v\n%s", err, rec.Body.String())
	}
	return v
}

func TestHealth(t *testing.T) {
	rec := newHarness(t, nil).do(http.MethodGet, "/api/health", "")
	if rec.Code != http.StatusOK || decode[map[string]string](t, rec)["status"] != "ok" {
		t.Errorf("health = %d %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestUnknownPathsAreNotFound(t *testing.T) {
	if rec := newHarness(t, nil).do(http.MethodGet, "/api/nope", ""); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestGuard(t *testing.T) {
	tests := []struct {
		name string
		mod  func(*http.Request)
		want int
	}{
		{"all headers present", func(*http.Request) {}, http.StatusOK},
		{"no request header", func(r *http.Request) { r.Header.Del(RequestHeader) }, http.StatusForbidden},
		{"form content type", func(r *http.Request) {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}, http.StatusUnsupportedMediaType},
		{"json with a charset", func(r *http.Request) {
			r.Header.Set("Content-Type", "application/json; charset=utf-8")
		}, http.StatusOK},
		{"origin of another site", func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }, http.StatusForbidden},
		{"origin of this server", func(r *http.Request) { r.Header.Set("Origin", "http://removarr.local:7272") }, http.StatusOK},
		{"origin that is not a URL", func(r *http.Request) { r.Header.Set("Origin", "null") }, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := newHarness(t, nil).do(http.MethodPut, "/api/settings", `{}`, tt.mod)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

func TestGuardLeavesReadsAlone(t *testing.T) {
	rec := newHarness(t, nil).do(http.MethodGet, "/api/settings", "", func(r *http.Request) {
		r.Header.Set("Origin", "http://evil.example")
	})
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: reads are not guarded", rec.Code)
	}
}
```

- [ ] **Step 6: Write the failing tests for the settings endpoints**

Create `internal/api/settings_test.go`:

```go
package api

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/providers"
)

type settingsBody struct {
	Rules       domain.Rules                           `json:"rules"`
	Connections map[string]map[string]config.FieldView `json:"connections"`
	DryRun      bool                                   `json:"dryRun"`
}

func TestGetSettings(t *testing.T) {
	h := newHarness(t, map[string]string{"sonarr.url": "http://sonarr:8989", "sonarr.api_key": "env-secret"})
	rec := h.do(http.MethodGet, "/api/settings", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	got := decode[settingsBody](t, rec)
	if !reflect.DeepEqual(got.Rules, domain.DefaultRules()) {
		t.Errorf("rules = %+v, want the defaults", got.Rules)
	}
	if !got.DryRun {
		t.Error("dryRun must reflect the environment")
	}
	if len(got.Connections) != 4 {
		t.Errorf("connections for %d apps, want 4", len(got.Connections))
	}
	if v := got.Connections["sonarr"]["url"]; v.Value != "http://sonarr:8989" || v.Source != config.SourceEnv {
		t.Errorf("sonarr url = %+v", v)
	}
	if v := got.Connections["sonarr"]["apiKey"]; !v.Set || v.Value != "" || v.Source != config.SourceEnv {
		t.Errorf("sonarr apiKey = %+v, want set, from env, without a value", v)
	}
	if strings.Contains(rec.Body.String(), "env-secret") {
		t.Error("the response must never contain an API key")
	}
}

func TestPutSettings(t *testing.T) {
	h := newHarness(t, nil)
	rec := h.do(http.MethodPut, "/api/settings", `{
		"rules": {"graceDays": 7, "countingUsers": ["u1"]},
		"connections": {"sonarr": {"url": "http://sonarr:8989/", "apiKey": "abc123"}}
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	got := decode[settingsBody](t, rec)
	if got.Rules.GraceDays != 7 || !reflect.DeepEqual(got.Rules.CountingUsers, []string{"u1"}) {
		t.Errorf("rules = %+v", got.Rules)
	}
	if got.Rules.WatchedMode != domain.WatchedAll || got.Rules.ProtectionTag != "keep" {
		t.Errorf("fields that were not sent must keep their values: %+v", got.Rules)
	}
	if v := got.Connections["sonarr"]["url"]; v.Value != "http://sonarr:8989" || v.Source != config.SourceUI {
		t.Errorf("sonarr url = %+v, want the normalised URL saved from the UI", v)
	}
	if v := got.Connections["sonarr"]["apiKey"]; !v.Set || v.Source != config.SourceUI {
		t.Errorf("sonarr apiKey = %+v", v)
	}
	if strings.Contains(rec.Body.String(), "abc123") {
		t.Error("the response must never contain an API key")
	}

	// The settings survive in the store.
	saved, err := h.config.Rules(context.Background())
	if err != nil || saved.GraceDays != 7 {
		t.Errorf("saved rules = %+v, %v", saved, err)
	}

	// null clears a saved value.
	rec = h.do(http.MethodPut, "/api/settings", `{"connections": {"sonarr": {"apiKey": null}}}`)
	got = decode[settingsBody](t, rec)
	if v := got.Connections["sonarr"]["apiKey"]; v.Set || v.Source != config.SourceNone {
		t.Errorf("sonarr apiKey after clearing = %+v, want unset", v)
	}
	if v := got.Connections["sonarr"]["url"]; v.Value != "http://sonarr:8989" {
		t.Errorf("sonarr url must be untouched, got %+v", v)
	}
}

func TestPutSettingsRejectsBadInputAndSavesNothing(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		field string
	}{
		{"invalid rule", `{"rules": {"graceDays": -1}}`, "rules.graceDays"},
		{"unknown rule", `{"rules": {"colour": "blue"}}`, "rules"},
		{"bad url", `{"connections": {"sonarr": {"url": "sonarr:8989"}}}`, "connections.sonarr.url"},
		{"unknown field", `{"connections": {"sonarr": {"token": "x"}}}`, "connections.sonarr.token"},
		{"unknown app", `{"connections": {"plex": {"url": "http://plex"}}}`, "connections.plex.url"},
		{"good rules with a bad url", `{"rules": {"graceDays": 9}, "connections": {"radarr": {"url": "nope"}}}`, "connections.radarr.url"},
		{"malformed json", `{"rules": `, ""},
		{"unknown top-level key", `{"theme": "dark"}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, nil)
			rec := h.do(http.MethodPut, "/api/settings", tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
			}
			got := decode[errorBody](t, rec)
			if got.Error == "" || got.Field != tt.field {
				t.Errorf("error body = %+v, want a message and field %q", got, tt.field)
			}
			rules, _ := h.config.Rules(context.Background())
			if !reflect.DeepEqual(rules, domain.DefaultRules()) {
				t.Errorf("rules were saved despite the error: %+v", rules)
			}
		})
	}
}

func TestConnectionTest(t *testing.T) {
	h := newHarness(t, nil)
	h.apps.testers[config.AppSonarr] = testerFunc(func(context.Context) (string, error) { return "Sonarr 4.0.9", nil })
	h.apps.testers[config.AppRadarr] = testerFunc(func(context.Context) (string, error) {
		return "", errors.New("unexpected status 401: Unauthorized")
	})

	type result struct {
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
		Error  string `json:"error"`
	}
	tests := []struct {
		app  string
		code int
		want result
	}{
		{"sonarr", http.StatusOK, result{OK: true, Detail: "Sonarr 4.0.9"}},
		{"radarr", http.StatusOK, result{Error: "unexpected status 401: Unauthorized"}},
		{"jellyfin", http.StatusOK, result{Error: "not configured"}},
	}
	for _, tt := range tests {
		rec := h.do(http.MethodPost, "/api/connections/"+tt.app+"/test", "")
		if rec.Code != tt.code {
			t.Errorf("%s: status = %d, want %d", tt.app, rec.Code, tt.code)
			continue
		}
		if got := decode[result](t, rec); got != tt.want {
			t.Errorf("%s: result = %+v, want %+v", tt.app, got, tt.want)
		}
	}
	if rec := h.do(http.MethodPost, "/api/connections/plex/test", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown app: status = %d, want 404", rec.Code)
	}
}

type fakeMediaServer struct {
	users []domain.User
	err   error
}

func (f fakeMediaServer) Users(context.Context) ([]domain.User, error) { return f.users, f.err }
func (f fakeMediaServer) Library(context.Context, []string) (domain.Library, error) {
	return domain.Library{}, nil
}

var _ providers.MediaServer = fakeMediaServer{}

func TestUsers(t *testing.T) {
	h := newHarness(t, nil)
	if rec := h.do(http.MethodGet, "/api/media-server/users", ""); rec.Code != http.StatusConflict {
		t.Errorf("not configured: status = %d, want 409", rec.Code)
	}

	h.apps.server = fakeMediaServer{users: []domain.User{{ID: "a", Name: "Alice"}}}
	rec := h.do(http.MethodGet, "/api/media-server/users", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if got := decode[[]domain.User](t, rec); !reflect.DeepEqual(got, []domain.User{{ID: "a", Name: "Alice"}}) {
		t.Errorf("users = %+v", got)
	}

	h.apps.server = fakeMediaServer{err: errors.New("connection refused")}
	if rec := h.do(http.MethodGet, "/api/media-server/users", ""); rec.Code != http.StatusBadGateway {
		t.Errorf("unreachable: status = %d, want 502", rec.Code)
	}
}
```

- [ ] **Step 7: Run the tests to verify they fail**

Run: `go test ./internal/api/`
Expected: FAIL to compile with `undefined: Server`, `undefined: RequestHeader`, `undefined: errorBody`.

- [ ] **Step 8: Write the server, the guard and health**

Create `internal/api/server.go`:

```go
// Package api is removarr's HTTP API (spec 7.6).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"net/url"

	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/providers"
	"github.com/removarr/removarr/internal/scanner"
	"github.com/removarr/removarr/internal/store"
)

// RequestHeader must be present on every request that changes something (spec section 10).
const RequestHeader = "X-Removarr-Request"

// Scanner is the part of the scanner the API uses. *scanner.Service satisfies it.
type Scanner interface {
	Start() error
	Status() scanner.Status
}

// Store is the part of the store the API uses. *store.Store satisfies it.
type Store interface {
	LatestScan(ctx context.Context) (*store.ScanRecord, error)
	Candidates(ctx context.Context, scanID int64, list domain.ListKind) ([]domain.Candidate, error)
}

// Apps gives access to the configured apps. *app.Apps satisfies it.
type Apps interface {
	Tester(ctx context.Context, app string) (providers.Tester, error)
	MediaServer(ctx context.Context) (providers.MediaServer, error)
}

// Server holds what the handlers need.
type Server struct {
	Config  *config.Service
	Store   Store
	Scanner Scanner
	Apps    Apps
	Log     *slog.Logger
}

// Handler returns the API's routes behind the request guard.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PUT /api/settings", s.putSettings)
	mux.HandleFunc("POST /api/connections/{app}/test", s.testConnection)
	mux.HandleFunc("GET /api/media-server/users", s.users)
	return guard(mux)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// guard rejects requests that change something unless they could only have been sent by
// removarr's own UI or a deliberate API client: they must carry RequestHeader, be JSON,
// and, when they name an origin, come from this server's own address.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get(RequestHeader) == "" {
			writeError(w, http.StatusForbidden, "missing "+RequestHeader+" header")
			return
		}
		if mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mediaType != "application/json" {
			writeError(w, http.StatusUnsupportedMediaType, "the content type must be application/json")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host == "" || u.Host != r.Host {
				writeError(w, http.StatusForbidden, "the request does not come from this server's address")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// errorBody is the JSON body of every error response.
type errorBody struct {
	Error string `json:"error"`
	Field string `json:"field,omitempty"` // the setting at fault, for example "rules.graceDays"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

// fail answers an error from a handler. A *domain.FieldError is the caller's mistake and
// becomes a 400 naming the field. Anything else is logged and becomes a 500.
func (s *Server) fail(w http.ResponseWriter, err error) {
	var fe *domain.FieldError
	if errors.As(err, &fe) {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: fe.Message, Field: fe.Field})
		return
	}
	s.Log.Error("request failed", "error", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
```

- [ ] **Step 9: Write the settings endpoints**

Create `internal/api/settings.go`:

```go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/providers"
)

type settingsResponse struct {
	Rules       domain.Rules                           `json:"rules"`
	Connections map[string]map[string]config.FieldView `json:"connections"`
	DryRun      bool                                   `json:"dryRun"`
}

func (s *Server) settings(ctx context.Context) (settingsResponse, error) {
	rules, err := s.Config.Rules(ctx)
	if err != nil {
		return settingsResponse{}, err
	}
	connections, err := s.Config.ConnView(ctx)
	if err != nil {
		return settingsResponse{}, err
	}
	return settingsResponse{Rules: rules, Connections: connections, DryRun: s.Config.Env().DryRun}, nil
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	resp, err := s.settings(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

type settingsRequest struct {
	Rules       json.RawMessage               `json:"rules"`
	Connections map[string]map[string]*string `json:"connections"`
}

// putSettings validates the whole request before saving any of it.
func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req settingsRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "the request body is not valid: "+err.Error())
		return
	}

	var rules *domain.Rules
	if len(req.Rules) > 0 && string(req.Rules) != "null" {
		merged, err := s.Config.Rules(ctx)
		if err != nil {
			s.fail(w, err)
			return
		}
		// Decoding over the saved rules changes only the fields the request names.
		rulesDecoder := json.NewDecoder(bytes.NewReader(req.Rules))
		rulesDecoder.DisallowUnknownFields()
		if err := rulesDecoder.Decode(&merged); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error(), Field: "rules"})
			return
		}
		if err := merged.Validate(); err != nil {
			var fe *domain.FieldError
			if errors.As(err, &fe) {
				writeJSON(w, http.StatusBadRequest, errorBody{Error: fe.Message, Field: "rules." + fe.Field})
				return
			}
			s.fail(w, err)
			return
		}
		rules = &merged
	}

	type change struct {
		field config.Field
		value string
	}
	var changes []change
	for app, fields := range req.Connections {
		for name, value := range fields {
			field, ok := config.FieldFor(app, name)
			if !ok {
				writeJSON(w, http.StatusBadRequest, errorBody{
					Error: "unknown connection setting",
					Field: "connections." + app + "." + name,
				})
				return
			}
			v := ""
			if value != nil {
				v = *value
			}
			if _, err := config.CheckConn(field, v); err != nil {
				s.fail(w, err)
				return
			}
			changes = append(changes, change{field: field, value: v})
		}
	}

	if rules != nil {
		if err := s.Config.SetRules(ctx, *rules); err != nil {
			s.fail(w, err)
			return
		}
	}
	for _, c := range changes {
		if err := s.Config.SetConn(ctx, c.field, c.value); err != nil {
			s.fail(w, err)
			return
		}
	}
	s.getSettings(w, r)
}

type testResponse struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

// testConnection tries the saved connection of one app. A failed test is a normal answer,
// not an HTTP error.
func (s *Server) testConnection(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	if !slices.Contains(config.Apps, app) {
		writeError(w, http.StatusNotFound, "unknown app")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	tester, err := s.Apps.Tester(ctx, app)
	if errors.Is(err, providers.ErrNotConfigured) {
		writeJSON(w, http.StatusOK, testResponse{Error: "not configured"})
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	detail, err := tester.Test(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, testResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, testResponse{OK: true, Detail: detail})
}

// users lists the media server's users for the counting-users picker.
func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	server, err := s.Apps.MediaServer(r.Context())
	if errors.Is(err, providers.ErrNotConfigured) {
		writeError(w, http.StatusConflict, "The media server is not configured.")
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	users, err := server.Users(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "The media server could not be read: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, users)
}
```

- [ ] **Step 10: Run all tests to verify they pass**

Run: `go vet ./... && go test ./...`
Expected: `ok` for every package with tests, including `internal/app` and `internal/api`; no vet output.

- [ ] **Step 11: Commit**

```bash
git add internal/app internal/api
git commit -m "feat: provider wiring and settings API with request guard"
```

---

### Task 13: Scan, candidates and status API, and the program entry point

**Files:**
- Create: `internal/api/scans.go`, `cmd/removarr/main.go`
- Modify: `internal/api/server.go` (the `Handler` method gains four routes), `README.md`
- Test: `internal/api/scans_test.go`

**Interfaces:**
- Consumes:
  - Task 5: `store.Open`, `store.ScanRecord`, `(*store.Store).BeginScan`, `FinishScan`, `LatestScan`, `Candidates`.
  - Task 6: `config.LoadEnv`, `config.NewService`, `(*config.Service).App`, `(*config.Service).Env`, `(*config.Service).Rules`, `config.Apps`.
  - Task 11: `scanner.Service`, `scanner.Status`, `scanner.ErrRunning`.
  - Task 12: `app.Apps`; `api.Server`, `writeJSON`, `writeError`, `(*Server).fail`; test helpers `newHarness`, `(*harness).do`, `decode`, `fakeScanner`.
- Produces: the endpoints below, and the `removarr` binary.

| Method and path | Success | Failure |
|---|---|---|
| `GET /api/status` | 200 `{"dryRun", "connections": {app: {"configured"}}, "scan": {...}, "lastScan": {...} or null}` | |
| `POST /api/scans` | 202 `{"started":true}` | 409 while a scan is running |
| `GET /api/scans/current` | 200 `{"status": {"running","phase","startedAt","lastError"}, "last": {...} or null}` | |
| `GET /api/candidates?list=movies` | 200 `{"list", "scannedAt", "error", "candidates": [...]}` | 400 when `list` is not `movies`, `series` or `torrents` |

`candidates` is always an array, never null. `error` is the reason the list could not be produced in the latest scan, and is absent when it could.

- [ ] **Step 1: Write the failing tests**

Create `internal/api/scans_test.go`:

```go
package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scanner"
	"github.com/removarr/removarr/internal/store"
)

// storeScan saves a finished scan with one movie row and a failed series list.
func storeScan(t *testing.T, h *harness) int64 {
	t.Helper()
	ctx := context.Background()
	started := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	id, err := h.store.BeginScan(ctx, started)
	if err != nil {
		t.Fatal(err)
	}
	summary := domain.ScanSummary{
		Lists: map[domain.ListKind]domain.ListSummary{
			domain.ListMovies:   {Candidates: 1},
			domain.ListSeries:   {Error: "The series manager is not configured."},
			domain.ListTorrents: {},
		},
		RootFolders: []domain.RootFolder{{Path: "/media/movies", FreeBytes: 100}},
	}
	candidates := []domain.Candidate{{
		List: domain.ListMovies, Key: "tmdb:603", Title: "The Matrix", Size: 5000, Torrents: []domain.TorrentInfo{},
	}}
	if err := h.store.FinishScan(ctx, id, started.Add(time.Minute), summary, candidates); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestStartScan(t *testing.T) {
	h := newHarness(t, nil)

	rec := h.do(http.MethodPost, "/api/scans", "")
	if rec.Code != http.StatusAccepted || h.scanner.started != 1 {
		t.Errorf("status = %d with %d scans started; want 202 and 1", rec.Code, h.scanner.started)
	}

	h.scanner.err = scanner.ErrRunning
	if rec := h.do(http.MethodPost, "/api/scans", ""); rec.Code != http.StatusConflict {
		t.Errorf("while running: status = %d, want 409", rec.Code)
	}

	h.scanner.err = nil
	rec = h.do(http.MethodPost, "/api/scans", "", func(r *http.Request) { r.Header.Del(RequestHeader) })
	if rec.Code != http.StatusForbidden || h.scanner.started != 1 {
		t.Errorf("without the request header: status = %d with %d scans started; want 403 and still 1", rec.Code, h.scanner.started)
	}
}

func TestCurrentScan(t *testing.T) {
	h := newHarness(t, nil)
	type body struct {
		Status scanner.Status    `json:"status"`
		Last   *store.ScanRecord `json:"last"`
	}

	got := decode[body](t, h.do(http.MethodGet, "/api/scans/current", ""))
	if got.Status.Running || got.Last != nil {
		t.Errorf("before any scan = %+v, want idle and no last scan", got)
	}

	id := storeScan(t, h)
	h.scanner.status = scanner.Status{Running: true, Phase: "reading the download client"}
	got = decode[body](t, h.do(http.MethodGet, "/api/scans/current", ""))
	if !got.Status.Running || got.Status.Phase != "reading the download client" {
		t.Errorf("status = %+v", got.Status)
	}
	if got.Last == nil || got.Last.ID != id || got.Last.Summary.Lists[domain.ListMovies].Candidates != 1 {
		t.Errorf("last = %+v", got.Last)
	}
}

func TestCandidates(t *testing.T) {
	h := newHarness(t, nil)
	type body struct {
		List       string             `json:"list"`
		ScannedAt  *time.Time         `json:"scannedAt"`
		Error      string             `json:"error"`
		Candidates []domain.Candidate `json:"candidates"`
	}

	rec := h.do(http.MethodGet, "/api/candidates?list=movies", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"candidates":[]`) {
		t.Errorf("before any scan = %d %s; want 200 with an empty array", rec.Code, rec.Body)
	}
	if got := decode[body](t, rec); got.ScannedAt != nil || got.List != "movies" {
		t.Errorf("before any scan = %+v", got)
	}

	storeScan(t, h)

	movies := decode[body](t, h.do(http.MethodGet, "/api/candidates?list=movies", ""))
	if len(movies.Candidates) != 1 || movies.Candidates[0].Title != "The Matrix" || movies.Error != "" {
		t.Errorf("movies = %+v", movies)
	}
	if want := time.Date(2026, 3, 10, 12, 1, 0, 0, time.UTC); movies.ScannedAt == nil || !movies.ScannedAt.Equal(want) {
		t.Errorf("scannedAt = %v, want %v", movies.ScannedAt, want)
	}

	rec = h.do(http.MethodGet, "/api/candidates?list=series", "")
	series := decode[body](t, rec)
	if series.Error != "The series manager is not configured." || !strings.Contains(rec.Body.String(), `"candidates":[]`) {
		t.Errorf("series = %s; want the error and an empty array", rec.Body)
	}

	for _, path := range []string{"/api/candidates?list=books", "/api/candidates"} {
		if rec := h.do(http.MethodGet, path, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", path, rec.Code)
		}
	}
}

func TestStatus(t *testing.T) {
	h := newHarness(t, map[string]string{"radarr.url": "http://radarr:7878", "radarr.api_key": "rk"})
	type body struct {
		DryRun      bool `json:"dryRun"`
		Connections map[string]struct {
			Configured bool `json:"configured"`
		} `json:"connections"`
		Scan     scanner.Status    `json:"scan"`
		LastScan *store.ScanRecord `json:"lastScan"`
	}

	got := decode[body](t, h.do(http.MethodGet, "/api/status", ""))
	if !got.DryRun {
		t.Error("dryRun must reflect the environment")
	}
	if len(got.Connections) != 4 || !got.Connections["radarr"].Configured || got.Connections["sonarr"].Configured {
		t.Errorf("connections = %+v, want four apps with only radarr configured", got.Connections)
	}
	if got.LastScan != nil {
		t.Errorf("lastScan = %+v, want null before any scan", got.LastScan)
	}

	id := storeScan(t, h)
	got = decode[body](t, h.do(http.MethodGet, "/api/status", ""))
	if got.LastScan == nil || got.LastScan.ID != id || len(got.LastScan.Summary.RootFolders) != 1 {
		t.Errorf("lastScan = %+v", got.LastScan)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/api/ -run 'TestStartScan|TestCurrentScan|TestCandidates|TestStatus'`
Expected: FAIL. The tests compile, but every request gets 404 because the routes do not exist yet, for example `status = 404 with 0 scans started; want 202 and 1`.

- [ ] **Step 3: Write the handlers**

Create `internal/api/scans.go`:

```go
package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scanner"
	"github.com/removarr/removarr/internal/store"
)

// startScan starts a scan in the background.
func (s *Server) startScan(w http.ResponseWriter, r *http.Request) {
	err := s.Scanner.Start()
	if errors.Is(err, scanner.ErrRunning) {
		writeError(w, http.StatusConflict, "A scan is already running.")
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}

type scanResponse struct {
	Status scanner.Status    `json:"status"`
	Last   *store.ScanRecord `json:"last"`
}

// currentScan reports the scan in progress and the latest finished scan.
func (s *Server) currentScan(w http.ResponseWriter, r *http.Request) {
	last, err := s.Store.LatestScan(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, scanResponse{Status: s.Scanner.Status(), Last: last})
}

type candidatesResponse struct {
	List       domain.ListKind    `json:"list"`
	ScannedAt  *time.Time         `json:"scannedAt"`
	Error      string             `json:"error,omitempty"`
	Candidates []domain.Candidate `json:"candidates"`
}

// candidates returns the rows of one list from the latest finished scan.
func (s *Server) candidates(w http.ResponseWriter, r *http.Request) {
	list := domain.ListKind(r.URL.Query().Get("list"))
	switch list {
	case domain.ListMovies, domain.ListSeries, domain.ListTorrents:
	default:
		writeError(w, http.StatusBadRequest, `list must be "movies", "series" or "torrents"`)
		return
	}

	resp := candidatesResponse{List: list, Candidates: []domain.Candidate{}}
	last, err := s.Store.LatestScan(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	if last != nil {
		resp.ScannedAt = last.FinishedAt
		resp.Error = last.Summary.Lists[list].Error
		if resp.Candidates, err = s.Store.Candidates(r.Context(), last.ID, list); err != nil {
			s.fail(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type connectionStatus struct {
	Configured bool `json:"configured"`
}

type statusResponse struct {
	DryRun      bool                        `json:"dryRun"`
	Connections map[string]connectionStatus `json:"connections"`
	Scan        scanner.Status              `json:"scan"`
	LastScan    *store.ScanRecord           `json:"lastScan"`
}

// status gives the header its overview: which apps are configured, the scan in progress,
// and the latest scan with its summary and free space.
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	resp := statusResponse{
		DryRun:      s.Config.Env().DryRun,
		Connections: map[string]connectionStatus{},
		Scan:        s.Scanner.Status(),
	}
	for _, app := range config.Apps {
		_, configured, err := s.Config.App(r.Context(), app)
		if err != nil {
			s.fail(w, err)
			return
		}
		resp.Connections[app] = connectionStatus{Configured: configured}
	}
	last, err := s.Store.LatestScan(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	resp.LastScan = last
	writeJSON(w, http.StatusOK, resp)
}
```

- [ ] **Step 4: Register the routes**

In `internal/api/server.go`, replace the whole `Handler` method with:

```go
// Handler returns the API's routes behind the request guard.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PUT /api/settings", s.putSettings)
	mux.HandleFunc("POST /api/connections/{app}/test", s.testConnection)
	mux.HandleFunc("GET /api/media-server/users", s.users)
	mux.HandleFunc("POST /api/scans", s.startScan)
	mux.HandleFunc("GET /api/scans/current", s.currentScan)
	mux.HandleFunc("GET /api/candidates", s.candidates)
	return guard(mux)
}
```

- [ ] **Step 5: Run the API tests to verify they pass**

Run: `go test ./internal/api/`
Expected: `ok  	github.com/removarr/removarr/internal/api`

- [ ] **Step 6: Write the entry point**

Create `cmd/removarr/main.go`:

```go
// Command removarr serves the removarr API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/removarr/removarr/internal/api"
	"github.com/removarr/removarr/internal/app"
	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/scanner"
	"github.com/removarr/removarr/internal/store"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("removarr stopped", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	env, err := config.LoadEnv(os.Getenv)
	if err != nil {
		return err
	}
	// The database path is made absolute so it does not depend on the working directory.
	configDir, err := filepath.Abs(env.ConfigDir)
	if err != nil {
		return fmt.Errorf("resolve config directory: %w", err)
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	st, err := store.Open(filepath.Join(configDir, "removarr.db"))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	cfg := config.NewService(env, st)
	apps := &app.Apps{Config: cfg, Timeout: 60 * time.Second}
	scans := &scanner.Service{
		Build: apps.Providers,
		Rules: cfg.Rules,
		Store: st,
		Now:   time.Now,
		Log:   log,
	}
	server := &api.Server{Config: cfg, Store: st, Scanner: scans, Apps: apps, Log: log}

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", env.Port),
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpServer.Shutdown(shutdownCtx)
	}()

	log.Info("removarr listening", "port", env.Port, "configDir", configDir, "dryRun", env.DryRun)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
```

- [ ] **Step 7: Run everything and smoke-test the binary**

Run: `go vet ./... && go test ./... && go build ./...`
Expected: `ok` for every package with tests, no vet output, and the build succeeds.

Then start the server with an empty configuration and exercise it:

```bash
REMOVARR_CONFIG_DIR="$(mktemp -d)" go run ./cmd/removarr &
until curl -s localhost:7272/api/health >/dev/null; do sleep 1; done
curl -s localhost:7272/api/health; echo
curl -s -X POST -H 'Content-Type: application/json' -H 'X-Removarr-Request: 1' localhost:7272/api/scans; echo
sleep 1
curl -s 'localhost:7272/api/candidates?list=torrents'; echo
curl -s -X POST localhost:7272/api/scans; echo
kill %1
```

Expected output, in order (the timestamp will differ):

```text
{"status":"ok"}
{"started":true}
{"list":"torrents","scannedAt":"2026-11-18T21:00:01.123456Z","error":"The download client is not configured.","candidates":[]}
{"error":"missing X-Removarr-Request header"}
```

- [ ] **Step 8: Document how to run it**

Replace the contents of `README.md` with:

````markdown
# removarr

removarr suggests what to delete from a self-hosted media server: watched movies, watched series and orphan torrents. It never deletes anything on its own.

Status: early development. The server can scan Jellyfin, Sonarr, Radarr and qBittorrent and serve the three suggestion lists over a JSON API. It cannot delete anything yet, and there is no web UI yet. The design is in `docs/superpowers/specs/2026-11-18-removarr-design.md`.

## Development

Requires Go 1.27.

    go test ./...

## Running

    REMOVARR_CONFIG_DIR=./config go run ./cmd/removarr

| Variable | Default | Purpose |
|---|---|---|
| `REMOVARR_PORT` | `7272` | Port the server listens on. |
| `REMOVARR_CONFIG_DIR` | `/config` | Directory holding the SQLite database. |
| `REMOVARR_DRY_RUN` | `false` | Reserved for deletions, which are not built yet. |
| `REMOVARR_JELLYFIN_URL`, `REMOVARR_JELLYFIN_API_KEY` | | Jellyfin connection. |
| `REMOVARR_SONARR_URL`, `REMOVARR_SONARR_API_KEY` | | Sonarr connection. |
| `REMOVARR_RADARR_URL`, `REMOVARR_RADARR_API_KEY` | | Radarr connection. |
| `REMOVARR_QBITTORRENT_URL`, `REMOVARR_QBITTORRENT_USERNAME`, `REMOVARR_QBITTORRENT_PASSWORD` | | qBittorrent connection. |

Connection settings saved through the API take priority over these variables.

## Trying the API

Requests that change something need two headers:

    curl -X POST -H 'Content-Type: application/json' -H 'X-Removarr-Request: 1' localhost:7272/api/scans

Choose whose watch state counts, start a scan, then read the lists:

    curl localhost:7272/api/media-server/users
    curl -X PUT -H 'Content-Type: application/json' -H 'X-Removarr-Request: 1' \
      -d '{"rules":{"countingUsers":["<user id>"]}}' localhost:7272/api/settings
    curl -X POST -H 'Content-Type: application/json' -H 'X-Removarr-Request: 1' localhost:7272/api/scans
    curl localhost:7272/api/scans/current
    curl 'localhost:7272/api/candidates?list=movies'
    curl 'localhost:7272/api/candidates?list=series'
    curl 'localhost:7272/api/candidates?list=torrents'

removarr has no login. Run it only on a network you trust, and do not expose it to the internet.
````

- [ ] **Step 9: Commit**

```bash
git add cmd internal/api README.md
git commit -m "feat: scan, candidates and status API and the removarr binary"
```

---

### Task 14: Check against the real apps

This task needs the owner: it uses the real Jellyfin, Sonarr, Radarr and qBittorrent. Nothing in it changes those apps. It settles the four assumptions in spec section 16 and confirms that the three lists are right. If any assumption turns out false, stop and report it to the owner before changing any rule, because stage 2 (deleting) is planned from these results.

**Files:**
- Create: `.env` (never committed; `.gitignore` already excludes it)
- Modify: `docs/superpowers/specs/2026-11-18-removarr-design.md` (section 16 only)
- Modify, only if a check below fails: the provider at fault and its test

**Interfaces:**
- Consumes: the `removarr` binary from Task 13 and its API.
- Produces: section 16 of the spec rewritten as verified facts.

- [ ] **Step 1: Get the connection details from the owner**

The checks below use `jq`. Run `jq --version`; if it is missing, ask the owner to install it (`brew install jq` on macOS) before continuing.

Ask the owner for the URL and API key of Jellyfin, Sonarr and Radarr, and the URL, username and password of qBittorrent, as reachable from this machine. Write them to `.env` in the repository root:

```bash
REMOVARR_JELLYFIN_URL=http://<host>:8096
REMOVARR_JELLYFIN_API_KEY=<key>
REMOVARR_SONARR_URL=http://<host>:8989
REMOVARR_SONARR_API_KEY=<key>
REMOVARR_RADARR_URL=http://<host>:7878
REMOVARR_RADARR_API_KEY=<key>
REMOVARR_QBITTORRENT_URL=http://<host>:8080
REMOVARR_QBITTORRENT_USERNAME=<username>
REMOVARR_QBITTORRENT_PASSWORD=<password>
```

Run: `git status --short .env`
Expected: no output, which confirms the file is ignored. Do not print the file's contents in any report.

- [ ] **Step 2: Start removarr against the real apps and test each connection**

```bash
set -a; . ./.env; set +a
REMOVARR_CONFIG_DIR=./config go run ./cmd/removarr &
until curl -s localhost:7272/api/health >/dev/null; do sleep 1; done
for app in jellyfin sonarr radarr qbittorrent; do
  curl -s -X POST -H 'Content-Type: application/json' -H 'X-Removarr-Request: 1' "localhost:7272/api/connections/$app/test"; echo
done
```

Expected: four lines of the form `{"ok":true,"detail":"Sonarr 4.x.x.x"}`. For any `{"ok":false,...}`, fix the connection details with the owner before continuing.

- [ ] **Step 3: Check assumption 1 — history links downloads to files**

Run for Sonarr, then again with the Radarr variables and `movieId` in place of `episodeId`:

```bash
curl -s -H "X-Api-Key: $REMOVARR_SONARR_API_KEY" \
  "$REMOVARR_SONARR_URL/api/v3/history?page=1&pageSize=200&sortKey=date&sortDirection=descending" \
  | jq '[.records[] | select(.eventType == "downloadFolderImported")
         | {episodeId, downloadId, fileId: (.data.fileId // .data.FileId)}] | .[0:10]'
```

Confirm all three:

1. Import records of torrent downloads have a non-empty `downloadId` of 40 hexadecimal characters.
2. Import records have a `fileId`.
3. The first record's `date` is the newest, so the order is newest first. Check with `jq '[.records[].date] | .[0:3]'` on the same URL.

If `fileId` is absent, the fallback in `arr.Links` applies; note that in Step 8. If `downloadId` is absent on imports, stop and report: the linking rule does not hold.

- [ ] **Step 4: Check assumption 2 — Sonarr exposes monitoring per episode and per season**

This only reads. Changing monitoring belongs to stage 2.

```bash
SERIES_ID=$(curl -s -H "X-Api-Key: $REMOVARR_SONARR_API_KEY" "$REMOVARR_SONARR_URL/api/v3/series" | jq '.[0].id')
curl -s -H "X-Api-Key: $REMOVARR_SONARR_API_KEY" "$REMOVARR_SONARR_URL/api/v3/series/$SERIES_ID" | jq '.seasons[0]'
curl -s -H "X-Api-Key: $REMOVARR_SONARR_API_KEY" "$REMOVARR_SONARR_URL/api/v3/episode?seriesId=$SERIES_ID" | jq '.[0] | {id, monitored, airDateUtc}'
```

Confirm that the season object and the episode object each have a `monitored` field.

- [ ] **Step 5: Check assumption 3 — Jellyfin reports watch state per user**

```bash
curl -s localhost:7272/api/media-server/users
```

Expected: the owner's Jellyfin users with their IDs. Ask the owner which users should count, then save them (replace the IDs):

```bash
curl -s -X PUT -H 'Content-Type: application/json' -H 'X-Removarr-Request: 1' \
  -d '{"rules":{"countingUsers":["<id>","<id>"]}}' localhost:7272/api/settings | jq '.rules'
```

Expected: the rules, with `countingUsers` holding those IDs.

- [ ] **Step 6: Run a scan and read the result**

```bash
curl -s -X POST -H 'Content-Type: application/json' -H 'X-Removarr-Request: 1' localhost:7272/api/scans; echo
while curl -s localhost:7272/api/scans/current | jq -e '.status.running' >/dev/null; do sleep 2; done
curl -s localhost:7272/api/scans/current | jq '{status, startedAt: .last.startedAt, finishedAt: .last.finishedAt, summary: .last.summary}'
```

Expected: `status.running` is false, `status.lastError` is absent, and none of the three lists in `summary.lists` has an `error`. Note the scan's duration from the two timestamps.

- [ ] **Step 7: Check the lists with the owner**

Show the owner a short view of each list:

```bash
for list in movies series torrents; do
  echo "== $list"
  curl -s "localhost:7272/api/candidates?list=$list" \
    | jq -r '.candidates[] | [.title, (.scope // .reason // ""), (.size/1073741824*100|round/100|tostring) + " GB", (.torrents|length|tostring) + " torrents", (if .belowSeedMin then "below seeding minimum" else "" end)] | @tsv' \
    | head -20
done
```

With the owner, confirm each of these on at least three rows per list, looking the items up in the apps themselves:

1. A movie row is a movie every counting user has watched in Jellyfin, and it still has a file in Radarr.
2. A series row is a series, or season, whose downloaded episodes are all watched. An unwatched special did not prevent the row.
3. An "orphan" torrent has no matching movie or series in Radarr or Sonarr.
4. An "unlinked" torrent belongs to something Radarr or Sonarr has since replaced or lost the file of.
5. Pick three torrents the owner knows are in active use by Radarr or Sonarr. None of them appears in the torrent list.
6. Torrent rows show a tracker host, a ratio and a seeding time (assumption 4).

Also compare `summary.lists.movies.unmatched` and `summary.lists.series.unmatched` with the library size. A large unmatched count means the ID matching between Jellyfin and the managers needs attention; report it.

Any row the owner says is wrong is a defect: write a failing test that reproduces it in the package that owns the rule (`internal/scan` for rule mistakes, the provider package for misread data), fix it, and repeat this step.

Stop the server when done: `kill %1`

- [ ] **Step 8: Record the results in the spec**

In `docs/superpowers/specs/2026-11-18-removarr-design.md`, replace section 16 ("Assumptions to verify early in implementation") with a section of the same number titled "Verified API behaviour". For each of the four assumptions, state what was observed, with the app version from Step 2 and today's date. State any deviation and how the code handles it. Include the scan duration from Step 6 and the library size (number of movies, series and torrents).

Do not include URLs, keys, user names or titles from the owner's library.

- [ ] **Step 9: Commit**

```bash
git status --short
git add docs/superpowers/specs/2026-11-18-removarr-design.md internal
git commit -m "docs: record verified API behaviour from the reference stack"
```

Before committing, confirm in the `git status` output that neither `.env` nor `config/` is listed.

---

## After this plan

Stage 1 ends with a server whose three lists the owner has confirmed against the real apps, and with the spec's API assumptions replaced by observed facts. The plan for stage 2 (ignore, the delete planner and executor, and history) is written next, from the updated spec.
