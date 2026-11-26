// Package providers defines what removarr needs from the apps it talks to. Adding support
// for another product means adding one implementation of one of these interfaces.
package providers

import (
	"context"
	"errors"

	"github.com/removarr/removarr/internal/domain"
)

// ErrNotConfigured is returned when an app's connection settings are incomplete.
var ErrNotConfigured = errors.New("not configured")

// MediaServer is where things are watched (Jellyfin in v1).
type MediaServer interface {
	Users(ctx context.Context) ([]domain.User, error)
	// Library returns the watch state of every movie and episode for the given users.
	Library(ctx context.Context, userIDs []string) (domain.Library, error)
}

// MovieManager manages movies (Radarr in v1).
type MovieManager interface {
	Movies(ctx context.Context) (domain.MovieSnapshot, error)
}

// SeriesManager manages series (Sonarr in v1).
type SeriesManager interface {
	Series(ctx context.Context) (domain.SeriesSnapshot, error)
}

// DownloadClient holds the downloads (qBittorrent in v1).
type DownloadClient interface {
	Downloads(ctx context.Context) ([]domain.Download, error)
	// Files returns the files of one download. It is used to link a download to library
	// files by content when history does not link it (spec 6.6).
	Files(ctx context.Context, id string) ([]domain.DownloadFile, error)
}

// MovieWriter changes a movie manager. Only the executor is given one (spec 7.2).
type MovieWriter interface {
	// RemoveMovie removes the movie and its files, and adds it to the import exclusion list
	// when addExclusion is set.
	RemoveMovie(ctx context.Context, id int, addExclusion bool) error
}

// SeriesWriter changes a series manager. Only the executor is given one (spec 7.2).
type SeriesWriter interface {
	RemoveSeries(ctx context.Context, id int, addExclusion bool) error
	DeleteEpisodeFiles(ctx context.Context, fileIDs []int) error
	UnmonitorEpisodes(ctx context.Context, episodeIDs []int) error
	UnmonitorSeasons(ctx context.Context, seriesID int, seasons []int) error
}

// DownloadRemover removes downloads from the download client. Only the executor is given one.
type DownloadRemover interface {
	// RemoveDownload removes the download, and its data when deleteData is set.
	RemoveDownload(ctx context.Context, id string, deleteData bool) error
}

// Tester checks that an app can be reached with the configured credentials.
// detail describes the app, for example "Sonarr 4.0.9".
type Tester interface {
	Test(ctx context.Context) (detail string, err error)
}
