package planner

import (
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
	"github.com/removarr/removarr/internal/scanner"
)

var now = time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

func played() map[string]domain.UserState {
	return map[string]domain.UserState{"a": {Played: true}}
}

func mfile(id int, size int64) *domain.MediaFile { return &domain.MediaFile{ID: id, Size: size} }

func at(t time.Time) *time.Time { return &t }

// world is a small library that tests change. Everything is watched by user "a":
//   - movie 1 "One" (tmdb:1), file 10, grabbed as and imported from torrent "m1";
//   - movie 2 "Two" (tmdb:2), file 20, from no torrent;
//   - the ended series 1 "Show" (tvdb:100): season 1, episodes 11 and 12 in files 11 and 12,
//     both from the pack "p1";
//   - the torrent "orphan", which no media manager knows.
func world() scanner.Fetched {
	lib := domain.Library{
		Movies: []domain.ServerMovie{
			{ID: "sm1", IDs: domain.ExternalIDs{TMDB: "1"}, States: played()},
			{ID: "sm2", IDs: domain.ExternalIDs{TMDB: "2"}, States: played()},
		},
		Series: []domain.ServerSeries{{ID: "ss1", IDs: domain.ExternalIDs{TVDB: "100"}}},
		Episodes: []domain.ServerEpisode{
			{SeriesID: "ss1", Season: 1, Episode: 1, States: played()},
			{SeriesID: "ss1", Season: 1, Episode: 2, States: played()},
		},
	}
	movies := domain.MovieSnapshot{
		Movies: []domain.Movie{
			{ID: 1, Title: "One", IDs: domain.ExternalIDs{TMDB: "1"}, File: mfile(10, 1000)},
			{ID: 2, Title: "Two", IDs: domain.ExternalIDs{TMDB: "2"}, File: mfile(20, 2000)},
		},
		Links: domain.Links{Grabbed: map[string][]int{"m1": {1}}, FileSource: map[int]string{10: "m1"}, Queue: map[string]bool{}},
	}
	series := domain.SeriesSnapshot{
		Series: []domain.Series{{
			ID: 1, Title: "Show", Ended: true, IDs: domain.ExternalIDs{TVDB: "100"},
			Episodes: []domain.Episode{
				{ID: 11, Season: 1, Number: 1, File: mfile(11, 500)},
				{ID: 12, Season: 1, Number: 2, File: mfile(12, 500)},
			},
		}},
		Links: domain.Links{Grabbed: map[string][]int{"p1": {11, 12}}, FileSource: map[int]string{11: "p1", 12: "p1"}, Queue: map[string]bool{}},
	}
	downloads := []domain.Download{
		{ID: "m1", Name: "One.Torrent", Size: 1000, ContentPath: "/dl/one"},
		{ID: "p1", Name: "Show.S01", Size: 1000, ContentPath: "/dl/show"},
		{ID: "orphan", Name: "Unknown.Torrent", Size: 300, ContentPath: "/dl/unknown"},
	}
	return scanner.Fetched{
		Users:     []domain.User{{ID: "a", Name: "Alice"}},
		Library:   scanner.Source[domain.Library]{Configured: true, Value: lib},
		Movies:    scanner.Source[domain.MovieSnapshot]{Configured: true, Value: movies},
		Series:    scanner.Source[domain.SeriesSnapshot]{Configured: true, Value: series},
		Downloads: scanner.Source[[]domain.Download]{Configured: true, Value: downloads},
		Files:     map[string][]domain.DownloadFile{},
	}
}

func rules() domain.Rules {
	r := domain.DefaultRules()
	r.CountingUsers = []string{"a"}
	return r
}

// scanned is what a scan of f stores: every row, listed and held.
func scanned(r domain.Rules, f scanner.Fetched) []domain.Candidate {
	return scanner.Evaluate(r, now, f).Candidates
}

// plan makes a plan from a read of f, with latest as the latest scan and the given ignores.
func plan(r domain.Rules, f scanner.Fetched, latest []domain.Candidate, ignores []domain.Ignore, selected ...domain.RowRef) ([]domain.PlanItem, []domain.SkippedRow) {
	set := scan.NewIgnoreSet(ignores)
	f.Ignored = set.Keys()
	return Make(Input{
		Rules: r, Now: now, Eval: scanner.Evaluate(r, now, f), Ignores: set, Latest: latest, Selected: selected,
	})
}

// same plans with an unchanged world: the latest scan saw what the planner sees.
func same(t *testing.T, r domain.Rules, f scanner.Fetched, ignores []domain.Ignore, selected ...domain.RowRef) ([]domain.PlanItem, []domain.SkippedRow) {
	t.Helper()
	return plan(r, f, scanned(r, f), ignores, selected...)
}

func sel(list domain.ListKind, key string) domain.RowRef { return domain.RowRef{List: list, Key: key} }

// scannerFetched shortens scanner.Fetched in test tables.
type scannerFetched = scanner.Fetched

var (
	movieOne = sel(domain.ListMovies, "tmdb:1")
	movieTwo = sel(domain.ListMovies, "tmdb:2")
	theShow  = sel(domain.ListSeries, "tvdb:100")
	orphan   = sel(domain.ListTorrents, "hash:orphan")
)

// rowKeys names the rows of an item as "list/key", marking added rows with a "+".
func rowKeys(item domain.PlanItem) []string {
	var out []string
	for _, r := range item.Rows {
		k := string(r.Row.List) + "/" + r.Row.Key
		if r.Added {
			k = "+" + k
		}
		out = append(out, k)
	}
	return out
}

func torrentIDs(item domain.PlanItem) []string {
	var out []string
	for _, t := range item.Torrents {
		out = append(out, t.Torrent.ID)
	}
	return out
}

// skips maps each skipped row's key to its reason.
func skips(skipped []domain.SkippedRow) map[string]string {
	out := map[string]string{}
	for _, s := range skipped {
		out[s.Row.Key] = s.Reason
	}
	return out
}
