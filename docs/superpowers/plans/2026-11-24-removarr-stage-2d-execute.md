# Stage 2d: execute plans and record history — Implementation Plan

Plan gate: waiting for the owner

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `POST /api/plans/{id}/execute` carries out a confirmed plan in Sonarr, Radarr and qBittorrent, item by item, records every item in a history that `GET /api/actions` returns, and removes what was deleted from the stored lists (issue #5). With `REMOVARR_DRY_RUN=true` nothing that changes an app is sent.

**Architecture:** The calls that change an app get interfaces of their own (`providers.MovieWriter`, `SeriesWriter`, `DownloadRemover`), separate from the read interfaces, and only the new `executor` package is given them, so the scanner and the planner cannot change an app. A new `gate` package lets one scan, plan building or execution run at a time. The executor takes the plan out of the store, runs each item (media manager steps, then torrents, the first failure stops the item), decides shared data again from a fresh download list right before each item's torrents, and records each item in the new `actions` table in the same transaction that drops its deleted rows from the latest scan.

**Tech Stack:** Go 1.27, standard library, `modernc.org/sqlite`.

**Spec:** `docs/superpowers/specs/2026-11-18-removarr-design.md`. Read 6.7, 6.8 and the new 6.11 in full before any task; 6.7, 6.8, 6.11, 7.1, 7.2, 7.4, 7.5, 7.6, 8 and 16.2 were amended for this plan (commit `89682ae`).

## Global Constraints

- Go 1.27 and the standard library; the only third-party module is `modernc.org/sqlite`. No new module.
- Only `internal/executor` calls a write interface. The scanner and the planner keep their read-only provider types.
- With dry run on, no request other than GET reaches an app (spec 6.8, rule 6; 6.11).
- `internal/scan` stays pure: no I/O.
- Every API route goes through `guard` in `internal/api/server.go`.
- The store has one database connection. Inside a transaction use the transaction, never `s.db`.
- No guessing from a partial answer (spec 6.8, rule 5): an incomplete or inconsistent answer is an error.
- Secrets are write-only; an error never repeats a URL with a credential. Outbound requests use `httpjson.SameHostRedirects`.
- Never print titles, names, URLs or keys from a real library. Tests use invented data.
- Test first; see it fail for the expected reason. Name tests after what must hold.
- The checks: `gofmt -l .` prints nothing, `go vet ./...`, `go test ./...`, `go test -race ./internal/api/ ./internal/config/ ./internal/store/ ./internal/scanner/ ./internal/scan/ ./internal/planner/ ./internal/executor/ ./internal/gate/`, `go build ./...`.
- Commit subjects: `type: what changed`, lower case, `feat|fix|docs|test|chore`, body says why, ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

- A torrent ID that is empty, `all` or holds a `|` would make qBittorrent delete every torrent or several: `RemoveDownload` refuses any ID that is not lower-case hex and sends nothing (Task 5).
- Two torrents of one plan with the same content path, in two items: the first keeps the data, the second takes it; when the first one's removal fails, the second keeps the data too (Task 6).
- A plan executed twice, for example by a double click: the second request is refused and nothing is sent twice (Tasks 3 and 7).
- Radarr was removed from the settings after the plan was built: the movie item fails with "not configured", in a dry run too, and the next item still runs (Task 6).
- The browser goes away during an execution: the request's context is cancelled, and every item still runs and is recorded (Task 7).

## File Structure

| File | Responsibility |
|---|---|
| `internal/gate/gate.go` (new) | One job at a time: scan, plan, execute |
| `internal/scanner/scanner.go` | Uses the gate; recovers a panicking scan |
| `internal/planner/service.go` | Uses the gate instead of its own mutex |
| `internal/planner/items.go` | Unmonitor before delete; each step names its row |
| `internal/scan/paths.go` (new) | The shared-data path rule, used by planner and executor |
| `internal/planner/torrents.go` | Uses `scan.KeepsData` |
| `internal/domain/plan.go` | `ManagerStep.RowKey` |
| `internal/domain/action.go` (new) | History record types |
| `internal/store/migrations/0004_actions.sql` (new) | `actions` table |
| `internal/store/plans.go` | `TakePlan` replaces `Plan`; expiry from the column |
| `internal/store/actions.go` (new) | `RecordAction`, `Actions` |
| `internal/providers/providers.go` | Write interfaces |
| `internal/httpjson/httpjson.go` | `Do` with method and JSON body |
| `internal/providers/arr/arr.go` | `Send` |
| `internal/providers/radarr/radarr.go`, `sonarr/sonarr.go` | Write calls |
| `internal/providers/qbittorrent/qbittorrent.go` | `RemoveDownload`, POST with re-login |
| `internal/executor/item.go` (new) | Runs one item |
| `internal/executor/service.go` (new) | Runs a plan, records it |
| `internal/app/apps.go` | `Writers` |
| `internal/api/execute.go` (new), `server.go`, `scans.go`, `plans.go` | Routes, 409 for a busy gate |
| `cmd/removarr/main.go` | Wiring |

---

### Task 1: one gate for scans, plans and executions

**Files:**
- Create: `internal/gate/gate.go`, `internal/gate/gate_test.go`
- Modify: `internal/scanner/scanner.go` (`Service`, `Start`, `Run`, `begin`, `finish`; remove `ErrRunning`), `internal/scanner/scanner_test.go`
- Modify: `internal/planner/service.go` (`Service`, `Plan`), `internal/planner/service_test.go`
- Modify: `internal/api/server.go` (add `busy`), `internal/api/scans.go` (`startScan`), `internal/api/plans.go` (`buildPlan`), `internal/api/scans_test.go`, `internal/api/plans_test.go`
- Modify: `cmd/removarr/main.go` (one gate shared)

**Interfaces:**
- Produces:
  - `gate.Scan`, `gate.Plan`, `gate.Execute` (string constants: the message a refused request gets).
  - `var gate.ErrBusy error`; `type gate.BusyError struct{ Job string }` with `Error() string` (returns `Job`) and `Is(error) bool` (true for `ErrBusy`).
  - `type gate.Gate struct` (zero value is free); `func (g *gate.Gate) Enter(job string) (release func(), err error)`.
  - `scanner.Service.Gate *gate.Gate` and `planner.Service.Gate *gate.Gate`: nil means a gate of their own.
  - `func busy(w http.ResponseWriter, err error) bool` in `internal/api/server.go`: writes 409 with the job's message and returns true for a `*gate.BusyError`.

- [ ] **Step 1: Write the failing tests**

`internal/gate/gate_test.go`:

```go
package gate

import (
	"errors"
	"testing"
)

func TestTheGateLetsOneJobInAtATime(t *testing.T) {
	var g Gate
	release, err := g.Enter(Scan)
	if err != nil {
		t.Fatal(err)
	}
	_, err = g.Enter(Execute)
	var busy *BusyError
	if !errors.As(err, &busy) || busy.Job != Scan || !errors.Is(err, ErrBusy) {
		t.Fatalf("second Enter = %v, want a BusyError naming the scan", err)
	}
	if err.Error() != Scan {
		t.Errorf("message = %q, want %q", err.Error(), Scan)
	}
	release()
	if _, err := g.Enter(Plan); err != nil {
		t.Errorf("after release: %v", err)
	}
}

func TestReleasingTwiceDoesNotFreeTheNextJob(t *testing.T) {
	var g Gate
	first, _ := g.Enter(Scan)
	first()
	if _, err := g.Enter(Plan); err != nil {
		t.Fatal(err)
	}
	first() // a second call of the first release must not free the plan's gate
	if _, err := g.Enter(Execute); !errors.Is(err, ErrBusy) {
		t.Errorf("Enter while the plan holds the gate = %v, want ErrBusy", err)
	}
}
```

Append to `internal/scanner/scanner_test.go` (and in `TestOnlyOneScanAtATime` replace both `errors.Is(err, ErrRunning)` with `errors.Is(err, gate.ErrBusy)` and the messages "want ErrRunning" with "want ErrBusy"; add the import `github.com/removarr/removarr/internal/gate`):

```go
func TestAScanIsRefusedWhileThePlannerHoldsTheGate(t *testing.T) {
	p, _ := world()
	svc := service(p, withUser(), &fakeStore{})
	svc.Gate = &gate.Gate{}
	release, err := svc.Gate.Enter(gate.Plan)
	if err != nil {
		t.Fatal(err)
	}
	var busy *gate.BusyError
	if err := svc.Start(); !errors.As(err, &busy) || busy.Job != gate.Plan {
		t.Fatalf("Start = %v, want refused while a plan is built", err)
	}
	if svc.Status().Running {
		t.Error("a refused scan must not show as running")
	}
	release()
	if err := svc.Run(ctx); err != nil {
		t.Errorf("after the plan: %v", err)
	}
}

// panicky is a download client that panics while it is read.
type panicky struct{ fakeDownloads }

func (panicky) Downloads(context.Context) ([]domain.Download, error) { panic("boom") }

func TestAPanickingScanFreesTheGateAndReportsIt(t *testing.T) {
	p, _ := world()
	p.Downloads = panicky{}
	svc := service(p, withUser(), &fakeStore{})
	svc.Gate = &gate.Gate{}
	if err := svc.Start(); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, svc)
	if s := svc.Status(); s.Running || !strings.Contains(s.LastError, "stopped unexpectedly") {
		t.Errorf("status = %+v, want idle with the panic reported", s)
	}
	if err := svc.Run(ctx); err == nil || !strings.Contains(err.Error(), "stopped unexpectedly") {
		t.Errorf("Run = %v, want the panic as an error", err)
	}
	release, err := svc.Gate.Enter(gate.Execute)
	if err != nil {
		t.Fatalf("the gate is still held after a panic: %v", err)
	}
	release()
}
```

If `fakeDownloads` in the scanner tests is used as a pointer (`*fakeDownloads`), embed that type the way the file defines it; the panic in `Downloads` is what matters.

Append to `internal/planner/service_test.go`:

```go
func TestPlanIsRefusedWhileAScanRuns(t *testing.T) {
	f := world()
	st := &fakeStore{latest: store.Latest{Rows: scanned(rules(), f)}}
	svc := service(providersOf(f), st)
	svc.Gate = &gate.Gate{}
	release, _ := svc.Gate.Enter(gate.Scan)
	defer release()
	if _, err := svc.Plan(ctx, []domain.RowRef{sel(domain.ListMovies, "tmdb:603")}); !errors.Is(err, gate.ErrBusy) {
		t.Fatalf("Plan = %v, want refused while a scan runs", err)
	}
	if len(st.saved) != 0 {
		t.Error("no plan may be saved")
	}
}
```

(`ctx` and `sel` exist in the planner tests; if `ctx` does not, use `context.Background()`.)

In `internal/api/scans_test.go` replace `h.scanner.err = scanner.ErrRunning` with `h.scanner.err = &gate.BusyError{Job: gate.Scan}` and check the message too:

```go
	h.scanner.err = &gate.BusyError{Job: gate.Plan}
	rec := h.do(http.MethodPost, "/api/scans", "")
	if rec.Code != http.StatusConflict || decode[errorBody](t, rec).Error != gate.Plan {
		t.Errorf("while a plan is built: %d %s, want 409 naming the plan", rec.Code, rec.Body)
	}
```

Append to `internal/api/plans_test.go`:

```go
func TestBuildPlanWhileBusyIsAConflict(t *testing.T) {
	h := newHarness(t, nil)
	h.planner.err = &gate.BusyError{Job: gate.Scan}
	rec := h.do(http.MethodPost, "/api/plans", ignoreBody("movies/tmdb:1"))
	if rec.Code != http.StatusConflict || decode[errorBody](t, rec).Error != gate.Scan {
		t.Errorf("status = %d %s, want 409 naming the scan", rec.Code, rec.Body)
	}
}
```

- [ ] **Step 2: Run the tests and see them fail**

Run: `go test ./internal/gate/ ./internal/scanner/ ./internal/planner/ ./internal/api/`
Expected: build failures (`gate` does not exist, `Gate` field unknown).

- [ ] **Step 3: Implement**

`internal/gate/gate.go`:

```go
// Package gate lets one scan, plan building or plan execution run at a time (spec 6.11).
// A plan built during a scan would compare against a scan that is about to change, and a
// scan or a plan during an execution would read the apps halfway through a deletion.
package gate

import (
	"errors"
	"sync"
)

// The jobs that share the gate. Each is the message a refused request gets.
const (
	Scan    = "A scan is running."
	Plan    = "A plan is being built."
	Execute = "A plan is being executed."
)

// ErrBusy matches every error Enter returns.
var ErrBusy = errors.New("busy")

// BusyError says which job holds the gate.
type BusyError struct{ Job string }

func (e *BusyError) Error() string        { return e.Job }
func (e *BusyError) Is(target error) bool { return target == ErrBusy }

// Gate admits one job at a time. The zero value is free.
type Gate struct {
	mu  sync.Mutex
	job string
}

// Enter takes the gate for job, or returns a *BusyError naming the job that holds it.
// release frees the gate; calling it again does nothing, so it cannot free a later job.
func (g *Gate) Enter(job string) (release func(), err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.job != "" {
		return nil, &BusyError{Job: g.job}
	}
	g.job = job
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			g.job = ""
			g.mu.Unlock()
		})
	}, nil
}
```

`internal/scanner/scanner.go`: remove `ErrRunning`. Add to `Service`:

```go
	// Gate is shared with the planner and the executor (spec 6.11). Nil gives the scanner a
	// gate of its own.
	Gate *gate.Gate

	own     gate.Gate
	release func() // frees the gate when the running scan finishes
```

and:

```go
func (s *Service) gate() *gate.Gate {
	if s.Gate != nil {
		return s.Gate
	}
	return &s.own
}

func (s *Service) begin() error {
	release, err := s.gate().Enter(gate.Scan)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	started := s.Now()
	s.status = Status{Running: true, Phase: "starting", StartedAt: &started}
	s.release = release
	return nil
}

func (s *Service) finish(err error) {
	s.mu.Lock()
	s.status = Status{}
	if err != nil {
		s.status.LastError = err.Error()
		s.Log.Error("scan failed", "error", err)
	}
	release := s.release
	s.release = nil
	s.mu.Unlock()
	if release != nil {
		release()
	}
}

// runSafely runs a scan and turns a panic into an error, so that the status and the gate
// are released whatever happens.
func (s *Service) runSafely(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("the scan stopped unexpectedly: %v", r)
		}
	}()
	return s.run(ctx)
}
```

In `Start` the goroutine calls `s.finish(s.runSafely(ctx))`; in `Run`, `err := s.runSafely(ctx)`. Update the doc comments of `Start` and `Run`: they return a `*gate.BusyError` when the gate is held.

`internal/planner/service.go`: replace `mu sync.Mutex` with

```go
	// Gate is shared with the scanner and the executor (spec 6.11). Nil gives the planner a
	// gate of its own.
	Gate *gate.Gate
	own  gate.Gate
```

and start `Plan` with

```go
	g := s.Gate
	if g == nil {
		g = &s.own
	}
	release, err := g.Enter(gate.Plan)
	if err != nil {
		return domain.Plan{}, err
	}
	defer release()
```

Update its doc comment: a plan is refused while a scan, another plan or an execution runs.

`internal/api/server.go`:

```go
// busy answers 409 with the job that holds the gate (spec 6.11). It reports whether err was
// such a refusal.
func busy(w http.ResponseWriter, err error) bool {
	var b *gate.BusyError
	if !errors.As(err, &b) {
		return false
	}
	writeError(w, http.StatusConflict, b.Job)
	return true
}
```

In `startScan` replace the `errors.Is(err, scanner.ErrRunning)` branch with `if busy(w, err) { return }`; in `buildPlan` add the same check before `s.fail`.

`cmd/removarr/main.go`: create `g := &gate.Gate{}` before the services and set `Gate: g` on `scans` and `plans`.

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./internal/gate/ ./internal/scanner/ ./internal/planner/ ./internal/api/ ./cmd/... && go test -race ./internal/gate/ ./internal/scanner/ ./internal/planner/`
Expected: PASS.

- [ ] **Step 5: Break it once**

In a scratch change, delete the `recover` in `runSafely` and run `go test ./internal/scanner/ -run Panicking`: the test must fail (the test binary panics). Restore it.

- [ ] **Step 6: Commit**

```bash
git add internal/gate internal/scanner internal/planner/service.go internal/planner/service_test.go internal/api cmd/removarr/main.go
git commit -m "feat: let one scan, plan or execution run at a time"
```

Body: why (spec 6.11; carried over from #4's reviews: a scan and a deletion never at the same time, and a panic must not leave "running" set).

---

### Task 2: plans unmonitor before deleting, and each step names its row

**Files:**
- Create: `internal/scan/paths.go`, `internal/scan/paths_test.go`
- Modify: `internal/planner/torrents.go` (remove `normalPath`, `overlaps`, `within`; `sharesData` uses `scan.KeepsData`)
- Modify: `internal/domain/plan.go` (`ManagerStep.RowKey`), `internal/planner/items.go` (`steps`)
- Test: `internal/planner/items_test.go`

**Interfaces:**
- Produces:
  - `func scan.NormalPath(p string) string`, `func scan.PathsOverlap(a, b string) bool` (both on normalised paths), `func scan.KeepsData(path string, others []string) bool`.
  - `domain.ManagerStep.RowKey string` with tag `json:"rowKey"`: the key of the row the step belongs to. With `Manager`, it names the row: `movies` → list `movies`, `series` → list `series`.
  - Step order of a season row or a running series row: `unmonitor-episodes`, `unmonitor-seasons`, `delete-episode-files`.

- [ ] **Step 1: Write the failing tests**

`internal/scan/paths_test.go`:

```go
package scan

import "testing"

func TestKeepsDataWhenUnsure(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		others []string
		want   bool
	}{
		{"nothing else stays", "/dl/one", nil, false},
		{"its own path is unknown", "", []string{"/dl/two"}, true},
		{"a path of separators only is unknown", "//", nil, true},
		{"another path is unknown", "/dl/one", []string{""}, true},
		{"the same path", "/dl/one", []string{"/dl/one"}, true},
		{"the same path in another case", "/dl/One", []string{"/DL/one/"}, true},
		{"the same path with Windows separators", `C:\dl\one`, []string{"c:/dl//one"}, true},
		{"inside another", "/dl/pack/one.mkv", []string{"/dl/pack"}, true},
		{"holding another", "/dl/pack", []string{"/dl/pack/one.mkv"}, true},
		{"only the same letters at the start", "/dl/one", []string{"/dl/one-two"}, false},
	}
	for _, c := range cases {
		if got := KeepsData(c.path, c.others); got != c.want {
			t.Errorf("%s: KeepsData(%q, %q) = %v, want %v", c.name, c.path, c.others, got, c.want)
		}
	}
}
```

In `internal/planner/items_test.go` change the expected steps of `TestMakeDeletesTheFilesOfARunningSeries`, `TestMakeDeletesTheFilesOfASeasonRow` and `TestMakeUnmonitorsOnlyTheFinishedSeasonsOfTheRow` to the new order, each with its row key (`"tvdb:100"` for the series row, `"tvdb:100:s1"` for the season row):

```go
	want := []domain.ManagerStep{
		{Manager: scan.ManagerSeries, Action: domain.StepUnmonitorEpisodes, ItemID: 1, EpisodeIDs: []int{11, 12}, RowKey: "tvdb:100"},
		{Manager: scan.ManagerSeries, Action: domain.StepUnmonitorSeasons, ItemID: 1, Seasons: []int{1}, RowKey: "tvdb:100"},
		{Manager: scan.ManagerSeries, Action: domain.StepDeleteEpisodeFiles, ItemID: 1, FileIDs: []int{111, 112}, RowKey: "tvdb:100"},
	}
