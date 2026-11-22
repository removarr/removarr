package scan

import (
	"reflect"
	"testing"

	"github.com/removarr/removarr/internal/domain"
)

var (
	seen   = map[string]domain.UserState{"a": played("2026-03-01T20:00:00Z")}
	unseen = map[string]domain.UserState{"a": {}}
)

// ep builds a managed episode. A fileID of 0 means the episode has no file; files are 100 bytes.
func ep(id, season, number, fileID int) domain.Episode {
	e := domain.Episode{ID: id, Season: season, Number: number}
	if fileID != 0 {
		e.File = &domain.MediaFile{ID: fileID, Size: 100}
	}
	return e
}

// show builds a managed series with TVDB ID 100.
func show(ended bool, eps ...domain.Episode) domain.Series {
	return domain.Series{
		ID: 1, Title: "The Show", Year: 2020, Ended: ended,
		IDs:      domain.ExternalIDs{TVDB: "100", IMDB: "tt100"},
		Episodes: eps,
	}
}

// sep builds a media server episode of the series "js1".
func sep(season, number int, states map[string]domain.UserState) domain.ServerEpisode {
	return domain.ServerEpisode{SeriesID: "js1", Season: season, Episode: number, States: states}
}

// seriesInput returns a scan input for one series. The media server knows it as "js1" by its
// TVDB ID. mods adjust the input before the link index is built.
func seriesInput(s domain.Series, eps []domain.ServerEpisode, mods ...func(*SeriesInput)) SeriesInput {
	in := SeriesInput{
		Rules: rules("all", "a"),
		Now:   *tp("2026-03-10T12:00:00Z"),
		Users: []domain.User{{ID: "a", Name: "Alice"}, {ID: "b", Name: "Bob"}},
		Library: domain.Library{
			Series:   []domain.ServerSeries{{ID: "js1", Title: s.Title, IDs: domain.ExternalIDs{TVDB: "100"}}},
			Episodes: eps,
		},
		Snapshot: domain.SeriesSnapshot{Series: []domain.Series{s}, Links: links()},
	}
	for _, m := range mods {
		m(&in)
	}
	var fileIDs []int
	for _, sr := range in.Snapshot.Series {
		for _, e := range sr.Episodes {
			if e.File != nil {
				fileIDs = append(fileIDs, e.File.ID)
			}
		}
	}
	in.Index = NewLinkIndex(ManagerFiles{Links: in.Snapshot.Links, FileIDs: fileIDs})
	return in
}

func perSeason(in *SeriesInput) { in.Rules.SeriesUnit = domain.UnitSeason }

func keysOf(cs []domain.Candidate) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Key)
	}
	return out
}

func torrentIDs(c domain.Candidate) []string {
	var out []string
	for _, t := range c.Torrents {
		out = append(out, t.ID)
	}
	return out
}

// twoSeasons is a series with a special, two episodes in season 1 and one in season 2.
func twoSeasons(ended bool) domain.Series {
	return show(ended, ep(1, 0, 1, 1), ep(11, 1, 1, 11), ep(12, 1, 2, 12), ep(21, 2, 1, 21))
}

func allSeen() []domain.ServerEpisode {
	return []domain.ServerEpisode{sep(1, 1, seen), sep(1, 2, seen), sep(2, 1, seen)}
}

func TestSeriesWholeSeriesEnded(t *testing.T) {
	res := Series(seriesInput(twoSeasons(true), allSeen()))
	if len(res.Candidates) != 1 {
		t.Fatalf("got %d rows, want 1", len(res.Candidates))
	}
	c := res.Candidates[0]
	if c.List != domain.ListSeries || c.Key != "tvdb:100" || c.Scope != domain.ScopeSeries || c.Title != "The Show" || c.Year != 2020 {
		t.Errorf("row fields wrong: %+v", c)
	}
	if c.StillAiring {
		t.Error("an ended series must not be labelled still airing")
	}
	if c.Size != 400 {
		t.Errorf("Size = %d, want 400: an ended series is removed with its specials", c.Size)
	}
	if !reflect.DeepEqual(c.WatchedBy, []string{"Alice"}) || !sameTime(c.WatchedAt, tp("2026-03-01T20:00:00Z")) {
		t.Errorf("WatchedBy = %v, WatchedAt = %v", c.WatchedBy, c.WatchedAt)
	}
	if res.Unmatched != 0 {
		t.Errorf("Unmatched = %d, want 0: specials are not counted", res.Unmatched)
	}
}

