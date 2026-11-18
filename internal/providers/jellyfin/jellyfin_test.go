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
	// maxPerPage, when set, caps the items in one response whatever Limit asks for.
	maxPerPage int
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
		if f.maxPerPage > 0 {
			limit = min(limit, f.maxPerPage)
		}
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

func TestAFavouriteUnnumberedEpisodeProtectsItsSeries(t *testing.T) {
	series := `{"Id":"s1","Name":"The Show","UserData":{"IsFavorite":false}}`
	f := &fakeJellyfin{items: map[string][]string{
		"a/Series":  {series},
		"b/Series":  {series},
		"a/Episode": {`{"Id":"sp1","SeriesId":"s1","ParentIndexNumber":0,"UserData":{"IsFavorite":true}}`},
		"b/Episode": {`{"Id":"sp1","SeriesId":"s1","ParentIndexNumber":0,"UserData":{"IsFavorite":false}}`},
	}}
	lib, err := start(t, f, "k").Library(ctx, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Episodes) != 0 {
		t.Errorf("got %d episodes, want none: an episode without a number cannot be matched", len(lib.Episodes))
	}
	if len(lib.Series) != 1 {
		t.Fatalf("got %d series, want 1", len(lib.Series))
	}
	if !reflect.DeepEqual(lib.Series[0].FavoriteOf, map[string]bool{"a": true}) {
		t.Errorf("FavoriteOf = %v, want Alice only: she marked an episode of the series a favourite", lib.Series[0].FavoriteOf)
	}
}

func TestAFavouriteEpisodeOfAnUnknownSeriesIsSkipped(t *testing.T) {
	f := &fakeJellyfin{items: map[string][]string{"a/Episode": {
		`{"Id":"e1","UserData":{"IsFavorite":true}}`,
		`{"Id":"e2","SeriesId":"gone","UserData":{"IsFavorite":true}}`,
	}}}
	lib, err := start(t, f, "k").Library(ctx, []string{"a"})
	if err != nil || len(lib.Series)+len(lib.Episodes) != 0 {
		t.Errorf("Library = %+v, %v; want nothing", lib, err)
	}
}

func TestLibraryReadsEveryPageWhenTheServerCapsThePageSize(t *testing.T) {
	var movies []string
	for i := 1; i <= 5; i++ {
		movies = append(movies, fmt.Sprintf(`{"Id":"m%d","Name":"Movie %d","UserData":{"Played":true}}`, i, i))
	}
	f := &fakeJellyfin{items: map[string][]string{"a/Movie": movies}, maxPerPage: 2}

	lib, err := start(t, f, "k").Library(ctx, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, m := range lib.Movies {
		seen[m.ID]++
	}
	for i := 1; i <= 5; i++ {
		if id := fmt.Sprintf("m%d", i); seen[id] != 1 {
			t.Errorf("movie %s appears %d times, want exactly once", id, seen[id])
		}
	}
	if len(lib.Movies) != 5 {
		t.Errorf("got %d movies, want 5", len(lib.Movies))
	}
}
