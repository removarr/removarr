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