func TestSeriesWholeSeriesStillAiring(t *testing.T) {
	res := Series(seriesInput(twoSeasons(false), allSeen()))
	if len(res.Candidates) != 1 {
		t.Fatalf("got %d rows, want 1", len(res.Candidates))
	}
	c := res.Candidates[0]
	if !c.StillAiring {
		t.Error("a series that has not ended must be labelled still airing")
	}
	if c.Size != 300 {
		t.Errorf("Size = %d, want 300: specials stay while the series stays", c.Size)
	}
}

func TestSeriesWholeSeriesNeedsEverythingWatched(t *testing.T) {
	eps := []domain.ServerEpisode{sep(1, 1, seen), sep(1, 2, seen), sep(2, 1, unseen)}
	res := Series(seriesInput(twoSeasons(true), eps))
	if len(res.Candidates) != 0 {
		t.Errorf("got rows %v, want none", keysOf(res.Candidates))
	}
}

func TestSeriesPerSeason(t *testing.T) {
	eps := []domain.ServerEpisode{sep(1, 1, seen), sep(1, 2, seen), sep(2, 1, unseen)}
	res := Series(seriesInput(twoSeasons(true), eps, perSeason))
	if len(res.Candidates) != 1 {
		t.Fatalf("got rows %v, want only season 1", keysOf(res.Candidates))
	}
	c := res.Candidates[0]
	if c.Key != "tvdb:100:s1" || c.Scope != domain.ScopeSeason || c.Season != 1 || c.Size != 200 {
		t.Errorf("season row wrong: %+v", c)
	}
	if c.StillAiring {
		t.Error("season rows are never labelled still airing")
	}
}

func TestSeriesPerSeasonMergesWhenEverythingIsWatched(t *testing.T) {
	res := Series(seriesInput(twoSeasons(true), allSeen(), perSeason))
	if got := keysOf(res.Candidates); !reflect.DeepEqual(got, []string{"tvdb:100"}) {
		t.Errorf("rows = %v, want one merged series row", got)
	}
}

func TestSeriesPerSeasonDoesNotMergeOverAnIgnoredSeason(t *testing.T) {
	res := Series(seriesInput(twoSeasons(true), allSeen(), perSeason, func(in *SeriesInput) {
		in.Ignored = map[string]bool{"tvdb:100:s2": true}
	}))
	if got := keysOf(res.Candidates); !reflect.DeepEqual(got, []string{"tvdb:100:s1", "tvdb:100:s2"}) {
		t.Errorf("rows = %v, want two season rows", got)
	}
}

func TestSeriesIgnoredSeasonHasNoEffectInWholeSeriesMode(t *testing.T) {
	res := Series(seriesInput(twoSeasons(true), allSeen(), func(in *SeriesInput) {
		in.Ignored = map[string]bool{"tvdb:100:s2": true}
	}))
	if got := keysOf(res.Candidates); !reflect.DeepEqual(got, []string{"tvdb:100"}) {
		t.Errorf("rows = %v, want the series row", got)
	}
}

func TestSeriesGracePeriod(t *testing.T) {
	res := Series(seriesInput(twoSeasons(true), allSeen(), func(in *SeriesInput) { in.Rules.GraceDays = 30 }))
	if len(res.Candidates) != 0 {
		t.Errorf("got rows %v, want none: watched nine days ago, grace is thirty", keysOf(res.Candidates))
	}
}

func TestSeriesUnmatchedEpisodeCountsAsUnwatched(t *testing.T) {
	eps := []domain.ServerEpisode{sep(1, 1, seen), sep(2, 1, seen)} // season 1 episode 2 is missing
	res := Series(seriesInput(twoSeasons(true), eps, perSeason))
	if got := keysOf(res.Candidates); !reflect.DeepEqual(got, []string{"tvdb:100:s2"}) {
		t.Errorf("rows = %v, want only season 2", got)
	}
	if res.Unmatched != 1 {
		t.Errorf("Unmatched = %d, want 1", res.Unmatched)
	}
}

