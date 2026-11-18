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
