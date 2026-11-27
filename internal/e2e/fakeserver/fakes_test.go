package main

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

// scanAndCount runs one scan through the fake wiring, as the Scan button starts one, and
// returns the number of listed rows of each list.
func scanAndCount(t *testing.T, w *world) map[domain.ListKind]int {
	t.Helper()
	if err := w.scans.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for w.scans.Status().Running {
		if time.Now().After(deadline) {
			t.Fatal("the scan did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if e := w.scans.Status().LastError; e != "" {
		t.Fatalf("the scan failed: %s", e)
	}
	latest, err := w.store.ReadLatest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if latest.Scan == nil {
		t.Fatal("no scan was stored")
	}
	for list, s := range latest.Scan.Summary.Lists {
		if s.Error != "" {
			t.Errorf("%s: %s", list, s.Error)
		}
	}
	counts := map[domain.ListKind]int{}
	for _, row := range latest.Rows {
		if !row.Waiting {
			counts[row.List]++
		}
	}
	return counts
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w, err := wire(t.TempDir(), 0, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

func TestTheFixtureLibraryListsTwoMoviesOneSeriesAndOneTorrent(t *testing.T) {
	w := newWorld(t)
	got := scanAndCount(t, w)
	want := map[domain.ListKind]int{domain.ListMovies: 2, domain.ListSeries: 1, domain.ListTorrents: 1}
	for list, n := range want {
		if got[list] != n {
			t.Errorf("%s: %d rows, want %d", list, got[list], n)
		}
	}
}

func TestADeletionChangesTheFakeLibrary(t *testing.T) {
	w := newWorld(t)
	scanAndCount(t, w)
	ctx := context.Background()
	plan, err := w.plans.Plan(ctx, []domain.RowRef{{List: domain.ListMovies, Key: "tmdb:101"}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.exec.Execute(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Actions) != 1 || res.Actions[0].Outcome != domain.OutcomeDone {
		t.Fatalf("execution: %+v, want one item done", res.Actions)
	}
	if calls := w.lib.writes(); len(calls) != 2 {
		t.Errorf("the fakes recorded %q, want the movie and its torrent removed", calls)
	}
	got := scanAndCount(t, w)
	if got[domain.ListMovies] != 1 || got[domain.ListTorrents] != 1 {
		t.Errorf("after the deletion: %v, want 1 movie and the orphan torrent", got)
	}
}

func TestTheTesterAnswersFake(t *testing.T) {
	w := newWorld(t)
	tester, err := w.apps.Tester(context.Background(), "jellyfin")
	if err != nil {
		t.Fatal(err)
	}
	if detail, err := tester.Test(context.Background()); err != nil || detail != "fake" {
		t.Errorf("Test() = %q, %v; want \"fake\"", detail, err)
	}
}
