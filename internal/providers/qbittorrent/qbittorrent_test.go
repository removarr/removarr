package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/httpjson"
)

var ctx = context.Background()

// fakeQbit imitates the parts of qBittorrent's Web API the client uses.
type fakeQbit struct {
	auth     bool              // require a session cookie
	logins   int               // login attempts seen
	torrents string            // body of /torrents/info
	trackers map[string]string // hash -> body of /torrents/trackers
	files    map[string]string // hash -> body of /torrents/files; an unknown hash gets 404
	deleted  []string          // "hash deleteFiles" of each removal seen
}

func (f *fakeQbit) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/auth/login", func(w http.ResponseWriter, r *http.Request) {
		f.logins++
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.FormValue("username") != "admin" || r.FormValue("password") != "secret" {
			fmt.Fprint(w, "Fails.")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "session", Path: "/"})
		fmt.Fprint(w, "Ok.")
	})
	guarded := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if f.auth {
				if c, err := r.Cookie("SID"); err != nil || c.Value != "session" {
					http.Error(w, "Forbidden", http.StatusForbidden)
					return
				}
			}
			h(w, r)
		}
	}
	mux.HandleFunc("/api/v2/torrents/info", guarded(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, f.torrents)
	}))
	mux.HandleFunc("/api/v2/torrents/trackers", guarded(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, f.trackers[r.URL.Query().Get("hash")])
	}))
	mux.HandleFunc("/api/v2/torrents/files", guarded(func(w http.ResponseWriter, r *http.Request) {
		body, ok := f.files[r.URL.Query().Get("hash")]
		if !ok {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		fmt.Fprint(w, body)
	}))
	mux.HandleFunc("/api/v2/torrents/delete", guarded(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			http.Error(w, "bad content type", http.StatusUnsupportedMediaType)
			return
		}
		f.deleted = append(f.deleted, r.PostFormValue("hashes")+" "+r.PostFormValue("deleteFiles"))
	}))
	mux.HandleFunc("/api/v2/app/version", guarded(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "v5.0.3")
	}))
	return mux
}

func start(t *testing.T, f *fakeQbit, username, password string) *Client {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, username, password, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const oneTorrent = `[
	{"hash":"ABCDEF0123","name":"The.Matrix.1999","size":5000,"state":"stalledUP","category":"radarr",
	 "tags":"keep, cross-seed","content_path":"/downloads/The.Matrix.1999","added_on":1767225600,
	 "ratio":1.5,"seeding_time":172800,"tracker":"https://tracker.example.org:443/announce?passkey=secret"},
	{"hash":"","name":"broken entry"}
]`

func TestDownloadsMapsFields(t *testing.T) {
	c := start(t, &fakeQbit{torrents: oneTorrent}, "", "")
	got, err := c.Downloads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d downloads, want 1: an entry without a hash is skipped", len(got))
	}
	d := got[0]
	if d.ID != "abcdef0123" {
		t.Errorf("ID = %q, want the hash in lower case", d.ID)
	}
	if d.Name != "The.Matrix.1999" || d.Size != 5000 || d.State != "stalledUP" || d.Category != "radarr" || d.ContentPath != "/downloads/The.Matrix.1999" {
		t.Errorf("plain fields wrong: %+v", d)
	}
	if !reflect.DeepEqual(d.Tags, []string{"keep", "cross-seed"}) {
		t.Errorf("Tags = %q, want [keep cross-seed]", d.Tags)
	}
	if want := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC); !d.AddedAt.Equal(want) {
		t.Errorf("AddedAt = %v, want %v", d.AddedAt, want)
	}
	if d.Ratio == nil || *d.Ratio != 1.5 {
		t.Errorf("Ratio = %v, want 1.5", d.Ratio)
	}
	if d.SeedingTime == nil || *d.SeedingTime != 48*time.Hour {
		t.Errorf("SeedingTime = %v, want 48h", d.SeedingTime)
	}
	if d.Tracker != "tracker.example.org" {
		t.Errorf("Tracker = %q, want only the host name, never the passkey", d.Tracker)
	}
}

