// Package radarr reads movies from Radarr and removes them.
package radarr

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/providers"
	"github.com/removarr/removarr/internal/providers/arr"
)

var (
	_ providers.MovieManager = (*Client)(nil)
	_ providers.MovieWriter  = (*Client)(nil)
	_ providers.Tester       = (*Client)(nil)
)

// Client talks to one Radarr instance.
type Client struct{ arr *arr.Client }

// RemoveMovie removes the movie with its file (spec 6.7).
func (c *Client) RemoveMovie(ctx context.Context, id int, addExclusion bool) error {
	q := url.Values{"deleteFiles": {"true"}, "addImportExclusion": {strconv.FormatBool(addExclusion)}}
	if err := c.arr.Send(ctx, http.MethodDelete, "/api/v3/movie/"+strconv.Itoa(id), q, nil, nil); err != nil {
		return fmt.Errorf("remove movie: %w", err)
	}
	return nil
}

// New returns a client. baseURL has no trailing slash.
func New(baseURL, apiKey string, hc *http.Client) *Client {
	return &Client{arr: &arr.Client{BaseURL: baseURL, APIKey: apiKey, HTTP: hc}}
}

type movieJSON struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Year    int    `json:"year"`
	TMDBID  int    `json:"tmdbId"`
	IMDBID  string `json:"imdbId"`
	Tags    []int  `json:"tags"`
	HasFile bool   `json:"hasFile"`
	File    *struct {
		ID   int   `json:"id"`
		Size int64 `json:"size"`
	} `json:"movieFile"`
}

// Movies returns every movie with its file, tags and download links.
func (c *Client) Movies(ctx context.Context) (domain.MovieSnapshot, error) {
	var raw []movieJSON
	if err := c.arr.Get(ctx, "/api/v3/movie", nil, &raw); err != nil {
		return domain.MovieSnapshot{}, fmt.Errorf("movies: %w", err)
	}
	tags, err := c.arr.Tags(ctx)
	if err != nil {
		return domain.MovieSnapshot{}, fmt.Errorf("tags: %w", err)
	}

	var snap domain.MovieSnapshot
	current := map[int]int{} // movie ID -> current file ID
	for _, m := range raw {
		movie := domain.Movie{
			ID:    m.ID,
			Title: m.Title,
			Year:  m.Year,
			IDs:   domain.ExternalIDs{TMDB: arr.IDString(m.TMDBID), IMDB: m.IMDBID},
			Tags:  arr.TagLabels(tags, m.Tags),
		}
		if m.HasFile && m.File != nil {
			movie.File = &domain.MediaFile{ID: m.File.ID, Size: m.File.Size}
			current[m.ID] = m.File.ID
		}
		snap.Movies = append(snap.Movies, movie)
	}

	if snap.Links, err = c.arr.Links(ctx, arr.LinkOptions{CurrentFiles: current}); err != nil {
		return domain.MovieSnapshot{}, err
	}
	if snap.RootFolders, err = c.arr.RootFolders(ctx); err != nil {
		return domain.MovieSnapshot{}, fmt.Errorf("root folders: %w", err)
	}
	return snap, nil
}

// Test checks the connection and returns the app name and version.
func (c *Client) Test(ctx context.Context) (string, error) { return c.arr.Status(ctx) }