```

Every other expected `ManagerStep` in the planner tests gets the `RowKey` of its row (for example `RowKey: "tmdb:603"` for the movie in `world()`). Add:

```go
func TestMakeUnmonitorsBeforeDeleting(t *testing.T) {
	f := world()
	running(&f, true)
	item := onlyItem(t)(same(t, rules(), f, nil, theShow))
	deleted := -1
	for i, s := range item.Steps {
		if s.Action == domain.StepDeleteEpisodeFiles {
			deleted = i
		}
	}
	if deleted != len(item.Steps)-1 {
		t.Errorf("steps = %+v, want the files deleted last, after every unmonitor", item.Steps)
	}
}
```

- [ ] **Step 2: Run the tests and see them fail**

Run: `go test ./internal/scan/ ./internal/planner/`
Expected: `KeepsData` undefined; `RowKey` unknown field.

- [ ] **Step 3: Implement**

`internal/scan/paths.go`: move `normalPath`, `overlaps` and `within` from `internal/planner/torrents.go` with their comments, exported as `NormalPath` and `PathsOverlap` (`within` stays unexported), and add:

```go
// KeepsData reports whether a download with content path path must keep its data when it is
// removed while downloads with the content paths others stay (spec 6.7, shared data). Unsure
// means keep: an unknown path, its own or one that stays, keeps the data.
func KeepsData(path string, others []string) bool {
	mine := NormalPath(path)
	if mine == "" {
		return true
	}
	for _, o := range others {
		if other := NormalPath(o); other == "" || PathsOverlap(mine, other) {
			return true
		}
	}
	return false
}
```

`internal/planner/torrents.go`:

```go
func (m *maker) sharesData(id string, removed map[string]bool) bool {
	var others []string
	for _, d := range m.in.Eval.Fetched.Downloads.Value {
		if !removed[d.ID] {
			others = append(others, d.ContentPath)
		}
	}
	return scan.KeepsData(m.downloads[id].ContentPath, others)
}
```

`internal/domain/plan.go`, in `ManagerStep` after `Action`:

```go
	RowKey       string `json:"rowKey"`                 // the row the step belongs to, in the list of Manager
```

`internal/planner/items.go`, `steps`: set `RowKey: c.Key` on every step, and in the `default` branch append the steps in the order unmonitor episodes, unmonitor seasons, delete files. Extend the function comment: "Unmonitoring comes before deleting (spec 6.7): a failed deletion then leaves files that are not downloaded again, never deleted episodes that are."

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./internal/scan/ ./internal/planner/ ./internal/store/ ./internal/api/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/scan/paths.go internal/scan/paths_test.go internal/planner internal/domain/plan.go
git commit -m "feat: unmonitor before deleting files, and name each step's row"
```

---

### Task 3: history records and taking a plan from the store

**Files:**
- Create: `internal/domain/action.go`, `internal/store/migrations/0004_actions.sql`, `internal/store/actions.go`, `internal/store/actions_test.go`
- Modify: `internal/store/plans.go` (replace `Plan` with `TakePlan`), `internal/store/plans_test.go`

**Interfaces:**
- Produces:

```go
// internal/domain/action.go
package domain

import "time"

// Outcomes of an executed item (spec 6.11).
const (
	OutcomeDone   = "done"
	OutcomeFailed = "failed"
	OutcomeDryRun = "dry-run"
)

// Results of one step (spec 6.11).
const (
	ResultDone    = "done"
	ResultFailed  = "failed"
	ResultSkipped = "skipped" // not sent: an earlier step of the item failed
	ResultGone    = "gone"    // the torrent was no longer in the download client
	ResultDryRun  = "dry-run" // dry run: it would have been sent
)

// Action is the history record of one executed plan item (spec 5.5, 6.11).
type Action struct {
	ID      int64        `json:"id"`
	PlanID  string       `json:"planId"`
	At      time.Time    `json:"at"`
	DryRun  bool         `json:"dryRun"`
	Outcome string       `json:"outcome"` // one of the Outcome constants
	Size    int64        `json:"size"`    // the sizes of its rows together
	Rows    []ActionRow  `json:"rows"`
	Steps   []StepResult `json:"steps"` // media manager steps first, then torrents, in the order they ran
}

// ActionRow is one row of an executed item.
type ActionRow struct {
	List   ListKind `json:"list"`
	Key    string   `json:"key"`
	Title  string   `json:"title"`
	Season int      `json:"season,omitempty"`
	Size   int64    `json:"size"`
	Added  bool     `json:"added,omitempty"` // not selected: tied to a selected row
}

// StepResult is one step of an executed item: a media manager step or a torrent.
type StepResult struct {
	Manager  *ManagerStep `json:"manager,omitempty"`
	Torrent  *TorrentInfo `json:"torrent,omitempty"`
	KeepData bool         `json:"keepData,omitempty"` // torrent: removed, or would be, without its data
	Result   string       `json:"result"`             // one of the Result constants
	Error    string       `json:"error,omitempty"`
}
```

  - `func (s *store.Store) TakePlan(ctx context.Context, id string, now time.Time) (domain.Plan, error)`: returns the plan and deletes every stored plan, in one transaction. `ErrPlanNotFound` when there is none, `ErrPlanExpired` when its `expires_at` column is at or before `now`; both change nothing.
  - `func (s *store.Store) RecordAction(ctx context.Context, a domain.Action, drop []domain.RowRef) (int64, error)`: inserts `a` and deletes the rows named by `drop` (by list and key) from the latest finished scan, in one transaction; returns the new ID.
  - `func (s *store.Store) Actions(ctx context.Context, before int64, limit int) ([]domain.Action, error)`: newest first; `before` 0 means from the newest, otherwise IDs below it; never nil.

- [ ] **Step 1: Write the failing tests**

Replace the tests of `Plan` in `internal/store/plans_test.go`:

