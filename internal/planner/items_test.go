package planner

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
	"github.com/removarr/removarr/internal/scanner"
)

// onlyItem checks that a plan has exactly one item and no skipped row, and returns the item.
// Call it as onlyItem(t)(same(…)).
func onlyItem(t *testing.T) func([]domain.PlanItem, []domain.SkippedRow) domain.PlanItem {
	return func(items []domain.PlanItem, skipped []domain.SkippedRow) domain.PlanItem {
		t.Helper()
		if len(items) != 1 || len(skipped) != 0 {
			t.Fatalf("items = %+v, skipped = %+v, want exactly one item", items, skipped)
		}
		return items[0]
	}
}

func TestMakeRemovesAMovieFromTheMovieManager(t *testing.T) {
	for _, exclude := range []bool{false, true} {
		r := rules()
		r.AddExclusionOnDelete = exclude
		item := onlyItem(t)(same(t, r, world(), nil, movieOne))
		want := []domain.ManagerStep{{Manager: scan.ManagerMovies, Action: domain.StepRemoveMovie, ItemID: 1, RowKey: "tmdb:1", AddExclusion: exclude}}
		if !reflect.DeepEqual(item.Steps, want) {
			t.Errorf("exclude=%v: steps = %+v, want %+v", exclude, item.Steps, want)
		}
		if !reflect.DeepEqual(torrentIDs(item), []string{"m1"}) {
			t.Errorf("torrents = %v, want [m1]", torrentIDs(item))
		}
	}
}

func TestMakeRemovesAnEndedSeriesFromTheSeriesManager(t *testing.T) {
	item := onlyItem(t)(same(t, rules(), world(), nil, theShow))
	want := []domain.ManagerStep{{Manager: scan.ManagerSeries, Action: domain.StepRemoveSeries, ItemID: 1, RowKey: "tvdb:100"}}
	if !reflect.DeepEqual(item.Steps, want) || !reflect.DeepEqual(torrentIDs(item), []string{"p1"}) {
		t.Errorf("item = %+v, want remove-series and the pack", item)
	}
}

// running makes the series of world() a running one: season 1 aired in the past, and a
// season 2 with one episode that has no file and airs in the future.
func running(f *scannerFetched, season1Aired bool) {
	s := &f.Series.Value.Series[0]
	s.Ended = false
	for i := range s.Episodes {
		if season1Aired {
			s.Episodes[i].AirDate = at(now.Add(-48 * time.Hour))
		}
	}
	s.Episodes = append(s.Episodes, domain.Episode{ID: 21, Season: 2, Number: 1, AirDate: at(now.Add(48 * time.Hour))})
}

func TestMakeDeletesTheFilesOfARunningSeries(t *testing.T) {
	f := world()
	running(&f, true)
	item := onlyItem(t)(same(t, rules(), f, nil, theShow))
	want := []domain.ManagerStep{
		{Manager: scan.ManagerSeries, Action: domain.StepUnmonitorEpisodes, ItemID: 1, EpisodeIDs: []int{11, 12}, RowKey: "tvdb:100"},
		{Manager: scan.ManagerSeries, Action: domain.StepUnmonitorSeasons, ItemID: 1, Seasons: []int{1}, RowKey: "tvdb:100"},
		{Manager: scan.ManagerSeries, Action: domain.StepDeleteEpisodeFiles, ItemID: 1, FileIDs: []int{111, 112}, RowKey: "tvdb:100"},
	}
	if !reflect.DeepEqual(item.Steps, want) {
		t.Errorf("steps = %+v, want %+v", item.Steps, want)
	}
}

func TestMakeLeavesASeasonMonitoredThatHasNotFinishedAiring(t *testing.T) {
	f := world()
	running(&f, false) // season 1 has no air dates
	item := onlyItem(t)(same(t, rules(), f, nil, theShow))
	for _, s := range item.Steps {
		if s.Action == domain.StepUnmonitorSeasons {
			t.Errorf("steps = %+v, want no season unmonitored", item.Steps)
		}
	}
	if len(item.Steps) != 2 {
		t.Errorf("steps = %+v, want delete and unmonitor episodes", item.Steps)
	}
}

