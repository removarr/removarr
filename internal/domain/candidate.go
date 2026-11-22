package domain

import (
	"fmt"
	"time"
)

// ListKind names one of the three suggestion lists.
type ListKind string

const (
	ListMovies   ListKind = "movies"
	ListSeries   ListKind = "series"
	ListTorrents ListKind = "torrents"
)

const (
	ScopeSeries = "series"
	ScopeSeason = "season"

	ReasonOrphan   = "orphan"
	ReasonUnlinked = "unlinked"
)

// TorrentInfo is what a row shows about one torrent.
type TorrentInfo struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Size         int64     `json:"size"`
	State        string    `json:"state"`
	Category     string    `json:"category"`
	Tracker      string    `json:"tracker"`
	Ratio        *float64  `json:"ratio"`
	SeedingDays  *float64  `json:"seedingDays"`
	AddedAt      time.Time `json:"addedAt"`
	BelowSeedMin bool      `json:"belowSeedMin"`
}

// Candidate is one row in a list: something removarr suggests deleting.
type Candidate struct {
	List         ListKind      `json:"list"`
	Key          string        `json:"key"`
	Title        string        `json:"title"`
	Year         int           `json:"year,omitempty"`
	Size         int64         `json:"size"`
	Scope        string        `json:"scope,omitempty"`       // series list: ScopeSeries or ScopeSeason
	Season       int           `json:"season,omitempty"`      // season rows
	StillAiring  bool          `json:"stillAiring,omitempty"` // series rows
	WatchedBy    []string      `json:"watchedBy,omitempty"`   // user names
	WatchedAt    *time.Time    `json:"watchedAt,omitempty"`
	Reason       string        `json:"reason,omitempty"`   // torrent list: ReasonOrphan or ReasonUnlinked
	Torrents     []TorrentInfo `json:"torrents"`           // media rows: torrents Delete would remove; torrent rows: the torrent itself
	BelowSeedMin bool          `json:"belowSeedMin"`       // true when any of Torrents is below the seeding minimum
	TiedWith     []RowRef      `json:"tiedWith,omitempty"` // rows that are deleted together with this one (spec 6.3)
}

// RowRef names another row.
type RowRef struct {
	List   ListKind `json:"list"`
	Key    string   `json:"key"`
	Title  string   `json:"title"`
	Season int      `json:"season,omitempty"` // season rows
}

// MovieKey is the stable key of a movie row: its TMDB ID, or its IMDb ID when there is none.
func MovieKey(ids ExternalIDs) string {
	if ids.TMDB != "" {
		return "tmdb:" + ids.TMDB
	}
	return "imdb:" + ids.IMDB
}

// SeriesKey is the stable key of a whole-series row.
func SeriesKey(tvdb string) string { return "tvdb:" + tvdb }

// SeasonKey is the stable key of a season row.
func SeasonKey(tvdb string, season int) string { return fmt.Sprintf("tvdb:%s:s%d", tvdb, season) }

// TorrentKey is the stable key of a torrent row.
func TorrentKey(hash string) string { return "hash:" + hash }
