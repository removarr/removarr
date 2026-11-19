# removarr Content Linking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A torrent that still holds a library file is linked to it by content, so it is no longer listed as orphan or unlinked and is removed together with its movie or series.

**Architecture:** The download client interface gains a call that lists one download's files. The scanner reads those lists for the downloads that history does not link, and a pure function in `internal/scan` turns exact size matches into the same link data that history produces. The three list rules read that link data and do not change.

**Tech Stack:** Go 1.27, standard library only. No new dependency.

**Spec:** `docs/superpowers/specs/2026-11-18-removarr-design.md`. The rule is section 6.6 under "Linking by content"; sections 6.7, 6.8 rule 5, 7.2, 7.3 and 16.1 refer to it. Section 16.1 explains why it exists.

**Background:** Stage 1 linked torrents to library files only through Sonarr and Radarr history. Checked against a real stack, 27 of the 68 torrents it listed as orphan or unlinked, 876 of 1,050 GB, still held library files: files imported by hand, and torrents added outside the media managers, have no history link.

## Global Constraints

- Go module path is `github.com/removarr/removarr`. `go.mod` keeps `go 1.27` and gains no dependency.
- **Read-only.** The only new request is `GET /api/v2/torrents/files?hash=<id>` to qBittorrent. No new non-GET request anywhere.
- A file is compared only when its size is at least 50 MB, which is `50 << 20` bytes. A match is exact equality of the size in bytes.
- History takes precedence: a library file whose source history names is never linked to another download by content.
- When files of several downloads match one library file, the download whose ID sorts first backs it.
- A download linked by content is associated with the movie, or with every episode, the matched file belongs to.
- File lists are requested only for downloads that the rules do not exclude, that are not in a media manager's queue, and that history does not show backing a library file.
- If any needed file list cannot be read, no list is produced (spec 6.8, rule 5). The lists' error reads `The download client could not be read: files of a download: <cause>`.
- Secrets are never logged and never put into an error.
- Everything else behaves as before. Existing tests keep passing unchanged, except where a step below changes one.

## Review Focus

Conditions the spec implies but does not spell out, most likely first. Each has a test in the task that owns the code.

1. **A torrent removed between listing the torrents and asking for its files.** qBittorrent answers 404. The scan must not go on with a partial answer: every list reports the error. Tests: `TestFilesOfAnUnknownTorrentIsAnError` (Task 1), `TestRunFailsEveryListWhenFilesCannotBeRead` (Task 3).
2. **Small files that happen to share a size**, such as subtitles and samples. Files under 50 MB are never compared. Test: `TestContentLinks`, case "files under 50 MB are not compared" (Task 2).
3. **One library file that belongs to several episodes.** Every one of them must be associated with the torrent, or deleting the series would leave the torrent behind. Tests: `TestContentLinks`, case "a file of several episodes associates every one of them" (Task 2), `TestComputeLinksAMultiEpisodeFileByContent` (Task 3).
4. **A second torrent holding a file that history already attributes to another download**, such as a cross-seed. History wins and the second torrent is not linked. Test: `TestContentLinks`, case "history names the source, so content does not replace it" (Task 2).
5. **A media manager that did not answer.** Without it there is nothing to match against; asking for every torrent's files would be hundreds of pointless requests. Test: `TestRunDoesNotReadFilesWhenNoMediaManagerAnswered` (Task 3).

## File Structure

| File | Change | Responsibility |
|---|---|---|
| `internal/domain/downloads.go` | modify | Adds `DownloadFile`. |
| `internal/providers/providers.go` | modify | `DownloadClient` gains `Files`. |
| `internal/providers/qbittorrent/qbittorrent.go` | modify | Implements `Files`. |
| `internal/app/apps.go` | modify | The stand-in for an incomplete connection implements `Files`. |
| `internal/scan/content.go` | create | The linking-by-content rule, pure. |
| `internal/scanner/scanner.go` | modify | Reads the file lists and applies the rule before the lists are computed. |
| `docs/superpowers/specs/2026-11-18-removarr-design.md` | modify | Section 16 records the result of the re-check. |

Tests live next to the code as `*_test.go`.

---

### Task 1: The download client can list a download's files

**Files:**
- Modify: `internal/domain/downloads.go`, `internal/providers/providers.go`, `internal/providers/qbittorrent/qbittorrent.go`, `internal/app/apps.go`
- Test: `internal/providers/qbittorrent/qbittorrent_test.go`, `internal/app/apps_test.go`, `internal/scanner/scanner_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - Package `domain`: `DownloadFile{Name string; Size int64}`.
  - Package `providers`: `DownloadClient` gains `Files(ctx context.Context, id string) ([]domain.DownloadFile, error)`.
  - Package `qbittorrent`: `(*Client).Files`, same signature. A torrent with no files gives an empty, non-nil list. An unknown torrent gives an error that wraps `*httpjson.StatusError` with status 404.
  - Package `scanner`, test file only: `fakeDownloads` gains the fields `files map[string][]domain.DownloadFile`, `filesErr error` and `fileCalls []string`, and the method `Files`. Task 3's tests use them.

How qBittorrent answers: `GET /api/v2/torrents/files?hash=<hash>` returns a JSON array with one object per file; the fields read are `name` (the file's path inside the torrent) and `size` (bytes). An unknown hash answers 404.

- [ ] **Step 1: Add the type and the interface method**

In `internal/domain/downloads.go`, replace

```go
	Tracker     string         // tracker host name; empty when unknown
}
```

with

```go
	Tracker     string         // tracker host name; empty when unknown
}