```go
func TestTakingAPlanReturnsItAndDropsEveryPlan(t *testing.T) {
	s := open(t)
	built := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	want := testPlan("abc", built)
	for _, p := range []domain.Plan{want, testPlan("other", built)} {
		if err := s.SavePlan(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.TakePlan(ctx, "abc", built.Add(domain.PlanLifetime-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("took %+v, want %+v", got, want)
	}
	if n := count(t, s, `SELECT COUNT(*) FROM plans`); n != 0 {
		t.Errorf("%d plans left, want none: the other plan is discarded too", n)
	}
	if _, err := s.TakePlan(ctx, "abc", built); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("taking it again: err = %v, want ErrPlanNotFound", err)
	}
}

func TestAnExpiredPlanCannotBeTakenAndStays(t *testing.T) {
	s := open(t)
	built := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	if err := s.SavePlan(ctx, testPlan("abc", built)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TakePlan(ctx, "abc", built.Add(domain.PlanLifetime)); !errors.Is(err, ErrPlanExpired) {
		t.Fatalf("at ten minutes: err = %v, want ErrPlanExpired", err)
	}
	if n := count(t, s, `SELECT COUNT(*) FROM plans`); n != 1 {
		t.Errorf("%d plans, want the refused plan left as it was", n)
	}
}

func TestTheStoredExpiryColumnDecides(t *testing.T) {
	s := open(t)
	built := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	if err := s.SavePlan(ctx, testPlan("abc", built)); err != nil {
		t.Fatal(err)
	}
	// The JSON still says ten minutes; the column says one.
	if _, err := s.db.ExecContext(ctx, `UPDATE plans SET expires_at = ?`, built.Add(time.Minute).UnixNano()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TakePlan(ctx, "abc", built.Add(2*time.Minute)); !errors.Is(err, ErrPlanExpired) {
		t.Errorf("err = %v, want ErrPlanExpired from the column", err)
	}
}

func TestAnUnknownPlanIsNotFound(t *testing.T) {
	if _, err := open(t).TakePlan(ctx, "nope", time.Now()); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("err = %v, want ErrPlanNotFound", err)
	}
}
```

Keep `TestSavingAPlanDropsThePlansThatExpired`, reading with `count` or `TakePlan` instead of `Plan`.

`internal/store/actions_test.go`:

```go
package store

import (
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

func action(planID string, at time.Time) domain.Action {
	return domain.Action{
		PlanID: planID, At: at, Outcome: domain.OutcomeDone, Size: 700,
		Rows: []domain.ActionRow{{List: domain.ListMovies, Key: "tmdb:1", Title: "One", Size: 700}},
		Steps: []domain.StepResult{
			{Manager: &domain.ManagerStep{Manager: "movies", Action: domain.StepRemoveMovie, ItemID: 1, RowKey: "tmdb:1"}, Result: domain.ResultDone},
			{Torrent: &domain.TorrentInfo{ID: "h1", Name: "One.Torrent"}, Result: domain.ResultDone},
		},
	}
}

func TestRecordActionStoresItAndDropsTheDeletedRowsFromTheLatestScan(t *testing.T) {
	s := open(t)
	at := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	id, err := s.BeginScan(ctx, at)
	if err != nil {
		t.Fatal(err)
	}
	rows := []domain.Candidate{
		{List: domain.ListMovies, Key: "tmdb:1", Torrents: []domain.TorrentInfo{}},
		{List: domain.ListMovies, Key: "tmdb:2", Torrents: []domain.TorrentInfo{}},
		{List: domain.ListTorrents, Key: "hash:h1", Torrents: []domain.TorrentInfo{}},
	}
	if err := s.FinishScan(ctx, id, at, domain.ScanSummary{}, rows); err != nil {
		t.Fatal(err)
	}
	a := action("p1", at)
	got, err := s.RecordAction(ctx, a, []domain.RowRef{{List: domain.ListMovies, Key: "tmdb:1"}, {List: domain.ListTorrents, Key: "hash:h1"}})
	if err != nil {
		t.Fatal(err)
	}
	latest, err := s.ReadLatest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(latest.Rows) != 1 || latest.Rows[0].Key != "tmdb:2" {
		t.Errorf("rows left = %+v, want only tmdb:2", latest.Rows)
	}
	list, err := s.Actions(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	a.ID = got
	if len(list) != 1 || !reflect.DeepEqual(list[0], a) {
		t.Errorf("history = %+v, want %+v", list, a)
	}
}

func TestRecordActionWithoutAScanStillRecords(t *testing.T) {
	s := open(t)
	if _, err := s.RecordAction(ctx, action("p1", time.Now().UTC()), []domain.RowRef{{List: domain.ListMovies, Key: "tmdb:1"}}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT COUNT(*) FROM actions`); n != 1 {
		t.Errorf("%d records, want 1", n)
	}
}

func TestActionsAreNewestFirstAndPaged(t *testing.T) {
	s := open(t)
	at := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	var ids []int64
	for i := 0; i < 5; i++ {
		id, err := s.RecordAction(ctx, action("p1", at.Add(time.Duration(i)*time.Minute)), nil)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	page, err := s.Actions(ctx, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page[0].ID != ids[4] || page[1].ID != ids[3] {
		t.Fatalf("first page = %v, want the two newest", idsOf(page))
	}
	page, _ = s.Actions(ctx, page[1].ID, 2)
	if len(page) != 2 || page[0].ID != ids[2] || page[1].ID != ids[1] {
		t.Errorf("second page = %v, want the next two", idsOf(page))
	}
	if empty, _ := s.Actions(ctx, ids[0], 2); empty == nil || len(empty) != 0 {
		t.Errorf("after the oldest = %#v, want an empty list", empty)
	}
}

func idsOf(as []domain.Action) []int64 {
	var out []int64
	for _, a := range as {
		out = append(out, a.ID)
	}
	return out
}
```

Append to `internal/domain/json_test.go` a test that a `StepResult` for a torrent has no `manager` key and a `StepResult` for a step has no `torrent` key (use the file's `keys` and `has` helpers).

- [ ] **Step 2: Run the tests and see them fail**

Run: `go test ./internal/store/ ./internal/domain/`
Expected: `TakePlan`, `RecordAction`, `Actions` undefined; `domain.Action` undefined.

- [ ] **Step 3: Implement**

`internal/store/migrations/0004_actions.sql`:

```sql
CREATE TABLE actions (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  plan_id     TEXT NOT NULL,
  executed_at TEXT NOT NULL,
  dry_run     INTEGER NOT NULL,
  outcome     TEXT NOT NULL,
  size        INTEGER NOT NULL,
  data        TEXT NOT NULL -- the whole record as JSON
);
```

`internal/store/plans.go`: remove `Plan`; add

```go
// TakePlan returns the stored plan with this ID and drops every stored plan, so that a plan
// runs once and the other pending plans, built from data the execution changes, are rebuilt
// (spec 6.11). The expires_at column decides whether the plan can still run. An unknown or
// expired plan is refused with ErrPlanNotFound or ErrPlanExpired and nothing changes.
func (s *Store) TakePlan(ctx context.Context, id string, now time.Time) (domain.Plan, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Plan{}, err
	}
	defer tx.Rollback()
	var (
		data    string
		expires int64
	)
	err = tx.QueryRowContext(ctx, `SELECT data, expires_at FROM plans WHERE id = ?`, id).Scan(&data, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Plan{}, ErrPlanNotFound
	}
	if err != nil {
		return domain.Plan{}, err
	}
	if expires <= now.UnixNano() {
		return domain.Plan{}, ErrPlanExpired
	}
	var p domain.Plan
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return domain.Plan{}, fmt.Errorf("plan %s: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM plans`); err != nil {
		return domain.Plan{}, err
	}
	return p, tx.Commit()
}
```

Update the comment of `ErrPlanNotFound`: "no such plan, or it was executed or discarded".

`internal/store/actions.go`:

```go
package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/removarr/removarr/internal/domain"
)

// RecordAction stores the record of one executed item and drops the rows it deleted from the
// latest finished scan, in one transaction, so that the lists and the history always agree
// (spec 6.11). It returns the record's ID.
func (s *Store) RecordAction(ctx context.Context, a domain.Action, drop []domain.RowRef) (int64, error) {
	data, err := json.Marshal(a)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO actions (plan_id, executed_at, dry_run, outcome, size, data) VALUES (?, ?, ?, ?, ?, ?)`,
		a.PlanID, formatTime(a.At), a.DryRun, a.Outcome, a.Size, string(data))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if len(drop) > 0 {
		scan, err := latestScan(ctx, tx)
		if err != nil {
			return 0, err
		}
		if scan != nil {
			for _, r := range drop {
				if _, err := tx.ExecContext(ctx, `DELETE FROM candidates WHERE scan_id = ? AND list = ? AND key = ?`, scan.ID, string(r.List), r.Key); err != nil {
					return 0, err
				}
			}
		}
	}
	return id, tx.Commit()
}

// Actions returns history records newest first: at most limit of them, with IDs below before,
// or from the newest when before is 0. It never returns nil.
func (s *Store) Actions(ctx context.Context, before int64, limit int) ([]domain.Action, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, data FROM actions WHERE ? = 0 OR id < ? ORDER BY id DESC LIMIT ?`, before, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Action{}
	for rows.Next() {
		var (
			id   int64
			data string
		)
		if err := rows.Scan(&id, &data); err != nil {
			return nil, err
		}
		var a domain.Action
		if err := json.Unmarshal([]byte(data), &a); err != nil {
			return nil, fmt.Errorf("action %d: %w", id, err)
		}
		a.ID = id
		out = append(out, a)
	}
	return out, rows.Err()
}
```

If `latestScan` filters on something other than a finished scan, use it as it is: it is what `ReadLatest` reads, and the drop must hit the rows the lists show.

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./internal/store/ ./internal/domain/ && go test -race ./internal/store/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/action.go internal/domain/json_test.go internal/store
git commit -m "feat: store the history and take a plan out of the store once"
```

