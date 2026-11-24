package store

import (
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

var firstIgnore = time.Date(2026, 3, 11, 9, 0, 0, 0, time.UTC)

func TestIgnoresAreSavedAndRemoved(t *testing.T) {
	s := open(t)
	if got, err := s.Ignores(ctx); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("Ignores on an empty store = %v, %v; want an empty, non-nil slice", got, err)
	}
	movie := domain.Ignore{Kind: domain.IgnoreMovie, Key: "tmdb:603", Title: "The Matrix", CreatedAt: firstIgnore}
	season := domain.Ignore{Kind: domain.IgnoreSeason, Key: "tvdb:100:s2", Title: "The Show", CreatedAt: firstIgnore}
	if err := s.AddIgnores(ctx, []domain.Ignore{season, movie, movie}); err != nil {
		t.Fatal(err)
	}
	// Ignoring again keeps the first title and time.
	again := movie
	again.Title, again.CreatedAt = "Renamed", firstIgnore.Add(time.Hour)
	if err := s.AddIgnores(ctx, []domain.Ignore{again}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Ignores(ctx)
	if err != nil || !reflect.DeepEqual(got, []domain.Ignore{movie, season}) {
		t.Fatalf("Ignores = %+v, %v; want the movie then the season, as first saved", got, err)
	}

	if err := s.RemoveIgnores(ctx, []domain.RowRef{
		{List: domain.ListMovies, Key: "tmdb:603"},
		{List: domain.ListTorrents, Key: "hash:never-ignored"},
	}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Ignores(ctx); !reflect.DeepEqual(got, []domain.Ignore{season}) {
		t.Errorf("after removing the movie: %+v, want the season alone", got)
	}
}

// The list is matched in the same statement that deletes, so a request that runs alongside
// cannot change what is removed between a read and the delete.
func TestRemoveIgnoresRemovesOnlyIgnoresOfTheListNamed(t *testing.T) {
	s := open(t)
	torrent := domain.Ignore{Kind: domain.IgnoreTorrent, Key: "hash:abc", Title: "Some.Torrent", CreatedAt: firstIgnore}
	season := domain.Ignore{Kind: domain.IgnoreSeason, Key: "tvdb:100:s2", Title: "The Show", CreatedAt: firstIgnore}
	if err := s.AddIgnores(ctx, []domain.Ignore{torrent, season}); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveIgnores(ctx, []domain.RowRef{
		{List: domain.ListMovies, Key: "hash:abc"},
		{List: domain.ListSeries, Key: "tvdb:100:s2"},
	}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Ignores(ctx); !reflect.DeepEqual(got, []domain.Ignore{torrent}) {
		t.Errorf("ignores = %+v, want the torrent kept and the season removed", got)
	}
}

func TestIgnoresPersistAcrossScans(t *testing.T) {
	s := open(t)
	ig := domain.Ignore{Kind: domain.IgnoreTorrent, Key: "hash:abc", Title: "Some.Torrent", CreatedAt: firstIgnore}
	if err := s.AddIgnores(ctx, []domain.Ignore{ig}); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		started := firstIgnore.Add(time.Duration(n+1) * time.Hour)
		id, err := s.BeginScan(ctx, started)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.FinishScan(ctx, id, started.Add(time.Minute), domain.ScanSummary{}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := s.Ignores(ctx); !reflect.DeepEqual(got, []domain.Ignore{ig}) {
		t.Errorf("after two scans: %+v, want the ignore unchanged", got)
	}
}
