package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/gate"
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

	h.scanner.err = &gate.BusyError{Job: gate.Scan}
	if rec := h.do(http.MethodPost, "/api/scans", ""); rec.Code != http.StatusConflict {
		t.Errorf("while running: status = %d, want 409", rec.Code)
	}
	h.scanner.err = &gate.BusyError{Job: gate.Plan}
	rec = h.do(http.MethodPost, "/api/scans", "")
	if rec.Code != http.StatusConflict || decode[errorBody](t, rec).Error != gate.Plan {
		t.Errorf("while a plan is built: %d %s, want 409 naming the plan", rec.Code, rec.Body)
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

// racingStore lets a scan finish right after every read, the way a scan that ends while a
// request is being answered would.
type racingStore struct {
	*store.Store
	afterRead func()
}

func (r *racingStore) ReadLatest(ctx context.Context) (store.Latest, error) {
	l, err := r.Store.ReadLatest(ctx)
	r.afterRead()
	return l, err
}

// Finishing a scan drops the rows of the scan before it. A request that read the older
// scan and then its rows used to answer with the older time and no rows.
func TestCandidatesBelongToTheScanTheyAreReportedFor(t *testing.T) {
	h := newHarness(t, nil)
	storeScan(t, h) // finished at 12:01 with the row tmdb:603

	second := time.Date(2026, 3, 10, 13, 0, 0, 0, time.UTC)
	finished := false
	racing := &racingStore{Store: h.store}
	racing.afterRead = func() {
		if finished {
			return
		}
		finished = true
		ctx := context.Background()
		id, err := h.store.BeginScan(ctx, second)
		if err != nil {
			t.Fatal(err)
		}
		summary := domain.ScanSummary{Lists: map[domain.ListKind]domain.ListSummary{domain.ListMovies: {Candidates: 1}}}
		rows := []domain.Candidate{{List: domain.ListMovies, Key: "tmdb:604", Title: "The Matrix Reloaded", Torrents: []domain.TorrentInfo{}}}
		if err := h.store.FinishScan(ctx, id, second.Add(time.Minute), summary, rows); err != nil {
			t.Fatal(err)
		}
	}
	h.handler = (&Server{
		Config: h.config, Store: racing, Scanner: h.scanner, Apps: h.apps,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}).Handler()

	type body struct {
		ScannedAt  *time.Time         `json:"scannedAt"`
		Candidates []domain.Candidate `json:"candidates"`
	}
	got := decode[body](t, h.do(http.MethodGet, "/api/candidates?list=movies", ""))

	if got.ScannedAt == nil || len(got.Candidates) != 1 {
		t.Fatalf("answer = %+v; want one scan time and its one row", got)
	}
	wantKey := map[time.Time]string{
		time.Date(2026, 3, 10, 12, 1, 0, 0, time.UTC): "tmdb:603",
		second.Add(time.Minute):                       "tmdb:604",
	}[got.ScannedAt.UTC()]
	if got.Candidates[0].Key != wantKey {
		t.Errorf("scan of %v answered with row %s, want %s", got.ScannedAt, got.Candidates[0].Key, wantKey)
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

// storeScanWithFingerprint saves a finished scan whose summary carries the given fingerprint.
func storeScanWithFingerprint(t *testing.T, h *harness, fingerprint string) {
	t.Helper()
	ctx := context.Background()
	started := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	id, err := h.store.BeginScan(ctx, started)
	if err != nil {
		t.Fatal(err)
	}
	summary := domain.ScanSummary{
		Lists:               map[domain.ListKind]domain.ListSummary{domain.ListMovies: {}},
		SettingsFingerprint: fingerprint,
	}
	if err := h.store.FinishScan(ctx, id, started.Add(time.Minute), summary, nil); err != nil {
		t.Fatal(err)
	}
}

type staleBody struct {
	Stale bool `json:"stale"`
}

func TestStatusReportsListsOutOfDateWhenSettingsChangedSinceTheScan(t *testing.T) {
	h := newHarness(t, nil)
	current, err := h.config.Fingerprint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	storeScanWithFingerprint(t, h, current)

	if got := decode[staleBody](t, h.do(http.MethodGet, "/api/status", "")); got.Stale {
		t.Error("stale = true right after a scan with the current settings")
	}

	rec := h.do(http.MethodPut, "/api/settings", `{"rules": {"graceDays": 3}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/settings = %d %s", rec.Code, rec.Body)
	}
	if got := decode[staleBody](t, h.do(http.MethodGet, "/api/status", "")); !got.Stale {
		t.Error("stale = false after the rules changed since the scan")
	}
}

func TestStatusDoesNotCallListsOutOfDateWithoutAFingerprint(t *testing.T) {
	h := newHarness(t, nil)
	if got := decode[staleBody](t, h.do(http.MethodGet, "/api/status", "")); got.Stale {
		t.Error("stale = true with no scan at all")
	}

	storeScanWithFingerprint(t, h, "")
	if got := decode[staleBody](t, h.do(http.MethodGet, "/api/status", "")); got.Stale {
		t.Error("stale = true for a scan from before fingerprints")
	}
}