Body: one expiry source (carried over from #4), a plan runs once and discards the others (spec 6.11).

---

### Task 4: Radarr and Sonarr write calls

**Files:**
- Modify: `internal/providers/providers.go` (write interfaces), `internal/httpjson/httpjson.go` (`Do`), `internal/httpjson/httpjson_test.go`
- Modify: `internal/providers/arr/arr.go` (`Send`)
- Modify: `internal/providers/radarr/radarr.go`, `internal/providers/radarr/radarr_test.go`
- Modify: `internal/providers/sonarr/sonarr.go`, `internal/providers/sonarr/sonarr_test.go`

**Interfaces:**
- Produces:

```go
// internal/providers/providers.go

// MovieWriter changes a movie manager. Only the executor is given one (spec 7.2).
type MovieWriter interface {
	// RemoveMovie removes the movie and its files, and adds it to the import exclusion list
	// when addExclusion is set.
	RemoveMovie(ctx context.Context, id int, addExclusion bool) error
}

// SeriesWriter changes a series manager. Only the executor is given one (spec 7.2).
type SeriesWriter interface {
	RemoveSeries(ctx context.Context, id int, addExclusion bool) error
	DeleteEpisodeFiles(ctx context.Context, fileIDs []int) error
	UnmonitorEpisodes(ctx context.Context, episodeIDs []int) error
	UnmonitorSeasons(ctx context.Context, seriesID int, seasons []int) error
}

// DownloadRemover removes downloads from the download client. Only the executor is given one.
type DownloadRemover interface {
	// RemoveDownload removes the download, and its data when deleteData is set.
	RemoveDownload(ctx context.Context, id string, deleteData bool) error
}
```

  - `func httpjson.Do(ctx context.Context, c *http.Client, method, rawURL string, header http.Header, body, out any) error`: `body` nil sends none, otherwise it is sent as JSON with `Content-Type: application/json`; `out` nil skips decoding. `Get` becomes `Do` with `http.MethodGet` and no body.
  - `func (c *arr.Client) Send(ctx context.Context, method, path string, q url.Values, body, out any) error`.
  - `*radarr.Client` implements `providers.MovieWriter`; `*sonarr.Client` implements `providers.SeriesWriter` (add both to the `var _` blocks).

The requests, from Sonarr 4.0.12 and Radarr 5.18.4 (spec 16.2):

| Call | Request |
|---|---|
| `RemoveMovie(id, excl)` | `DELETE /api/v3/movie/{id}?addImportExclusion={excl}&deleteFiles=true` |
| `RemoveSeries(id, excl)` | `DELETE /api/v3/series/{id}?addImportListExclusion={excl}&deleteFiles=true` |
| `DeleteEpisodeFiles(ids)` | `DELETE /api/v3/episodefile/bulk`, body `{"episodeFileIds":[…]}` |
| `UnmonitorEpisodes(ids)` | `PUT /api/v3/episode/monitor`, body `{"episodeIds":[…],"monitored":false}` |
| `UnmonitorSeasons(id, seasons)` | `GET /api/v3/series/{id}`, set `monitored` to `false` on the named seasons, `PUT /api/v3/series/{id}` with the whole object as it came, every other field untouched |

- [ ] **Step 1: Write the failing tests**

`internal/httpjson/httpjson_test.go`:

```go
func TestDoSendsTheMethodAndAJSONBody(t *testing.T) {
	var method, ctype, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		method, ctype, body = r.Method, r.Header.Get("Content-Type"), string(b)
	}))
	defer srv.Close()
	if err := Do(context.Background(), srv.Client(), http.MethodPut, srv.URL, nil, map[string]any{"ids": []int{1, 2}}, nil); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPut || ctype != "application/json" || body != `{"ids":[1,2]}` {
		t.Errorf("got %s %q %s", method, ctype, body)
	}
}
```

(Trim a trailing newline from `body` if the encoder adds one, or compare with `strings.TrimSpace`.)

`internal/providers/radarr/radarr_test.go`:

```go
func TestRemoveMovieDeletesItsFilesAndAddsTheExclusionWhenAsked(t *testing.T) {
	for _, excl := range []bool{true, false} {
		var got string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.Method + " " + r.URL.Path + "?" + r.URL.RawQuery + " key=" + r.Header.Get("X-Api-Key")
		}))
		c := New(srv.URL, "k", srv.Client())
		if err := c.RemoveMovie(context.Background(), 7, excl); err != nil {
			t.Fatal(err)
		}
		srv.Close()
		want := fmt.Sprintf("DELETE /api/v3/movie/7?addImportExclusion=%v&deleteFiles=true key=k", excl)
		if got != want {
			t.Errorf("request = %q, want %q", got, want)
		}
	}
}

func TestRemoveMovieReportsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Movie not found", http.StatusNotFound)
	}))
	defer srv.Close()
	if err := New(srv.URL, "k", srv.Client()).RemoveMovie(context.Background(), 7, false); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v, want the 404", err)
	}
}
```

`internal/providers/sonarr/sonarr_test.go`:

```go
// recorder serves Sonarr's write endpoints and records each request as
// "METHOD path?query body".
type recorder struct {
	t      *testing.T
	series string // body of GET /api/v3/series/1
	got    []string
}

func (rec *recorder) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		line := r.Method + " " + r.URL.Path
		if r.URL.RawQuery != "" {
			line += "?" + r.URL.RawQuery
		}
		if len(b) > 0 {
			line += " " + strings.TrimSpace(string(b))
		}
		rec.got = append(rec.got, line)
		if r.Method == http.MethodGet && r.URL.Path == "/api/v3/series/1" {
			fmt.Fprint(w, rec.series)
		}
	}))
}

func TestSonarrWriteRequests(t *testing.T) {
	rec := &recorder{t: t}
	srv := rec.server()
	defer srv.Close()
	c := New(srv.URL, "k", srv.Client())
	bg := context.Background()
	if err := c.RemoveSeries(bg, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteEpisodeFiles(bg, []int{111, 112}); err != nil {
		t.Fatal(err)
	}
	if err := c.UnmonitorEpisodes(bg, []int{11, 12}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"DELETE /api/v3/series/1?addImportListExclusion=true&deleteFiles=true",
		`DELETE /api/v3/episodefile/bulk {"episodeFileIds":[111,112]}`,
		`PUT /api/v3/episode/monitor {"episodeIds":[11,12],"monitored":false}`,
	}
	if !reflect.DeepEqual(rec.got, want) {
		t.Errorf("requests =\n%s\nwant\n%s", strings.Join(rec.got, "\n"), strings.Join(want, "\n"))
	}
}

func TestUnmonitorSeasonsChangesOnlyTheNamedSeasonsAndKeepsEveryOtherField(t *testing.T) {
	rec := &recorder{t: t, series: `{"id":1,"title":"The Show","qualityProfileId":4,"unknownField":{"a":[1]},
		"seasons":[{"seasonNumber":0,"monitored":false},{"seasonNumber":1,"monitored":true,"statistics":{"x":1}},{"seasonNumber":2,"monitored":true}]}`}
	srv := rec.server()
	defer srv.Close()
	if err := New(srv.URL, "k", srv.Client()).UnmonitorSeasons(context.Background(), 1, []int{1}); err != nil {
		t.Fatal(err)
	}
	if len(rec.got) != 2 || rec.got[0] != "GET /api/v3/series/1" || !strings.HasPrefix(rec.got[1], "PUT /api/v3/series/1 ") {
		t.Fatalf("requests = %q, want a GET and a PUT of series 1", rec.got)
	}
	var sent struct {
		Title            string          `json:"title"`
		QualityProfileID int             `json:"qualityProfileId"`
		Unknown          json.RawMessage `json:"unknownField"`
		Seasons          []struct {
			Number     int             `json:"seasonNumber"`
			Monitored  bool            `json:"monitored"`
			Statistics json.RawMessage `json:"statistics"`
		} `json:"seasons"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(rec.got[1], "PUT /api/v3/series/1 ")), &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Title != "The Show" || sent.QualityProfileID != 4 || string(sent.Unknown) != `{"a":[1]}` {
		t.Errorf("the series lost fields: %+v", sent)
	}
	monitored := map[int]bool{}
	for _, s := range sent.Seasons {
		monitored[s.Number] = s.Monitored
	}
	if !reflect.DeepEqual(monitored, map[int]bool{0: false, 1: false, 2: true}) || string(sent.Seasons[1].Statistics) != `{"x":1}` {
		t.Errorf("seasons sent = %+v, want only season 1 changed", sent.Seasons)
	}
}

func TestUnmonitorSeasonsRefusesASeasonSonarrDoesNotList(t *testing.T) {
	rec := &recorder{t: t, series: `{"id":1,"seasons":[{"seasonNumber":1,"monitored":true}]}`}
	srv := rec.server()
	defer srv.Close()
	err := New(srv.URL, "k", srv.Client()).UnmonitorSeasons(context.Background(), 1, []int{1, 3})
	if err == nil || !strings.Contains(err.Error(), "season 3") {
		t.Errorf("err = %v, want the missing season named", err)
	}
	if len(rec.got) != 1 {
		t.Errorf("requests = %q, want no PUT", rec.got)
	}
}

func TestUnmonitorSeasonsRefusesASeriesWithoutSeasons(t *testing.T) {
	rec := &recorder{t: t, series: `{"id":1}`}
	srv := rec.server()
	defer srv.Close()
	if err := New(srv.URL, "k", srv.Client()).UnmonitorSeasons(context.Background(), 1, []int{1}); err == nil {
		t.Error("want an error for an answer without seasons")
	}
	if len(rec.got) != 1 {
		t.Errorf("requests = %q, want no PUT", rec.got)
	}
}
```

- [ ] **Step 2: Run the tests and see them fail**

Run: `go test ./internal/httpjson/ ./internal/providers/...`
Expected: `Do`, `RemoveMovie`, `RemoveSeries`, … undefined.

- [ ] **Step 3: Implement**

`internal/httpjson/httpjson.go`: move the body of `Get` into `Do`:

```go
// Do sends a request and decodes the JSON response into out. A nil body sends none; any other
// body is sent as JSON. A nil out skips decoding. Credentials belong in header, never in
// rawURL, so they cannot end up in error messages.
func Do(ctx context.Context, c *http.Client, method, rawURL string, header http.Header, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return err
	}
	for name, values := range header {
		req.Header[name] = values
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// … the rest of the former Get, unchanged
}

// Get sends a GET request and decodes the JSON response into out. A nil out skips decoding.
func Get(ctx context.Context, c *http.Client, rawURL string, header http.Header, out any) error {
	return Do(ctx, c, http.MethodGet, rawURL, header, nil, out)
}
```

`internal/providers/arr/arr.go`:

```go
// Send sends a request with an optional JSON body and decodes the JSON response into out.
// It does not change q.
func (c *Client) Send(ctx context.Context, method, path string, q url.Values, body, out any) error {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return httpjson.Do(ctx, c.HTTP, method, u, http.Header{"X-Api-Key": {c.APIKey}}, body, out)
}
```

and let `Get` call `c.Send(ctx, http.MethodGet, path, q, nil, out)`.

`internal/providers/radarr/radarr.go` (package comment: "reads movies from Radarr and removes them"):

```go
// RemoveMovie removes the movie with its file (spec 6.7).
func (c *Client) RemoveMovie(ctx context.Context, id int, addExclusion bool) error {
	q := url.Values{"deleteFiles": {"true"}, "addImportExclusion": {strconv.FormatBool(addExclusion)}}
	if err := c.arr.Send(ctx, http.MethodDelete, "/api/v3/movie/"+strconv.Itoa(id), q, nil, nil); err != nil {
		return fmt.Errorf("remove movie: %w", err)
	}
	return nil
}
```

`internal/providers/sonarr/sonarr.go` (package comment: "reads series from Sonarr and changes them as a plan says"):

```go
// RemoveSeries removes the series with its files, specials included (spec 6.7).
func (c *Client) RemoveSeries(ctx context.Context, id int, addExclusion bool) error {
	q := url.Values{"deleteFiles": {"true"}, "addImportListExclusion": {strconv.FormatBool(addExclusion)}}
	if err := c.arr.Send(ctx, http.MethodDelete, "/api/v3/series/"+strconv.Itoa(id), q, nil, nil); err != nil {
		return fmt.Errorf("remove series: %w", err)
	}
	return nil
}

// DeleteEpisodeFiles deletes these episode files in one request.
func (c *Client) DeleteEpisodeFiles(ctx context.Context, fileIDs []int) error {
	body := struct {
		IDs []int `json:"episodeFileIds"`
	}{fileIDs}
	if err := c.arr.Send(ctx, http.MethodDelete, "/api/v3/episodefile/bulk", nil, body, nil); err != nil {
		return fmt.Errorf("delete episode files: %w", err)
	}
	return nil
}

// UnmonitorEpisodes unmonitors these episodes.
func (c *Client) UnmonitorEpisodes(ctx context.Context, episodeIDs []int) error {
	body := struct {
		IDs       []int `json:"episodeIds"`
		Monitored bool  `json:"monitored"`
	}{episodeIDs, false}
	if err := c.arr.Send(ctx, http.MethodPut, "/api/v3/episode/monitor", nil, body, nil); err != nil {
		return fmt.Errorf("unmonitor episodes: %w", err)
	}
	return nil
}

