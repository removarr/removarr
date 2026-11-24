package scan

import (
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

var ignoredAt = time.Date(2026, 3, 11, 9, 0, 0, 0, time.UTC)

func ignore(kind, key string) domain.Ignore {
	return domain.Ignore{Kind: kind, Key: key, Title: "Title of " + key, CreatedAt: ignoredAt}
}

func tie(list domain.ListKind, key string) []domain.RowRef {
	return []domain.RowRef{{List: list, Key: key}}
}

// storedRows is what one invented scan stored: three movies, one of them tied to a series
// row and one to a season row; season rows of another series, one of them held back; a
// torrent.
func storedRows() []domain.Candidate {
	none := []domain.TorrentInfo{}
	return []domain.Candidate{
		{List: domain.ListMovies, Key: "tmdb:1", Torrents: none},
		{List: domain.ListMovies, Key: "tmdb:2", Torrents: none, TiedWith: tie(domain.ListSeries, "tvdb:100")},
		{List: domain.ListMovies, Key: "tmdb:3", Torrents: none, TiedWith: tie(domain.ListSeries, "tvdb:200:s1")},
		{List: domain.ListSeries, Key: "tvdb:100", Scope: domain.ScopeSeries, Torrents: none, TiedWith: tie(domain.ListMovies, "tmdb:2")},
		{List: domain.ListSeries, Key: "tvdb:200:s1", Scope: domain.ScopeSeason, Season: 1, Torrents: none, TiedWith: tie(domain.ListMovies, "tmdb:3")},
		{List: domain.ListSeries, Key: "tvdb:200:s2", Scope: domain.ScopeSeason, Season: 2, Torrents: none, Waiting: true},
		{List: domain.ListSeries, Key: "tvdb:2000:s1", Scope: domain.ScopeSeason, Season: 1, Torrents: none},
		{List: domain.ListTorrents, Key: "hash:a", Torrents: none},
	}
}

type viewWant struct {
	shown   []string
	ignored []string
	waiting int
}

func keys(cs []domain.Candidate) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.Key)
	}
	return out
}

