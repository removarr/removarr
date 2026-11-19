package scanner

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

var (
	ctx = context.Background()
	now = time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
)

type fakeServer struct {
	lib   domain.Library
	err   error
	calls int
}

func (f *fakeServer) Users(context.Context) ([]domain.User, error) {
	return []domain.User{{ID: "a", Name: "Alice"}}, f.err
}

func (f *fakeServer) Library(context.Context, []string) (domain.Library, error) {
	f.calls++
	return f.lib, f.err
}

type fakeMovies struct {
	snap domain.MovieSnapshot
	err  error
}

func (f *fakeMovies) Movies(context.Context) (domain.MovieSnapshot, error) { return f.snap, f.err }

type fakeSeries struct {
	snap domain.SeriesSnapshot
	err  error
}

func (f *fakeSeries) Series(context.Context) (domain.SeriesSnapshot, error) { return f.snap, f.err }

type fakeDownloads struct {
	list      []domain.Download
	err       error
	files     map[string][]domain.DownloadFile // download ID -> its files
	filesErr  error
	fileCalls []string      // the download IDs Files was asked for, in order
	entered   chan struct{} // when set, receives once Downloads has been called
	release   chan struct{} // when set, Downloads waits until it is closed
}

func (f *fakeDownloads) Files(_ context.Context, id string) ([]domain.DownloadFile, error) {
	f.fileCalls = append(f.fileCalls, id)
	return f.files[id], f.filesErr
}

func (f *fakeDownloads) Downloads(context.Context) ([]domain.Download, error) {
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.release != nil {
		<-f.release
	}
	return f.list, f.err
}

type fakeStore struct {
	begun, finished int
	summary         domain.ScanSummary
	candidates      []domain.Candidate
}

func (f *fakeStore) BeginScan(context.Context, time.Time) (int64, error) {
	f.begun++
	return int64(f.begun), nil
}

func (f *fakeStore) FinishScan(_ context.Context, _ int64, _ time.Time, s domain.ScanSummary, c []domain.Candidate) error {
	f.finished++
	f.summary, f.candidates = s, c
	return nil
}

// world returns providers for a small library: one watched movie backed by torrent h1, one
// watched ended series, and one torrent no media manager knows.
func world() (Providers, *fakeServer) {
	seen := map[string]domain.UserState{"a": {Played: true}}
	server := &fakeServer{lib: domain.Library{
		Movies:   []domain.ServerMovie{{ID: "m1", IDs: domain.ExternalIDs{TMDB: "603"}, States: seen}},
		Series:   []domain.ServerSeries{{ID: "s1", IDs: domain.ExternalIDs{TVDB: "100"}}},
		Episodes: []domain.ServerEpisode{{SeriesID: "s1", Season: 1, Episode: 1, States: seen}},
	}}
	movies := &fakeMovies{snap: domain.MovieSnapshot{
		Movies: []domain.Movie{{
			ID: 1, Title: "The Matrix", IDs: domain.ExternalIDs{TMDB: "603"},
			File: &domain.MediaFile{ID: 10, Size: 5000},
		}},
		Links: domain.Links{
			Grabbed:    map[string][]int{"h1": {1}},
			FileSource: map[int]string{10: "h1"},
			Queue:      map[string]bool{},
		},
		RootFolders: []domain.RootFolder{{Path: "/media/movies", FreeBytes: 100}},
	}}
	series := &fakeSeries{snap: domain.SeriesSnapshot{
		Series: []domain.Series{{
			ID: 1, Title: "The Show", Ended: true, IDs: domain.ExternalIDs{TVDB: "100"},
			Episodes: []domain.Episode{{ID: 11, Season: 1, Number: 1, File: &domain.MediaFile{ID: 11, Size: 900}}},
		}},
		Links:       domain.Links{Grabbed: map[string][]int{}, FileSource: map[int]string{}, Queue: map[string]bool{}},
		RootFolders: []domain.RootFolder{{Path: "/media/tv", FreeBytes: 200}, {Path: "/media/movies", FreeBytes: 100}},
	}}
	downloads := &fakeDownloads{list: []domain.Download{{ID: "h1", Name: "The.Matrix"}, {ID: "orphan1", Name: "Unknown"}}}
	return Providers{MediaServer: server, Movies: movies, Series: series, Downloads: downloads}, server
}