// UnmonitorSeasons unmonitors these seasons of a series. Sonarr changes a series only as a
// whole, so the series is read, the seasons are changed, and every other field is sent back
// as it came. A season Sonarr does not list is an error: no guessing (spec 6.8, rule 5).
// Sonarr 4.0.12 then unmonitors the episodes of those seasons too (spec 16.2).
func (c *Client) UnmonitorSeasons(ctx context.Context, seriesID int, seasons []int) error {
	path := "/api/v3/series/" + strconv.Itoa(seriesID)
	var series map[string]json.RawMessage
	if err := c.arr.Get(ctx, path, nil, &series); err != nil {
		return fmt.Errorf("unmonitor seasons: %w", err)
	}
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(series["seasons"], &list); err != nil {
		return fmt.Errorf("unmonitor seasons: the series has no season list: %w", err)
	}
	want := map[int]bool{}
	for _, n := range seasons {
		want[n] = true
	}
	for _, s := range list {
		var n int
		if err := json.Unmarshal(s["seasonNumber"], &n); err != nil {
			return fmt.Errorf("unmonitor seasons: a season has no number: %w", err)
		}
		if want[n] {
			s["monitored"] = json.RawMessage("false")
			delete(want, n)
		}
	}
	if len(want) > 0 {
		missing := make([]int, 0, len(want))
		for n := range want {
			missing = append(missing, n)
		}
		sort.Ints(missing)
		return fmt.Errorf("unmonitor seasons: Sonarr does not list season %d", missing[0])
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	series["seasons"] = raw
	if err := c.arr.Send(ctx, http.MethodPut, path, nil, series, nil); err != nil {
		return fmt.Errorf("unmonitor seasons: %w", err)
	}
	return nil
}
```

Add the write interfaces to `internal/providers/providers.go` as listed above, and `providers.MovieWriter = (*Client)(nil)` / `providers.SeriesWriter = (*Client)(nil)` to the `var _` blocks.

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./internal/httpjson/ ./internal/providers/...`
Expected: PASS. The read tests still pass: `Get` behaves as before.

- [ ] **Step 5: Commit**

```bash
git add internal/httpjson internal/providers/providers.go internal/providers/arr internal/providers/radarr internal/providers/sonarr
git commit -m "feat: let radarr and sonarr remove and unmonitor what a plan names"
```

---

### Task 5: qBittorrent removes a torrent

**Files:**
- Modify: `internal/providers/qbittorrent/qbittorrent.go` (`get` becomes `request`; `once` takes a method and a form; add `RemoveDownload`, `validHash`)
- Test: `internal/providers/qbittorrent/qbittorrent_test.go`

**Interfaces:**
- Produces: `func (c *qbittorrent.Client) RemoveDownload(ctx context.Context, id string, deleteData bool) error`; `*Client` implements `providers.DownloadRemover`.
- Request: `POST /api/v2/torrents/delete` with the form `hashes={id}&deleteFiles={true|false}`, `Content-Type: application/x-www-form-urlencoded`, the `Referer` header as for reads; a 403 logs in and retries once, as reads do.

- [ ] **Step 1: Write the failing tests**

Extend `fakeQbit` with a field `deleted []string` (each `"hash deleteFiles"`) and a handler:

```go
	mux.HandleFunc("/api/v2/torrents/delete", guarded(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		f.deleted = append(f.deleted, r.FormValue("hashes")+" "+r.FormValue("deleteFiles"))
	}))
```

Tests:

```go
func TestRemoveDownloadSendsTheHashAndWhetherToDeleteTheData(t *testing.T) {
	f := &fakeQbit{}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c, _ := New(srv.URL, "", "", time.Second)
	hash := "0123456789abcdef0123456789abcdef01234567"
	if err := c.RemoveDownload(ctx, hash, true); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveDownload(ctx, hash, false); err != nil {
		t.Fatal(err)
	}
	want := []string{hash + " true", hash + " false"}
	if !reflect.DeepEqual(f.deleted, want) {
		t.Errorf("deleted = %q, want %q", f.deleted, want)
	}
}

func TestRemoveDownloadLogsInWhenAsked(t *testing.T) {
	f := &fakeQbit{auth: true}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c, _ := New(srv.URL, "admin", "secret", time.Second)
	if err := c.RemoveDownload(ctx, "ab", false); err != nil {
		t.Fatal(err)
	}
	if f.logins != 1 || len(f.deleted) != 1 {
		t.Errorf("logins = %d, deleted = %q; want one login and one removal", f.logins, f.deleted)
	}
}

func TestRemoveDownloadWithoutCredentialsNeedsALogin(t *testing.T) {
	f := &fakeQbit{auth: true}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c, _ := New(srv.URL, "", "", time.Second)
	if err := c.RemoveDownload(ctx, "ab", false); !errors.Is(err, ErrLoginRequired) {
		t.Errorf("err = %v, want ErrLoginRequired", err)
	}
}

func TestRemoveDownloadRefusesAnIDThatIsNotAHash(t *testing.T) {
	f := &fakeQbit{}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c, _ := New(srv.URL, "", "", time.Second)
	// "all" would remove every torrent, and "|" separates several hashes.
	for _, id := range []string{"", "all", "ab|cd", "AB", "ab cd", "ab\n"} {
		if err := c.RemoveDownload(ctx, id, true); err == nil {
			t.Errorf("RemoveDownload(%q) = nil, want refused", id)
		}
	}
	if len(f.deleted) != 0 {
		t.Errorf("requests sent: %q", f.deleted)
	}
}
```

- [ ] **Step 2: Run the tests and see them fail**

Run: `go test ./internal/providers/qbittorrent/`
Expected: `RemoveDownload` undefined.

- [ ] **Step 3: Implement**

Turn `once(ctx, path, q)` into `once(ctx context.Context, method, path string, q, form url.Values) ([]byte, int, error)`: with a non-nil `form` the body is `form.Encode()` and `Content-Type` is `application/x-www-form-urlencoded`. Turn `get` into `request(ctx context.Context, method, path string, q, form url.Values) ([]byte, error)` with the same 403 handling, and keep `get(ctx, path, q)` as `request(ctx, http.MethodGet, path, q, nil)`. Then:

```go
// RemoveDownload removes one torrent, with its data when deleteData is set (spec 6.7). id must
// be a hash as Downloads reports it: qBittorrent reads "all" as every torrent and "|" as a
// separator, so anything else is refused before a request is sent.
func (c *Client) RemoveDownload(ctx context.Context, id string, deleteData bool) error {
	if !validHash(id) {
		return errors.New("remove a torrent: the ID is not a torrent hash")
	}
	form := url.Values{"hashes": {id}, "deleteFiles": {strconv.FormatBool(deleteData)}}
	if _, err := c.request(ctx, http.MethodPost, "/api/v2/torrents/delete", nil, form); err != nil {
		return fmt.Errorf("remove a torrent: %w", err)
	}
	return nil
}

// validHash reports whether id is lower-case hexadecimal, as Downloads reports hashes.
func validHash(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
```

Add `providers.DownloadRemover = (*Client)(nil)` to the `var _` block and update the package comment ("reads torrents from qBittorrent's Web API (v2) and removes them").

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./internal/providers/qbittorrent/`
Expected: PASS, the read tests included.

- [ ] **Step 5: Break it once**

Make `validHash` return true for every non-empty ID; `TestRemoveDownloadRefusesAnIDThatIsNotAHash` must fail. Restore it.

- [ ] **Step 6: Commit**

```bash
git add internal/providers/qbittorrent
git commit -m "feat: let qbittorrent remove a torrent, refusing anything but one hash"
```

---

### Task 6: the executor runs one item

**Files:**
- Create: `internal/executor/item.go`, `internal/executor/item_test.go`, `internal/executor/fakes_test.go`

**Interfaces:**
- Consumes: the write interfaces (Task 4, 5), `domain.Action` and friends (Task 3), `domain.ManagerStep.RowKey` and `scan.KeepsData`, `scan.NormalPath` (Task 2).
- Produces:

```go
// Providers are the apps an execution reaches. A nil field means the app is not configured.
type Providers struct {
	Movies    providers.MovieWriter
	Series    providers.SeriesWriter
	Downloads providers.DownloadClient  // read right before an item's torrents
	Remover   providers.DownloadRemover
}

// run is one execution in progress.
type run struct {
	p       Providers
	dryRun  bool
	removed map[string]bool // torrents this execution removed, or would have in a dry run
}

func newRun(p Providers, dryRun bool) *run

// item runs one plan item (spec 6.8, rules 3 and 4; 6.11). It returns the item's record
// without ID, plan and time, and the stored rows it deleted: none in a dry run.
func (r *run) item(ctx context.Context, it domain.PlanItem) (domain.Action, []domain.RowRef)
```

Rules the code follows (spec 6.11):

1. Each manager step: an app that is not configured fails the step with `"<the movie manager|the series manager>: not configured"` (wrap `providers.ErrNotConfigured`), in a dry run too. An unknown manager or action fails it. In a dry run the step is `ResultDryRun` and nothing is called. Otherwise the call's error fails it, else `ResultDone`.
2. After the first failed step every later step and every torrent is `ResultSkipped`, and nothing more is called.
3. Torrents only when every manager step passed and the item has torrents: read `Downloads` once (a nil `Downloads` or `Remover` fails every torrent with `"the download client: not configured"`; a read error fails every torrent with that error). `live` = the read, minus `r.removed`. Order the item's torrents by the length of `scan.NormalPath` of their live content path, longest first, then by ID.
4. Per torrent: not in `live` → `ResultGone`, no call. Otherwise `keep := t.KeepData || scan.KeepsData(path, paths of every other download in live)`. Dry run → `ResultDryRun`; else call `RemoveDownload(ctx, id, !keep)`: error → `ResultFailed` and stop; success → `ResultDone`. On `ResultDone` or `ResultDryRun` add the ID to `r.removed` and delete it from `live`. `StepResult.KeepData` is `keep` for done and dry run, `t.KeepData` otherwise.
5. Outcome: `OutcomeFailed` when any step failed; else `OutcomeDryRun` in a dry run; else `OutcomeDone`.
6. Rows: each `PlanRow` becomes an `ActionRow` (`Added` copied); `Size` is their sum.
7. Drops (none in a dry run): a movie or series row whose steps (`RowKey` equal to its key, `Manager` equal to its list) were all done, or that has no step of its own while every manager step of the item was done; a torrent-list row whose torrent was done or gone; and `domain.TorrentKey(id)` in the torrent list for every torrent done or gone.

- [ ] **Step 1: Write the fakes**

`internal/executor/fakes_test.go`:

```go
package executor

import (
	"context"
	"fmt"
	"testing"

	"github.com/removarr/removarr/internal/domain"
)

var ctx = context.Background()

// apps records every call in one log, so tests can check the order across apps.
type apps struct {
	t        *testing.T
	log      []string
	fail     map[string]error // call -> its error
	list     []domain.Download
	readErr  error
	gone     map[string]bool // removed so far
	noWrites bool            // a dry run: any write fails the test
}

func newApps(t *testing.T, list ...domain.Download) *apps {
	return &apps{t: t, fail: map[string]error{}, list: list, gone: map[string]bool{}}
}

func (a *apps) call(s string) error {
	if a.noWrites {
		a.t.Errorf("a dry run sent %q", s)
	}
	a.log = append(a.log, s)
	return a.fail[s]
}

func (a *apps) RemoveMovie(_ context.Context, id int, excl bool) error {
	return a.call(fmt.Sprintf("remove movie %d exclusion=%v", id, excl))
}
func (a *apps) RemoveSeries(_ context.Context, id int, excl bool) error {
	return a.call(fmt.Sprintf("remove series %d exclusion=%v", id, excl))
}
func (a *apps) DeleteEpisodeFiles(_ context.Context, ids []int) error {
	return a.call(fmt.Sprintf("delete files %v", ids))
}
func (a *apps) UnmonitorEpisodes(_ context.Context, ids []int) error {
	return a.call(fmt.Sprintf("unmonitor episodes %v", ids))
}
func (a *apps) UnmonitorSeasons(_ context.Context, id int, seasons []int) error {
	return a.call(fmt.Sprintf("unmonitor seasons %d %v", id, seasons))
}
func (a *apps) RemoveDownload(_ context.Context, id string, deleteData bool) error {
	s := fmt.Sprintf("remove torrent %s data=%v", id, deleteData)
	if err := a.call(s); err != nil {
		return err
	}
	a.gone[id] = true
	return nil
}
func (a *apps) Downloads(context.Context) ([]domain.Download, error) {
	a.log = append(a.log, "read downloads")
	if a.readErr != nil {
		return nil, a.readErr
	}
	var out []domain.Download
	for _, d := range a.list {
		if !a.gone[d.ID] {
			out = append(out, d)
		}
	}
	return out, nil
}
func (a *apps) Files(context.Context, string) ([]domain.DownloadFile, error) { return nil, nil }

func (a *apps) providers() Providers {
	return Providers{Movies: a, Series: a, Downloads: a, Remover: a}
}

func dl(id, path string) domain.Download { return domain.Download{ID: id, ContentPath: path} }

func torrent(id string) domain.PlanTorrent { return domain.PlanTorrent{Torrent: domain.TorrentInfo{ID: id}} }

func movieItem(id int, key string, size int64, torrents ...domain.PlanTorrent) domain.PlanItem {
	return domain.PlanItem{
		Rows:     []domain.PlanRow{{Row: domain.Candidate{List: domain.ListMovies, Key: key, Title: key, Size: size}}},
		Steps:    []domain.ManagerStep{{Manager: "movies", Action: domain.StepRemoveMovie, ItemID: id, RowKey: key}},
		Torrents: torrents,
	}
}

func results(a domain.Action) []string {
	var out []string
	for _, s := range a.Steps {
		out = append(out, s.Result)
	}
	return out
}
```

- [ ] **Step 2: Write the failing tests**

`internal/executor/item_test.go`:

```go
package executor

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/removarr/removarr/internal/domain"
)

func TestAnItemRunsItsManagerStepsBeforeItsTorrents(t *testing.T) {
	a := newApps(t, dl("h1", "/dl/one"))
	act, drop := newRun(a.providers(), false).item(ctx, movieItem(7, "tmdb:1", 700, torrent("h1")))
	want := []string{"remove movie 7 exclusion=false", "read downloads", "remove torrent h1 data=true"}
	if !reflect.DeepEqual(a.log, want) {
		t.Errorf("calls = %q, want %q", a.log, want)
	}
	if act.Outcome != domain.OutcomeDone || act.Size != 700 || len(act.Rows) != 1 {
		t.Errorf("record = %+v", act)
	}
	wantDrop := []domain.RowRef{{List: domain.ListMovies, Key: "tmdb:1"}, {List: domain.ListTorrents, Key: "hash:h1"}}
	if !reflect.DeepEqual(drop, wantDrop) {
		t.Errorf("drop = %+v, want %+v", drop, wantDrop)
	}
}

func TestAFailedStepStopsTheItemAndKeepsItsTorrents(t *testing.T) {
	a := newApps(t, dl("h1", "/dl/one"))
	a.fail["unmonitor seasons 1 [1]"] = errors.New("unexpected status 500")
	it := domain.PlanItem{
		Rows: []domain.PlanRow{{Row: domain.Candidate{List: domain.ListSeries, Key: "tvdb:100:s1"}}},
		Steps: []domain.ManagerStep{
			{Manager: "series", Action: domain.StepUnmonitorEpisodes, ItemID: 1, EpisodeIDs: []int{11}, RowKey: "tvdb:100:s1"},
			{Manager: "series", Action: domain.StepUnmonitorSeasons, ItemID: 1, Seasons: []int{1}, RowKey: "tvdb:100:s1"},
			{Manager: "series", Action: domain.StepDeleteEpisodeFiles, ItemID: 1, FileIDs: []int{111}, RowKey: "tvdb:100:s1"},
		},
		Torrents: []domain.PlanTorrent{torrent("h1")},
	}
	act, drop := newRun(a.providers(), false).item(ctx, it)
	if want := []string{"unmonitor episodes [11]", "unmonitor seasons 1 [1]"}; !reflect.DeepEqual(a.log, want) {
		t.Errorf("calls = %q, want %q: nothing after the failure", a.log, want)
	}
	want := []string{domain.ResultDone, domain.ResultFailed, domain.ResultSkipped, domain.ResultSkipped}
	if !reflect.DeepEqual(results(act), want) || act.Outcome != domain.OutcomeFailed {
		t.Errorf("results = %q, outcome %s; want %q, failed", results(act), act.Outcome, want)
	}
	if !strings.Contains(act.Steps[1].Error, "500") {
		t.Errorf("error = %q, want the app's answer", act.Steps[1].Error)
	}
	if len(drop) != 0 {
		t.Errorf("drop = %+v, want nothing: the row was not deleted", drop)
	}
}