func TestFilesListsTheFilesOfOneTorrent(t *testing.T) {
	f := &fakeQbit{files: map[string]string{"abc": `[
		{"index":0,"name":"Show.S01/Show.S01E01.mkv","size":1500000000,"progress":1,"priority":1},
		{"index":1,"name":"Show.S01/Show.S01E01.nfo","size":812,"progress":1,"priority":1}
	]`}}
	c := start(t, f, "", "")
	got, err := c.Files(ctx, "abc")
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.DownloadFile{
		{Name: "Show.S01/Show.S01E01.mkv", Size: 1500000000},
		{Name: "Show.S01/Show.S01E01.nfo", Size: 812},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Files = %+v, want %+v", got, want)
	}
}

func TestFilesOfATorrentWithoutFilesIsEmptyNotNil(t *testing.T) {
	c := start(t, &fakeQbit{files: map[string]string{"abc": `[]`}}, "", "")
	got, err := c.Files(ctx, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("Files = %#v, want an empty, non-nil list", got)
	}
}

// A torrent removed between listing the torrents and asking for its files answers 404.
// That must be an error, so the scan does not go on with a partial answer.
func TestFilesOfAnUnknownTorrentIsAnError(t *testing.T) {
	c := start(t, &fakeQbit{files: map[string]string{}}, "", "")
	_, err := c.Files(ctx, "gone")
	var status *httpjson.StatusError
	if !errors.As(err, &status) || status.Status != http.StatusNotFound {
		t.Errorf("err = %v, want a 404 status error", err)
	}
}

func TestFilesLogsInWhenNeeded(t *testing.T) {
	f := &fakeQbit{auth: true, files: map[string]string{"abc": `[{"name":"a.mkv","size":7}]`}}
	c := start(t, f, "admin", "secret")
	got, err := c.Files(ctx, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Size != 7 || f.logins != 1 {
		t.Errorf("Files = %+v after %d logins, want one file after one login", got, f.logins)
	}
}

func TestDownloadsLogsInOnce(t *testing.T) {
	f := &fakeQbit{auth: true, torrents: oneTorrent}
	c := start(t, f, "admin", "secret")
	for i := 0; i < 2; i++ {
		if _, err := c.Downloads(ctx); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	if f.logins != 1 {
		t.Errorf("logged in %d times, want 1: the session is reused", f.logins)
	}
}

func TestDownloadsWithAWrongPassword(t *testing.T) {
	c := start(t, &fakeQbit{auth: true, torrents: oneTorrent}, "admin", "wrong")
	if _, err := c.Downloads(ctx); !errors.Is(err, ErrLogin) {
		t.Errorf("err = %v, want ErrLogin", err)
	}
}

func TestDownloadsWhenALoginIsNeededButNotConfigured(t *testing.T) {
	f := &fakeQbit{auth: true, torrents: oneTorrent}
	c := start(t, f, "", "")
	if _, err := c.Downloads(ctx); !errors.Is(err, ErrLoginRequired) {
		t.Errorf("err = %v, want ErrLoginRequired", err)
	}
	if f.logins != 0 {
		t.Errorf("logged in %d times, want 0: there are no credentials to send", f.logins)
	}
}

func TestDownloadsFallsBackToTheTrackerList(t *testing.T) {
	f := &fakeQbit{
		torrents: `[{"hash":"aa11","name":"x","tracker":""}]`,
		trackers: map[string]string{
			"aa11": `[{"url":"** [DHT] **"},{"url":"** [PeX] **"},{"url":"udp://open.tracker.example:1337/announce"}]`,
		},
	}
	got, err := start(t, f, "", "").Downloads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Tracker != "open.tracker.example" {
		t.Errorf("Tracker = %q, want open.tracker.example", got[0].Tracker)
	}
}

func TestTestReportsTheVersion(t *testing.T) {
	detail, err := start(t, &fakeQbit{auth: true}, "admin", "secret").Test(ctx)
	if err != nil || detail != "qBittorrent v5.0.3" {
		t.Errorf("Test = %q, %v", detail, err)
	}
}

func TestBasePathIsKept(t *testing.T) {
	f := &fakeQbit{auth: true, torrents: oneTorrent}
	srv := httptest.NewServer(http.StripPrefix("/qbt", f.handler()))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL+"/qbt", "admin", "secret", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.Downloads(ctx); err != nil || len(got) != 1 {
		t.Errorf("behind a base path: %d downloads, %v", len(got), err)
	}
}

func TestARedirectToAnotherHostIsNotFollowed(t *testing.T) {
	var reached atomic.Int32
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Add(1) }))
	t.Cleanup(b.Close)
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, b.URL, http.StatusFound)
	}))
	t.Cleanup(a.Close)

	c, err := New(a.URL, "", "", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Test(context.Background()); err == nil {
		t.Error("Test must fail when qBittorrent redirects to another host")
	}
	if n := reached.Load(); n != 0 {
		t.Errorf("the other host received %d requests, want none", n)
	}
}