func TestMakeDeletesTheFilesOfASeasonRow(t *testing.T) {
	r := rules()
	r.SeriesUnit = domain.UnitSeason
	f := world()
	running(&f, true)
	// Season 2 gets a file that is not watched, so season 1 is a row of its own.
	s := &f.Series.Value.Series[0]
	s.Episodes[2].File = mfile(121, 500)
	item := onlyItem(t)(same(t, r, f, nil, sel(domain.ListSeries, "tvdb:100:s1")))
	want := []domain.ManagerStep{
		{Manager: scan.ManagerSeries, Action: domain.StepUnmonitorEpisodes, ItemID: 1, EpisodeIDs: []int{11, 12}, RowKey: "tvdb:100:s1"},
		{Manager: scan.ManagerSeries, Action: domain.StepUnmonitorSeasons, ItemID: 1, Seasons: []int{1}, RowKey: "tvdb:100:s1"},
		{Manager: scan.ManagerSeries, Action: domain.StepDeleteEpisodeFiles, ItemID: 1, FileIDs: []int{111, 112}, RowKey: "tvdb:100:s1"},
	}
	if !reflect.DeepEqual(item.Steps, want) {
		t.Errorf("steps = %+v, want %+v", item.Steps, want)
	}
}

func TestMakeUnmonitorsOnlyTheFinishedSeasonsOfTheRow(t *testing.T) {
	r := rules()
	r.SeriesUnit = domain.UnitSeason
	f := world()
	s := &f.Series.Value.Series[0]
	s.Ended = false
	for i := range s.Episodes {
		s.Episodes[i].AirDate = at(now.Add(-48 * time.Hour))
	}
	// Season 2 has finished airing too, because season 3 exists, but its episode is not
	// watched: it is no row, and the plan for season 1 must leave it monitored.
	s.Episodes = append(s.Episodes,
		domain.Episode{ID: 21, Season: 2, Number: 1, AirDate: at(now.Add(-24 * time.Hour)), File: mfile(121, 500)},
		domain.Episode{ID: 31, Season: 3, Number: 1, AirDate: at(now.Add(48 * time.Hour))})
	if !scan.SeasonFinished(*s, 2, now) {
		t.Fatal("season 2 has not finished airing; the test needs it to have")
	}
	item := onlyItem(t)(same(t, r, f, nil, sel(domain.ListSeries, "tvdb:100:s1")))
	want := []domain.ManagerStep{
		{Manager: scan.ManagerSeries, Action: domain.StepUnmonitorEpisodes, ItemID: 1, EpisodeIDs: []int{11, 12}, RowKey: "tvdb:100:s1"},
		{Manager: scan.ManagerSeries, Action: domain.StepUnmonitorSeasons, ItemID: 1, Seasons: []int{1}, RowKey: "tvdb:100:s1"},
		{Manager: scan.ManagerSeries, Action: domain.StepDeleteEpisodeFiles, ItemID: 1, FileIDs: []int{111, 112}, RowKey: "tvdb:100:s1"},
	}
	if !reflect.DeepEqual(item.Steps, want) {
		t.Errorf("steps = %+v, want %+v", item.Steps, want)
	}
}

func TestMakeDeletesAFileSharedByTwoSeasonRowsOnce(t *testing.T) {
	r := rules()
	r.SeriesUnit = domain.UnitSeason
	f := world()
	s := &f.Series.Value.Series[0]
	s.Ended = false
	// Episode 12 (season 1) and episode 21 (season 2) share file 130; season 3 is not watched,
	// so the seasons are not merged into one series row.
	s.Episodes[1].File = mfile(130, 500)
	s.Episodes = append(s.Episodes,
		domain.Episode{ID: 21, Season: 2, Number: 1, File: mfile(130, 500)},
		domain.Episode{ID: 31, Season: 3, Number: 1, File: mfile(131, 500)})
	f.Series.Value.Links = domain.Links{Grabbed: map[string][]int{}, FileSource: map[int]string{}, Queue: map[string]bool{}}
	f.Library.Value.Episodes = append(f.Library.Value.Episodes, domain.ServerEpisode{SeriesID: "ss1", Season: 2, Episode: 1, States: played()})
	items, skipped := same(t, r, f, nil, sel(domain.ListSeries, "tvdb:100:s1"))
	item := onlyItem(t)(items, skipped)
	if !reflect.DeepEqual(rowKeys(item), []string{"series/tvdb:100:s1", "+series/tvdb:100:s2"}) {
		t.Fatalf("rows = %v, want season 1 and the tied season 2", rowKeys(item))
	}
	deleted := map[int]int{}
	for _, step := range item.Steps {
		if step.Action == domain.StepDeleteEpisodeFiles {
			for _, id := range step.FileIDs {
				deleted[id]++
			}
		}
	}
	if !reflect.DeepEqual(deleted, map[int]int{111: 1, 130: 1}) {
		t.Errorf("files deleted = %v, want 111 and 130 once each", deleted)
	}
}

