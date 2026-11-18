// Package jellyfin reads users and watch state from Jellyfin.
package jellyfin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/httpjson"
	"github.com/removarr/removarr/internal/providers"
)

var (
	_ providers.MediaServer = (*Client)(nil)
	_ providers.Tester      = (*Client)(nil)
)

const defaultPageSize = 500

// Client talks to one Jellyfin server.
type Client struct {
	baseURL  string
	apiKey   string
	http     *http.Client
	PageSize int // items per request; 0 means 500
}

// New returns a client. baseURL has no trailing slash.
func New(baseURL, apiKey string, hc *http.Client) *Client {
	return &Client{baseURL: baseURL, apiKey: apiKey, http: hc}
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	header := http.Header{"Authorization": {`MediaBrowser Token="` + c.apiKey + `"`}}
	return httpjson.Get(ctx, c.http, u, header, out)
}

// Users returns the server's user accounts.
func (c *Client) Users(ctx context.Context) ([]domain.User, error) {
	var raw []struct {
		ID   string `json:"Id"`
		Name string `json:"Name"`
	}
	if err := c.get(ctx, "/Users", nil, &raw); err != nil {
		return nil, fmt.Errorf("users: %w", err)
	}
	out := make([]domain.User, 0, len(raw))
	for _, u := range raw {
		out = append(out, domain.User{ID: u.ID, Name: u.Name})
	}
	return out, nil
}

// Test checks the connection and returns the server's name and version.
func (c *Client) Test(ctx context.Context) (string, error) {
	var raw struct {
		ProductName string `json:"ProductName"`
		Version     string `json:"Version"`
	}
	if err := c.get(ctx, "/System/Info", nil, &raw); err != nil {
		return "", err
	}
	if raw.ProductName == "" {
		raw.ProductName = "Jellyfin"
	}
	return strings.TrimSpace(raw.ProductName + " " + raw.Version), nil
}

type itemJSON struct {
	ID                string            `json:"Id"`
	Name              string            `json:"Name"`
	SeriesID          string            `json:"SeriesId"`
	ParentIndexNumber *int              `json:"ParentIndexNumber"`
	IndexNumber       *int              `json:"IndexNumber"`
	IndexNumberEnd    *int              `json:"IndexNumberEnd"`
	ProviderIDs       map[string]string `json:"ProviderIds"`
	UserData          struct {
		Played         bool   `json:"Played"`
		LastPlayedDate string `json:"LastPlayedDate"`
		IsFavorite     bool   `json:"IsFavorite"`
	} `json:"UserData"`
}

func (it itemJSON) state() domain.UserState {
	return domain.UserState{
		Played:     it.UserData.Played,
		LastPlayed: httpjson.ParseTime(it.UserData.LastPlayedDate),
		Favorite:   it.UserData.IsFavorite,
	}
}

func (it itemJSON) externalIDs() domain.ExternalIDs {
	var ids domain.ExternalIDs
	for key, value := range it.ProviderIDs {
		switch strings.ToLower(key) {
		case "tmdb":
			ids.TMDB = value
		case "tvdb":
			ids.TVDB = value
		case "imdb":
			ids.IMDB = value
		}
	}
	return ids
}

// items returns every item of one type as one user sees it, reading page by page. Each
// request starts after the items received so far, so a server that sends fewer items than
// asked for skips none.
func (c *Client) items(ctx context.Context, userID, itemType string) ([]itemJSON, error) {
	size := c.PageSize
	if size <= 0 {
		size = defaultPageSize
	}
	var all []itemJSON
	for {
		q := url.Values{
			"userId":           {userID},
			"IncludeItemTypes": {itemType},
			"Recursive":        {"true"},
			"Fields":           {"ProviderIds"},
			"EnableUserData":   {"true"},
			"StartIndex":       {strconv.Itoa(len(all))},
			"Limit":            {strconv.Itoa(size)},
		}
		var page struct {
			Items []itemJSON `json:"Items"`
			Total int        `json:"TotalRecordCount"`
		}
		if err := c.get(ctx, "/Items", q, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		switch {
		case len(page.Items) == 0:
			return all, nil
		case page.Total > 0 && len(all) >= page.Total:
			return all, nil
		case page.Total == 0 && len(page.Items) < size:
			// The server sent no total: a short page is the last one.
			return all, nil
		}
	}
}

// Library returns the watch state of every movie and episode for the given users.
func (c *Client) Library(ctx context.Context, userIDs []string) (domain.Library, error) {
	movies := map[string]*domain.ServerMovie{}
	series := map[string]*domain.ServerSeries{}
	episodes := map[string]*domain.ServerEpisode{}
	var movieOrder, seriesOrder, episodeOrder []string // first-seen order, for stable output

	for _, user := range userIDs {
		items, err := c.items(ctx, user, "Movie")
		if err != nil {
			return domain.Library{}, fmt.Errorf("movies: %w", err)
		}
		for _, it := range items {
			m := movies[it.ID]
			if m == nil {
				m = &domain.ServerMovie{ID: it.ID, Title: it.Name, IDs: it.externalIDs(), States: map[string]domain.UserState{}}
				movies[it.ID] = m
				movieOrder = append(movieOrder, it.ID)
			}
			m.States[user] = it.state()
		}

		if items, err = c.items(ctx, user, "Series"); err != nil {
			return domain.Library{}, fmt.Errorf("series: %w", err)
		}
		for _, it := range items {
			s := series[it.ID]
			if s == nil {
				s = &domain.ServerSeries{ID: it.ID, Title: it.Name, IDs: it.externalIDs(), FavoriteOf: map[string]bool{}}
				series[it.ID] = s
				seriesOrder = append(seriesOrder, it.ID)
			}
			if it.UserData.IsFavorite {
				s.FavoriteOf[user] = true
			}
		}

		// A favourite season counts as a favourite on its series.
		if items, err = c.items(ctx, user, "Season"); err != nil {
			return domain.Library{}, fmt.Errorf("seasons: %w", err)
		}
		for _, it := range items {
			if s := series[it.SeriesID]; s != nil && it.UserData.IsFavorite {
				s.FavoriteOf[user] = true
			}
		}

		if items, err = c.items(ctx, user, "Episode"); err != nil {
			return domain.Library{}, fmt.Errorf("episodes: %w", err)
		}
		for _, it := range items {
			if it.SeriesID == "" || it.ParentIndexNumber == nil || it.IndexNumber == nil {
				// Cannot be matched to a managed episode, but a favourite on it still
				// protects its series.
				if s := series[it.SeriesID]; s != nil && it.UserData.IsFavorite {
					s.FavoriteOf[user] = true
				}
				continue
			}
			e := episodes[it.ID]
			if e == nil {
				e = &domain.ServerEpisode{
					SeriesID: it.SeriesID,
					Season:   *it.ParentIndexNumber,
					Episode:  *it.IndexNumber,
					States:   map[string]domain.UserState{},
				}
				if it.IndexNumberEnd != nil {
					e.EpisodeEnd = *it.IndexNumberEnd
				}
				episodes[it.ID] = e
				episodeOrder = append(episodeOrder, it.ID)
			}
			e.States[user] = it.state()
		}
	}

	var lib domain.Library
	for _, id := range movieOrder {
		lib.Movies = append(lib.Movies, *movies[id])
	}
	for _, id := range seriesOrder {
		lib.Series = append(lib.Series, *series[id])
	}
	for _, id := range episodeOrder {
		lib.Episodes = append(lib.Episodes, *episodes[id])
	}
	return lib, nil
}
