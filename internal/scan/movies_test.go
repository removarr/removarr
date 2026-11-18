package scan

import (
	"reflect"
	"testing"

	"github.com/removarr/removarr/internal/domain"
)

// movieInput returns a scan input with one movie, The Matrix, watched by Alice nine days ago
// and backed by torrent h1. mods adjust the input before the link index is built.
func movieInput(mods ...func(*MoviesInput)) MoviesInput {
	l := links()
	l.Grabbed["h1"] = []int{1}
	l.FileSource[10] = "h1"
	in := MoviesInput{
		Rules: rules("all", "a"),
		Now:   *tp("2026-03-10T12:00:00Z"),
		Users: []domain.User{{ID: "a", Name: "Alice"}, {ID: "b", Name: "Bob"}},
		Library: domain.Library{Movies: []domain.ServerMovie{{
			ID: "j1", Title: "The Matrix",
			IDs:    domain.ExternalIDs{TMDB: "603", IMDB: "tt0133093"},
			States: map[string]domain.UserState{"a": played("2026-03-01T20:00:00Z")},
		}}},
		Snapshot: domain.MovieSnapshot{
			Movies: []domain.Movie{{
				ID: 1, Title: "The Matrix", Year: 1999,
				IDs:  domain.ExternalIDs{TMDB: "603", IMDB: "tt0133093"},
				File: &domain.MediaFile{ID: 10, Size: 5000},
			}},
			Links: l,
		},
		Downloads: []domain.Download{dl("h1")},
	}
	for _, m := range mods {
		m(&in)
	}
	var fileIDs []int
	for _, m := range in.Snapshot.Movies {
		if m.File != nil {
			fileIDs = append(fileIDs, m.File.ID)
		}
	}
	in.Index = NewLinkIndex(ManagerFiles{Links: in.Snapshot.Links, FileIDs: fileIDs})
	return in
}

func TestMoviesSuggestsAWatchedMovie(t *testing.T) {
	res := Movies(movieInput())
	if len(res.Candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(res.Candidates))
	}
	c := res.Candidates[0]
	if c.List != domain.ListMovies || c.Key != "tmdb:603" || c.Title != "The Matrix" || c.Year != 1999 || c.Size != 5000 {
		t.Errorf("row fields wrong: %+v", c)
	}
	if !reflect.DeepEqual(c.WatchedBy, []string{"Alice"}) {
		t.Errorf("WatchedBy = %v, want [Alice]", c.WatchedBy)
	}
	if !sameTime(c.WatchedAt, tp("2026-03-01T20:00:00Z")) {
		t.Errorf("WatchedAt = %v", c.WatchedAt)
	}
	if len(c.Torrents) != 1 || c.Torrents[0].ID != "h1" || c.BelowSeedMin {
		t.Errorf("torrents wrong: %+v", c.Torrents)
	}
	if res.Protected != 0 || res.Unmatched != 0 {
		t.Errorf("counts = %d protected, %d unmatched, want 0 and 0", res.Protected, res.Unmatched)
	}
}

func TestMoviesSkips(t *testing.T) {
	tests := []struct {
		name      string
		mod       func(*MoviesInput)
		protected int
		unmatched int
	}{
		{"not watched", func(in *MoviesInput) {
			in.Library.Movies[0].States = map[string]domain.UserState{"a": {}}
		}, 0, 0},
		{"grace period not over", func(in *MoviesInput) { in.Rules.GraceDays = 30 }, 0, 0},
		{"no file", func(in *MoviesInput) { in.Snapshot.Movies[0].File = nil }, 0, 0},
		{"no match on the media server", func(in *MoviesInput) { in.Library.Movies = nil }, 0, 1},
		{"protected by tag", func(in *MoviesInput) { in.Snapshot.Movies[0].Tags = []string{"Keep"} }, 1, 0},
		{"protected by favourite", func(in *MoviesInput) {
			st := played("2026-03-01T20:00:00Z")
			st.Favorite = true
			in.Library.Movies[0].States = map[string]domain.UserState{"a": st}
		}, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Movies(movieInput(tt.mod))
			if len(res.Candidates) != 0 {
				t.Errorf("got %d candidates, want 0", len(res.Candidates))
			}
			if res.Protected != tt.protected || res.Unmatched != tt.unmatched {
				t.Errorf("counts = %d protected, %d unmatched, want %d and %d",
					res.Protected, res.Unmatched, tt.protected, tt.unmatched)
			}
		})
	}
}

func TestMoviesUnwatchedProtectedMovieIsNotCounted(t *testing.T) {
	res := Movies(movieInput(func(in *MoviesInput) {
		in.Snapshot.Movies[0].Tags = []string{"keep"}
		in.Library.Movies[0].States = map[string]domain.UserState{"a": {}}
	}))
	if res.Protected != 0 {
		t.Errorf("Protected = %d, want 0: only movies that would otherwise be suggested count", res.Protected)
	}
}

func TestMoviesFavouriteProtectionCanBeTurnedOff(t *testing.T) {
	res := Movies(movieInput(func(in *MoviesInput) {
		in.Rules.ProtectFavorites = false
		st := played("2026-03-01T20:00:00Z")
		st.Favorite = true
		in.Library.Movies[0].States = map[string]domain.UserState{"a": st}
	}))
	if len(res.Candidates) != 1 {
		t.Errorf("got %d candidates, want 1", len(res.Candidates))
	}
}

func TestMoviesFallsBackToIMDb(t *testing.T) {
	res := Movies(movieInput(func(in *MoviesInput) {
		in.Library.Movies[0].IDs = domain.ExternalIDs{IMDB: "tt0133093"}
	}))
	if len(res.Candidates) != 1 {
		t.Errorf("got %d candidates, want 1 (matched by IMDb ID)", len(res.Candidates))
	}
}

func TestMoviesMergesSeveralServerItems(t *testing.T) {
	res := Movies(movieInput(func(in *MoviesInput) {
		in.Rules.CountingUsers = []string{"a", "b"}
		in.Library.Movies = append(in.Library.Movies, domain.ServerMovie{
			ID: "j2", Title: "The Matrix (4K)",
			IDs:    domain.ExternalIDs{TMDB: "603"},
			States: map[string]domain.UserState{"b": played("2026-03-05T20:00:00Z")},
		})
	}))
	if len(res.Candidates) != 1 {
		t.Fatalf("got %d candidates, want 1: Alice and Bob each watched one copy", len(res.Candidates))
	}
	c := res.Candidates[0]
	if !reflect.DeepEqual(c.WatchedBy, []string{"Alice", "Bob"}) || !sameTime(c.WatchedAt, tp("2026-03-05T20:00:00Z")) {
		t.Errorf("WatchedBy = %v, WatchedAt = %v", c.WatchedBy, c.WatchedAt)
	}
}

func TestMoviesFlagsSeedingAndToleratesMissingTorrents(t *testing.T) {
	res := Movies(movieInput(func(in *MoviesInput) { in.Rules.MinRatio = 3 }))
	if !res.Candidates[0].BelowSeedMin {
		t.Error("ratio 2 is below a minimum of 3, the row must be flagged")
	}

	res = Movies(movieInput(func(in *MoviesInput) { in.Downloads = nil }))
	if got := res.Candidates[0].Torrents; got == nil || len(got) != 0 {
		t.Errorf("Torrents = %v, want an empty, non-nil list when the torrent is gone from the client", got)
	}
}