func TestRemoveDownloadSendsTheHashAndWhetherToDeleteTheData(t *testing.T) {
	f := &fakeQbit{}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c, _ := New(srv.URL, "", "", time.Second)
	hash := "0123456789abcdef0123456789abcdef01234567"
	if err := c.RemoveDownload(ctx, hash, true); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveDownload(ctx, hash, false); err != nil {
		t.Fatal(err)
	}
	want := []string{hash + " true", hash + " false"}
	if !reflect.DeepEqual(f.deleted, want) {
		t.Errorf("deleted = %q, want %q", f.deleted, want)
	}
}

func TestRemoveDownloadLogsInWhenAsked(t *testing.T) {
	f := &fakeQbit{auth: true}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c, _ := New(srv.URL, "admin", "secret", time.Second)
	if err := c.RemoveDownload(ctx, "ab", false); err != nil {
		t.Fatal(err)
	}
	if f.logins != 1 || !reflect.DeepEqual(f.deleted, []string{"ab false"}) {
		t.Errorf("logins = %d, deleted = %q; want one login and one removal", f.logins, f.deleted)
	}
}

func TestRemoveDownloadWithoutCredentialsNeedsALogin(t *testing.T) {
	f := &fakeQbit{auth: true}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c, _ := New(srv.URL, "", "", time.Second)
	if err := c.RemoveDownload(ctx, "ab", false); !errors.Is(err, ErrLoginRequired) {
		t.Errorf("err = %v, want ErrLoginRequired", err)
	}
}

func TestRemoveDownloadRefusesAnIDThatIsNotAHash(t *testing.T) {
	f := &fakeQbit{}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c, _ := New(srv.URL, "", "", time.Second)
	// "all" would remove every torrent, and "|" separates several hashes.
	for _, id := range []string{"", "all", "ab|cd", "AB", "ab cd", "ab\n"} {
		if err := c.RemoveDownload(ctx, id, true); err == nil {
			t.Errorf("RemoveDownload(%q) = nil, want refused", id)
		}
	}
	if len(f.deleted) != 0 {
		t.Errorf("requests sent: %q", f.deleted)
	}
}

func TestATransportErrorDoesNotRepeatTheURL(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base, addr := srv.URL, srv.Listener.Addr().String()
	srv.Close()
	c, err := New(base, "", "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	err = c.RemoveDownload(ctx, "0123456789abcdef0123456789abcdef01234567", true)
	if err == nil {
		t.Fatal("want an error from a server that is gone")
	}
	for _, s := range []string{base, addr, "deleteFiles"} {
		if strings.Contains(err.Error(), s) {
			t.Errorf("err = %q repeats %q", err, s)
		}
	}
}

func TestALoginTransportErrorDoesNotRepeatTheURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/auth/login" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			conn.Close() // no answer at all
		}
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, "admin", "secret", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Downloads(ctx)
	if err == nil {
		t.Fatal("want an error when the login gets no answer")
	}
	for _, s := range []string{srv.URL, srv.Listener.Addr().String(), "auth/login"} {
		if strings.Contains(err.Error(), s) {
			t.Errorf("err = %q repeats %q", err, s)
		}
	}
}
