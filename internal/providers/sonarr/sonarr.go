// Package sonarr reads series from Sonarr.
package sonarr

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/httpjson"
	"github.com/removarr/removarr/internal/providers"
	"github.com/removarr/removarr/internal/providers/arr"
)

var (
	_ providers.SeriesManager = (*Client)(nil)
	_ providers.Tester        = (*Client)(nil)
)

// Client talks to one Sonarr instance.
type Client struct{ arr *arr.Client }

// New returns a client. baseURL has no trailing slash.
func New(baseURL, apiKey string, hc *http.Client) *Client {
	return &Client{arr: &arr.Client{BaseURL: baseURL, APIKey: apiKey, HTTP: hc}}
}

type seriesJSON struct {
	ID         int    `json:"id"`
	Title      string `json:"title"`
	Year       int    `json:"year"`
	TVDBID     int    `json:"tvdbId"`
	TMDBID     int    `json:"tmdbId"`
	IMDBID     string `json:"imdbId"`
	Status     string `json:"status"`
	Tags       []int  `json:"tags"`
	Statistics *struct {
		EpisodeFileCount int `json:"episodeFileCount"`
	} `json:"statistics"`
}

// hasFiles reports whether the series may have files. When Sonarr sends no statistics the
// answer is yes, so a series is never skipped by mistake.
func (s seriesJSON) hasFiles() bool {
	return s.Statistics == nil || s.Statistics.EpisodeFileCount > 0
}

type episodeJSON struct {
	ID            int    `json:"id"`
	SeasonNumber  int    `json:"seasonNumber"`
	EpisodeNumber int    `json:"episodeNumber"`
	AirDateUTC    string `json:"airDateUtc"`
	HasFile       bool   `json:"hasFile"`
	EpisodeFileID int    `json:"episodeFileId"`
}

type fileJSON struct {
	ID   int   `json:"id"`
	Size int64 `json:"size"`
}

// Series returns every series with its tags and download links. Episodes are included
// for the series that have at least one file.
func (c *Client) Series(ctx context.Context) (domain.SeriesSnapshot, error) {
	var raw []seriesJSON
	if err := c.arr.Get(ctx, "/api/v3/series", nil, &raw); err != nil {
		return domain.SeriesSnapshot{}, fmt.Errorf("series: %w", err)
	}
	tags, err := c.arr.Tags(ctx)
	if err != nil {
		return domain.SeriesSnapshot{}, fmt.Errorf("tags: %w", err)
	}

	var snap domain.SeriesSnapshot
	current := map[int]int{} // episode ID -> current file ID
	for _, s := range raw {
		series := domain.Series{
			ID:    s.ID,
			Title: s.Title,
			Year:  s.Year,
			IDs:   domain.ExternalIDs{TVDB: arr.IDString(s.TVDBID), TMDB: arr.IDString(s.TMDBID), IMDB: s.IMDBID},
			Tags:  arr.TagLabels(tags, s.Tags),
			Ended: s.Status == "ended",
		}
		if s.hasFiles() {
			episodes, err := c.episodes(ctx, s.ID)
			if err != nil {
				return domain.SeriesSnapshot{}, fmt.Errorf("episodes of %q: %w", s.Title, err)
			}
			series.Episodes = episodes
			for _, e := range episodes {
				if e.File != nil {
					current[e.ID] = e.File.ID
				}
			}
		}
		snap.Series = append(snap.Series, series)
	}

	if snap.Links, err = c.arr.Links(ctx, arr.LinkOptions{Episodes: true, CurrentFiles: current}); err != nil {
		return domain.SeriesSnapshot{}, err
	}
	if snap.RootFolders, err = c.arr.RootFolders(ctx); err != nil {
		return domain.SeriesSnapshot{}, fmt.Errorf("root folders: %w", err)
	}
	return snap, nil
}

// episodes returns the episodes of one series, each with its file when it has one.
func (c *Client) episodes(ctx context.Context, seriesID int) ([]domain.Episode, error) {
	q := url.Values{"seriesId": {strconv.Itoa(seriesID)}}

	var files []fileJSON
	if err := c.arr.Get(ctx, "/api/v3/episodefile", q, &files); err != nil {
		return nil, err
	}
	sizes := make(map[int]int64, len(files))
	for _, f := range files {
		sizes[f.ID] = f.Size
	}

	var raw []episodeJSON
	if err := c.arr.Get(ctx, "/api/v3/episode", q, &raw); err != nil {
		return nil, err
	}
	out := make([]domain.Episode, 0, len(raw))
	for _, e := range raw {
		episode := domain.Episode{
			ID:      e.ID,
			Season:  e.SeasonNumber,
			Number:  e.EpisodeNumber,
			AirDate: httpjson.ParseTime(e.AirDateUTC),
		}
		if e.HasFile && e.EpisodeFileID != 0 {
			episode.File = &domain.MediaFile{ID: e.EpisodeFileID, Size: sizes[e.EpisodeFileID]}
		}
		out = append(out, episode)
	}
	return out, nil
}

// Test checks the connection and returns the app name and version.
func (c *Client) Test(ctx context.Context) (string, error) { return c.arr.Status(ctx) }
