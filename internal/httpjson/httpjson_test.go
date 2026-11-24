package httpjson

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var ctx = context.Background()

func serve(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func TestDoSendsTheMethodAndAJSONBody(t *testing.T) {
	var method, ctype, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		method, ctype, body = r.Method, r.Header.Get("Content-Type"), strings.TrimSpace(string(b))
	}))
	defer srv.Close()
	if err := Do(context.Background(), srv.Client(), http.MethodPut, srv.URL, nil, map[string]any{"ids": []int{1, 2}}, nil); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPut || ctype != "application/json" || body != `{"ids":[1,2]}` {
		t.Errorf("got %s %q %s", method, ctype, body)
	}
}

func TestGetDecodesJSONAndSendsHeaders(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "yes" || r.Header.Get("Accept") != "application/json" {
			http.Error(w, "bad headers", http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"name":"ok"}`))
	})
	var out struct {
		Name string `json:"name"`
	}
	if err := Get(ctx, srv.Client(), srv.URL, http.Header{"X-Test": {"yes"}}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Name != "ok" {
		t.Errorf("Name = %q, want ok", out.Name)
	}
}

func TestGetReturnsAStatusError(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
	err := Get(ctx, srv.Client(), srv.URL, nil, nil)
	var se *StatusError
	if !errors.As(err, &se) || se.Status != http.StatusUnauthorized || !strings.Contains(se.Body, "Unauthorized") {
		t.Errorf("err = %v, want a StatusError for 401 carrying the body", err)
	}
}

func TestGetReportsBadJSON(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>login</html>")) })
	var out map[string]any
	err := Get(ctx, srv.Client(), srv.URL, nil, &out)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Errorf("err = %v, want a decode error", err)
	}
}

func TestParseTime(t *testing.T) {
	good := map[string]string{
		"2026-03-01T20:15:30Z":         "2026-03-01T20:15:30Z",
		"2026-03-01T20:15:30.1234567Z": "2026-03-01T20:15:30.1234567Z",
		"2026-03-01T21:15:30+01:00":    "2026-03-01T20:15:30Z",
		"2026-03-01T20:15:30":          "2026-03-01T20:15:30Z", // no zone: taken as UTC
		"2026-03-01T20:15:30.5":        "2026-03-01T20:15:30.5Z",
	}
	for in, want := range good {
		wantTime, err := time.Parse(time.RFC3339Nano, want)
		if err != nil {
			t.Fatal(err)
		}
		got := ParseTime(in)
		if got == nil || !got.Equal(wantTime) {
			t.Errorf("ParseTime(%q) = %v, want %v", in, got, wantTime)
		}
	}
	for _, in := range []string{"", "yesterday", "0001-01-01T00:00:00Z", "2026-13-45T00:00:00Z"} {
		if got := ParseTime(in); got != nil {
			t.Errorf("ParseTime(%q) = %v, want nil", in, got)
		}
	}
}

func TestSameHostRedirectsRefusesAnotherHost(t *testing.T) {
	var reached atomic.Int32
	b := serve(t, func(w http.ResponseWriter, r *http.Request) { reached.Add(1) })
	a := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, b.URL, http.StatusFound)
	})
	c := &http.Client{CheckRedirect: SameHostRedirects}
	if err := Get(ctx, c, a.URL, http.Header{"X-Api-Key": {"k"}}, nil); err == nil {
		t.Error("a redirect to another host must fail")
	}
	if n := reached.Load(); n != 0 {
		t.Errorf("the other host received %d requests, want none", n)
	}
}

func TestSameHostRedirectsRefusesARedirectThatTurnsAWriteIntoAGet(t *testing.T) {
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther} {
		var methods []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			methods = append(methods, r.Method)
			if r.URL.Path == "/new" {
				w.Write([]byte(`{}`))
				return
			}
			http.Redirect(w, r, "/new", status)
		}))
		c := &http.Client{CheckRedirect: SameHostRedirects}
		err := Do(ctx, c, http.MethodDelete, srv.URL+"/old", nil, nil, nil)
		srv.Close()
		if err == nil {
			t.Errorf("status %d: the DELETE reported success after a redirect", status)
		}
		if err != nil && strings.Contains(err.Error(), srv.URL) {
			t.Errorf("status %d: the error repeats the URL: %v", status, err)
		}
		if len(methods) != 1 {
			t.Errorf("status %d: server saw %v, want only the DELETE", status, methods)
		}
	}
}

func TestSameHostRedirectsFollowsTheSameHost(t *testing.T) {
	a := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/new", http.StatusFound)
			return
		}
		w.Write([]byte(`{"name":"moved"}`))
	})
	c := &http.Client{CheckRedirect: SameHostRedirects}
	var out struct {
		Name string `json:"name"`
	}
	if err := Get(ctx, c, a.URL+"/old", nil, &out); err != nil || out.Name != "moved" {
		t.Errorf("Get = %v, %+v; want the redirected body", err, out)
	}
}

// The host is the same, but the API key would travel unencrypted.
func TestSameHostRedirectsRefusesADowngradeToHTTP(t *testing.T) {
	request := func(rawURL string) *http.Request {
		t.Helper()
		r, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	first := request("https://media.example/api")

	if err := SameHostRedirects(request("http://media.example/api"), []*http.Request{first}); err == nil {
		t.Error("a redirect from https to http on the same host must fail")
	}
	later := []*http.Request{first, request("https://media.example/moved")}
	if err := SameHostRedirects(request("http://media.example/api"), later); err == nil {
		t.Error("a later redirect from https to http must fail too")
	}
	if err := SameHostRedirects(request("https://media.example/moved"), []*http.Request{first}); err != nil {
		t.Errorf("https to https on the same host: %v", err)
	}
	if err := SameHostRedirects(request("https://media.example/api"), []*http.Request{request("http://media.example/api")}); err != nil {
		t.Errorf("an upgrade from http to https on the same host: %v", err)
	}
}

func TestSameHostRedirectsStopsAfterTenRedirects(t *testing.T) {
	a := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusFound)
	})
	c := &http.Client{CheckRedirect: SameHostRedirects}
	if err := Get(ctx, c, a.URL, nil, nil); err == nil {
		t.Error("an endless redirect loop must fail")
	}
}