// DownloadFile is one file inside a download, as the download client reports it.
type DownloadFile struct {
	Name string // path of the file inside the download
	Size int64  // size in bytes
}
```

In `internal/providers/providers.go`, replace

```go
type DownloadClient interface {
	Downloads(ctx context.Context) ([]domain.Download, error)
}
```

with

```go
type DownloadClient interface {
	Downloads(ctx context.Context) ([]domain.Download, error)
	// Files returns the files of one download. It is used to link a download to library
	// files by content when history does not link it (spec 6.6).
	Files(ctx context.Context, id string) ([]domain.DownloadFile, error)
}
```

- [ ] **Step 2: Run the build to verify it fails**

Run: `go vet ./...`
Expected: errors saying that `*qbittorrent.Client` and `app.incomplete` do not implement `providers.DownloadClient` (missing method `Files`). Nothing implements the new method yet.

- [ ] **Step 3: Write the failing tests for the qBittorrent client**

In `internal/providers/qbittorrent/qbittorrent_test.go`, replace

```go
	"testing"
	"time"
)
```

with

```go
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/httpjson"
)
```

In `internal/providers/qbittorrent/qbittorrent_test.go`, replace

```go
	trackers map[string]string // hash -> body of /torrents/trackers
}
```

with

```go
	trackers map[string]string // hash -> body of /torrents/trackers
	files    map[string]string // hash -> body of /torrents/files; an unknown hash gets 404
}
```

In `internal/providers/qbittorrent/qbittorrent_test.go`, replace

```go
	mux.HandleFunc("/api/v2/app/version", guarded(func(w http.ResponseWriter, r *http.Request) {
```

with

```go
	mux.HandleFunc("/api/v2/torrents/files", guarded(func(w http.ResponseWriter, r *http.Request) {
		body, ok := f.files[r.URL.Query().Get("hash")]
		if !ok {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		fmt.Fprint(w, body)
	}))
	mux.HandleFunc("/api/v2/app/version", guarded(func(w http.ResponseWriter, r *http.Request) {
```

In `internal/providers/qbittorrent/qbittorrent_test.go`, replace

```go
func TestDownloadsLogsInOnce(t *testing.T) {
```

with

```go
func TestFilesListsTheFilesOfOneTorrent(t *testing.T) {
	f := &fakeQbit{files: map[string]string{"abc": `[
		{"index":0,"name":"Show.S01/Show.S01E01.mkv","size":1500000000,"progress":1,"priority":1},
		{"index":1,"name":"Show.S01/Show.S01E01.nfo","size":812,"progress":1,"priority":1}
	]`}}
	c := start(t, f, "", "")
	got, err := c.Files(ctx, "abc")
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.DownloadFile{
		{Name: "Show.S01/Show.S01E01.mkv", Size: 1500000000},
		{Name: "Show.S01/Show.S01E01.nfo", Size: 812},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Files = %+v, want %+v", got, want)
	}
}

func TestFilesOfATorrentWithoutFilesIsEmptyNotNil(t *testing.T) {
	c := start(t, &fakeQbit{files: map[string]string{"abc": `[]`}}, "", "")
	got, err := c.Files(ctx, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("Files = %#v, want an empty, non-nil list", got)
	}
}

// A torrent removed between listing the torrents and asking for its files answers 404.
// That must be an error, so the scan does not go on with a partial answer.
func TestFilesOfAnUnknownTorrentIsAnError(t *testing.T) {
	c := start(t, &fakeQbit{files: map[string]string{}}, "", "")
	_, err := c.Files(ctx, "gone")
	var status *httpjson.StatusError
	if !errors.As(err, &status) || status.Status != http.StatusNotFound {
		t.Errorf("err = %v, want a 404 status error", err)
	}
}

func TestFilesLogsInWhenNeeded(t *testing.T) {
	f := &fakeQbit{auth: true, files: map[string]string{"abc": `[{"name":"a.mkv","size":7}]`}}
	c := start(t, f, "admin", "secret")
	got, err := c.Files(ctx, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Size != 7 || f.logins != 1 {
		t.Errorf("Files = %+v after %d logins, want one file after one login", got, f.logins)
	}
}

func TestDownloadsLogsInOnce(t *testing.T) {
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./internal/providers/qbittorrent/`
Expected: a build failure mentioning `c.Files undefined` (or the missing method `Files`).

- [ ] **Step 5: Implement `Files` in the qBittorrent client**

In `internal/providers/qbittorrent/qbittorrent.go`, replace

```go
// Test checks the connection and returns the app name and version.
```

with

```go
// Files returns the files of one torrent.
func (c *Client) Files(ctx context.Context, id string) ([]domain.DownloadFile, error) {
	body, err := c.get(ctx, "/api/v2/torrents/files", url.Values{"hash": {id}})
	if err != nil {
		return nil, fmt.Errorf("files of a torrent: %w", err)
	}
	var raw []struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("files of a torrent: decode response: %w", err)
	}
	out := make([]domain.DownloadFile, 0, len(raw))
	for _, f := range raw {
		out = append(out, domain.DownloadFile{Name: f.Name, Size: f.Size})
	}
	return out, nil
}

// Test checks the connection and returns the app name and version.
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/providers/qbittorrent/`
Expected: `ok  	github.com/removarr/removarr/internal/providers/qbittorrent`

- [ ] **Step 7: Extend the test for an incomplete connection**

A partly entered qBittorrent connection is replaced by a stand-in whose every read fails. `Files` is now one of those reads.

In `internal/app/apps_test.go`, replace

```go
			_, err := p.Downloads.Downloads(ctx)
			return true, err
		},
	}
```

with

```go
			_, err := p.Downloads.Downloads(ctx)
			return true, err
		},
		"Files": func(p scanner.Providers) (bool, error) {
			if p.Downloads == nil {
				return false, nil
			}
			_, err := p.Downloads.Files(ctx, "abc")
			return true, err
		},
	}
```

In `internal/app/apps_test.go`, replace

```go
		{"qbittorrent username without URL", map[string]string{"qbittorrent.username": "admin"}, []string{"Downloads"}},
		{"qbittorrent password without URL", map[string]string{"qbittorrent.password": "secret"}, []string{"Downloads"}},
```

with

```go
		{"qbittorrent username without URL", map[string]string{"qbittorrent.username": "admin"}, []string{"Downloads", "Files"}},
		{"qbittorrent password without URL", map[string]string{"qbittorrent.password": "secret"}, []string{"Downloads", "Files"}},
```

- [ ] **Step 8: Run the test to verify it fails**

Run: `go test ./internal/app/`
Expected: a build failure saying `incomplete` does not implement `providers.DownloadClient` (missing method `Files`).

- [ ] **Step 9: Give the stand-in the method**

In `internal/app/apps.go`, replace

```go
func (incomplete) Downloads(context.Context) ([]domain.Download, error) { return nil, ErrIncomplete }
```

with

```go
func (incomplete) Downloads(context.Context) ([]domain.Download, error) { return nil, ErrIncomplete }
func (incomplete) Files(context.Context, string) ([]domain.DownloadFile, error) {
	return nil, ErrIncomplete
}
```

- [ ] **Step 10: Give the scanner's test fake the method**

The scanner's tests have their own fake download client. It must satisfy the interface again; the fields added here are used by Task 3.

In `internal/scanner/scanner_test.go`, replace

```go
type fakeDownloads struct {
	list    []domain.Download
	err     error
	entered chan struct{} // when set, receives once Downloads has been called
	release chan struct{} // when set, Downloads waits until it is closed
}
```

with

```go
type fakeDownloads struct {
	list      []domain.Download
	err       error
	files     map[string][]domain.DownloadFile // download ID -> its files
	filesErr  error
	fileCalls []string      // the download IDs Files was asked for, in order
	entered   chan struct{} // when set, receives once Downloads has been called
	release   chan struct{} // when set, Downloads waits until it is closed
}

func (f *fakeDownloads) Files(_ context.Context, id string) ([]domain.DownloadFile, error) {
	f.fileCalls = append(f.fileCalls, id)
	return f.files[id], f.filesErr
}
```

- [ ] **Step 11: Run everything to verify it passes**

Run: `gofmt -l . ; go vet ./... && go test ./...`
Expected: no output from `gofmt` or `go vet`, and `ok` for every package with tests.

- [ ] **Step 12: Commit**

```bash
git add internal/domain internal/providers internal/app internal/scanner
git commit -m "feat: list the files of a download"
```

---

### Task 2: The linking-by-content rule

**Files:**
- Create: `internal/scan/content.go`
- Test: `internal/scan/content_test.go`

**Interfaces:**
- Consumes:
  - Task 1: `domain.DownloadFile{Name string; Size int64}`.
  - Already in the repository: `domain.Links{Grabbed map[string][]int; FileSource map[int]string; Queue map[string]bool}`, `domain.Rules`, `domain.Download`; in package `scan`: `LinkIndex` with `Queued`, `Backing` and `Removable`, `NewLinkIndex`, `ManagerFiles`, `Torrents`, `TorrentsInput`, the unexported `excluded(r domain.Rules, d domain.Download) bool`, and the test helpers `links()` and `dl(id string, mods ...func(*domain.Download)) domain.Download`.
- Produces, in package `scan`:
  - `MinContentSize int64` with the value `50 << 20`.
  - `LibraryFile{ID int; Size int64; Items []int}`.
  - `NeedsContentCheck(r domain.Rules, x *LinkIndex, d domain.Download) bool`.
  - `ContentLinks(links domain.Links, library []LibraryFile, files map[string][]domain.DownloadFile) domain.Links`. It returns new link data and leaves `links` unchanged.

How the rule fits the existing link data (spec 6.6):

- `Links.FileSource` maps a library file's ID to the download it came from. History fills it. This task adds entries for library files history names no source for.
- `Links.Grabbed` maps a download to the items (movie IDs or episode IDs) it is associated with. This task adds the matched file's items.
- Because the result has the same shape as history's, `NewLinkIndex`, `Removable` and the three list rules use it unchanged.

- [ ] **Step 1: Write the failing tests**

Create `internal/scan/content_test.go`:

```go
package scan

import (
	"reflect"
	"testing"

	"github.com/removarr/removarr/internal/domain"
)

const mb = int64(1) << 20

// sized returns a download's file list holding files of the given sizes.
func sized(sizes ...int64) []domain.DownloadFile {
	out := make([]domain.DownloadFile, 0, len(sizes))
	for _, s := range sizes {
		out = append(out, domain.DownloadFile{Name: "file", Size: s})
	}
	return out
}

func TestContentLinks(t *testing.T) {
	movie := LibraryFile{ID: 10, Size: 700 * mb, Items: []int{1}}
	tests := []struct {
		name       string
		history    func(l *domain.Links)
		library    []LibraryFile
		files      map[string][]domain.DownloadFile
		wantSource map[int]string
		wantItems  map[string][]int
	}{
		{
			name:       "a download's file with exactly the library file's size backs it",
			library:    []LibraryFile{movie},
			files:      map[string][]domain.DownloadFile{"pack": sized(700 * mb)},
			wantSource: map[int]string{10: "pack"},
			wantItems:  map[string][]int{"pack": {1}},
		},
		{
			name:       "one byte off is not a match",
			library:    []LibraryFile{movie},
			files:      map[string][]domain.DownloadFile{"pack": sized(700*mb + 1)},
			wantSource: map[int]string{},
			wantItems:  map[string][]int{},
		},
		{
			name:       "files under 50 MB are not compared",
			library:    []LibraryFile{{ID: 10, Size: 50*mb - 1, Items: []int{1}}},
			files:      map[string][]domain.DownloadFile{"pack": sized(50*mb - 1)},
			wantSource: map[int]string{},
			wantItems:  map[string][]int{},
		},
		{
			name:       "a file of exactly 50 MB is compared",
			library:    []LibraryFile{{ID: 10, Size: 50 * mb, Items: []int{1}}},
			files:      map[string][]domain.DownloadFile{"pack": sized(50 * mb)},
			wantSource: map[int]string{10: "pack"},
			wantItems:  map[string][]int{"pack": {1}},
		},
		{
			name: "history names the source, so content does not replace it",
			history: func(l *domain.Links) {
				l.FileSource[10] = "auto"
				l.Grabbed["auto"] = []int{1}
			},
			library:    []LibraryFile{movie},
			files:      map[string][]domain.DownloadFile{"crossseed": sized(700 * mb)},
			wantSource: map[int]string{10: "auto"},
			wantItems:  map[string][]int{"auto": {1}},
		},
		{
			name:       "a file of several episodes associates every one of them",
			library:    []LibraryFile{{ID: 20, Size: 900 * mb, Items: []int{11, 12}}},
			files:      map[string][]domain.DownloadFile{"double": sized(900 * mb)},
			wantSource: map[int]string{20: "double"},
			wantItems:  map[string][]int{"double": {11, 12}},
		},
		{
			name:       "a pack backs every library file it holds",
			library:    []LibraryFile{movie, {ID: 11, Size: 800 * mb, Items: []int{2}}},
			files:      map[string][]domain.DownloadFile{"pack": sized(700*mb, 4096, 800*mb)},
			wantSource: map[int]string{10: "pack", 11: "pack"},
			wantItems:  map[string][]int{"pack": {1, 2}},
		},
		{
			name:       "of two downloads holding the same file, the one whose ID sorts first backs it",
			library:    []LibraryFile{movie},
			files:      map[string][]domain.DownloadFile{"bbb": sized(700 * mb), "aaa": sized(700 * mb)},
			wantSource: map[int]string{10: "aaa"},
			wantItems:  map[string][]int{"aaa": {1}},
		},
		{
			name:       "a download history already associates with the item keeps it once",
			history:    func(l *domain.Links) { l.Grabbed["pack"] = []int{1} },
			library:    []LibraryFile{movie},
			files:      map[string][]domain.DownloadFile{"pack": sized(700 * mb)},
			wantSource: map[int]string{10: "pack"},
			wantItems:  map[string][]int{"pack": {1}},
		},
		{
			name:       "a download without an ID links nothing",
			library:    []LibraryFile{movie},
			files:      map[string][]domain.DownloadFile{"": sized(700 * mb)},
			wantSource: map[int]string{},
			wantItems:  map[string][]int{},
		},
		{
			name:       "without file lists nothing changes",
			history:    func(l *domain.Links) { l.Grabbed["old"] = []int{1} },
			library:    []LibraryFile{movie},
			wantSource: map[int]string{},
			wantItems:  map[string][]int{"old": {1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := links()
			if tt.history != nil {
				tt.history(&l)
			}
			got := ContentLinks(l, tt.library, tt.files)
			if !reflect.DeepEqual(got.FileSource, tt.wantSource) {
				t.Errorf("FileSource = %v, want %v", got.FileSource, tt.wantSource)
			}
			if !reflect.DeepEqual(got.Grabbed, tt.wantItems) {
				t.Errorf("Grabbed = %v, want %v", got.Grabbed, tt.wantItems)
			}
		})
	}
}

func TestContentLinksLeavesItsInputAlone(t *testing.T) {
	l := links()
	l.Grabbed["old"] = []int{1}
	l.Queue["queued"] = true

	got := ContentLinks(l, []LibraryFile{{ID: 10, Size: 700 * mb, Items: []int{1, 2}}},
		map[string][]domain.DownloadFile{"old": sized(700 * mb)})

	if len(l.FileSource) != 0 || !reflect.DeepEqual(l.Grabbed, map[string][]int{"old": {1}}) {
		t.Errorf("the input changed: FileSource %v, Grabbed %v", l.FileSource, l.Grabbed)
	}
	if !reflect.DeepEqual(got.Grabbed["old"], []int{1, 2}) || got.FileSource[10] != "old" || !got.Queue["queued"] {
		t.Errorf("result = %+v, want old backing file 10 for items 1 and 2, and the queue kept", got)
	}
}

func TestNeedsContentCheck(t *testing.T) {
	l := links()
	l.Queue["queued"] = true
	l.Grabbed["linked"] = []int{1}
	l.FileSource[10] = "linked"
	l.Grabbed["replaced"] = []int{1}
	x := NewLinkIndex(ManagerFiles{Links: l, FileIDs: []int{10}})

	r := domain.DefaultRules()
	r.ExcludedCategories = []string{"games"}
	r.ExcludedTags = []string{"keep"}

	tests := []struct {
		name string
		d    domain.Download
		want bool
	}{
		{"a download no manager knows", dl("stray"), true},
		{"a download history knows but that backs nothing", dl("replaced"), true},
		{"a download history shows backing a file", dl("linked"), false},
		{"a queued download", dl("queued"), false},
		{"a download in an excluded category", dl("stray", func(d *domain.Download) { d.Category = "games" }), false},
		{"a download with an excluded tag", dl("stray", func(d *domain.Download) { d.Tags = []string{"keep"} }), false},
		{"a download without an ID", dl(""), false},
	}
	for _, tt := range tests {
		if got := NeedsContentCheck(r, x, tt.d); got != tt.want {
			t.Errorf("%s: NeedsContentCheck = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// A torrent linked by content must behave like one linked by history everywhere else: it
// is not listed as an orphan, and a deletion that leaves it backing nothing removes it.
func TestAContentLinkedDownloadIsNotListedAndGoesWithItsItems(t *testing.T) {
	library := []LibraryFile{{ID: 10, Size: 700 * mb, Items: []int{1}}}
	linked := ContentLinks(links(), library, map[string][]domain.DownloadFile{
		"pack":  sized(700 * mb),
		"stray": sized(650 * mb),
	})
	x := NewLinkIndex(ManagerFiles{Links: linked, FileIDs: []int{10}})

	res := Torrents(TorrentsInput{
		Rules:     domain.DefaultRules(),
		Downloads: []domain.Download{dl("pack"), dl("stray")},
		Index:     x,
	})
	if len(res.Candidates) != 1 || res.Candidates[0].Key != "hash:stray" || res.Candidates[0].Reason != domain.ReasonOrphan {
		t.Errorf("listed %+v, want only the stray torrent, as an orphan", res.Candidates)
	}

	if got := x.Removable(linked, []int{1}, []int{10}); !reflect.DeepEqual(got, []string{"pack"}) {
		t.Errorf("deleting the movie and its file removes %v, want [pack]", got)
	}
	if got := x.Removable(linked, []int{1}, nil); got != nil {
		t.Errorf("while the file stays, a deletion removes %v, want nothing", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/scan/`
Expected: a build failure with `undefined: LibraryFile`, `undefined: ContentLinks` and `undefined: NeedsContentCheck`.

- [ ] **Step 3: Write the rule**

Create `internal/scan/content.go`:

```go
package scan

import (
	"sort"

	"github.com/removarr/removarr/internal/domain"
)

// MinContentSize is the smallest file that linking by content compares: 50 MB (spec 6.6).
// Smaller files, such as subtitles and samples, share a size by chance too easily.
const MinContentSize int64 = 50 << 20

// LibraryFile is one current library file and the items it belongs to: one movie, or
// every episode of the file.
type LibraryFile struct {
	ID    int
	Size  int64
	Items []int
}

// NeedsContentCheck reports whether a download's files are needed for linking by content
// (spec 6.6): the rules do not exclude the download, it is not in a queue, and history
// does not show it backing a library file. x must be built from history alone.
func NeedsContentCheck(r domain.Rules, x *LinkIndex, d domain.Download) bool {
	return d.ID != "" && !excluded(r, d) && !x.Queued(d.ID) && x.Backing(d.ID) == 0
}

// ContentLinks returns links extended by content (spec 6.6). A download backs a library
// file when one of the download's files has exactly the library file's size, that size is
// at least MinContentSize, and links names no source for the library file. The download
// is then also associated with the file's items. History takes precedence: a source that
// links names is never replaced. When files of several downloads match one library file,
// the download whose ID sorts first backs it. links itself is not modified.
func ContentLinks(links domain.Links, library []LibraryFile, files map[string][]domain.DownloadFile) domain.Links {
	out := domain.Links{
		Grabbed:    make(map[string][]int, len(links.Grabbed)),
		FileSource: make(map[int]string, len(links.FileSource)),
		Queue:      make(map[string]bool, len(links.Queue)),
	}
	for id, items := range links.Grabbed {
		out.Grabbed[id] = append([]int(nil), items...)
	}
	for file, id := range links.FileSource {
		out.FileSource[file] = id
	}
	for id, queued := range links.Queue {
		out.Queue[id] = queued
	}

	// The library files history names no source for, by size.
	unsourced := map[int64][]LibraryFile{}
	for _, f := range library {
		if f.Size >= MinContentSize && links.FileSource[f.ID] == "" {
			unsourced[f.Size] = append(unsourced[f.Size], f)
		}
	}
	if len(unsourced) == 0 {
		return out
	}

	ids := make([]string, 0, len(files))
	for id := range files {
		if id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		for _, file := range files[id] {
			for _, match := range unsourced[file.Size] {
				if out.FileSource[match.ID] != "" {
					continue // a download that sorts earlier already backs it
				}
				out.FileSource[match.ID] = id
				out.Grabbed[id] = addItems(out.Grabbed[id], match.Items)
			}
		}
	}
	return out
}

// addItems appends the items that are not in have yet, keeping the order.
func addItems(have, items []int) []int {
	for _, item := range items {
		found := false
		for _, h := range have {
			if h == item {
				found = true
				break
			}
		}
		if !found {
			have = append(have, item)
		}
	}
	return have
}
```

- [ ] **Step 4: Run all tests to verify they pass**

Run: `gofmt -l . ; go vet ./... && go test ./...`
Expected: no output from `gofmt` or `go vet`, and `ok` for every package with tests.

- [ ] **Step 5: Commit**

```bash
git add internal/scan
git commit -m "feat: link downloads to library files by content"
```

---

### Task 3: The scanner applies linking by content

**Files:**
- Modify: `internal/scanner/scanner.go`
- Test: `internal/scanner/scanner_test.go`

**Interfaces:**
- Consumes:
  - Task 1: `providers.DownloadClient.Files`; in the test file, `fakeDownloads` with `files`, `filesErr`, `fileCalls`.
  - Task 2: `scan.LibraryFile`, `scan.NeedsContentCheck`, `scan.ContentLinks`.
  - Already in `scanner.go`: `Fetched`, `Source[T]` with `ok()` and `problem(role)`, `(*Service).fetch`, `Compute`, `torrentsProblem`.
- Produces, in package `scanner`:
  - `Fetched` gains `Files map[string][]domain.DownloadFile`: the files of the downloads that needed the content check, by download ID; nil when none was read.
  - `Compute` extends every answering media manager's links by content before it builds the link index, and hands the extended links to the movie and series rules.

What changes in a scan (spec 7.3):

1. The download client and the media managers are read as before.
2. If the download client answered and at least one media manager answered, the scanner asks for the files of each download for which `scan.NeedsContentCheck` is true, using a link index built from history alone.
3. A failure there becomes the download client's error, so every list reports it (spec 6.8, rule 5).
4. `Compute` applies `scan.ContentLinks` per media manager and goes on as before.

- [ ] **Step 1: Write the failing tests**

In `internal/scanner/scanner_test.go`, replace

```go
	"log/slog"
	"strings"
```

with

```go
	"log/slog"
	"reflect"
	"strings"
```

Append to the end of `internal/scanner/scanner_test.go`:

```go
const mb = int64(1) << 20

// manualWorld is world() after a manual import: history no longer links the movie's file
// to torrent h1, but h1 still holds a file of exactly the movie file's size.
func manualWorld() (Providers, *fakeDownloads) {
	p, _ := world()
	movies := p.Movies.(*fakeMovies)
	movies.snap.Movies[0].File = &domain.MediaFile{ID: 10, Size: 700 * mb}
	movies.snap.Links = domain.Links{Grabbed: map[string][]int{}, FileSource: map[int]string{}, Queue: map[string]bool{}}
	downloads := p.Downloads.(*fakeDownloads)
	downloads.files = map[string][]domain.DownloadFile{
		"h1":      {{Name: "The.Matrix.mkv", Size: 700 * mb}, {Name: "The.Matrix.nfo", Size: 900}},
		"orphan1": {{Name: "Unknown.mkv", Size: 650 * mb}},
	}
	return p, downloads
}

func TestRunLinksATorrentToAManuallyImportedFileByContent(t *testing.T) {
	p, downloads := manualWorld()
	st := run(t, p, withUser())

	expect(t, st, domain.ListMovies, 1, "")
	expect(t, st, domain.ListTorrents, 1, "")
	sawMovie := false
	for _, c := range st.candidates {
		switch c.Key {
		case "hash:h1":
			t.Errorf("h1 is listed as %s although it holds the movie's file", c.Reason)
		case "tmdb:603":
			sawMovie = true
			if len(c.Torrents) != 1 || c.Torrents[0].ID != "h1" {
				t.Errorf("deleting the movie would remove torrents %+v, want h1", c.Torrents)
			}
		}
	}
	if !sawMovie {
		t.Error("the movie row is missing")
	}
	if !reflect.DeepEqual(downloads.fileCalls, []string{"h1", "orphan1"}) {
		t.Errorf("files were read for %v, want h1 and orphan1: history links neither", downloads.fileCalls)
	}
}

func TestRunReadsFilesOnlyForDownloadsHistoryDoesNotLink(t *testing.T) {
	p, _ := world() // history links h1 to the movie's file
	downloads := p.Downloads.(*fakeDownloads)
	downloads.list = append(downloads.list,
		domain.Download{ID: "queued1", Name: "Still.Importing"},
		domain.Download{ID: "game1", Name: "A.Game", Category: "games"},
	)
	p.Series.(*fakeSeries).snap.Links.Queue["queued1"] = true
	r := withUser()
	r.ExcludedCategories = []string{"games"}
	run(t, p, r)

	if !reflect.DeepEqual(downloads.fileCalls, []string{"orphan1"}) {
		t.Errorf("files were read for %v, want only orphan1", downloads.fileCalls)
	}
}

// A file list that cannot be read is an incomplete answer from the download client
// (spec 6.8, rule 5): no list may be produced from it.
func TestRunFailsEveryListWhenFilesCannotBeRead(t *testing.T) {
	p, downloads := manualWorld()
	downloads.filesErr = errors.New("boom")
	st := run(t, p, withUser())

	for _, list := range []domain.ListKind{domain.ListMovies, domain.ListSeries, domain.ListTorrents} {
		expect(t, st, list, 0, "download client could not be read: files of a download: boom")
	}
	if len(downloads.fileCalls) != 1 {
		t.Errorf("files were read %d times, want the scan to stop asking after the first failure", len(downloads.fileCalls))
	}
}

func TestRunDoesNotReadFilesWhenNoMediaManagerAnswered(t *testing.T) {
	p, downloads := manualWorld()
	p.Movies = &fakeMovies{err: errors.New("timeout")}
	p.Series = nil
	run(t, p, withUser())

	if len(downloads.fileCalls) != 0 {
		t.Errorf("files were read for %v, want none: without a media manager there is nothing to match", downloads.fileCalls)
	}
}

func TestComputeLinksAMultiEpisodeFileByContent(t *testing.T) {
	file := &domain.MediaFile{ID: 50, Size: 900 * mb}
	f := Fetched{
		Series: Source[domain.SeriesSnapshot]{Configured: true, Value: domain.SeriesSnapshot{
			Series: []domain.Series{{
				ID: 1, Title: "The Show", Ended: true, IDs: domain.ExternalIDs{TVDB: "100"},
				Episodes: []domain.Episode{
					{ID: 11, Season: 1, Number: 1, File: file},
					{ID: 12, Season: 1, Number: 2, File: file},
				},
			}},
			Links: domain.Links{Grabbed: map[string][]int{}, FileSource: map[int]string{}, Queue: map[string]bool{}},
		}},
		Downloads: Source[[]domain.Download]{Configured: true, Value: []domain.Download{{ID: "double", Name: "The.Show.S01E01E02"}}},
		Files:     map[string][]domain.DownloadFile{"double": {{Name: "The.Show.S01E01E02.mkv", Size: 900 * mb}}},
	}

	summary, candidates := Compute(domain.DefaultRules(), now, f)

	if got := summary.Lists[domain.ListTorrents]; got.Error != "" || got.Candidates != 0 || len(candidates) != 0 {
		t.Errorf("torrent list = %+v with rows %+v, want none: the torrent holds the episodes' file", got, candidates)
	}
	if len(f.Series.Value.Links.FileSource) != 0 || len(f.Series.Value.Links.Grabbed) != 0 {
		t.Errorf("Compute changed the links it was given: %+v", f.Series.Value.Links)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/scanner/`
Expected: a build failure with `unknown field Files in struct literal of type Fetched`.

- [ ] **Step 3: Let `Fetched` carry the file lists**

In `internal/scanner/scanner.go`, replace

```go
	Downloads Source[[]domain.Download]
}
```

with

```go
	Downloads Source[[]domain.Download]
	// Files holds the files of the downloads that needed the content check (spec 6.6),
	// by download ID. It is nil when no file list was read.
	Files map[string][]domain.DownloadFile
}
```

- [ ] **Step 4: Read the file lists after the media managers**

In `internal/scanner/scanner.go`, replace

```go
		f.Series.Value, f.Series.Err = p.Series.Series(ctx)
	}
	if p.MediaServer != nil {
```

with

```go
		f.Series.Value, f.Series.Err = p.Series.Series(ctx)
	}
	// Without a media manager's answer there is no library file to match against, and no
	// list that could use the result.
	if f.Downloads.ok() && (f.Movies.ok() || f.Series.ok()) {
		s.setPhase("matching downloads to library files")
		f.Files, f.Downloads.Err = readFiles(ctx, p.Downloads, rules, f)
	}
	if p.MediaServer != nil {
```

- [ ] **Step 5: Add the helpers**

In `internal/scanner/scanner.go`, replace

```go
const (
	roleMediaServer = "media server"
```

with

```go
// readFiles asks the download client for the files of every download that history does
// not link to a library file (spec 6.6). The first failure is returned and nothing else:
// with a partial answer, a download that holds library files would look unlinked.
func readFiles(ctx context.Context, client providers.DownloadClient, rules domain.Rules, f Fetched) (map[string][]domain.DownloadFile, error) {
	history := scan.NewLinkIndex(linkData(&f, nil)...)
	files := map[string][]domain.DownloadFile{}
	for _, d := range f.Downloads.Value {
		if !scan.NeedsContentCheck(rules, history, d) {
			continue
		}
		list, err := client.Files(ctx, d.ID)
		if err != nil {
			return nil, fmt.Errorf("files of a download: %w", err)
		}
		files[d.ID] = list
	}
	return files, nil
}

// linkData returns the link data of every media manager that answered. With files given,
// each manager's links are first extended by content (spec 6.6) and stored back into f, so
// the list rules see the same links the index is built from.
func linkData(f *Fetched, files map[string][]domain.DownloadFile) []scan.ManagerFiles {
	var managers []scan.ManagerFiles
	if f.Movies.ok() {
		library := movieFiles(f.Movies.Value)
		if files != nil {
			f.Movies.Value.Links = scan.ContentLinks(f.Movies.Value.Links, library, files)
		}
		managers = append(managers, scan.ManagerFiles{Links: f.Movies.Value.Links, FileIDs: fileIDs(library)})
	}
	if f.Series.ok() {
		library := episodeFiles(f.Series.Value)
		if files != nil {
			f.Series.Value.Links = scan.ContentLinks(f.Series.Value.Links, library, files)
		}
		managers = append(managers, scan.ManagerFiles{Links: f.Series.Value.Links, FileIDs: fileIDs(library)})
	}
	return managers
}

// movieFiles lists the current movie files with the movie each belongs to.
func movieFiles(s domain.MovieSnapshot) []scan.LibraryFile {
	var out []scan.LibraryFile
	for _, m := range s.Movies {
		if m.File != nil {
			out = append(out, scan.LibraryFile{ID: m.File.ID, Size: m.File.Size, Items: []int{m.ID}})
		}
	}
	return out
}

// episodeFiles lists the current episode files with the episodes each belongs to. The
// episodes of a multi-episode file share one file.
func episodeFiles(s domain.SeriesSnapshot) []scan.LibraryFile {
	var out []scan.LibraryFile
	at := map[int]int{} // file ID -> its position in out
	for _, show := range s.Series {
		for _, e := range show.Episodes {
			if e.File == nil {
				continue
			}
			i, seen := at[e.File.ID]
			if !seen {
				i = len(out)
				at[e.File.ID] = i
				out = append(out, scan.LibraryFile{ID: e.File.ID, Size: e.File.Size})
			}
			out[i].Items = append(out[i].Items, e.ID)
		}
	}
	return out
}

func fileIDs(files []scan.LibraryFile) []int {
	ids := make([]int, 0, len(files))
	for _, f := range files {
		ids = append(ids, f.ID)
	}
	return ids
}

const (
	roleMediaServer = "media server"
```

- [ ] **Step 6: Build the link index from links extended by content**

In `Compute`, two edits. First remove the declaration that is no longer used:

In `internal/scanner/scanner.go`, replace

```go
	var managers []scan.ManagerFiles
	seenFolder := map[string]bool{}
```

with

```go
	seenFolder := map[string]bool{}
```

Then replace the block that built the managers by hand:

In `internal/scanner/scanner.go`, replace

```go
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
```

with

```go
	if f.Movies.ok() {
		addFolders(f.Movies.Value.RootFolders)
	}
	if f.Series.ok() {
		addFolders(f.Series.Value.RootFolders)
	}
	// f is this function's own copy, so extending its links by content stays local.
	index := scan.NewLinkIndex(linkData(&f, f.Files)...)
```

- [ ] **Step 7: Run all tests to verify they pass**

Run: `gofmt -l . ; go vet ./... && go test ./...`
Expected: no output from `gofmt` or `go vet`, and `ok` for every package with tests.

Then run: `go test -race -count=2 ./internal/scanner/`
Expected: `ok  	github.com/removarr/removarr/internal/scanner`, with no race report.

- [ ] **Step 8: Commit**

```bash
git add internal/scanner
git commit -m "feat: apply linking by content in a scan"
```

---

### Task 4: Re-check against the real apps

This task needs the owner. It only reads from Jellyfin, Sonarr, Radarr and qBittorrent. It confirms on the reference stack that torrents holding library files are no longer listed, and records the result in the spec.

The removarr binary cannot reach the owner's local network from an agent session on their machine; the owner runs the scan from their own terminal. The connection details stay in the owner's `.env` and are never printed.

**Files:**
- Modify: `docs/superpowers/specs/2026-11-18-removarr-design.md` (sections 16.1 and 16.5 only)
- Modify, only if a check below fails: the package at fault and its test

**Interfaces:**
- Consumes: the `removarr` binary built from the branch, and its API.
- Produces: spec section 16 updated with the measured result.

- [ ] **Step 1: Build the binary and hand the owner the scan command**

Build the binary from the branch head into a directory outside the repository, with a fresh config directory. Give the owner one command that starts it with their `.env` loaded, tests the four connections, runs one scan, saves the three lists as JSON, and stops it.

- [ ] **Step 2: Read the result**

Expected, all three:

1. No list carries an error.
2. The movie and series lists have the same rows as before this plan: 45 and 18 on the reference stack, unless the library changed in between.
3. The torrent list is shorter than the 68 rows it had before.

- [ ] **Step 3: Compare with an independent content check**

Recompute, outside removarr and from the apps' raw answers, which torrents the rule of spec 6.6 links by content: for every torrent that history does not show backing a file and that is not queued, compare its files of 50 MB or more with the library files history names no source for, by exact size, giving a contested library file to the torrent whose ID sorts first. The set of torrents removarr lists must equal the set this check leaves unlinked.

Any difference is a defect: write a failing test that reproduces it in the package that owns the rule (`internal/scan` for the rule, `internal/scanner` for which downloads are asked, the provider package for misread data), fix it, and repeat this task.

- [ ] **Step 4: Record the result in the spec**

In `docs/superpowers/specs/2026-11-18-removarr-design.md`:

- In section 16.1, after the paragraph that starts "**Decision, 2026-11-19.**", add the measured outcome: how many torrents are listed after linking by content, their total size, how many were linked by content, and how many file-list requests one scan made.
- In section 16.5, update the torrent row of the table and the scan duration if it changed.

Do not include URLs, keys, user names or titles from the owner's library.

- [ ] **Step 5: Commit**

```bash
git status --short
git add docs/superpowers/specs/2026-11-18-removarr-design.md
git commit -m "docs: record the result of linking by content on the reference stack"
```

Before committing, confirm in the `git status` output that neither `.env` nor `config/` is listed.

---

## After this plan

Stage 2 (ignore and delete) can now be planned on torrent lists that mean what they say. Two limits of linking by content stay open, both stated in spec 6.6: of two torrents holding the same file only one is linked, and a torrent that history already links to some library file is not asked for its files.
