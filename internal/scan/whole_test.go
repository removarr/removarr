package scan

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

// link builds the links of one media manager: which downloads were grabbed for which items,
// which download each file was imported from, and which downloads are queued.
func link(grabbed map[string][]int, sources map[int]string, queued ...string) domain.Links {
	l := links()
	for id, items := range grabbed {
		l.Grabbed[id] = items
	}
	for file, id := range sources {
		l.FileSource[file] = id
	}
	for _, id := range queued {
		l.Queue[id] = true
	}
	return l
}

// libFiles lists current library files of 100 bytes each.
func libFiles(ids ...int) []LibraryFile {
	out := make([]LibraryFile, 0, len(ids))
	for _, id := range ids {
		out = append(out, LibraryFile{ID: id, Size: 100})
	}
	return out
}

// held is a library file of 100 bytes that holds the given items.
func held(id int, items ...int) LibraryFile {
	return LibraryFile{ID: id, Size: 100, Items: items}
}

// movieRow is a movie row that deletes movie item and its file.
func movieRow(key string, item, file int) Row {
	return Row{
		Candidate: domain.Candidate{List: domain.ListMovies, Key: key, Title: "Movie " + key, Size: 100},
		Manager:   ManagerMovies,
		ItemIDs:   []int{item},
		FileIDs:   []int{file},
	}
}

// seriesRow is a series row of the invented series with TVDB ID 100.
func seriesRow(items, files []int) Row {
	return Row{
		Candidate: domain.Candidate{List: domain.ListSeries, Key: "tvdb:100", Title: "The Show", Scope: domain.ScopeSeries},
		Manager:   ManagerSeries,
		ItemIDs:   items,
		FileIDs:   files,
	}
}

// seasonRow is a season row of the invented series with TVDB ID 100.
func seasonRow(season int, items, files []int) Row {
	return Row{
		Candidate: domain.Candidate{
			List: domain.ListSeries, Key: domain.SeasonKey("100", season), Title: "The Show",
			Scope: domain.ScopeSeason, Season: season,
		},
		Manager: ManagerSeries,
		ItemIDs: items,
		FileIDs: files,
	}
}

// listedRow is what a test expects of one listed row: its torrents and the rows it is tied
// to, the latter written as "list/key".
type listedRow struct {
	key      string
	torrents []string
	tied     []string
}

type wholeCase struct {
	name           string
	movies, series ManagerFiles
	downloads      []string // the IDs of the downloads in the client
	movieRows      []Row
	seriesRows     []Row
	wantMovies     []listedRow
	wantSeries     []listedRow
	waitingMovies  int
	waitingSeries  int
}

func runWhole(t *testing.T, tt wholeCase) {
	t.Helper()
	tt.movies.Manager, tt.series.Manager = ManagerMovies, ManagerSeries
	x := NewLinkIndex(tt.movies, tt.series)
	var downloads []domain.Download
	for _, id := range tt.downloads {
		downloads = append(downloads, dl(id))
	}
	movies := &ListResult{Rows: tt.movieRows}
	series := &ListResult{Rows: tt.seriesRows}
	WholeTorrents(domain.DefaultRules(), x, downloads, movies, series)

	check := func(list string, res *ListResult, want []listedRow, waiting int) {
		t.Helper()
		if res.Waiting != waiting {
			t.Errorf("%s: Waiting = %d, want %d", list, res.Waiting, waiting)
		}
		if len(res.Held) != res.Waiting {
			t.Errorf("%s: %d rows held, Waiting = %d; want them equal", list, len(res.Held), res.Waiting)
		}
		for _, h := range res.Held {
			if !h.Waiting {
				t.Errorf("%s: held row %s is not marked waiting", list, h.Key)
			}
		}
		var got []listedRow
		for _, c := range res.Candidates {
			if c.Waiting {
				t.Errorf("%s: listed row %s is marked waiting", list, c.Key)
			}
			if c.Torrents == nil {
				t.Errorf("%s: %s has nil Torrents, want a non-nil list", list, c.Key)
			}
			r := listedRow{key: c.Key, torrents: torrentIDs(c)}
			for _, ref := range c.TiedWith {
				r.tied = append(r.tied, string(ref.List)+"/"+ref.Key)
			}
			got = append(got, r)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: listed %s, want %s", list, fmt.Sprint(got), fmt.Sprint(want))
		}
	}
	check("movies", movies, tt.wantMovies, tt.waitingMovies)
	check("series", series, tt.wantSeries, tt.waitingSeries)
}