func TestSeriesUnknownToTheMediaServer(t *testing.T) {
	res := Series(seriesInput(twoSeasons(true), allSeen(), func(in *SeriesInput) { in.Library.Series = nil }))
	if len(res.Candidates) != 0 {
		t.Errorf("got rows %v, want none", keysOf(res.Candidates))
	}
	if res.Unmatched != 3 {
		t.Errorf("Unmatched = %d, want 3 regular-season episodes", res.Unmatched)
	}
}

// rangeEp builds a media server item of the series "js1" that covers episodes first to last.
func rangeEp(season, first, last int, states map[string]domain.UserState) domain.ServerEpisode {
	e := sep(season, first, states)
	e.EpisodeEnd = last
	return e
}

func TestSeriesEpisodeRangeCountsOnlyWhereSonarrHoldsOneFile(t *testing.T) {
	fav := played("2026-03-01T20:00:00Z")
	fav.Favorite = true
	favorite := map[string]domain.UserState{"a": fav}
	s2 := sep(2, 1, seen)
	tests := []struct {
		name          string
		series        domain.Series
		eps           []domain.ServerEpisode
		wantKeys      []string
		wantUnmatched int
		wantProtected int
	}{
		{
			name:     "one file holds both episodes",
			series:   show(true, ep(11, 1, 1, 10), ep(12, 1, 2, 10), ep(21, 2, 1, 21)),
			eps:      []domain.ServerEpisode{rangeEp(1, 1, 2, seen), s2},
			wantKeys: []string{"tvdb:100"},
		},
		{
			name:          "two files: only the first episode is watched",
			series:        show(true, ep(11, 1, 1, 11), ep(12, 1, 2, 12), ep(21, 2, 1, 21)),
			eps:           []domain.ServerEpisode{rangeEp(1, 1, 2, seen), s2},
			wantKeys:      []string{"tvdb:100:s2"},
			wantUnmatched: 1,
		},
		{
			name:          "no file for the first episode",
			series:        show(true, ep(11, 1, 1, 0), ep(12, 1, 2, 12), ep(21, 2, 1, 21)),
			eps:           []domain.ServerEpisode{rangeEp(1, 1, 2, seen), s2},
			wantKeys:      []string{"tvdb:100:s2"},
			wantUnmatched: 1,
		},
		{
			name:   "range over two files and a separate item for the third episode",
			series: show(true, ep(11, 1, 1, 10), ep(12, 1, 2, 10), ep(13, 1, 3, 13), ep(21, 2, 1, 21)),
			eps: []domain.ServerEpisode{
				rangeEp(1, 1, 3, seen), sep(1, 3, seen), s2,
			},
			wantKeys: []string{"tvdb:100"},
		},
		{
			name:     "a range that does not apply leaves the other item's states",
			series:   show(true, ep(11, 1, 1, 11), ep(12, 1, 2, 12), ep(21, 2, 1, 21)),
			eps:      []domain.ServerEpisode{rangeEp(1, 1, 2, seen), sep(1, 2, unseen), s2},
			wantKeys: []string{"tvdb:100:s2"},
		},
		{
			name:          "favourite on a range that applies",
			series:        show(true, ep(11, 1, 1, 10), ep(12, 1, 2, 10), ep(21, 2, 1, 21)),
			eps:           []domain.ServerEpisode{rangeEp(1, 1, 2, favorite), s2},
			wantProtected: 1,
		},
		{
			name:          "favourite on a range that does not apply",
			series:        show(true, ep(11, 1, 1, 11), ep(12, 1, 2, 12), ep(21, 2, 1, 21)),
			eps:           []domain.ServerEpisode{rangeEp(1, 1, 2, favorite), s2},
			wantUnmatched: 1,
			wantProtected: 1,
		},
		{
			name:     "absurd end",
			series:   show(true, ep(11, 1, 1, 10), ep(12, 1, 2, 10), ep(21, 2, 1, 21)),
			eps:      []domain.ServerEpisode{rangeEp(1, 1, 2_000_000_000, seen), s2},
			wantKeys: []string{"tvdb:100"},
		},
		{
			name:          "end 0 is a single episode",
			series:        show(true, ep(11, 1, 1, 10), ep(12, 1, 2, 10), ep(21, 2, 1, 21)),
			eps:           []domain.ServerEpisode{rangeEp(1, 1, 0, seen), s2},
			wantKeys:      []string{"tvdb:100:s2"},
			wantUnmatched: 1,
		},
		{
			name:          "end before the start is a single episode",
			series:        show(true, ep(11, 1, 1, 10), ep(12, 1, 2, 10), ep(21, 2, 1, 21)),
			eps:           []domain.ServerEpisode{rangeEp(1, 2, 1, seen), s2},
			wantKeys:      []string{"tvdb:100:s2"},
			wantUnmatched: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Series(seriesInput(tt.series, tt.eps, perSeason))
			if got := keysOf(res.Candidates); !reflect.DeepEqual(got, tt.wantKeys) {
				t.Errorf("rows = %v, want %v", got, tt.wantKeys)
			}
			if res.Unmatched != tt.wantUnmatched {
				t.Errorf("Unmatched = %d, want %d", res.Unmatched, tt.wantUnmatched)
			}
			if res.Protected != tt.wantProtected {
				t.Errorf("Protected = %d, want %d", res.Protected, tt.wantProtected)
			}
		})
	}
}

