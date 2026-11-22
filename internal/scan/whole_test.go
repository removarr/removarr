package scan

import (
	"fmt"
	"reflect"
	"testing"

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
		var got []listedRow
		for _, c := range res.Candidates {
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