func TestATiedRowWhoseStepsSucceededIsDroppedWhenAnotherFails(t *testing.T) {
	a := newApps(t)
	a.fail["remove series 2 exclusion=false"] = errors.New("unexpected status 404")
	it := domain.PlanItem{
		Rows: []domain.PlanRow{
			{Row: domain.Candidate{List: domain.ListMovies, Key: "tmdb:1"}},
			{Row: domain.Candidate{List: domain.ListSeries, Key: "tvdb:2"}, Added: true},
		},
		Steps: []domain.ManagerStep{
			{Manager: "movies", Action: domain.StepRemoveMovie, ItemID: 1, RowKey: "tmdb:1"},
			{Manager: "series", Action: domain.StepRemoveSeries, ItemID: 2, RowKey: "tvdb:2"},
		},
		Torrents: []domain.PlanTorrent{torrent("h1")},
	}
	_, drop := newRun(a.providers(), false).item(ctx, it)
	if want := []domain.RowRef{{List: domain.ListMovies, Key: "tmdb:1"}}; !reflect.DeepEqual(drop, want) {
		t.Errorf("drop = %+v, want only the movie", drop)
	}
	for _, c := range a.log {
		if strings.HasPrefix(c, "remove torrent") || c == "read downloads" {
			t.Errorf("the shared torrent was touched: %q", a.log)
		}
	}
}

func TestATorrentNoLongerInTheClientIsGone(t *testing.T) {
	a := newApps(t) // the client holds nothing
	act, drop := newRun(a.providers(), false).item(ctx, movieItem(7, "tmdb:1", 0, torrent("h1")))
	if got := results(act); !reflect.DeepEqual(got, []string{domain.ResultDone, domain.ResultGone}) || act.Outcome != domain.OutcomeDone {
		t.Errorf("results = %q, outcome %s", got, act.Outcome)
	}
	for _, c := range a.log {
		if strings.HasPrefix(c, "remove torrent") {
			t.Errorf("a removal was sent for a torrent that is gone: %q", a.log)
		}
	}
	if len(drop) != 2 {
		t.Errorf("drop = %+v, want the movie and the torrent row", drop)
	}
}

func TestADownloadListThatCannotBeReadFailsTheTorrents(t *testing.T) {
	a := newApps(t, dl("h1", "/dl/one"))
	a.readErr = errors.New("connection refused")
	act, _ := newRun(a.providers(), false).item(ctx, movieItem(7, "tmdb:1", 0, torrent("h1"), torrent("h2")))
	want := []string{domain.ResultDone, domain.ResultFailed, domain.ResultFailed}
	if !reflect.DeepEqual(results(act), want) || act.Outcome != domain.OutcomeFailed || !strings.Contains(act.Steps[1].Error, "connection refused") {
		t.Errorf("record = %+v", act)
	}
}

func TestTheLastOfTwoTorrentsWithOneContentPathTakesTheData(t *testing.T) {
	a := newApps(t, dl("h1", "/dl/one"), dl("h2", "/dl/one"))
	newRun(a.providers(), false).item(ctx, movieItem(7, "tmdb:1", 0, torrent("h1"), torrent("h2")))
	want := []string{"remove movie 7 exclusion=false", "read downloads", "remove torrent h1 data=false", "remove torrent h2 data=true"}
	if !reflect.DeepEqual(a.log, want) {
		t.Errorf("calls = %q, want %q", a.log, want)
	}
}

func TestATorrentInsideAnotherGoesFirstAndTheDirectoryGoesWhole(t *testing.T) {
	a := newApps(t, dl("aa", "/dl/pack"), dl("zz", "/dl/pack/one.mkv"))
	newRun(a.providers(), false).item(ctx, movieItem(7, "tmdb:1", 0, torrent("aa"), torrent("zz")))
	want := []string{"remove movie 7 exclusion=false", "read downloads", "remove torrent zz data=false", "remove torrent aa data=true"}
	if !reflect.DeepEqual(a.log, want) {
		t.Errorf("calls = %q, want %q", a.log, want)
	}
}

func TestDataSharedAcrossItemsGoesWithTheLastAndStaysWhenTheFirstFails(t *testing.T) {
	// Both removals succeed: the first keeps the data, the second takes it.
	a := newApps(t, dl("h1", "/dl/one"), dl("h2", "/dl/one"))
	r := newRun(a.providers(), false)
	r.item(ctx, movieItem(1, "tmdb:1", 0, torrent("h1")))
	r.item(ctx, movieItem(2, "tmdb:2", 0, torrent("h2")))
	if !contains(a.log, "remove torrent h1 data=false") || !contains(a.log, "remove torrent h2 data=true") {
		t.Errorf("calls = %q", a.log)
	}

	// The first removal fails: h1 stays, so h2 must keep the data.
	a = newApps(t, dl("h1", "/dl/one"), dl("h2", "/dl/one"))
	a.fail["remove torrent h1 data=false"] = errors.New("unexpected status 500")
	r = newRun(a.providers(), false)
	r.item(ctx, movieItem(1, "tmdb:1", 0, torrent("h1")))
	r.item(ctx, movieItem(2, "tmdb:2", 0, torrent("h2")))
	if !contains(a.log, "remove torrent h2 data=false") {
		t.Errorf("calls = %q, want h2 removed without its data", a.log)
	}
}

func TestThePlansKeepDataHoldsWhenNothingSharesItNow(t *testing.T) {
	a := newApps(t, dl("h1", "/dl/one"))
	keep := domain.PlanTorrent{Torrent: domain.TorrentInfo{ID: "h1"}, KeepData: true}
	act, _ := newRun(a.providers(), false).item(ctx, movieItem(7, "tmdb:1", 0, keep))
	if !contains(a.log, "remove torrent h1 data=false") || !act.Steps[1].KeepData {
		t.Errorf("calls = %q, record %+v: the user confirmed keeping the data", a.log, act.Steps[1])
	}
}

func TestADryRunSendsNothingAndDecidesAsARealRun(t *testing.T) {
	a := newApps(t, dl("h1", "/dl/one"), dl("h2", "/dl/one"))
	a.noWrites = true
	r := newRun(a.providers(), true)
	first, drop := r.item(ctx, movieItem(1, "tmdb:1", 0, torrent("h1")))
	second, _ := r.item(ctx, movieItem(2, "tmdb:2", 0, torrent("h2")))
	if first.Outcome != domain.OutcomeDryRun || !first.DryRun || results(first)[0] != domain.ResultDryRun {
		t.Errorf("first = %+v", first)
	}
	if !first.Steps[1].KeepData || second.Steps[1].KeepData {
		t.Errorf("keepData = %v then %v, want true then false as a real run would decide", first.Steps[1].KeepData, second.Steps[1].KeepData)
	}
	if len(drop) != 0 {
		t.Errorf("drop = %+v, want none in a dry run", drop)
	}
}

func TestAStepOfAnAppThatIsNotConfiguredFailsTheItem(t *testing.T) {
	for _, dry := range []bool{false, true} {
		a := newApps(t)
		p := a.providers()
		p.Movies = nil
		act, _ := newRun(p, dry).item(ctx, movieItem(7, "tmdb:1", 0))
		if act.Outcome != domain.OutcomeFailed || !strings.Contains(act.Steps[0].Error, "not configured") {
			t.Errorf("dry run %v: record = %+v, want failed as not configured", dry, act)
		}
	}
}

func TestATorrentRowIsDroppedWhenItsTorrentGoes(t *testing.T) {
	a := newApps(t, dl("h9", "/dl/orphan"))
	it := domain.PlanItem{
		Rows:     []domain.PlanRow{{Row: domain.Candidate{List: domain.ListTorrents, Key: "hash:h9", Size: 50}}},
		Steps:    []domain.ManagerStep{},
		Torrents: []domain.PlanTorrent{torrent("h9")},
	}
	act, drop := newRun(a.providers(), false).item(ctx, it)
	if act.Outcome != domain.OutcomeDone || act.Size != 50 {
		t.Errorf("record = %+v", act)
	}
	if want := []domain.RowRef{{List: domain.ListTorrents, Key: "hash:h9"}}; !reflect.DeepEqual(drop, want) {
		t.Errorf("drop = %+v, want %+v (once)", drop, want)
	}
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}
```

The drop list holds each row once: a torrent row and the torrent-list key of its torrent are the same row.

- [ ] **Step 3: Run the tests and see them fail**

Run: `go test ./internal/executor/`
Expected: build failure, `newRun` undefined.

- [ ] **Step 4: Implement `internal/executor/item.go`**

```go
// Package executor carries out a confirmed plan: the only code in removarr that changes
// anything in an app (spec 6.8, 6.11).
package executor

import (
	"context"
	"fmt"
	"sort"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/providers"
	"github.com/removarr/removarr/internal/scan"
)

// Providers … (as in Interfaces)

type run struct { … }

func newRun(p Providers, dryRun bool) *run {
	return &run{p: p, dryRun: dryRun, removed: map[string]bool{}}
}

func (r *run) item(ctx context.Context, it domain.PlanItem) (domain.Action, []domain.RowRef) {
	act := domain.Action{DryRun: r.dryRun, Rows: []domain.ActionRow{}, Steps: []domain.StepResult{}}
	for _, row := range it.Rows {
		c := row.Row
		act.Rows = append(act.Rows, domain.ActionRow{List: c.List, Key: c.Key, Title: c.Title, Season: c.Season, Size: c.Size, Added: row.Added})
		act.Size += c.Size
	}
	failed := false
	for i := range it.Steps {
		s := it.Steps[i]
		res := domain.StepResult{Manager: &s}
		switch {
		case failed:
			res.Result = domain.ResultSkipped
		default:
			if err := r.step(ctx, s); err != nil {
				res.Result, res.Error, failed = domain.ResultFailed, err.Error(), true
			} else if r.dryRun {
				res.Result = domain.ResultDryRun
			} else {
				res.Result = domain.ResultDone
			}
		}
		act.Steps = append(act.Steps, res)
	}
	if len(it.Torrents) > 0 {
		failed = r.torrents(ctx, it.Torrents, failed, &act)
	}
	switch {
	case failed:
		act.Outcome = domain.OutcomeFailed
	case r.dryRun:
		act.Outcome = domain.OutcomeDryRun
	default:
		act.Outcome = domain.OutcomeDone
	}
	if r.dryRun {
		return act, nil
	}
	return act, drops(it, act)
}