func TestSeriesEpisodeRangeCostDoesNotGrowWithItsLength(t *testing.T) {
	allocs := func(last int) float64 {
		lib := domain.Library{Episodes: []domain.ServerEpisode{rangeEp(1, 1, last, seen)}}
		return testing.AllocsPerRun(5, func() { indexLibrary(lib) })
	}
	if short, long := allocs(2), allocs(1<<20); long != short {
		t.Errorf("indexing a range of 2^20 episodes took %v allocations, one of 2 took %v", long, short)
	}
}

func TestSeriesProtection(t *testing.T) {
	fav := played("2026-03-01T20:00:00Z")
	fav.Favorite = true
	tests := []struct {
		name string
		eps  []domain.ServerEpisode
		mod  func(*SeriesInput)
	}{
		{"tag", allSeen(), func(in *SeriesInput) { in.Snapshot.Series[0].Tags = []string{"keep"} }},
		{"series favourite", allSeen(), func(in *SeriesInput) {
			in.Library.Series[0].FavoriteOf = map[string]bool{"a": true}
		}},
		{"episode favourite", []domain.ServerEpisode{
			sep(1, 1, map[string]domain.UserState{"a": fav}), sep(1, 2, seen), sep(2, 1, seen),
		}, func(*SeriesInput) {}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Series(seriesInput(twoSeasons(true), tt.eps, perSeason, tt.mod))
			if len(res.Candidates) != 0 || res.Protected != 1 {
				t.Errorf("rows = %v, Protected = %d; want no rows and 1 protected series",
					keysOf(res.Candidates), res.Protected)
			}
		})
	}
}

func TestSeriesUnwatchedProtectedSeriesIsNotCounted(t *testing.T) {
	eps := []domain.ServerEpisode{sep(1, 1, unseen), sep(1, 2, unseen), sep(2, 1, unseen)}
	res := Series(seriesInput(twoSeasons(true), eps, func(in *SeriesInput) {
		in.Snapshot.Series[0].Tags = []string{"keep"}
	}))
	if res.Protected != 0 {
		t.Errorf("Protected = %d, want 0: only series that would otherwise be suggested count", res.Protected)
	}
}

func TestSeriesFallsBackToIMDb(t *testing.T) {
	res := Series(seriesInput(twoSeasons(true), allSeen(), func(in *SeriesInput) {
		in.Library.Series[0].IDs = domain.ExternalIDs{IMDB: "tt100"}
	}))
	if len(res.Candidates) != 1 {
		t.Errorf("got %d rows, want 1 (matched by IMDb ID)", len(res.Candidates))
	}
}

func TestSeriesWithoutFilesIsSkipped(t *testing.T) {
	res := Series(seriesInput(show(true, ep(11, 1, 1, 0)), []domain.ServerEpisode{sep(1, 1, seen)}))
	if len(res.Candidates) != 0 || res.Unmatched != 0 || res.Protected != 0 {
		t.Errorf("a series with no files must produce nothing: %+v", res)
	}
}