func TestViewAppliesIgnores(t *testing.T) {
	tests := []struct {
		name    string
		ignores []domain.Ignore
		want    map[domain.ListKind]viewWant
	}{
		{
			name: "nothing ignored: listed rows are shown and held rows wait",
			want: map[domain.ListKind]viewWant{
				domain.ListMovies:   {shown: []string{"tmdb:1", "tmdb:2", "tmdb:3"}, ignored: []string{}},
				domain.ListSeries:   {shown: []string{"tvdb:100", "tvdb:2000:s1", "tvdb:200:s1"}, ignored: []string{}, waiting: 1},
				domain.ListTorrents: {shown: []string{"hash:a"}, ignored: []string{}},
			},
		},
		{
			name:    "an ignored row is hidden and counted nowhere",
			ignores: []domain.Ignore{ignore(domain.IgnoreMovie, "tmdb:1"), ignore(domain.IgnoreTorrent, "hash:a")},
			want: map[domain.ListKind]viewWant{
				domain.ListMovies:   {shown: []string{"tmdb:2", "tmdb:3"}, ignored: []string{"tmdb:1"}},
				domain.ListSeries:   {shown: []string{"tvdb:100", "tvdb:2000:s1", "tvdb:200:s1"}, ignored: []string{}, waiting: 1},
				domain.ListTorrents: {shown: []string{}, ignored: []string{"hash:a"}},
			},
		},
		{
			name:    "a row tied to an ignored row waits, also in the other list",
			ignores: []domain.Ignore{ignore(domain.IgnoreMovie, "tmdb:2")},
			want: map[domain.ListKind]viewWant{
				domain.ListMovies:   {shown: []string{"tmdb:1", "tmdb:3"}, ignored: []string{"tmdb:2"}},
				domain.ListSeries:   {shown: []string{"tvdb:2000:s1", "tvdb:200:s1"}, ignored: []string{}, waiting: 2},
				domain.ListTorrents: {shown: []string{"hash:a"}, ignored: []string{}},
			},
		},
		{
			name:    "a series ignore hides its season rows, held ones too, and no other series",
			ignores: []domain.Ignore{ignore(domain.IgnoreSeries, "tvdb:200")},
			want: map[domain.ListKind]viewWant{
				// tmdb:3 is tied to tvdb:200:s1, which only the ignore of its series hides.
				domain.ListMovies:   {shown: []string{"tmdb:1", "tmdb:2"}, ignored: []string{}, waiting: 1},
				domain.ListSeries:   {shown: []string{"tvdb:100", "tvdb:2000:s1"}, ignored: []string{"tvdb:200"}},
				domain.ListTorrents: {shown: []string{"hash:a"}, ignored: []string{}},
			},
		},
		{
			name:    "a season ignore never hides the series row",
			ignores: []domain.Ignore{ignore(domain.IgnoreSeason, "tvdb:100:s1")},
			want: map[domain.ListKind]viewWant{
				domain.ListMovies:   {shown: []string{"tmdb:1", "tmdb:2", "tmdb:3"}, ignored: []string{}},
				domain.ListSeries:   {shown: []string{"tvdb:100", "tvdb:2000:s1", "tvdb:200:s1"}, ignored: []string{"tvdb:100:s1"}, waiting: 1},
				domain.ListTorrents: {shown: []string{"hash:a"}, ignored: []string{}},
			},
		},
		{
			name:    "an ignored held row is no longer counted as waiting",
			ignores: []domain.Ignore{ignore(domain.IgnoreSeason, "tvdb:200:s2")},
			want: map[domain.ListKind]viewWant{
				domain.ListMovies:   {shown: []string{"tmdb:1", "tmdb:2", "tmdb:3"}, ignored: []string{}},
				domain.ListSeries:   {shown: []string{"tvdb:100", "tvdb:2000:s1", "tvdb:200:s1"}, ignored: []string{"tvdb:200:s2"}},
				domain.ListTorrents: {shown: []string{"hash:a"}, ignored: []string{}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := NewIgnoreSet(tt.ignores)
			for list, want := range tt.want {
				v := View(list, storedRows(), set)
				got := viewWant{shown: keys(v.Shown), ignored: keys(v.Ignored), waiting: v.Waiting}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s: got %+v, want %+v", list, got, want)
				}
			}
		})
	}
}

func TestViewIgnoredEntries(t *testing.T) {
	set := NewIgnoreSet([]domain.Ignore{
		ignore(domain.IgnoreSeason, "tvdb:200:s2"), // held back by the scan
		ignore(domain.IgnoreSeries, "tvdb:200"),    // no series row stored: per-season mode
		ignore(domain.IgnoreSeason, "tvdb:100:s3"), // no season row: whole-series mode
	})
	v := View(domain.ListSeries, storedRows(), set)
	if got := keys(v.Ignored); !reflect.DeepEqual(got, []string{"tvdb:100:s3", "tvdb:200", "tvdb:200:s2"}) {
		t.Fatalf("ignored entries = %v", got)
	}
	for _, c := range v.Ignored {
		if c.IgnoredAt == nil || !c.IgnoredAt.Equal(ignoredAt) || c.Torrents == nil || c.List != domain.ListSeries {
			t.Errorf("%s: %+v; want the ignore's time, a non-nil torrent list and the series list", c.Key, c)
		}
	}
	stale, series, held := v.Ignored[0], v.Ignored[1], v.Ignored[2]
	if !stale.NotInScan || stale.Title != "Title of tvdb:100:s3" || stale.Scope != domain.ScopeSeason || stale.Season != 3 {
		t.Errorf("season ignore without a row = %+v; want key, title, scope and season from the ignore", stale)
	}
	if !series.NotInScan || series.Scope != domain.ScopeSeries || series.Season != 0 {
		t.Errorf("series ignore without a row = %+v", series)
	}
	if held.NotInScan || !held.Waiting || held.Season != 2 {
		t.Errorf("ignored held row = %+v; want the stored row, still marked waiting", held)
	}
	if shown := View(domain.ListSeries, storedRows(), set).Shown; len(shown) == 0 || shown[0].IgnoredAt != nil {
		t.Errorf("shown rows = %+v; want rows without an ignore time", shown)
	}
}