func TestWholeTorrents(t *testing.T) {
	tests := []wholeCase{
		{
			name:       "1. a row whose file no torrent backs is listed without torrents",
			movies:     ManagerFiles{Links: links(), Files: libFiles(10)},
			movieRows:  []Row{movieRow("tmdb:1", 1, 10)},
			wantMovies: []listedRow{{key: "tmdb:1"}},
		},
		{
			name:       "2. a row with its own torrent names it",
			movies:     ManagerFiles{Links: link(map[string][]int{"h1": {1}}, map[int]string{10: "h1"}), Files: libFiles(10)},
			downloads:  []string{"h1"},
			movieRows:  []Row{movieRow("tmdb:1", 1, 10)},
			wantMovies: []listedRow{{key: "tmdb:1", torrents: []string{"h1"}}},
		},
		{
			name: "3. a pack over two seasons of which only one qualifies holds that season back",
			series: ManagerFiles{
				Links: link(map[string][]int{"pack": {11, 21}}, map[int]string{11: "pack", 21: "pack"}),
				Files: libFiles(11, 21),
			},
			downloads:     []string{"pack"},
			seriesRows:    []Row{seasonRow(1, []int{11}, []int{11})},
			waitingSeries: 1,
		},
		{
			name: "4. a pack over two qualifying seasons ties their rows",
			series: ManagerFiles{
				Links: link(map[string][]int{"pack": {11, 21}}, map[int]string{11: "pack", 21: "pack"}),
				Files: libFiles(11, 21),
			},
			downloads:  []string{"pack"},
			seriesRows: []Row{seasonRow(1, []int{11}, []int{11}), seasonRow(2, []int{21}, []int{21})},
			wantSeries: []listedRow{
				{key: "tvdb:100:s1", torrents: []string{"pack"}, tied: []string{"series/tvdb:100:s2"}},
				{key: "tvdb:100:s2", torrents: []string{"pack"}, tied: []string{"series/tvdb:100:s1"}},
			},
		},
		{
			name: "5. a collection pack with a movie that is not a row holds the other two back",
			movies: ManagerFiles{
				Links: link(map[string][]int{"coll": {1, 2, 3}}, map[int]string{10: "coll", 20: "coll", 30: "coll"}),
				Files: libFiles(10, 20, 30),
			},
			downloads:     []string{"coll"},
			movieRows:     []Row{movieRow("tmdb:1", 1, 10), movieRow("tmdb:2", 2, 20)},
			waitingMovies: 2,
		},
		{
			name: "6a. a still-airing series row whose pack also backs a special waits",
			series: ManagerFiles{
				Links: link(map[string][]int{"pack": {1, 11, 12}}, map[int]string{1: "pack", 11: "pack", 12: "pack"}),
				Files: libFiles(1, 11, 12),
			},
			downloads:     []string{"pack"},
			seriesRows:    []Row{seriesRow([]int{11, 12}, []int{11, 12})},
			waitingSeries: 1,
		},
		{
			name: "6b. the same series ended is listed and names the pack",
			series: ManagerFiles{
				Links: link(map[string][]int{"pack": {1, 11, 12}}, map[int]string{1: "pack", 11: "pack", 12: "pack"}),
				Files: libFiles(1, 11, 12),
			},
			downloads:  []string{"pack"},
			seriesRows: []Row{seriesRow([]int{1, 11, 12}, []int{1, 11, 12})},
			wantSeries: []listedRow{{key: "tvdb:100", torrents: []string{"pack"}}},
		},
		{
			name:          "7a. a queued torrent that backs a row's file makes the row wait",
			movies:        ManagerFiles{Links: link(map[string][]int{"q": {1}}, map[int]string{10: "q"}, "q"), Files: libFiles(10)},
			downloads:     []string{"q"},
			movieRows:     []Row{movieRow("tmdb:1", 1, 10)},
			waitingMovies: 1,
		},
		{
			name:       "7b. a queued torrent that is only associated is not named and blocks nothing",
			movies:     ManagerFiles{Links: link(map[string][]int{"q": {1}}, nil, "q"), Files: libFiles(10)},
			downloads:  []string{"q"},
			movieRows:  []Row{movieRow("tmdb:1", 1, 10)},
			wantMovies: []listedRow{{key: "tmdb:1"}},
		},
		{
			name: "8. a chain of torrents to a file that stays holds back every row on it",
			series: ManagerFiles{
				Links: link(nil, map[int]string{11: "t1", 21: "t1", 22: "t2", 31: "t2"}),
				Files: libFiles(11, 21, 22, 31),
			},
			downloads: []string{"t1", "t2"},
			seriesRows: []Row{
				seasonRow(1, []int{11}, []int{11}),
				seasonRow(2, []int{21, 22}, []int{21, 22}),
			},
			waitingSeries: 2,
		},
		{
			name: "8b. the same chain with every file deleted ties the rows at both ends",
			series: ManagerFiles{
				Links: link(nil, map[int]string{11: "t1", 21: "t1", 22: "t2", 31: "t2"}),
				Files: libFiles(11, 21, 22, 31),
			},
			downloads: []string{"t1", "t2"},
			seriesRows: []Row{
				seasonRow(1, []int{11}, []int{11}),
				seasonRow(2, []int{21, 22}, []int{21, 22}),
				seasonRow(3, []int{31}, []int{31}),
			},
			wantSeries: []listedRow{
				{key: "tvdb:100:s1", torrents: []string{"t1"}, tied: []string{"series/tvdb:100:s2", "series/tvdb:100:s3"}},
				{key: "tvdb:100:s2", torrents: []string{"t1", "t2"}, tied: []string{"series/tvdb:100:s1", "series/tvdb:100:s3"}},
				{key: "tvdb:100:s3", torrents: []string{"t2"}, tied: []string{"series/tvdb:100:s1", "series/tvdb:100:s2"}},
			},
		},
		{
			name: "9. equal file and item numbers in the two managers neither tie nor block",
			movies: ManagerFiles{
				Links: link(map[string][]int{"m": {1}}, map[int]string{10: "m"}),
				Files: libFiles(10),
			},
			series: ManagerFiles{
				Links: link(map[string][]int{"s": {1}}, map[int]string{10: "s", 20: "s"}),
				Files: libFiles(10, 20),
			},
			downloads: []string{"m", "s"},
			movieRows: []Row{movieRow("tmdb:1", 1, 10)},
			seriesRows: []Row{
				seasonRow(1, []int{1}, []int{10}),
				seasonRow(2, []int{2}, []int{20}),
			},
			wantMovies: []listedRow{{key: "tmdb:1", torrents: []string{"m"}}},
			wantSeries: []listedRow{
				{key: "tvdb:100:s1", torrents: []string{"s"}, tied: []string{"series/tvdb:100:s2"}},
				{key: "tvdb:100:s2", torrents: []string{"s"}, tied: []string{"series/tvdb:100:s1"}},
			},
		},
		{
			name:       "10a. a torrent that backs a movie file and an episode file ties the two rows",
			movies:     ManagerFiles{Links: link(map[string][]int{"x": {1}}, map[int]string{10: "x"}), Files: libFiles(10)},
			series:     ManagerFiles{Links: link(map[string][]int{"x": {11}}, map[int]string{11: "x"}), Files: libFiles(11)},
			downloads:  []string{"x"},
			movieRows:  []Row{movieRow("tmdb:1", 1, 10)},
			seriesRows: []Row{seasonRow(1, []int{11}, []int{11})},
			wantMovies: []listedRow{{key: "tmdb:1", torrents: []string{"x"}, tied: []string{"series/tvdb:100:s1"}}},
			wantSeries: []listedRow{{key: "tvdb:100:s1", torrents: []string{"x"}, tied: []string{"movies/tmdb:1"}}},
		},
		{
			name:       "10c. a waiting group across the two lists counts each row in its own list",
			movies:     ManagerFiles{Links: link(nil, map[int]string{10: "x"}), Files: libFiles(10)},
			series:     ManagerFiles{Links: link(nil, map[int]string{11: "x", 12: "x"}), Files: libFiles(11, 12)},
			downloads:  []string{"x"},
			movieRows:  []Row{movieRow("tmdb:1", 1, 10)},
			seriesRows: []Row{seasonRow(1, []int{11}, []int{11})},
			// File 12 is a series file no row deletes.
			waitingMovies: 1,
			waitingSeries: 1,
		},
		{
			name:          "10d. a movie file with the same number as a deleted episode file still stays",
			movies:        ManagerFiles{Links: link(nil, map[int]string{10: "x"}), Files: libFiles(10)},
			series:        ManagerFiles{Links: link(nil, map[int]string{10: "x"}), Files: libFiles(10)},
			downloads:     []string{"x"},
			seriesRows:    []Row{seasonRow(1, []int{1}, []int{10})},
			waitingSeries: 1,
		},
		{
			name:          "10b. the same torrent with only the movie qualifying holds it back",
			movies:        ManagerFiles{Links: link(map[string][]int{"x": {1}}, map[int]string{10: "x"}), Files: libFiles(10)},
			series:        ManagerFiles{Links: link(map[string][]int{"x": {11}}, map[int]string{11: "x"}), Files: libFiles(11)},
			downloads:     []string{"x"},
			movieRows:     []Row{movieRow("tmdb:1", 1, 10)},
			waitingMovies: 1,
		},
		{
			name: "11. a history source that is gone from the client neither ties, holds back nor is named",
			series: ManagerFiles{
				Links: link(map[string][]int{"gone": {11, 21}}, map[int]string{11: "gone", 21: "gone", 31: "gone"}),
				Files: libFiles(11, 21, 31),
			},
			seriesRows: []Row{seasonRow(1, []int{11}, []int{11}), seasonRow(2, []int{21}, []int{21})},
			wantSeries: []listedRow{{key: "tvdb:100:s1"}, {key: "tvdb:100:s2"}},
		},
		{
			name:       "12a. an old download that is associated with the row's item and backs nothing is named",
			movies:     ManagerFiles{Links: link(map[string][]int{"old": {1}}, nil), Files: libFiles(10)},
			downloads:  []string{"old"},
			movieRows:  []Row{movieRow("tmdb:1", 1, 10)},
			wantMovies: []listedRow{{key: "tmdb:1", torrents: []string{"old"}}},
		},
		{
			name: "12b. an associated download that backs a file elsewhere is not named and holds nothing back",
			movies: ManagerFiles{
				Links: link(map[string][]int{"other": {1, 2}}, map[int]string{20: "other"}),
				Files: libFiles(10, 20),
			},
			downloads:  []string{"other"},
			movieRows:  []Row{movieRow("tmdb:1", 1, 10)},
			wantMovies: []listedRow{{key: "tmdb:1"}},
		},
		{
			name: "12c. a pack grabbed for two movies that backs only one file is not named by the other row",
			movies: ManagerFiles{
				Links: link(map[string][]int{"pack": {1, 2}}, map[int]string{20: "pack"}),
				Files: libFiles(10, 20),
			},
			downloads: []string{"pack"},
			movieRows: []Row{movieRow("tmdb:1", 1, 10), movieRow("tmdb:2", 2, 20)},
			wantMovies: []listedRow{
				{key: "tmdb:1"},
				{key: "tmdb:2", torrents: []string{"pack"}},
			},
		},
		{
			name: "13. candidates are sorted by key and the rows they are tied to by list, then key",
			movies: ManagerFiles{
				Links: link(nil, map[int]string{10: "x", 20: "x", 30: "y"}),
				Files: libFiles(10, 20, 30),
			},
			series:     ManagerFiles{Links: link(nil, map[int]string{11: "x"}), Files: libFiles(11)},
			downloads:  []string{"x", "y"},
			movieRows:  []Row{movieRow("tmdb:3", 3, 30), movieRow("tmdb:2", 2, 20), movieRow("tmdb:1", 1, 10)},
			seriesRows: []Row{seasonRow(1, []int{11}, []int{11})},
			wantMovies: []listedRow{
				{key: "tmdb:1", torrents: []string{"x"}, tied: []string{"movies/tmdb:2", "series/tvdb:100:s1"}},
				{key: "tmdb:2", torrents: []string{"x"}, tied: []string{"movies/tmdb:1", "series/tvdb:100:s1"}},
				{key: "tmdb:3", torrents: []string{"y"}},
			},
			wantSeries: []listedRow{
				{key: "tvdb:100:s1", torrents: []string{"x"}, tied: []string{"movies/tmdb:1", "movies/tmdb:2"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { runWhole(t, tt) })
	}
}

// A library file goes as a whole too: rows that delete the same file belong to one group,
// and a file that holds an item no row of the group deletes holds the group back (spec 6.3).
func TestWholeFiles(t *testing.T) {
	tests := []wholeCase{
		{
			name:          "1a. a file that also holds an episode of a season that does not qualify holds the row back",
			series:        ManagerFiles{Links: links(), Files: []LibraryFile{held(30, 1, 2)}},
			seriesRows:    []Row{seasonRow(2, []int{2}, []int{30})},
			waitingSeries: 1,
		},
		{
			name:          "1b. the same with a torrent backing the file",
			series:        ManagerFiles{Links: link(nil, map[int]string{30: "x"}), Files: []LibraryFile{held(30, 1, 2)}},
			downloads:     []string{"x"},
			seriesRows:    []Row{seasonRow(2, []int{2}, []int{30})},
			waitingSeries: 1,
		},
		{
			name:       "2. two qualifying seasons that share a file and no torrent are tied",
			series:     ManagerFiles{Links: links(), Files: []LibraryFile{held(30, 1, 2)}},
			seriesRows: []Row{seasonRow(1, []int{1}, []int{30}), seasonRow(2, []int{2}, []int{30})},
			wantSeries: []listedRow{
				{key: "tvdb:100:s1", tied: []string{"series/tvdb:100:s2"}},
				{key: "tvdb:100:s2", tied: []string{"series/tvdb:100:s1"}},
			},
		},
		{
			name:          "3a. a still-airing series whose regular file also holds a special waits",
			series:        ManagerFiles{Links: links(), Files: []LibraryFile{held(11, 11), held(12, 12, 1)}},
			seriesRows:    []Row{seriesRow([]int{11, 12}, []int{11, 12})},
			waitingSeries: 1,
		},
		{
			name:       "3b. the same series ended is listed",
			series:     ManagerFiles{Links: links(), Files: []LibraryFile{held(11, 11), held(12, 12, 1)}},
			seriesRows: []Row{seriesRow([]int{1, 11, 12}, []int{11, 12})},
			wantSeries: []listedRow{{key: "tvdb:100"}},
		},
		{
			name:       "4a. equal file numbers in the two managers do not tie",
			movies:     ManagerFiles{Links: links(), Files: []LibraryFile{held(10, 1)}},
			series:     ManagerFiles{Links: links(), Files: []LibraryFile{held(10, 1)}},
			movieRows:  []Row{movieRow("tmdb:1", 1, 10)},
			seriesRows: []Row{seasonRow(1, []int{1}, []int{10})},
			wantMovies: []listedRow{{key: "tmdb:1"}},
			wantSeries: []listedRow{{key: "tvdb:100:s1"}},
		},
		{
			name: "4b. a movie with the number of an episode that stays does not stand in for it",
			// One torrent ties movie 2 to the season; the season's file also holds episode 2.
			movies:        ManagerFiles{Links: link(nil, map[int]string{20: "x"}), Files: []LibraryFile{held(20, 2)}},
			series:        ManagerFiles{Links: link(nil, map[int]string{10: "x"}), Files: []LibraryFile{held(10, 1, 2)}},
			downloads:     []string{"x"},
			movieRows:     []Row{movieRow("tmdb:2", 2, 20)},
			seriesRows:    []Row{seasonRow(1, []int{1}, []int{10})},
			waitingMovies: 1,
			waitingSeries: 1,
		},
		{
			name:          "5. a file whose source is not in the client still holds the row back",
			series:        ManagerFiles{Links: link(nil, map[int]string{30: "gone"}), Files: []LibraryFile{held(30, 1, 2)}},
			seriesRows:    []Row{seasonRow(2, []int{2}, []int{30})},
			waitingSeries: 1,
		},
		{
			name:       "6a. rows joined by a shared file and by a torrent are all tied",
			series:     ManagerFiles{Links: link(nil, map[int]string{40: "x", 50: "x"}), Files: []LibraryFile{held(30, 1, 2), held(40, 3), held(50, 4)}},
			downloads:  []string{"x"},
			seriesRows: []Row{seasonRow(1, []int{1}, []int{30}), seasonRow(2, []int{2, 3}, []int{30, 40}), seasonRow(3, []int{4}, []int{50})},
			wantSeries: []listedRow{
				{key: "tvdb:100:s1", tied: []string{"series/tvdb:100:s2", "series/tvdb:100:s3"}},
				{key: "tvdb:100:s2", torrents: []string{"x"}, tied: []string{"series/tvdb:100:s1", "series/tvdb:100:s3"}},
				{key: "tvdb:100:s3", torrents: []string{"x"}, tied: []string{"series/tvdb:100:s1", "series/tvdb:100:s2"}},
			},
		},
		{
			name:          "6b. the same, with an episode that no row deletes in the last file: all three wait",
			series:        ManagerFiles{Links: link(nil, map[int]string{40: "x", 50: "x"}), Files: []LibraryFile{held(30, 1, 2), held(40, 3), held(50, 4, 5)}},
			downloads:     []string{"x"},
			seriesRows:    []Row{seasonRow(1, []int{1}, []int{30}), seasonRow(2, []int{2, 3}, []int{30, 40}), seasonRow(3, []int{4}, []int{50})},
			waitingSeries: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { runWhole(t, tt) })
	}
}

// WholeTorrents fills the lists from their rows each time: a second call on the same lists
// leaves what the first call found.
func TestWholeTorrentsCanBeCalledTwice(t *testing.T) {
	x := NewLinkIndex(ManagerFiles{
		Manager: ManagerMovies,
		Links:   link(nil, map[int]string{20: "q"}, "q"),
		Files:   libFiles(10, 20),
	})
	movies := &ListResult{Rows: []Row{movieRow("tmdb:1", 1, 10), movieRow("tmdb:2", 2, 20)}}
	downloads := []domain.Download{dl("q")}

	WholeTorrents(domain.DefaultRules(), x, downloads, movies)
	wantCandidates, wantWaiting, wantHeld := movies.Candidates, movies.Waiting, movies.Held
	if len(wantCandidates) != 1 || wantWaiting != 1 || len(wantHeld) != 1 {
		t.Fatalf("first call: %d listed, %d waiting, %d held, want 1, 1 and 1", len(wantCandidates), wantWaiting, len(wantHeld))
	}
	WholeTorrents(domain.DefaultRules(), x, downloads, movies)
	if !reflect.DeepEqual(movies.Candidates, wantCandidates) || movies.Waiting != wantWaiting {
		t.Errorf("second call: candidates %+v, waiting %d; want %+v and %d", movies.Candidates, movies.Waiting, wantCandidates, wantWaiting)
	}
	if !reflect.DeepEqual(movies.Held, wantHeld) {
		t.Errorf("second call: held %+v; want %+v", movies.Held, wantHeld)
	}
}

// The rows the rule holds back are kept, so that the lists can apply ignores later without
// a new scan (spec 6.5).
func TestWholeTorrentsKeepsTheRowsItHoldsBack(t *testing.T) {
	x := NewLinkIndex(ManagerFiles{
		Manager: ManagerSeries,
		Links:   link(map[string][]int{"pack": {1, 2}}, map[int]string{11: "pack", 21: "pack"}),
		Files:   libFiles(11, 21),
	})
	series := &ListResult{Rows: []Row{
		seasonRow(1, []int{1}, []int{11}), // the pack also backs file 21, which stays
		seasonRow(3, []int{3}, []int{31}), // backed by nothing
	}}
	WholeTorrents(domain.DefaultRules(), x, []domain.Download{dl("pack")}, series)

	if len(series.Candidates) != 1 || series.Candidates[0].Key != domain.SeasonKey("100", 3) || series.Candidates[0].Waiting {
		t.Fatalf("listed %+v, want season 3 alone and not marked waiting", series.Candidates)
	}
	if series.Waiting != 1 || len(series.Held) != 1 {
		t.Fatalf("Waiting = %d, Held = %+v; want one row held back", series.Waiting, series.Held)
	}
	h := series.Held[0]
	if h.Key != domain.SeasonKey("100", 1) || !h.Waiting || h.Torrents == nil || len(h.Torrents) != 0 || h.TiedWith != nil || h.BelowSeedMin {
		t.Errorf("held row = %+v; want season 1, marked waiting, with no torrents and no ties", h)
	}
}

// Held rows are sorted by key, like the listed ones, whatever the order of the rows.
func TestWholeTorrentsSortsTheRowsItHoldsBackByKey(t *testing.T) {
	x := NewLinkIndex(ManagerFiles{
		Manager: ManagerMovies,
		Links:   link(nil, map[int]string{10: "coll", 20: "coll", 30: "coll"}),
		Files:   libFiles(10, 20, 30),
	})
	movies := &ListResult{Rows: []Row{movieRow("tmdb:2", 2, 20), movieRow("tmdb:1", 1, 10)}}
	WholeTorrents(domain.DefaultRules(), x, []domain.Download{dl("coll")}, movies)
	var keys []string
	for _, h := range movies.Held {
		keys = append(keys, h.Key)
	}
	if want := []string{"tmdb:1", "tmdb:2"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("held %v, want %v", keys, want)
	}
}

// A tied row names the rows it is tied to with what the list shows of them.
func TestWholeTorrentsNamesTiedRows(t *testing.T) {
	x := NewLinkIndex(ManagerFiles{
		Manager: ManagerSeries,
		Links:   link(nil, map[int]string{11: "pack", 21: "pack"}),
		Files:   libFiles(11, 21),
	})
	series := &ListResult{Rows: []Row{seasonRow(1, []int{11}, []int{11}), seasonRow(2, []int{21}, []int{21})}}
	WholeTorrents(domain.DefaultRules(), x, []domain.Download{dl("pack")}, series)
	want := []domain.RowRef{{List: domain.ListSeries, Key: "tvdb:100:s2", Title: "The Show", Season: 2}}
	if len(series.Candidates) != 2 || !reflect.DeepEqual(series.Candidates[0].TiedWith, want) {
		t.Fatalf("candidates = %+v, want season 1 tied to %+v", series.Candidates, want)
	}
}

// The seeding flag comes from the torrents a row names; the row's own fields pass through.
func TestWholeTorrentsFlagsSeedingAndKeepsTheRow(t *testing.T) {
	r := domain.DefaultRules()
	r.MinRatio = 3
	x := NewLinkIndex(ManagerFiles{Manager: ManagerMovies, Links: link(nil, map[int]string{10: "h1"}), Files: libFiles(10)})
	movies := &ListResult{Rows: []Row{movieRow("tmdb:1", 1, 10)}}
	WholeTorrents(r, x, []domain.Download{dl("h1")}, movies)
	c := movies.Candidates[0]
	if !c.BelowSeedMin || c.Title != "Movie tmdb:1" || c.Size != 100 || c.List != domain.ListMovies {
		t.Errorf("candidate = %+v, want the row's fields and the seeding flag", c)
	}
}

// A row states, for each torrent it names, how much of it is not in the library, when that is
// 50 MB or more. It is information only.
func TestWholeTorrentsStatesDataOutsideTheLibrary(t *testing.T) {
	const mb = int64(1 << 20)
	sized := func(id int, size int64) LibraryFile { return LibraryFile{ID: id, Size: size} }
	tests := []struct {
		name   string
		size   int64 // the torrent's size
		movie  []LibraryFile
		series []LibraryFile
		want   int64
	}{
		{"a pack larger than the library files it backs", 3000 * mb, []LibraryFile{sized(10, 2000*mb)}, nil, 1000 * mb},
		{"a difference of 50 MB minus one byte", 50*mb - 1 + 100, []LibraryFile{sized(10, 100)}, nil, 0},
		{"a difference of exactly 50 MB", 50*mb + 100, []LibraryFile{sized(10, 100)}, nil, 50 * mb},
		{"a torrent smaller than the files it backs", 100, []LibraryFile{sized(10, 500*mb)}, nil, 0},
		{"a torrent backing a movie file and an episode file", 1000 * mb, []LibraryFile{sized(10, 300*mb)}, []LibraryFile{sized(11, 200*mb)}, 500 * mb},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x := NewLinkIndex(
				ManagerFiles{Manager: ManagerMovies, Links: link(nil, map[int]string{10: "x"}), Files: tt.movie},
				ManagerFiles{Manager: ManagerSeries, Links: link(nil, map[int]string{11: "x"}), Files: tt.series},
			)
			movies := &ListResult{Rows: []Row{movieRow("tmdb:1", 1, 10)}}
			series := &ListResult{}
			if len(tt.series) > 0 {
				series.Rows = []Row{seasonRow(1, []int{1}, []int{11})}
			}
			WholeTorrents(domain.DefaultRules(), x, []domain.Download{dl("x", func(d *domain.Download) { d.Size = tt.size })}, movies, series)
			if len(movies.Candidates) != 1 {
				t.Fatalf("movie candidates = %d, want 1", len(movies.Candidates))
			}
			for _, l := range []*ListResult{movies, series} {
				for _, c := range l.Candidates {
					if len(c.Torrents) != 1 || c.Torrents[0].OutsideLibrary != tt.want {
						t.Errorf("%s: torrents = %+v, want OutsideLibrary %d", c.Key, c.Torrents, tt.want)
					}
				}
			}
		})
	}

	t.Run("an old download that backs nothing", func(t *testing.T) {
		x := NewLinkIndex(ManagerFiles{Manager: ManagerMovies, Links: link(map[string][]int{"old": {1}}, nil), Files: libFiles(10)})
		movies := &ListResult{Rows: []Row{movieRow("tmdb:1", 1, 10)}}
		WholeTorrents(domain.DefaultRules(), x, []domain.Download{dl("old", func(d *domain.Download) { d.Size = 700 * mb })}, movies)
		if len(movies.Candidates) != 1 || len(movies.Candidates[0].Torrents) != 1 || movies.Candidates[0].Torrents[0].OutsideLibrary != 700*mb {
			t.Fatalf("candidates = %+v, want one torrent with its whole size outside", movies.Candidates)
		}
	})

	t.Run("a row of the torrent list does not state it", func(t *testing.T) {
		res := Torrents(TorrentsInput{
			Rules:     domain.DefaultRules(),
			Downloads: []domain.Download{dl("orphan", func(d *domain.Download) { d.Size = 700 * mb })},
			Index:     NewLinkIndex(),
		})
		if len(res.Candidates) != 1 || res.Candidates[0].Torrents[0].OutsideLibrary != 0 {
			t.Fatalf("candidates = %+v, want OutsideLibrary unset", res.Candidates)
		}
	})
}

// One pack can back thousands of files of one row. Whether the pack stays is the same answer
// for each of them, so it is worked out once and not once per file: with 40,000 files that
// is 40,000 lookups instead of 1,600,000,000.
func TestWholeTorrentsJudgesALargePackOncePerGroup(t *testing.T) {
	const n = 40000
	ids := make([]int, n)
	sources := make(map[int]string, n)
	files := make([]LibraryFile, n)
	for i := range ids {
		ids[i] = i + 1
		sources[i+1] = "pack"
		files[i] = held(i+1, i+1)
	}
	x := NewLinkIndex(ManagerFiles{
		Manager: ManagerSeries,
		Links:   link(map[string][]int{"pack": ids}, sources),
		Files:   files,
	})
	series := &ListResult{Rows: []Row{seriesRow(ids, ids)}}
	downloads := []domain.Download{dl("pack")}

	done := make(chan struct{}, 1) // buffered: the goroutine can finish after a timeout
	go func() {
		WholeTorrents(domain.DefaultRules(), x, downloads, series)
		done <- struct{}{}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a row backed by one pack of 40,000 files was not judged within 5 seconds")
	}
	if len(series.Candidates) != 1 || len(series.Candidates[0].Torrents) != 1 || series.Candidates[0].Torrents[0].ID != "pack" {
		t.Errorf("candidates = %+v, want the one row naming the pack", series.Candidates)
	}
}

func TestWholeTorrentsSaysWhatAHeldRowWaitsFor(t *testing.T) {
	tests := []struct {
		name      string
		series    ManagerFiles
		downloads []string
		rows      []Row
		want      string
	}{
		{
			name:      "its torrent is in a queue",
			series:    ManagerFiles{Links: link(nil, map[int]string{11: "q"}, "q"), Files: libFiles(11)},
			downloads: []string{"q"},
			rows:      []Row{seasonRow(1, []int{11}, []int{11})},
			want:      WaitQueued,
		},
		{
			name:      "its pack backs a file that stays",
			series:    ManagerFiles{Links: link(nil, map[int]string{11: "pack", 21: "pack"}), Files: libFiles(11, 21)},
			downloads: []string{"pack"},
			rows:      []Row{seasonRow(1, []int{11}, []int{11})},
			want:      WaitTorrentStays,
		},
		{
			name:   "its file holds an episode that stays",
			series: ManagerFiles{Links: links(), Files: []LibraryFile{held(30, 12, 21)}},
			rows:   []Row{seasonRow(1, []int{12}, []int{30})},
			want:   WaitFileStays,
		},
		{
			name:      "a queue is named before a pack that stays",
			series:    ManagerFiles{Links: link(nil, map[int]string{11: "q", 12: "pack", 21: "pack"}, "q"), Files: libFiles(11, 12, 21)},
			downloads: []string{"q", "pack"},
			rows:      []Row{seasonRow(1, []int{11, 12}, []int{11, 12})},
			want:      WaitQueued,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.series.Manager = ManagerSeries
			x := NewLinkIndex(tt.series)
			var downloads []domain.Download
			for _, id := range tt.downloads {
				downloads = append(downloads, dl(id))
			}
			res := &ListResult{Rows: tt.rows}
			WholeTorrents(domain.DefaultRules(), x, downloads, res)
			if len(res.Held) != 1 {
				t.Fatalf("held %d rows, want 1", len(res.Held))
			}
			if got := res.Held[0].WaitReason; got != tt.want {
				t.Errorf("WaitReason = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWholeTorrentsGivesAListedRowNoWaitReason(t *testing.T) {
	x := NewLinkIndex(ManagerFiles{Manager: ManagerMovies, Links: link(nil, map[int]string{10: "h1"}), Files: libFiles(10)})
	res := &ListResult{Rows: []Row{movieRow("tmdb:1", 1, 10)}}
	WholeTorrents(domain.DefaultRules(), x, []domain.Download{dl("h1")}, res)
	if len(res.Candidates) != 1 || res.Candidates[0].WaitReason != "" {
		t.Errorf("candidates = %+v, want one listed row without a wait reason", res.Candidates)
	}
}