// Season 2's episode shares the file season 1 deletes: it must be unmonitored before that
// file goes, or a failed step leaves a deleted episode that Sonarr downloads again.
func TestEveryUnmonitorStepOfAnItemComesBeforeItsDeletes(t *testing.T) {
	r := rules()
	r.SeriesUnit = domain.UnitSeason
	f := world()
	s := &f.Series.Value.Series[0]
	s.Ended = false
	s.Episodes[1].File = mfile(130, 500)
	s.Episodes = append(s.Episodes,
		domain.Episode{ID: 21, Season: 2, Number: 1, File: mfile(130, 500)},
		domain.Episode{ID: 31, Season: 3, Number: 1, File: mfile(131, 500)})
	f.Series.Value.Links = domain.Links{Grabbed: map[string][]int{}, FileSource: map[int]string{}, Queue: map[string]bool{}}
	f.Library.Value.Episodes = append(f.Library.Value.Episodes, domain.ServerEpisode{SeriesID: "ss1", Season: 2, Episode: 1, States: played()})
	item := onlyItem(t)(same(t, r, f, nil, sel(domain.ListSeries, "tvdb:100:s1")))
	var order []string
	deleted := false
	for _, step := range item.Steps {
		order = append(order, step.RowKey+" "+step.Action)
		switch step.Action {
		case domain.StepDeleteEpisodeFiles:
			deleted = true
		case domain.StepUnmonitorEpisodes, domain.StepUnmonitorSeasons:
			if deleted {
				t.Errorf("steps = %q: an unmonitor step comes after a delete", order)
			}
		}
	}
	if !deleted || !slices.Contains(order, "tvdb:100:s2 "+domain.StepUnmonitorEpisodes) {
		t.Fatalf("steps = %q, want season 2 unmonitored and a delete", order)
	}
}

func TestMakeAddsTiedRowsToTheItem(t *testing.T) {
	f := world()
	f.Movies.Value.Links.FileSource[20] = "m1" // m1 backs both movies: they are tied
	item := onlyItem(t)(same(t, rules(), f, nil, movieOne))
	if !reflect.DeepEqual(rowKeys(item), []string{"movies/tmdb:1", "+movies/tmdb:2"}) {
		t.Fatalf("rows = %v, want One and the added Two", rowKeys(item))
	}
	added := item.Rows[1]
	if added.AddedWith == nil || added.AddedWith.Key != "tmdb:1" || added.AddedWith.Title != "One" {
		t.Errorf("AddedWith = %+v, want One", added.AddedWith)
	}
	if !reflect.DeepEqual(added.Shares, []string{"m1"}) || !reflect.DeepEqual(item.Rows[0].Shares, []string{"m1"}) {
		t.Errorf("shares = %v and %v, want [m1] on both", item.Rows[0].Shares, added.Shares)
	}
	if len(item.Steps) != 2 || item.Steps[0].ItemID != 1 || item.Steps[1].ItemID != 2 {
		t.Errorf("steps = %+v, want remove-movie 1 and 2", item.Steps)
	}
	if !reflect.DeepEqual(torrentIDs(item), []string{"m1"}) {
		t.Errorf("torrents = %v, want m1 once", torrentIDs(item))
	}
}

func TestMakeMakesOneItemOfSeveralSelectedRowsOfAGroup(t *testing.T) {
	f := world()
	f.Movies.Value.Links.FileSource[20] = "m1"
	item := onlyItem(t)(same(t, rules(), f, nil, movieTwo, movieOne))
	if !reflect.DeepEqual(rowKeys(item), []string{"movies/tmdb:1", "movies/tmdb:2"}) {
		t.Errorf("rows = %v, want both, neither added", rowKeys(item))
	}
	for _, r := range item.Rows {
		if r.AddedWith != nil {
			t.Errorf("row %s has AddedWith %+v, want none: it was selected", r.Row.Key, r.AddedWith)
		}
	}
}