func TestIgnoreSetHides(t *testing.T) {
	set := NewIgnoreSet([]domain.Ignore{
		ignore(domain.IgnoreSeries, "tvdb:100"),
		ignore(domain.IgnoreSeason, "tvdb:300:s1"),
		ignore(domain.IgnoreMovie, "tmdb:1"),
	})
	tests := []struct {
		list domain.ListKind
		key  string
		want bool
	}{
		{domain.ListSeries, "tvdb:100", true},
		{domain.ListSeries, "tvdb:100:s4", true},
		{domain.ListSeries, "tvdb:1000:s4", false},
		{domain.ListSeries, "tvdb:300:s1", true},
		{domain.ListSeries, "tvdb:300", false},
		{domain.ListSeries, "tvdb:300:s2", false},
		{domain.ListMovies, "tmdb:1", true},
		{domain.ListTorrents, "tmdb:1", false}, // an ignore hides rows of its own list only
	}
	for _, tt := range tests {
		if got := set.Hides(tt.list, tt.key); got != tt.want {
			t.Errorf("Hides(%s, %s) = %v, want %v", tt.list, tt.key, got, tt.want)
		}
	}
	if got := set.Keys(); !reflect.DeepEqual(got, map[string]bool{"tvdb:100": true, "tvdb:300:s1": true, "tmdb:1": true}) {
		t.Errorf("Keys() = %v", got)
	}
}

// A series row the per-season merge produced while one of its seasons was not yet ignored
// waits, and so does every row tied to it (spec 6.5).
func TestViewHoldsBackASeriesRowMergedOverANowIgnoredSeason(t *testing.T) {
	none := []domain.TorrentInfo{}
	rows := []domain.Candidate{
		{List: domain.ListMovies, Key: "tmdb:30", Torrents: none, TiedWith: tie(domain.ListSeries, "tvdb:300")},
		{List: domain.ListMovies, Key: "tmdb:40", Torrents: none, TiedWith: tie(domain.ListSeries, "tvdb:400")},
		{List: domain.ListSeries, Key: "tvdb:300", Scope: domain.ScopeSeries, Merged: true, Torrents: none, TiedWith: tie(domain.ListMovies, "tmdb:30")},
		{List: domain.ListSeries, Key: "tvdb:400", Scope: domain.ScopeSeries, Torrents: none, TiedWith: tie(domain.ListMovies, "tmdb:40")}, // whole-series mode
		{List: domain.ListSeries, Key: "tvdb:500", Scope: domain.ScopeSeries, Merged: true, Torrents: none},
	}
	set := NewIgnoreSet([]domain.Ignore{
		ignore(domain.IgnoreSeason, "tvdb:300:s1"),
		ignore(domain.IgnoreSeason, "tvdb:400:s1"),
	})

	want := map[domain.ListKind]viewWant{
		domain.ListMovies: {shown: []string{"tmdb:40"}, ignored: []string{}, waiting: 1},
		domain.ListSeries: {shown: []string{"tvdb:400", "tvdb:500"}, ignored: []string{"tvdb:300:s1", "tvdb:400:s1"}, waiting: 1},
	}
	views := map[domain.ListKind]ListView{}
	for list, w := range want {
		v := View(list, rows, set)
		views[list] = v
		if got := (viewWant{shown: keys(v.Shown), ignored: keys(v.Ignored), waiting: v.Waiting}); !reflect.DeepEqual(got, w) {
			t.Errorf("%s: got %+v, want %+v", list, got, w)
		}
	}
	for _, c := range views[domain.ListSeries].Ignored {
		if !c.NotInScan || c.Scope != domain.ScopeSeason || c.Season != 1 {
			t.Errorf("season ignore entry %s = %+v; want it by key and title, not in the scan", c.Key, c)
		}
	}
}