func withUser() domain.Rules {
	r := domain.DefaultRules()
	r.CountingUsers = []string{"a"}
	return r
}

func service(p Providers, r domain.Rules, st *fakeStore) *Service {
	return &Service{
		Build: func(context.Context) (Providers, error) { return p, nil },
		Rules: func(context.Context) (domain.Rules, error) { return r, nil },
		Store: st,
		Now:   func() time.Time { return now },
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// run performs one scan and returns what was stored.
func run(t *testing.T, p Providers, r domain.Rules) *fakeStore {
	t.Helper()
	st := &fakeStore{}
	if err := service(p, r, st).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if st.begun != 1 || st.finished != 1 {
		t.Fatalf("scan recorded %d starts and %d finishes, want 1 and 1", st.begun, st.finished)
	}
	return st
}

// expect checks one list's summary: its row count when it was produced, or that its error
// mentions wantError when it was not.
func expect(t *testing.T, st *fakeStore, list domain.ListKind, rows int, wantError string) {
	t.Helper()
	got := st.summary.Lists[list]
	if wantError == "" {
		if got.Error != "" || got.Candidates != rows {
			t.Errorf("%s: %d rows, error %q; want %d rows and no error", list, got.Candidates, got.Error, rows)
		}
		return
	}
	if !strings.Contains(got.Error, wantError) || got.Candidates != 0 {
		t.Errorf("%s: %d rows, error %q; want no rows and an error mentioning %q", list, got.Candidates, got.Error, wantError)
	}
	for _, c := range st.candidates {
		if c.List == list {
			t.Errorf("%s: row %s stored although the list failed", list, c.Key)
		}
	}
}

func TestRunProducesAllThreeLists(t *testing.T) {
	p, _ := world()
	st := run(t, p, withUser())

	expect(t, st, domain.ListMovies, 1, "")
	expect(t, st, domain.ListSeries, 1, "")
	expect(t, st, domain.ListTorrents, 1, "")

	keys := map[string]bool{}
	for _, c := range st.candidates {
		keys[c.Key] = true
	}
	for _, want := range []string{"tmdb:603", "tvdb:100", "hash:orphan1"} {
		if !keys[want] {
			t.Errorf("row %s missing; stored rows: %v", want, keys)
		}
	}
	if len(st.candidates) != 3 {
		t.Errorf("%d rows stored, want 3", len(st.candidates))
	}
	if len(st.summary.RootFolders) != 2 {
		t.Errorf("RootFolders = %+v, want /media/movies and /media/tv once each", st.summary.RootFolders)
	}
}

func TestRunWithoutCountingUsers(t *testing.T) {
	p, server := world()
	st := run(t, p, domain.DefaultRules())

	expect(t, st, domain.ListMovies, 0, "No counting users")
	expect(t, st, domain.ListSeries, 0, "No counting users")
	expect(t, st, domain.ListTorrents, 1, "")
	if server.calls != 0 {
		t.Errorf("the media server library was read %d times, want 0", server.calls)
	}
}

func TestRunWhenAMediaManagerFails(t *testing.T) {
	p, _ := world()
	p.Movies = &fakeMovies{err: errors.New("timeout")}
	st := run(t, p, withUser())

	expect(t, st, domain.ListMovies, 0, "movie manager could not be read: timeout")
	expect(t, st, domain.ListSeries, 1, "")
	// h1 belongs to a movie. With the movie manager down it would look like an orphan.
	expect(t, st, domain.ListTorrents, 0, "movie manager could not be read")
}

func TestRunWithOnlyOneMediaManagerConfigured(t *testing.T) {
	p, _ := world()
	p.Movies = nil
	st := run(t, p, withUser())

	expect(t, st, domain.ListMovies, 0, "movie manager is not configured")
	expect(t, st, domain.ListSeries, 1, "")
	expect(t, st, domain.ListTorrents, 2, "") // nothing manages h1 any more
}

func TestRunWithoutAnyMediaManager(t *testing.T) {
	p, _ := world()
	p.Movies, p.Series = nil, nil
	st := run(t, p, withUser())

	expect(t, st, domain.ListTorrents, 0, "No media manager is configured")
}

func TestRunWhenTheDownloadClientFails(t *testing.T) {
	p, _ := world()
	p.Downloads = &fakeDownloads{err: errors.New("connection refused")}
	st := run(t, p, withUser())

	for _, list := range []domain.ListKind{domain.ListMovies, domain.ListSeries, domain.ListTorrents} {
		expect(t, st, list, 0, "download client could not be read: connection refused")
	}
}

func TestRunWithoutAMediaServer(t *testing.T) {
	p, _ := world()
	p.MediaServer = nil
	st := run(t, p, withUser())

	expect(t, st, domain.ListMovies, 0, "media server is not configured")
	expect(t, st, domain.ListSeries, 0, "media server is not configured")
	expect(t, st, domain.ListTorrents, 1, "")
}

func TestRunWhenTheMediaServerFails(t *testing.T) {
	p, server := world()
	server.err = errors.New("401 Unauthorized")
	st := run(t, p, withUser())

	expect(t, st, domain.ListMovies, 0, "media server could not be read: 401 Unauthorized")
	expect(t, st, domain.ListTorrents, 1, "")
}

func waitIdle(t *testing.T, s *Service) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for s.Status().Running {
		if time.Now().After(deadline) {
			t.Fatal("the scan did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestOnlyOneScanAtATime(t *testing.T) {
	p, _ := world()
	dl := &fakeDownloads{entered: make(chan struct{}), release: make(chan struct{})}
	p.Downloads = dl
	st := &fakeStore{}
	svc := service(p, withUser(), st)

	if err := svc.Start(); err != nil {
		t.Fatal(err)
	}
	<-dl.entered // the scan is now waiting inside the download client

	if status := svc.Status(); !status.Running || status.Phase == "" || status.StartedAt == nil {
		t.Errorf("status during a scan = %+v", status)
	}
	if err := svc.Start(); !errors.Is(err, ErrRunning) {
		t.Errorf("second Start = %v, want ErrRunning", err)
	}
	if err := svc.Run(ctx); !errors.Is(err, ErrRunning) {
		t.Errorf("Run during a scan = %v, want ErrRunning", err)
	}

	close(dl.release)
	waitIdle(t, svc)
	if st.finished != 1 {
		t.Errorf("%d scans finished, want 1", st.finished)
	}
	if status := svc.Status(); status.Running || status.LastError != "" {
		t.Errorf("status after a scan = %+v", status)
	}

	dl.entered, dl.release = nil, nil
	if err := svc.Run(ctx); err != nil {
		t.Errorf("a new scan after the first finished: %v", err)
	}
}

func TestAScanThatCannotStartIsReported(t *testing.T) {
	p, _ := world()
	st := &fakeStore{}
	svc := service(p, withUser(), st)
	svc.Rules = func(context.Context) (domain.Rules, error) { return domain.Rules{}, errors.New("database is locked") }

	if err := svc.Run(ctx); err == nil {
		t.Fatal("want an error")
	}
	if st.begun != 0 {
		t.Error("no scan must be recorded when the rules cannot be loaded")
	}
	status := svc.Status()
	if status.Running || !strings.Contains(status.LastError, "database is locked") {
		t.Errorf("status = %+v, want idle with the error kept", status)
	}
}
