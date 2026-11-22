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

func TestLinksReadsEveryPageWhenTheServerCapsThePageSize(t *testing.T) {
	var history []string
	for i := 1; i <= 5; i++ {
		history = append(history, fmt.Sprintf(`{"eventType":"grabbed","downloadId":"H%d","movieId":%d}`, i, i))
	}
	const limit = 2 // the server sends at most this many records, whatever pageSize asks for
	var historyQueries []url.Values
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/v3/history": func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			historyQueries = append(historyQueries, q)
			page, _ := strconv.Atoi(q.Get("page"))
			start := (page - 1) * limit
			var slice []string
			if start >= 0 && start < len(history) {
				slice = history[start:min(start+limit, len(history))]
			}
			fmt.Fprintf(w, `{"page":%d,"pageSize":%d,"totalRecords":%d,"records":[%s]}`,
				page, limit, len(history), strings.Join(slice, ","))
		},
		"/api/v3/queue": paged(nil, nil),
	})

	links, err := c.Links(ctx, LinkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("h%d", i)
		if !reflect.DeepEqual(links.Grabbed[id], []int{i}) {
			t.Errorf("Grabbed[%s] = %v, want [%d]: every record must be read", id, links.Grabbed[id], i)
		}
	}
	if len(historyQueries) != 3 {
		t.Errorf("made %d history requests, want 3 pages of 2", len(historyQueries))
	}
}
