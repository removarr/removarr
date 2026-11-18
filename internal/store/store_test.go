package store

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

var ctx = context.Background()

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "removarr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func count(t *testing.T, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestOpenCreatesAPrivateFileAndCanBeReopened(t *testing.T) {
	path := filepath.Join(t.TempDir(), "removarr.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, "k", "v"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("database file mode = %o, want 600", perm)
	}

	s, err = Open(path) // migrations must not run a second time
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer s.Close()
	if v, ok, _ := s.GetSetting(ctx, "k"); !ok || v != "v" {
		t.Errorf("setting after reopen = %q, %v; want v, true", v, ok)
	}
}

func TestSettings(t *testing.T) {
	s := open(t)
	if _, ok, err := s.GetSetting(ctx, "missing"); ok || err != nil {
		t.Errorf("missing key: ok = %v, err = %v; want false, nil", ok, err)
	}
	if err := s.SetSetting(ctx, "a", "1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, "a", "2"); err != nil {
		t.Fatalf("overwriting: %v", err)
	}
	if v, ok, _ := s.GetSetting(ctx, "a"); !ok || v != "2" {
		t.Errorf("a = %q, %v; want 2, true", v, ok)
	}
	if err := s.DeleteSetting(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetSetting(ctx, "a"); ok {
		t.Error("a must be gone after delete")
	}
	if err := s.DeleteSetting(ctx, "a"); err != nil {
		t.Errorf("deleting a missing key must not fail: %v", err)
	}
}

func TestScanLifecycle(t *testing.T) {
	s := open(t)
	if rec, err := s.LatestScan(ctx); rec != nil || err != nil {
		t.Fatalf("LatestScan on an empty store = %v, %v; want nil, nil", rec, err)
	}

	started := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	finished := started.Add(42 * time.Second)
	id, err := s.BeginScan(ctx, started)
	if err != nil {
		t.Fatal(err)
	}
	if rec, _ := s.LatestScan(ctx); rec != nil {
		t.Error("a running scan must not be returned as the latest scan")
	}

	summary := domain.ScanSummary{
		Lists: map[domain.ListKind]domain.ListSummary{
			domain.ListMovies:   {Candidates: 2, Protected: 1},
			domain.ListSeries:   {Error: "The series manager could not be read: timeout"},
			domain.ListTorrents: {Candidates: 1},
		},
		RootFolders: []domain.RootFolder{{Path: "/media/movies", FreeBytes: 1 << 30}},
	}
	candidates := []domain.Candidate{
		{List: domain.ListMovies, Key: "tmdb:603", Title: "The Matrix", Size: 5000, Torrents: []domain.TorrentInfo{}},
		{List: domain.ListMovies, Key: "tmdb:27205", Title: "Inception", Size: 7000, Torrents: []domain.TorrentInfo{}},
		{List: domain.ListTorrents, Key: "hash:abc", Title: "some.torrent", Reason: domain.ReasonOrphan, Torrents: []domain.TorrentInfo{{ID: "abc"}}},
	}
	if err := s.FinishScan(ctx, id, finished, summary, candidates); err != nil {
		t.Fatal(err)
	}

	rec, err := s.LatestScan(ctx)
	if err != nil || rec == nil {
		t.Fatalf("LatestScan = %v, %v", rec, err)
	}
	if rec.ID != id || rec.Status != ScanDone || !rec.StartedAt.Equal(started) || rec.FinishedAt == nil || !rec.FinishedAt.Equal(finished) {
		t.Errorf("scan record wrong: %+v", rec)
	}
	if !reflect.DeepEqual(rec.Summary, summary) {
		t.Errorf("summary = %+v, want %+v", rec.Summary, summary)
	}

	movies, err := s.Candidates(ctx, id, domain.ListMovies)
	if err != nil {
		t.Fatal(err)
	}
	if len(movies) != 2 || movies[0].Key != "tmdb:27205" || movies[1].Key != "tmdb:603" {
		t.Errorf("movies = %+v, want two rows sorted by key", movies)
	}
	if movies[1].Title != "The Matrix" || movies[1].Size != 5000 {
		t.Errorf("movie row did not round-trip: %+v", movies[1])
	}
	torrents, _ := s.Candidates(ctx, id, domain.ListTorrents)
	if len(torrents) != 1 || torrents[0].Reason != domain.ReasonOrphan {
		t.Errorf("torrents = %+v", torrents)
	}
	series, err := s.Candidates(ctx, id, domain.ListSeries)
	if err != nil || series == nil || len(series) != 0 {
		t.Errorf("series = %v, %v; want an empty, non-nil slice", series, err)
	}
}

func TestANewScanReplacesTheOldCandidates(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

	first, _ := s.BeginScan(ctx, now)
	old := []domain.Candidate{{List: domain.ListMovies, Key: "tmdb:1"}}
	if err := s.FinishScan(ctx, first, now, domain.ScanSummary{}, old); err != nil {
		t.Fatal(err)
	}
	second, _ := s.BeginScan(ctx, now)
	fresh := []domain.Candidate{{List: domain.ListMovies, Key: "tmdb:2"}}
	if err := s.FinishScan(ctx, second, now, domain.ScanSummary{}, fresh); err != nil {
		t.Fatal(err)
	}

	if n := count(t, s, `SELECT COUNT(*) FROM candidates`); n != 1 {
		t.Errorf("%d candidate rows stored, want 1: only the latest scan keeps its rows", n)
	}
	got, _ := s.Candidates(ctx, second, domain.ListMovies)
	if len(got) != 1 || got[0].Key != "tmdb:2" {
		t.Errorf("latest candidates = %+v", got)
	}
}

func TestOnlyTheLastTenScansAreKept(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 12; i++ {
		id, err := s.BeginScan(ctx, now)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.FinishScan(ctx, id, now, domain.ScanSummary{}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if n := count(t, s, `SELECT COUNT(*) FROM scans`); n != 10 {
		t.Errorf("%d scans kept, want 10", n)
	}
}

func TestBeginScanDropsAScanLeftRunning(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	if _, err := s.BeginScan(ctx, now); err != nil { // never finished, as after a crash
		t.Fatal(err)
	}
	if _, err := s.BeginScan(ctx, now); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT COUNT(*) FROM scans WHERE status = ?`, ScanRunning); n != 1 {
		t.Errorf("%d running scans, want 1", n)
	}
}

func TestFinishScanRejectsAnUnknownScan(t *testing.T) {
	s := open(t)
	if err := s.FinishScan(ctx, 999, time.Now(), domain.ScanSummary{}, nil); err == nil {
		t.Error("finishing a scan that does not exist must fail")
	}
}