func TestMakeSkipsAnItemThatWouldRemoveATorrentTheScanDidNotShow(t *testing.T) {
	f := world()
	latest := scanned(rules(), f)
	// Since the scan, history names a second torrent for movie 1, and it is in the client.
	f.Movies.Value.Links.Grabbed["m2"] = []int{1}
	f.Downloads.Value = append(f.Downloads.Value, domain.Download{ID: "m2", Name: "One.Again"})
	items, skipped := plan(rules(), f, latest, nil, movieOne)
	if len(items) != 0 {
		t.Fatalf("items = %+v, want none", items)
	}
	if got := skips(skipped)["tmdb:1"]; !strings.Contains(got, `"One.Again"`) || !strings.Contains(got, "Scan again") {
		t.Errorf("reason = %q, want it to name One.Again and ask for a scan", got)
	}
}

func TestMakeSkipsAnItemThatWouldRemoveATorrentTheScanNamedOnlyForAnotherRow(t *testing.T) {
	f := world()
	// The scan named m2 for movie 2 alone: history records it as grabbed for movie 2, and it
	// backs no file.
	f.Movies.Value.Links.Grabbed["m2"] = []int{2}
	f.Downloads.Value = append(f.Downloads.Value, domain.Download{ID: "m2", Name: "Two.Torrent"})
	latest := scanned(rules(), f)
	// Since the scan, history names m2 for movie 1 as well. The movies are not tied: m2
	// backs no file.
	f.Movies.Value.Links.Grabbed["m2"] = []int{1, 2}
	items, skipped := plan(rules(), f, latest, nil, movieOne)
	if len(items) != 0 {
		t.Fatalf("items = %+v, want none", items)
	}
	if got := skips(skipped)["tmdb:1"]; !strings.Contains(got, `"Two.Torrent"`) || !strings.Contains(got, "Scan again") {
		t.Errorf("reason = %q, want it to name Two.Torrent and ask for a scan", got)
	}
}

func TestMakeSkipsAnItemWhoseTiedRowWouldRemoveATorrentTheScanDidNotShow(t *testing.T) {
	f := world()
	f.Movies.Value.Links.FileSource[20] = "m1" // m1 backs both movies: they are tied
	latest := scanned(rules(), f)
	// Since the scan, history names a second torrent for movie 2, the row that is not selected.
	f.Movies.Value.Links.Grabbed["m3"] = []int{2}
	f.Downloads.Value = append(f.Downloads.Value, domain.Download{ID: "m3", Name: "Two.Again"})
	items, skipped := plan(rules(), f, latest, nil, movieOne)
	if len(items) != 0 {
		t.Fatalf("items = %+v, want none", items)
	}
	if got := skips(skipped)["tmdb:1"]; !strings.Contains(got, `"Two.Again"`) || !strings.Contains(got, "Scan again") {
		t.Errorf("reason = %q, want it to name Two.Again and ask for a scan", got)
	}
}

func TestMakeUsesTheNewReadWhenATorrentIsGoneSinceTheScan(t *testing.T) {
	f := world()
	latest := scanned(rules(), f)
	f.Downloads.Value = f.Downloads.Value[1:] // m1 is gone from the client
	item := onlyItem(t)(plan(rules(), f, latest, nil, movieOne))
	if len(item.Torrents) != 0 {
		t.Errorf("torrents = %v, want none", torrentIDs(item))
	}
}

func TestMakeSkipsAnItemWhenATorrentTheScanNamedWouldNowStay(t *testing.T) {
	f := world()
	latest := scanned(rules(), f)
	// Since the scan, history no longer links m1 to movie 1, and m1 is still in the client.
	delete(f.Movies.Value.Links.FileSource, 10)
	delete(f.Movies.Value.Links.Grabbed, "m1")
	items, skipped := plan(rules(), f, latest, nil, movieOne)
	if len(items) != 0 {
		t.Fatalf("items = %+v, want none", items)
	}
	want := `It changed since the scan: the torrent "One.Torrent" that the scan showed would now stay. Scan again.`
	if got := skips(skipped)["tmdb:1"]; got != want {
		t.Errorf("reason = %q, want %q", got, want)
	}
}

