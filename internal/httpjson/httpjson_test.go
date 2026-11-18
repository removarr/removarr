package httpjson

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
