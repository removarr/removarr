package sonarr

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

// The file list and the episode list are two requests. An episode that points at a file the
// file list does not hold means the library changed in between. A size of 0 would be a
// guess: the row would show the wrong size and the file could not be linked by content.
func TestSeriesFailsWhenAnEpisodeFileIsMissingFromTheFileList(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/series", body(`[
		{"id":1,"title":"The Show","tvdbId":100,"status":"ended","tags":[],"statistics":{"episodeFileCount":2}}
	]`))
	mux.HandleFunc("/api/v3/episode", body(`[
		{"id":11,"seasonNumber":1,"episodeNumber":1,"hasFile":true,"episodeFileId":111},
		{"id":12,"seasonNumber":1,"episodeNumber":2,"hasFile":true,"episodeFileId":222}
	]`))
	mux.HandleFunc("/api/v3/episodefile", body(`[{"id":111,"size":900}]`))
	mux.HandleFunc("/api/v3/tag", body(`[]`))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, err := New(srv.URL, "k", srv.Client()).Series(context.Background())
	if err == nil {
		t.Fatal("Series succeeded although episode 12 points at file 222, which the file list does not hold")
	}
	for _, want := range []string{`"The Show"`, "222"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}