func TestSeriesTorrents(t *testing.T) {
	threeSeasons := show(false, ep(11, 1, 1, 11), ep(12, 1, 2, 12), ep(21, 2, 1, 21), ep(31, 3, 1, 31))
	withLinks := func(in *SeriesInput) {
		l := in.Snapshot.Links
		l.Grabbed["s1pack"] = []int{11, 12} // season 1 pack
		l.FileSource[11], l.FileSource[12] = "s1pack", "s1pack"
		l.Grabbed["bigpack"] = []int{21, 31} // pack covering seasons 2 and 3
		l.FileSource[21], l.FileSource[31] = "bigpack", "bigpack"
		l.Grabbed["old"] = []int{11} // replaced by s1pack
		in.Downloads = []domain.Download{dl("s1pack"), dl("bigpack"), dl("old")}
	}

	// Seasons 1 and 2 watched, season 3 not: the big pack still backs season 3.
	eps := []domain.ServerEpisode{sep(1, 1, seen), sep(1, 2, seen), sep(2, 1, seen), sep(3, 1, unseen)}
	res := Series(seriesInput(threeSeasons, eps, perSeason, withLinks))
	if got := keysOf(res.Candidates); !reflect.DeepEqual(got, []string{"tvdb:100:s1", "tvdb:100:s2"}) {
		t.Fatalf("rows = %v, want seasons 1 and 2", got)
	}
	if got := torrentIDs(res.Candidates[0]); !reflect.DeepEqual(got, []string{"old", "s1pack"}) {
		t.Errorf("season 1 torrents = %v, want [old s1pack]", got)
	}
	if got := res.Candidates[1].Torrents; got == nil || len(got) != 0 {
		t.Errorf("season 2 torrents = %v, want empty: the big pack stays", got)
	}

	// Everything watched: one series row that frees every torrent.
	eps[3] = sep(3, 1, seen)
	res = Series(seriesInput(threeSeasons, eps, perSeason, withLinks))
	if got := torrentIDs(res.Candidates[0]); !reflect.DeepEqual(got, []string{"bigpack", "old", "s1pack"}) {
		t.Errorf("series torrents = %v, want [bigpack old s1pack]", got)
	}
}

func TestSeriesEndedRowTakesTorrentsOfEpisodesWithoutFiles(t *testing.T) {
	withStale := func(in *SeriesInput) {
		in.Snapshot.Links.Grabbed["stale"] = []int{13} // grabbed for an episode whose file is gone
		in.Downloads = []domain.Download{dl("stale")}
	}
	eps := []domain.ServerEpisode{sep(1, 1, seen)}

	ended := show(true, ep(11, 1, 1, 11), ep(13, 1, 3, 0))
	res := Series(seriesInput(ended, eps, withStale))
	if got := torrentIDs(res.Candidates[0]); !reflect.DeepEqual(got, []string{"stale"}) {
		t.Errorf("ended series torrents = %v, want [stale]: the whole series goes", got)
	}

	airing := show(false, ep(11, 1, 1, 11), ep(13, 1, 3, 0))
	res = Series(seriesInput(airing, eps, withStale))
	if got := res.Candidates[0].Torrents; len(got) != 0 {
		t.Errorf("still-airing series torrents = %v, want none: only deleted episodes count", got)
	}
}

func TestSeriesWatchedByInAnyMode(t *testing.T) {
	byA := map[string]domain.UserState{"a": played("2026-03-01T20:00:00Z")}
	byB := map[string]domain.UserState{"b": played("2026-03-02T20:00:00Z")}
	anyMode := func(in *SeriesInput) {
		in.Rules.WatchedMode = domain.WatchedAny
		in.Rules.CountingUsers = []string{"a", "b"}
	}
	s := show(true, ep(11, 1, 1, 11), ep(12, 1, 2, 12))

	res := Series(seriesInput(s, []domain.ServerEpisode{sep(1, 1, byA), sep(1, 2, byB)}, anyMode))
	if len(res.Candidates) != 1 {
		t.Fatalf("got %d rows, want 1: every episode was watched by someone", len(res.Candidates))
	}
	if got := res.Candidates[0].WatchedBy; len(got) != 0 {
		t.Errorf("WatchedBy = %v, want empty: nobody watched every episode", got)
	}

	res = Series(seriesInput(s, []domain.ServerEpisode{sep(1, 1, byA), sep(1, 2, byA)}, anyMode))
	if got := res.Candidates[0].WatchedBy; !reflect.DeepEqual(got, []string{"Alice"}) {
		t.Errorf("WatchedBy = %v, want [Alice]", got)
	}
}
