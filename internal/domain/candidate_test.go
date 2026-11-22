package domain

import "testing"

func TestKeys(t *testing.T) {
	tests := []struct{ got, want string }{
		{MovieKey(ExternalIDs{TMDB: "603", IMDB: "tt0133093"}), "tmdb:603"},
		{MovieKey(ExternalIDs{IMDB: "tt0133093"}), "imdb:tt0133093"},
		{SeriesKey("81189"), "tvdb:81189"},
		{SeasonKey("81189", 2), "tvdb:81189:s2"},
		{TorrentKey("abc"), "hash:abc"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("got %q, want %q", tt.got, tt.want)
		}
	}
}