// step checks that the step's app is configured and the step is known, and sends it unless
// this is a dry run.
func (r *run) step(ctx context.Context, s domain.ManagerStep) error {
	switch s.Manager {
	case scan.ManagerMovies:
		if r.p.Movies == nil {
			return fmt.Errorf("the movie manager: %w", providers.ErrNotConfigured)
		}
		if s.Action != domain.StepRemoveMovie {
			return fmt.Errorf("unknown step %q for the movie manager", s.Action)
		}
		if r.dryRun {
			return nil
		}
		return r.p.Movies.RemoveMovie(ctx, s.ItemID, s.AddExclusion)
	case scan.ManagerSeries:
		if r.p.Series == nil {
			return fmt.Errorf("the series manager: %w", providers.ErrNotConfigured)
		}
		var send func() error
		switch s.Action {
		case domain.StepRemoveSeries:
			send = func() error { return r.p.Series.RemoveSeries(ctx, s.ItemID, s.AddExclusion) }
		case domain.StepDeleteEpisodeFiles:
			send = func() error { return r.p.Series.DeleteEpisodeFiles(ctx, s.FileIDs) }
		case domain.StepUnmonitorEpisodes:
			send = func() error { return r.p.Series.UnmonitorEpisodes(ctx, s.EpisodeIDs) }
		case domain.StepUnmonitorSeasons:
			send = func() error { return r.p.Series.UnmonitorSeasons(ctx, s.ItemID, s.Seasons) }
		default:
			return fmt.Errorf("unknown step %q for the series manager", s.Action)
		}
		if r.dryRun {
			return nil
		}
		return send()
	default:
		return fmt.Errorf("unknown media manager %q", s.Manager)
	}
}
```

```go
// torrents removes an item's torrents after its media manager steps (rules 3 and 4) and
// reports whether the item failed. failed says whether a manager step failed already.
func (r *run) torrents(ctx context.Context, planned []domain.PlanTorrent, failed bool, act *domain.Action) bool {
	var live map[string]domain.Download
	if !failed {
		var err error
		if live, err = r.read(ctx); err != nil {
			for i := range planned {
				t := planned[i]
				act.Steps = append(act.Steps, domain.StepResult{Torrent: &t.Torrent, KeepData: t.KeepData, Result: domain.ResultFailed, Error: err.Error()})
			}
			return true
		}
	}
	order := append([]domain.PlanTorrent(nil), planned...)
	sort.SliceStable(order, func(i, j int) bool {
		a, b := len(scan.NormalPath(live[order[i].Torrent.ID].ContentPath)), len(scan.NormalPath(live[order[j].Torrent.ID].ContentPath))
		if a != b {
			return a > b
		}
		return order[i].Torrent.ID < order[j].Torrent.ID
	})
	for i := range order {
		t := order[i]
		res := domain.StepResult{Torrent: &t.Torrent, KeepData: t.KeepData}
		d, there := live[t.Torrent.ID]
		switch {
		case failed:
			res.Result = domain.ResultSkipped
		case !there:
			res.Result = domain.ResultGone
		default:
			var others []string
			for id, o := range live {
				if id != d.ID {
					others = append(others, o.ContentPath)
				}
			}
			res.KeepData = t.KeepData || scan.KeepsData(d.ContentPath, others)
			if r.dryRun {
				res.Result = domain.ResultDryRun
			} else if err := r.p.Remover.RemoveDownload(ctx, d.ID, !res.KeepData); err != nil {
				res.Result, res.Error, failed = domain.ResultFailed, err.Error(), true
			} else {
				res.Result = domain.ResultDone
			}
			if !failed {
				r.removed[d.ID] = true
				delete(live, d.ID)
			}
		}
		act.Steps = append(act.Steps, res)
	}
	return failed
}

// read returns the downloads in the client now, without those this execution removed.
func (r *run) read(ctx context.Context) (map[string]domain.Download, error) {
	if r.p.Downloads == nil || r.p.Remover == nil {
		return nil, fmt.Errorf("the download client: %w", providers.ErrNotConfigured)
	}
	list, err := r.p.Downloads.Downloads(ctx)
	if err != nil {
		return nil, err
	}
	live := make(map[string]domain.Download, len(list))
	for _, d := range list {
		if !r.removed[d.ID] {
			live[d.ID] = d
		}
	}
	return live, nil
}

// drops returns the stored rows an item deleted, each once (rule 7): its rows in plan order,
// then the torrent-list row of each torrent it removed or found gone.
func drops(it domain.PlanItem, act domain.Action) []domain.RowRef {
	allDone := true
	stepsOf := map[string][]string{} // list/key -> results of the row's own steps
	for _, s := range act.Steps {
		if s.Manager == nil {
			continue
		}
		id := s.Manager.Manager + "/" + s.Manager.RowKey
		stepsOf[id] = append(stepsOf[id], s.Result)
		if s.Result != domain.ResultDone {
			allDone = false
		}
	}
	removed := map[string]bool{}
	for _, s := range act.Steps {
		if s.Torrent != nil && (s.Result == domain.ResultDone || s.Result == domain.ResultGone) {
			removed[s.Torrent.ID] = true
		}
	}
	out := []domain.RowRef{}
	seen := map[string]bool{}
	add := func(list domain.ListKind, key string) {
		if id := string(list) + "/" + key; !seen[id] {
			seen[id] = true
			out = append(out, domain.RowRef{List: list, Key: key})
		}
	}
	for _, row := range it.Rows {
		c := row.Row
		if c.List == domain.ListTorrents {
			if len(c.Torrents) == 1 && removed[c.Torrents[0].ID] || removed[strings.TrimPrefix(c.Key, "hash:")] {
				add(c.List, c.Key)
			}
			continue
		}
		results, own := stepsOf[string(c.List)+"/"+c.Key]
		done := own
		for _, res := range results {
			done = done && res == domain.ResultDone
		}
		if done || !own && allDone {
			add(c.List, c.Key)
		}
	}
	for _, t := range it.Torrents {
		if removed[t.Torrent.ID] {
			add(domain.ListTorrents, domain.TorrentKey(t.Torrent.ID))
		}
	}
	return out
}
```

This relies on `scan.ManagerMovies == string(domain.ListMovies)` and `scan.ManagerSeries == string(domain.ListSeries)` (both `"movies"` and `"series"` today); add a test in `item_test.go` that pins this:

```go
func TestManagerNamesAreListNames(t *testing.T) {
	if scan.ManagerMovies != string(domain.ListMovies) || scan.ManagerSeries != string(domain.ListSeries) {
		t.Fatal("drops matches a step's manager to a row's list by name")
	}
}
```

Use `scan.KeepsData` for the shared-data decision and `scan.NormalPath` for the order; do not copy the path rules.

- [ ] **Step 5: Run the tests and see them pass**

Run: `go test ./internal/executor/ && go test -race ./internal/executor/`
Expected: PASS.

- [ ] **Step 6: Break it, three times**

One at a time, in a scratch change, and check a test fails each time: (a) drop the `t.KeepData ||` from the keep decision; (b) leave a done torrent in `live`; (c) send the steps after a failure. Restore each.

- [ ] **Step 7: Commit**

```bash
git add internal/executor
git commit -m "feat: run one plan item, media manager first, deciding shared data again"
```

---

### Task 7: the executor runs a plan and records it

**Files:**
- Create: `internal/executor/service.go`, `internal/executor/service_test.go`

**Interfaces:**
- Consumes: `run` (Task 6), `gate` (Task 1), `store.ErrPlanNotFound`, `store.ErrPlanExpired` (Task 3).
- Produces:

```go
// Store is the persistence an execution needs. *store.Store satisfies it.
type Store interface {
	TakePlan(ctx context.Context, id string, now time.Time) (domain.Plan, error)
	RecordAction(ctx context.Context, a domain.Action, drop []domain.RowRef) (int64, error)
}

// Result is what an execution did.
type Result struct {
	PlanID  string          `json:"planId"`
	DryRun  bool            `json:"dryRun"`
	Actions []domain.Action `json:"actions"` // one per item, in plan order; never nil
}

// Service executes plans, one at a time and never during a scan or plan building.
type Service struct {
	Build   func(ctx context.Context) (Providers, error) // the apps from the current settings
	Store   Store
	Gate    *gate.Gate // shared with the scanner and the planner; nil gives a gate of its own
	DryRun  bool
	Now     func() time.Time
	Timeout time.Duration // limit for one execution; 0 means 30 minutes

	own gate.Gate
}

// Execute runs the plan with this ID (spec 6.11).
func (s *Service) Execute(ctx context.Context, planID string) (Result, error)
```

Order in `Execute`: enter the gate (a `*gate.BusyError` is returned as it is); `Build` (error: `load connections: %w`, the plan is not taken); `TakePlan(ctx, planID, s.Now())` (its error returned as it is, so `errors.Is` finds `store.ErrPlanNotFound` and `store.ErrPlanExpired`); then a context that the request cannot cancel, `context.WithTimeout(context.WithoutCancel(ctx), timeout)`; then each item: `run.item`, set `PlanID` and `At = s.Now()`, `RecordAction`; on a record error stop and return the result so far with `record an item: %w`; else set the ID and append.

- [ ] **Step 1: Write the failing tests**

`internal/executor/service_test.go`:

```go
package executor

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/gate"
	"github.com/removarr/removarr/internal/store"
)

var at = time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

type fakeStore struct {
	plan     domain.Plan
	takeErr  error
	taken    int
	recorded []domain.Action
	drops    [][]domain.RowRef
	failAt   int // RecordAction fails on this call, counted from 1; 0 never
}

func (f *fakeStore) TakePlan(_ context.Context, id string, now time.Time) (domain.Plan, error) {
	f.taken++
	if f.takeErr != nil {
		return domain.Plan{}, f.takeErr
	}
	return f.plan, nil
}

func (f *fakeStore) RecordAction(_ context.Context, a domain.Action, drop []domain.RowRef) (int64, error) {
	if len(f.recorded)+1 == f.failAt {
		return 0, errors.New("disk full")
	}
	f.recorded = append(f.recorded, a)
	f.drops = append(f.drops, drop)
	return int64(len(f.recorded)), nil
}

func newService(a *apps, st *fakeStore) *Service {
	return &Service{
		Build: func(context.Context) (Providers, error) { return a.providers(), nil },
		Store: st,
		Gate:  &gate.Gate{},
		Now:   func() time.Time { return at },
	}
}

func TestExecuteRecordsEachItemAndGoesOnAfterAFailure(t *testing.T) {
	a := newApps(t)
	a.fail["remove movie 1 exclusion=false"] = errors.New("unexpected status 500")
	st := &fakeStore{plan: domain.Plan{ID: "p1", Items: []domain.PlanItem{movieItem(1, "tmdb:1", 0), movieItem(2, "tmdb:2", 0)}}}
	res, err := newService(a, st).Execute(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Actions) != 2 || res.Actions[0].Outcome != domain.OutcomeFailed || res.Actions[1].Outcome != domain.OutcomeDone {
		t.Fatalf("actions = %+v", res.Actions)
	}
	if res.Actions[1].ID != 2 || res.Actions[1].PlanID != "p1" || !res.Actions[1].At.Equal(at) {
		t.Errorf("second record = %+v, want ID 2, plan p1 and the time", res.Actions[1])
	}
	if !reflect.DeepEqual(st.drops[1], []domain.RowRef{{List: domain.ListMovies, Key: "tmdb:2"}}) || len(st.drops[0]) != 0 {
		t.Errorf("drops = %+v", st.drops)
	}
}

