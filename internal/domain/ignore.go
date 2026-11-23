package domain

import (
	"strconv"
	"strings"
	"time"
)

// The kinds of ignore (spec 6.5).
const (
	IgnoreMovie   = "movie"
	IgnoreSeries  = "series"
	IgnoreSeason  = "season"
	IgnoreTorrent = "torrent"
)

// Ignore is a row the user chose to hide (spec 6.5). It is kept by the row's stable key.
type Ignore struct {
	Kind      string // IgnoreMovie, IgnoreSeries, IgnoreSeason or IgnoreTorrent
	Key       string // the row's stable key
	Title     string // the row's title when it was ignored
	CreatedAt time.Time
}

// List returns the list whose rows the ignore can hide.
func (i Ignore) List() ListKind {
	switch i.Kind {
	case IgnoreMovie:
		return ListMovies
	case IgnoreTorrent:
		return ListTorrents
	default:
		return ListSeries
	}
}

// IgnoreKinds returns the kinds of ignore whose rows belong to list, the inverse of
// Ignore.List. It returns nil for an unknown list.
func IgnoreKinds(list ListKind) []string {
	switch list {
	case ListMovies:
		return []string{IgnoreMovie}
	case ListSeries:
		return []string{IgnoreSeries, IgnoreSeason}
	case ListTorrents:
		return []string{IgnoreTorrent}
	default:
		return nil
	}
}

// IgnoreKind returns the kind of ignore that hides the row.
func IgnoreKind(c Candidate) string {
	switch {
	case c.List == ListMovies:
		return IgnoreMovie
	case c.List == ListTorrents:
		return IgnoreTorrent
	case c.Scope == ScopeSeason:
		return IgnoreSeason
	default:
		return IgnoreSeries
	}
}

// ParseSeasonKey splits the key of a season row into the key of its series and the season
// number. It accepts exactly the keys SeasonKey makes.
func ParseSeasonKey(key string) (seriesKey string, season int, ok bool) {
	const prefix = "tvdb:"
	i := strings.LastIndex(key, ":s")
	if i < len(prefix) || !strings.HasPrefix(key, prefix) {
		return "", 0, false
	}
	tvdb := key[len(prefix):i]
	n, err := strconv.Atoi(key[i+2:])
	if err != nil || n < 0 || tvdb == "" || SeasonKey(tvdb, n) != key {
		return "", 0, false
	}
	return SeriesKey(tvdb), n, true
}
