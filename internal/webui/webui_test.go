package webui

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

var built = fstest.MapFS{
	"index.html":       {Data: []byte("<!doctype html><title>removarr</title>")},
	"assets/app-1a.js": {Data: []byte("console.log(1)")},
}

func get(t *testing.T, h http.Handler, method, path string) *http.Response {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec.Result()
}

func body(t *testing.T, r *http.Response) string {
	b, _ := io.ReadAll(r.Body)
	return string(b)
}

func TestServesAFileWithLongCachingForAssets(t *testing.T) {
	r := get(t, Handler(built), "GET", "/assets/app-1a.js")
	if r.StatusCode != 200 || body(t, r) != "console.log(1)" {
		t.Fatalf("got %d %q", r.StatusCode, body(t, r))
	}
	if cc := r.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q", cc)
	}
}

func TestServesTheIndexForAPathTheUIHandlesItself(t *testing.T) {
	for _, p := range []string{"/", "/series", "/settings/connections"} {
		r := get(t, Handler(built), "GET", p)
		if r.StatusCode != 200 || !strings.Contains(body(t, r), "<title>removarr</title>") {
			t.Errorf("%s: got %d", p, r.StatusCode)
		}
		if cc := r.Header.Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("%s: Cache-Control = %q", p, cc)
		}
	}
}

func TestServesTheIndexForAPathWithATrailingSlash(t *testing.T) {
	sub, err := fs.Sub(fstest.MapFS{"dist/index.html": built["index.html"]}, "dist") // like Files(): its Stat rejects names that are not valid paths
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/series/", "/settings/connections/"} {
		r := get(t, Handler(sub), "GET", p)
		if r.StatusCode != 200 || !strings.Contains(body(t, r), "<title>removarr</title>") {
			t.Errorf("%s: got %d", p, r.StatusCode)
		}
	}
}

func TestRefusesMethodsOtherThanGetAndHead(t *testing.T) {
	r := get(t, Handler(built), "POST", "/")
	if r.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("got %d", r.StatusCode)
	}
	if a := r.Header.Get("Allow"); a != "GET, HEAD" {
		t.Errorf("Allow = %q", a)
	}
}

func TestSaysSoWhenTheUIWasNotBuilt(t *testing.T) {
	r := get(t, Handler(fstest.MapFS{".gitkeep": {}}), "GET", "/movies")
	if r.StatusCode != 200 || !strings.Contains(body(t, r), "pnpm --dir web build") {
		t.Fatalf("got %d %q", r.StatusCode, body(t, r))
	}
	if ct := r.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestEmbeddedFilesAreRootedAtDist(t *testing.T) {
	if _, err := fs.Stat(Files(), ".gitkeep"); err != nil {
		t.Fatalf("Files() is not rooted at dist: %v", err)
	}
}
