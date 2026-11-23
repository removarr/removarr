package api

import (
	"context"
	"net/http"
	"reflect"
	"strings"
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
		ignoreBody("movies/tmdb:604", "movies/tmdb:999"), // not in the scan
		ignoreBody("movies/tmdb:604", "series/tmdb:603"), // in the scan, but in another list
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

// Un-ignoring removes only ignores of the list named; a key of another list is skipped like
// a key that is not ignored.
func TestUnignoreRemovesOnlyIgnoresOfTheListNamed(t *testing.T) {
	h := newHarness(t, nil)
	storeTiedScan(t, h)
	if rec := h.do(http.MethodPost, "/api/ignores", ignoreBody("torrents/hash:abc")); rec.Code != http.StatusNoContent {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	if rec := h.do(http.MethodDelete, "/api/ignores", ignoreBody("movies/hash:abc")); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d %s, want 204", rec.Code, rec.Body)
	}
	if got := savedIgnores(t, h); !reflect.DeepEqual(got, []string{"torrent hash:abc Some.Torrent"}) {
		t.Errorf("saved = %v, want the torrent ignore kept", got)
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
