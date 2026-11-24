package scan

import (
	"sort"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

// SeriesInput is everything the watched series list is computed from.
type SeriesInput struct {
	Rules    domain.Rules
	Now      time.Time
	Users    []domain.User
	Library  domain.Library
	Snapshot domain.SeriesSnapshot
	// Ignored holds the keys the user had ignored when the scan started. Here it only stops
	// the per-season merge into one series row (spec 6.3). Ignored rows are still returned:
	// the lists apply the ignores when they are read (View, spec 6.5).
	Ignored map[string]bool
}

type epKey struct{ season, number int }

// seasonAgg collects what the rules need to know about one season of one series.
type seasonAgg struct {
	number   int
	files    map[int]int64  // library file ID -> size
	itemIDs  []int          // IDs of the episodes that have a file
	watched  int            // how many of those episodes are watched
	at       *time.Time     // latest watched date among them
	playedBy map[string]int // user ID -> how many of those episodes the user played
}

// qualifies reports whether every episode with a file is watched and past the grace period.
func (a *seasonAgg) qualifies(r domain.Rules, now time.Time) bool {
	return len(a.itemIDs) > 0 && a.watched == len(a.itemIDs) && GraceOK(r, a.at, now)
}

// Series returns the qualifying series and seasons of the watched series list (spec 6.2 to
// 6.4) as Rows, sorted by key. Candidates stays empty: WholeTorrents fills it.
func Series(in SeriesInput) ListResult {
	var res ListResult
	lib := indexLibrary(in.Library)
	b := rowBuilder{in: in, names: userNames(in.Users)}

	for _, s := range in.Snapshot.Series {
		if s.IDs.TVDB == "" {
			continue
		}
		serverIDs := lib.match(s.IDs)
		fileOf := fileIDsOf(s)
		seasons := map[int]*seasonAgg{}
		for _, e := range s.Episodes {
			if e.File == nil {
				continue
			}
			a := seasons[e.Season]
			if a == nil {
				a = &seasonAgg{number: e.Season, files: map[int]int64{}, playedBy: map[string]int{}}
				seasons[e.Season] = a
			}
			a.files[e.File.ID] = e.File.Size
			a.itemIDs = append(a.itemIDs, e.ID)

			states, found := lib.episodeStates(serverIDs, e.Season, e.Number, e.File.ID, fileOf)
			if !found {
				if e.Season > 0 {
					res.Unmatched++
				}
				continue
			}
			v := Evaluate(in.Rules, states)
			if !v.Watched {
				continue
			}
			a.watched++
			a.at = later(a.at, v.At)
			for _, u := range v.By {
				a.playedBy[u]++
			}
		}

		var regular []*seasonAgg
		for number, a := range seasons {
			if number > 0 {
				regular = append(regular, a)
			}
		}
		if len(regular) == 0 {
			continue
		}
		sort.Slice(regular, func(i, j int) bool { return regular[i].number < regular[j].number })

		var qualifying []*seasonAgg
		for _, a := range regular {
			if a.qualifies(in.Rules, in.Now) {
				qualifying = append(qualifying, a)
			}
		}
		all := len(qualifying) == len(regular)
		perSeason := in.Rules.SeriesUnit == domain.UnitSeason

		var rows []Row
		switch {
		case all && !(perSeason && anySeasonIgnored(in.Ignored, s, regular)):
			row := b.seriesRow(s, regular, seasons)
			// Only per-season mode merges: there a season ignore made during the scan holds the row back (View).
			row.Candidate.Merged = perSeason
			rows = append(rows, row)
		case perSeason:
			for _, a := range qualifying {
				rows = append(rows, b.seasonRow(s, a))
			}
		}
		if len(rows) == 0 {
			continue
		}
		if hasTag(s.Tags, in.Rules.ProtectionTag) || (in.Rules.ProtectFavorites && lib.favorite(in.Rules, serverIDs)) {
			res.Protected++
			continue
		}
		res.Rows = append(res.Rows, rows...)
	}
	sortRows(res.Rows)
	return res
}

// fileIDsOf maps each episode of a managed series that has a file to that file's ID. A
// (season, number) listed with two different files is left out: its file is not known.
func fileIDsOf(s domain.Series) map[epKey]int {
	out := map[epKey]int{}
	ambiguous := map[epKey]bool{}
	for _, e := range s.Episodes {
		if e.File == nil {
			continue
		}
		k := epKey{e.Season, e.Number}
		if ambiguous[k] {
			continue
		}
		if id, ok := out[k]; ok && id != e.File.ID {
			delete(out, k)
			ambiguous[k] = true
			continue
		}
		out[k] = e.File.ID
	}
	return out
}

func anySeasonIgnored(ignored map[string]bool, s domain.Series, regular []*seasonAgg) bool {
	for _, a := range regular {
		if ignored[domain.SeasonKey(s.IDs.TVDB, a.number)] {
			return true
		}
	}
	return false
}

// rowBuilder turns seasons into rows.
type rowBuilder struct {
	in    SeriesInput
	names map[string]string
}

// seriesRow builds a whole-series row. regular are the regular seasons with files, all of
// which qualify; seasons also holds the specials.
func (b rowBuilder) seriesRow(s domain.Series, regular []*seasonAgg, seasons map[int]*seasonAgg) Row {
	deleted := regular
	var itemIDs []int
	if s.Ended {
		// An ended series is removed from the series manager, specials included.
		deleted = nil
		for _, a := range seasons {
			deleted = append(deleted, a)
		}
		for _, e := range s.Episodes {
			itemIDs = append(itemIDs, e.ID)
		}
	} else {
		// The series stays: only the watched regular-season files go.
		for _, a := range regular {
			itemIDs = append(itemIDs, a.itemIDs...)
		}
	}
	row := b.row(s, regular, deleted, itemIDs)
	row.Candidate.Key = domain.SeriesKey(s.IDs.TVDB)
	row.Candidate.Scope = domain.ScopeSeries
	row.Candidate.StillAiring = !s.Ended
	return row
}

// seasonRow builds the row of one qualifying season.
func (b rowBuilder) seasonRow(s domain.Series, a *seasonAgg) Row {
	one := []*seasonAgg{a}
	row := b.row(s, one, one, a.itemIDs)
	row.Candidate.Key = domain.SeasonKey(s.IDs.TVDB, a.number)
	row.Candidate.Scope = domain.ScopeSeason
	row.Candidate.Season = a.number
	return row
}

// row builds what series and season rows share. watchedOver are the seasons the watched date
// and watchers come from; deleted are the seasons whose files Delete would remove; itemIDs
// are the episodes Delete affects.
func (b rowBuilder) row(s domain.Series, watchedOver, deleted []*seasonAgg, itemIDs []int) Row {
	var size int64
	var fileIDs []int
	counted := map[int]bool{} // a file can hold episodes of two seasons
	for _, a := range deleted {
		for id, bytes := range a.files {
			if counted[id] {
				continue
			}
			counted[id] = true
			size += bytes
			fileIDs = append(fileIDs, id)
		}
	}

	var at *time.Time
	episodes := 0
	played := map[string]int{}
	for _, a := range watchedOver {
		at = later(at, a.at)
		episodes += len(a.itemIDs)
		for u, n := range a.playedBy {
			played[u] += n
		}
	}
	// A user is listed when they played every episode with a file. In "any" mode this can be nobody.
	var by []string
	for _, u := range b.in.Rules.CountingUsers {
		if played[u] == episodes {
			by = append(by, u)
		}
	}

	return Row{
		Candidate: domain.Candidate{
			List:      domain.ListSeries,
			Title:     s.Title,
			Year:      s.Year,
			Size:      size,
			WatchedBy: namesOf(b.names, by),
			WatchedAt: at,
		},
		Manager: ManagerSeries,
		ItemIDs: itemIDs,
		FileIDs: fileIDs,
	}
}

// libraryIndex looks up the media server's view of a managed series.
type libraryIndex struct {
	byTVDB, byIMDB, byTMDB map[string][]string // external ID -> media server series IDs
	favoriteOf             map[string]map[string]bool
	// episodes holds the states of every item under its first episode.
	episodes map[string]map[epKey]map[string]domain.UserState
	// ranges holds the items that cover more than one episode, by media server series and season.
	ranges map[string]map[int][]episodeRange
}

// episodeRange is a media server item that covers episodes first to last of one season.
type episodeRange struct {
	first, last int
	states      map[string]domain.UserState
}

func indexLibrary(lib domain.Library) *libraryIndex {
	x := &libraryIndex{
		byTVDB:     map[string][]string{},
		byIMDB:     map[string][]string{},
		byTMDB:     map[string][]string{},
		favoriteOf: map[string]map[string]bool{},
		episodes:   map[string]map[epKey]map[string]domain.UserState{},
		ranges:     map[string]map[int][]episodeRange{},
	}
	for _, s := range lib.Series {
		if s.IDs.TVDB != "" {
			x.byTVDB[s.IDs.TVDB] = append(x.byTVDB[s.IDs.TVDB], s.ID)
		}
		if s.IDs.IMDB != "" {
			x.byIMDB[s.IDs.IMDB] = append(x.byIMDB[s.IDs.IMDB], s.ID)
		}
		if s.IDs.TMDB != "" {
			x.byTMDB[s.IDs.TMDB] = append(x.byTMDB[s.IDs.TMDB], s.ID)
		}
		x.favoriteOf[s.ID] = s.FavoriteOf
	}
	for _, e := range lib.Episodes {
		byEpisode := x.episodes[e.SeriesID]
		if byEpisode == nil {
			byEpisode = map[epKey]map[string]domain.UserState{}
			x.episodes[e.SeriesID] = byEpisode
		}
		k := epKey{e.Season, e.Episode}
		byEpisode[k] = mergeStates(byEpisode[k], e.States)
		// The range itself is kept as two numbers: never expanded, whatever its end.
		if e.EpisodeEnd > e.Episode {
			bySeason := x.ranges[e.SeriesID]
			if bySeason == nil {
				bySeason = map[int][]episodeRange{}
				x.ranges[e.SeriesID] = bySeason
			}
			bySeason[e.Season] = append(bySeason[e.Season], episodeRange{e.Episode, e.EpisodeEnd, e.States})
		}
	}
	return x
}

// match returns the media server series that correspond to a managed series:
// by TVDB ID, falling back to IMDb ID, then TMDB ID (spec 6.2).
func (x *libraryIndex) match(ids domain.ExternalIDs) []string {
	if ids.TVDB != "" && len(x.byTVDB[ids.TVDB]) > 0 {
		return x.byTVDB[ids.TVDB]
	}
	if ids.IMDB != "" && len(x.byIMDB[ids.IMDB]) > 0 {
		return x.byIMDB[ids.IMDB]
	}
	if ids.TMDB != "" && len(x.byTMDB[ids.TMDB]) > 0 {
		return x.byTMDB[ids.TMDB]
	}
	return nil
}

// episodeStates returns the merged watch states of one episode across the matched series,
// and whether the media server has the episode at all. fileID is the file the series manager
// holds the episode in; fileOf maps the managed series' episodes to their file IDs.
//
// An item that covers a range counts for its first episode, and for a further episode only
// when the series manager holds the range's first episode in the same file (spec 6.2).
func (x *libraryIndex) episodeStates(serverIDs []string, season, number, fileID int, fileOf map[epKey]int) (map[string]domain.UserState, bool) {
	var states map[string]domain.UserState
	found := false
	for _, id := range serverIDs {
		if st, ok := x.episodes[id][epKey{season, number}]; ok {
			states = mergeStates(states, st)
			found = true
		}
		for _, r := range x.ranges[id][season] {
			if number <= r.first || number > r.last {
				continue
			}
			if firstFile, ok := fileOf[epKey{season, r.first}]; !ok || firstFile != fileID {
				continue
			}
			states = mergeStates(states, r.states)
			found = true
		}
	}
	return states, found
}

// favorite reports whether a counting user marked the series, one of its seasons or one of
// its episodes a favourite (spec 6.4).
func (x *libraryIndex) favorite(r domain.Rules, serverIDs []string) bool {
	for _, id := range serverIDs {
		for _, u := range r.CountingUsers {
			if x.favoriteOf[id][u] {
				return true
			}
		}
		for _, states := range x.episodes[id] {
			if anyFavorite(r, states) {
				return true
			}
		}
	}
	return false
}