func TestExecuteIsRefusedWhileTheGateIsHeld(t *testing.T) {
	a := newApps(t)
	st := &fakeStore{}
	svc := newService(a, st)
	release, _ := svc.Gate.Enter(gate.Scan)
	defer release()
	if _, err := svc.Execute(ctx, "p1"); !errors.Is(err, gate.ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	if st.taken != 0 {
		t.Error("the plan was taken although the execution was refused")
	}
}

func TestAnUnknownOrExpiredPlanChangesNothing(t *testing.T) {
	for _, want := range []error{store.ErrPlanNotFound, store.ErrPlanExpired} {
		a := newApps(t)
		a.noWrites = true
		st := &fakeStore{takeErr: want}
		if _, err := newService(a, st).Execute(ctx, "p1"); !errors.Is(err, want) {
			t.Errorf("err = %v, want %v", err, want)
		}
		if len(st.recorded) != 0 || len(a.log) != 0 {
			t.Errorf("%v: recorded %d, calls %q", want, len(st.recorded), a.log)
		}
	}
}

func TestExecuteDoesNotTakeThePlanWhenTheAppsCannotBeBuilt(t *testing.T) {
	st := &fakeStore{}
	svc := newService(newApps(t), st)
	svc.Build = func(context.Context) (Providers, error) { return Providers{}, errors.New("database is locked") }
	if _, err := svc.Execute(ctx, "p1"); err == nil || st.taken != 0 {
		t.Errorf("err = %v, taken %d; want an error and the plan left", err, st.taken)
	}
}

// cancelling is an app that cancels the request when the first item runs and checks that
// later calls still get a live context.
type cancelling struct {
	*apps
	cancel func()
	dead   int
}

func (c *cancelling) RemoveMovie(ctx context.Context, id int, excl bool) error {
	c.cancel()
	if ctx.Err() != nil {
		c.dead++
	}
	return c.apps.RemoveMovie(ctx, id, excl)
}

func TestExecuteRunsToTheEndWhenTheRequestGoesAway(t *testing.T) {
	a := newApps(t)
	reqCtx, cancel := context.WithCancel(context.Background())
	c := &cancelling{apps: a, cancel: cancel}
	st := &fakeStore{plan: domain.Plan{ID: "p1", Items: []domain.PlanItem{movieItem(1, "tmdb:1", 0), movieItem(2, "tmdb:2", 0)}}}
	svc := newService(a, st)
	svc.Build = func(context.Context) (Providers, error) {
		p := a.providers()
		p.Movies = c
		return p, nil
	}
	res, err := svc.Execute(reqCtx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if c.dead != 0 || len(res.Actions) != 2 || res.Actions[1].Outcome != domain.OutcomeDone {
		t.Errorf("dead contexts %d, actions %+v; want both items run with a live context", c.dead, res.Actions)
	}
}

func TestExecuteStopsWhenAnItemCannotBeRecorded(t *testing.T) {
	a := newApps(t)
	st := &fakeStore{failAt: 1, plan: domain.Plan{ID: "p1", Items: []domain.PlanItem{movieItem(1, "tmdb:1", 0), movieItem(2, "tmdb:2", 0)}}}
	if _, err := newService(a, st).Execute(ctx, "p1"); err == nil {
		t.Fatal("want an error")
	}
	if contains(a.log, "remove movie 2 exclusion=false") {
		t.Errorf("calls = %q: nothing may be deleted after a record failed", a.log)
	}
}

func TestADryRunExecutionIsMarked(t *testing.T) {
	a := newApps(t)
	a.noWrites = true
	st := &fakeStore{plan: domain.Plan{ID: "p1", Items: []domain.PlanItem{movieItem(1, "tmdb:1", 0)}}}
	svc := newService(a, st)
	svc.DryRun = true
	res, err := svc.Execute(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || res.Actions[0].Outcome != domain.OutcomeDryRun || len(st.drops[0]) != 0 {
		t.Errorf("result = %+v, drops %+v", res, st.drops)
	}
}
```

- [ ] **Step 2: Run the tests and see them fail**

Run: `go test ./internal/executor/`
Expected: `Service` undefined.

- [ ] **Step 3: Implement `internal/executor/service.go`**

```go
const defaultTimeout = 30 * time.Minute

// Execute runs the plan with this ID (spec 6.11). It is refused with a *gate.BusyError while
// another job runs, and with store.ErrPlanNotFound or store.ErrPlanExpired; those change
// nothing. Once the plan is taken it runs to the end, even when ctx is cancelled, and every
// item is recorded as it finishes. It stops early only when an item cannot be recorded:
// nothing more is deleted than the history holds.
func (s *Service) Execute(ctx context.Context, planID string) (Result, error) {
	g := s.Gate
	if g == nil {
		g = &s.own
	}
	release, err := g.Enter(gate.Execute)
	if err != nil {
		return Result{}, err
	}
	defer release()

	p, err := s.Build(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load connections: %w", err)
	}
	plan, err := s.Store.TakePlan(ctx, planID, s.Now())
	if err != nil {
		return Result{}, err
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	r := newRun(p, s.DryRun)
	res := Result{PlanID: plan.ID, DryRun: s.DryRun, Actions: []domain.Action{}}
	for _, it := range plan.Items {
		a, drop := r.item(ctx, it)
		a.PlanID, a.At = plan.ID, s.Now()
		id, err := s.Store.RecordAction(ctx, a, drop)
		if err != nil {
			return res, fmt.Errorf("record an item: %w", err)
		}
		a.ID = id
		res.Actions = append(res.Actions, a)
	}
	return res, nil
}
```

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./internal/executor/ && go test -race ./internal/executor/`
Expected: PASS.

- [ ] **Step 5: Break it once**

Use the request's context instead of `context.WithoutCancel`; `TestExecuteRunsToTheEndWhenTheRequestGoesAway` must fail. Restore it.

- [ ] **Step 6: Commit**

```bash
git add internal/executor
git commit -m "feat: execute a plan item by item and record each in the history"
```

---

### Task 8: routes, wiring and documents

**Files:**
- Modify: `internal/app/apps.go` (`Writers`), `internal/app/apps_test.go`
- Create: `internal/api/execute.go`, `internal/api/execute_test.go`
- Modify: `internal/api/server.go` (`Executor` interface and field, `Store` gains `Actions`, two routes), `internal/api/server_test.go` (fake executor in the harness)
- Modify: `cmd/removarr/main.go`
- Modify: `AGENTS.md`, `README.md`, `CHANGELOG.md`, `LEARNINGS.md`

**Interfaces:**
- Consumes: `executor.Service`, `executor.Result`, `executor.Providers`, `store.Store.Actions`.
- Produces:
  - `func (a *app.Apps) Writers(ctx context.Context) (executor.Providers, error)`: a client for each app whose settings are complete, the same clients as `Providers` builds; an app that is not configured or partly entered stays nil, so its steps fail as not configured.
  - `api.Executor` interface: `Execute(ctx context.Context, planID string) (executor.Result, error)`; `Server.Executor Executor`.
  - `POST /api/plans/{id}/execute`: 200 with `executor.Result`; 409 when busy; 404 `"There is no such plan. It was executed or discarded already: build it again."` for `store.ErrPlanNotFound`; 410 `"The plan has expired. Build it again."` for `store.ErrPlanExpired`; 500 otherwise.
  - `GET /api/actions?before=&limit=`: 200 with `{"actions":[…],"next":<id>}`; `next` is the ID of the last record when the page is full, otherwise absent. `before` must be a whole number of 0 or more, `limit` from 1 to 200, default 50; otherwise 400 naming the parameter.

- [ ] **Step 1: Write the failing tests**

In the API harness add `executor *fakeExecutor` (fields `plan string`, `result executor.Result`, `err error`) and set `Executor: h.executor` on the server.

`internal/api/execute_test.go`:

```go
package api

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/executor"
	"github.com/removarr/removarr/internal/gate"
	"github.com/removarr/removarr/internal/store"
)

func TestExecutePassesThePlanAndReturnsTheRecords(t *testing.T) {
	h := newHarness(t, nil)
	h.executor.result = executor.Result{PlanID: "abc", DryRun: true, Actions: []domain.Action{{ID: 1, PlanID: "abc", Outcome: domain.OutcomeDryRun}}}
	rec := h.do(http.MethodPost, "/api/plans/abc/execute", "{}")
	if rec.Code != http.StatusOK || h.executor.plan != "abc" {
		t.Fatalf("status = %d %s, plan %q", rec.Code, rec.Body, h.executor.plan)
	}
	if got := decode[executor.Result](t, rec); got.PlanID != "abc" || len(got.Actions) != 1 {
		t.Errorf("response = %+v", got)
	}
}

func TestExecuteRefusals(t *testing.T) {
	cases := []struct {
		err  error
		code int
	}{
		{&gate.BusyError{Job: gate.Scan}, http.StatusConflict},
		{store.ErrPlanNotFound, http.StatusNotFound},
		{fmt.Errorf("take: %w", store.ErrPlanExpired), http.StatusGone},
		{errors.New("disk full"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		h := newHarness(t, nil)
		h.executor.err = c.err
		if rec := h.do(http.MethodPost, "/api/plans/abc/execute", "{}"); rec.Code != c.code {
			t.Errorf("%v: status = %d, want %d", c.err, rec.Code, c.code)
		}
	}
}

func TestExecuteGoesThroughTheGuard(t *testing.T) {
	h := newHarness(t, nil)
	rec := h.do(http.MethodPost, "/api/plans/abc/execute", "{}", func(r *http.Request) { r.Header.Del(RequestHeader) })
	if rec.Code != http.StatusForbidden || h.executor.plan != "" {
		t.Errorf("status = %d, executed %q; want 403 and nothing executed", rec.Code, h.executor.plan)
	}
}

func TestActionsArePagedNewestFirst(t *testing.T) {
	h := newHarness(t, nil)
	at := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if _, err := h.store.RecordAction(ctx, domain.Action{PlanID: "p", At: at, Outcome: domain.OutcomeDone, Rows: []domain.ActionRow{}, Steps: []domain.StepResult{}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	type page struct {
		Actions []domain.Action `json:"actions"`
		Next    *int64          `json:"next"`
	}
	first := decode[page](t, h.do(http.MethodGet, "/api/actions?limit=2", ""))
	if len(first.Actions) != 2 || first.Actions[0].ID != 3 || first.Next == nil || *first.Next != 2 {
		t.Fatalf("first page = %+v", first)
	}
	second := decode[page](t, h.do(http.MethodGet, fmt.Sprintf("/api/actions?limit=2&before=%d", *first.Next), ""))
	if len(second.Actions) != 1 || second.Actions[0].ID != 1 || second.Next != nil {
		t.Errorf("second page = %+v", second)
	}
	for _, q := range []string{"limit=0", "limit=201", "limit=x", "before=-1", "before=x"} {
		if rec := h.do(http.MethodGet, "/api/actions?"+q, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", q, rec.Code)
		}
	}
}
```

(If `ctx` is not defined in the API tests, use `context.Background()`.)

In `internal/app/apps_test.go`, following its existing pattern for `Providers`: with all four apps configured, `Writers` returns non-nil `Movies`, `Series`, `Downloads` and `Remover`; with Radarr partly entered, `Movies` is nil.

- [ ] **Step 2: Run the tests and see them fail**

Run: `go test ./internal/api/ ./internal/app/`
Expected: build failures.

- [ ] **Step 3: Implement**

`internal/api/execute.go` holds `executePlan` (reads `r.PathValue("id")`, maps the errors as listed, using `busy` first) and `listActions` (parses `before` and `limit` with `strconv.ParseInt`/`Atoi`, answers 400 with `writeError(w, http.StatusBadRequest, "before must be a record ID")` or `"limit must be from 1 to 200"`, then `s.Store.Actions`, and sets `next` when `len(actions) == limit`). Register in `Handler`:

```go
	mux.HandleFunc("POST /api/plans/{id}/execute", s.executePlan)
	mux.HandleFunc("GET /api/actions", s.listActions)
```

Add `Actions(ctx context.Context, before int64, limit int) ([]domain.Action, error)` to the API's `Store` interface.

`internal/app/apps.go`:

```go
// Writers returns the apps an execution changes, from the current settings (spec 6.11). An
// app whose settings are missing or incomplete stays nil, and its steps fail as not
// configured.
func (a *Apps) Writers(ctx context.Context) (executor.Providers, error) {
	var p executor.Providers
	c, ok, err := a.Config.App(ctx, config.AppRadarr)
	if err != nil {
		return executor.Providers{}, err
	}
	if ok {
		p.Movies = radarr.New(c.URL, c.APIKey, a.client())
	}
	if c, ok, err = a.Config.App(ctx, config.AppSonarr); err != nil {
		return executor.Providers{}, err
	}
	if ok {
		p.Series = sonarr.New(c.URL, c.APIKey, a.client())
	}
	if c, ok, err = a.Config.App(ctx, config.AppQBittorrent); err != nil {
		return executor.Providers{}, err
	}
	if ok {
		client, err := qbittorrent.New(c.URL, c.Username, c.Password, a.Timeout)
		if err != nil {
			return executor.Providers{}, err
		}
		p.Downloads, p.Remover = client, client
	}
	return p, nil
}
```

`cmd/removarr/main.go`:

```go
	exec := &executor.Service{Build: apps.Writers, Store: st, Gate: g, DryRun: env.DryRun, Now: time.Now}
	server := &api.Server{Config: cfg, Store: st, Scanner: scans, Apps: apps, Planner: plans, Executor: exec, Log: log}
```

Documents:

- `AGENTS.md`: replace the rule "No deleting call yet" with: "**Only the executor changes an app.** `internal/executor` is the only code given the write interfaces (`providers.MovieWriter`, `SeriesWriter`, `DownloadRemover`). The scanner and the planner send GET requests only, plus the qBittorrent login. With `REMOVARR_DRY_RUN=true` nothing but GET reaches an app." Add `internal/executor` and `internal/gate` to the layout table.
- `README.md`: the `REMOVARR_DRY_RUN` row says "With `true`, executing a plan sends nothing that changes an app and records a dry run in the history." Document `POST /api/plans/{id}/execute` and `GET /api/actions` where the API is described.
- `CHANGELOG.md`: a "Stage 2d: execute plans and record history (issue #5)" section under "Unreleased", above stage 2c, with Scope (execution, order, failures, shared data at execution, dry run, history, stored lists, one job at a time, unmonitor before delete), Deployment (migration `0004_actions`; start with `REMOVARR_DRY_RUN=true` and compare the history with the plans before deleting for real), Breaking changes (building a plan during a scan is now refused with 409 instead of waiting; plan steps list unmonitoring before deleting and carry `rowKey`).
- `LEARNINGS.md`: under "Sonarr and Radarr", that saving a series with a season unmonitored unmonitors its episodes in Sonarr 4.0.12 (source), and that a series is changed only as a whole, so it is read and sent back with every field; under "qBittorrent", that `hashes=all` removes every torrent and `|` separates hashes, so only a lower-case hex ID is sent; under "Plans", that shared data is decided per torrent at the moment of removal, longest path first.

- [ ] **Step 4: Run every check**

Run the checks in "Global Constraints".
Expected: all pass; `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
git add internal/app internal/api cmd/removarr AGENTS.md README.md CHANGELOG.md LEARNINGS.md
git commit -m "feat: execute plans and list the history over the api"
```

---

## Check on the real apps

Prepared for after the plan gate and the build, not part of any task:

1. **Dry run, through the read-only replay** (`~/.claude/removarr-check`, see "Checking against real apps" in `LEARNINGS.md`): start removarr with `REMOVARR_DRY_RUN=true`, scan, build a plan for one movie, one series or season and one torrent row, execute it, and compare the history with the plan: every step `dry-run`, the shared-data decisions as the plan states them, the stored lists unchanged. The replay refuses every request but GET, so nothing can change; its log must show no refused request.
2. **Dry run off, by the owner:** delete one item of each kind (a movie, a season of a series that is still airing, an ended series, an orphan torrent) and confirm in Sonarr, Radarr and qBittorrent what was removed, and that the season's missing episodes are unmonitored too (spec 16.2). If qBittorrent asks for a login, once with the login configured.
