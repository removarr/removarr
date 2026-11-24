package radarr

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/removarr/removarr/internal/domain"
)

func body(s string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, s) }
}

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
