// Package domain holds the product-neutral types the rest of removarr works with.
// It imports nothing from the other removarr packages.
package domain

import "time"

// User is an account on the media server.
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ExternalIDs identify a movie or series across products. An empty string means unknown.
type ExternalIDs struct {
	TMDB string
	TVDB string
	IMDB string
}

// UserState is one user's watch state for one item.
type UserState struct {
	Played     bool
	LastPlayed *time.Time // nil when the media server gives no date
	Favorite   bool
}

// ServerMovie is a movie as the media server sees it.
type ServerMovie struct {
	ID     string
	Title  string
	IDs    ExternalIDs
	States map[string]UserState // keyed by user ID
}

// ServerSeries is a series as the media server sees it.
type ServerSeries struct {
	ID         string
	Title      string
	IDs        ExternalIDs
	FavoriteOf map[string]bool // IDs of users who marked the series, or one of its seasons, a favourite
}

// ServerEpisode is an episode item on the media server. One item can cover a range of episodes.
type ServerEpisode struct {
	SeriesID   string // the media server's ID of the series
	Season     int
	Episode    int
	EpisodeEnd int // last episode of the range; 0 for a single episode
	States     map[string]UserState
}

// Library is a snapshot of the media server for a set of users.
type Library struct {
	Movies   []ServerMovie
	Series   []ServerSeries
	Episodes []ServerEpisode
}
