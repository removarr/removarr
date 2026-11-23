package domain

import "testing"

func TestParseSeasonKeyAcceptsOnlyKeysThatSeasonKeyMakes(t *testing.T) {
	tests := []struct {
		key    string
		series string
		season int
		ok     bool
	}{
		{SeasonKey("100", 2), "tvdb:100", 2, true},
		{SeasonKey("100", 0), "tvdb:100", 0, true},
		{SeasonKey("100", 12), "tvdb:100", 12, true},
		{SeriesKey("100"), "", 0, false},
		{"tvdb:100:s02", "", 0, false},
		{"tvdb:100:s+2", "", 0, false},
		{"tvdb:100:s-1", "", 0, false},
		{"tvdb::s2", "", 0, false},
		{"tvdb:s2", "", 0, false}, // the ":s" overlaps the prefix
		{"tvdb:100:s", "", 0, false},
		{"tmdb:100:s2", "", 0, false},
		{"hash:abc", "", 0, false},
	}
	for _, tt := range tests {
		series, season, ok := ParseSeasonKey(tt.key)
		if series != tt.series || season != tt.season || ok != tt.ok {
			t.Errorf("ParseSeasonKey(%q) = %q, %d, %v; want %q, %d, %v", tt.key, series, season, ok, tt.series, tt.season, tt.ok)
		}
	}
}

func TestIgnoreKindAndListFollowTheRow(t *testing.T) {
	tests := []struct {
		row  Candidate
		kind string
		list ListKind
	}{
		{Candidate{List: ListMovies, Key: "tmdb:603"}, IgnoreMovie, ListMovies},
		{Candidate{List: ListSeries, Key: SeriesKey("100"), Scope: ScopeSeries}, IgnoreSeries, ListSeries},
		{Candidate{List: ListSeries, Key: SeasonKey("100", 2), Scope: ScopeSeason, Season: 2}, IgnoreSeason, ListSeries},
		{Candidate{List: ListTorrents, Key: TorrentKey("abc")}, IgnoreTorrent, ListTorrents},
	}
	for _, tt := range tests {
		kind := IgnoreKind(tt.row)
		if kind != tt.kind {
			t.Errorf("IgnoreKind(%s) = %q, want %q", tt.row.Key, kind, tt.kind)
		}
		if list := (Ignore{Kind: kind, Key: tt.row.Key}).List(); list != tt.list {
			t.Errorf("list of a %s ignore = %q, want %q", kind, list, tt.list)
		}
	}
}