func TestMakeMarksAnAddedRowTheLatestScanDidNotHold(t *testing.T) {
	f := world()
	f.Library.Value.Movies[1].States = nil // movie 2 is not watched: it is no row of the scan
	latest := scanned(rules(), f)
	// Since the scan, movie 2 was watched, and m1 backs its file too: the movies are tied.
	f.Library.Value.Movies[1].States = played()
	f.Movies.Value.Links.FileSource[20] = "m1"
	item := onlyItem(t)(plan(rules(), f, latest, nil, movieOne))
	if !reflect.DeepEqual(rowKeys(item), []string{"movies/tmdb:1", "+movies/tmdb:2"}) {
		t.Fatalf("rows = %v, want One and the added Two", rowKeys(item))
	}
	if item.Rows[0].NotInScan || !item.Rows[1].NotInScan {
		t.Errorf("NotInScan = %v, %v; want false for One and true for Two", item.Rows[0].NotInScan, item.Rows[1].NotInScan)
	}
}

func TestMakeSkipsARowWhoseEvaluationHasNoRowToDelete(t *testing.T) {
	f := world()
	f.Movies.Value.Links.FileSource[20] = "m1" // m1 backs both movies: they are tied
	eval := scanner.Evaluate(rules(), now, f)
	var rows []scan.Row
	for _, r := range eval.Rows {
		if r.Candidate.Key != "tmdb:2" { // the tied row has a candidate, but nothing to delete
			rows = append(rows, r)
		}
	}
	eval.Rows = rows
	items, skipped := Make(Input{
		Rules: rules(), Now: now, Eval: eval, Ignores: scan.NewIgnoreSet(nil),
		Latest: scanned(rules(), f), Selected: []domain.RowRef{movieOne},
	})
	if len(items) != 0 || skips(skipped)["tmdb:1"] != ReasonNoLonger {
		t.Errorf("items = %+v, skipped = %+v, want tmdb:1 skipped as no longer qualifying", items, skipped)
	}
}

// chain turns world() into a chain of three rows: movie 1 shares m1 with the show, and the
// show shares the new torrent m2 with the ended series 3 "Other" (tvdb:300), which movie 1
// shares nothing with.
func chain() scannerFetched {
	f := world()
	f.Library.Value.Series = append(f.Library.Value.Series, domain.ServerSeries{ID: "ss3", IDs: domain.ExternalIDs{TVDB: "300"}})
	f.Library.Value.Episodes = append(f.Library.Value.Episodes, domain.ServerEpisode{SeriesID: "ss3", Season: 1, Episode: 1, States: played()})
	f.Series.Value.Series = append(f.Series.Value.Series, domain.Series{
		ID: 3, Title: "Other", Ended: true, IDs: domain.ExternalIDs{TVDB: "300"},
		Episodes: []domain.Episode{{ID: 31, Season: 1, Number: 1, File: mfile(311, 500)}},
	})
	f.Series.Value.Links = domain.Links{
		Grabbed:    map[string][]int{"m1": {11}, "m2": {12, 31}},
		FileSource: map[int]string{111: "m1", 112: "m2", 311: "m2"},
		Queue:      map[string]bool{},
	}
	f.Downloads.Value = append(f.Downloads.Value, domain.Download{ID: "m2", Name: "Shared.Torrent", Size: 1000, ContentPath: "/dl/shared"})
	return f
}

func TestMakeSkipsARowWhoseChainReachesAnIgnoredRow(t *testing.T) {
	f := chain()
	var tied []string
	for _, c := range scanned(rules(), f) {
		if c.Key == "tmdb:1" {
			for _, ref := range c.TiedWith {
				tied = append(tied, ref.Key)
			}
		}
	}
	if !reflect.DeepEqual(tied, []string{"tvdb:100", "tvdb:300"}) {
		t.Fatalf("movie 1 is tied to %v, want the show and Other", tied)
	}
	ign := []domain.Ignore{{Kind: domain.IgnoreSeries, Key: "tvdb:300", Title: "Other"}}
	items, skipped := same(t, rules(), f, ign, movieOne)
	if len(items) != 0 || skips(skipped)["tmdb:1"] != `It is tied to "Other", which is ignored.` {
		t.Errorf("items = %+v, skipped = %+v, want tmdb:1 skipped, naming Other", items, skipped)
	}
}
