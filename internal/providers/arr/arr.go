// Package arr holds what the Sonarr and Radarr clients share. Both apps expose the same
// v3 API shape for tags, root folders, status, history and the queue.
package arr

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/httpjson"
)

const (
	defaultPageSize = 1000
	eventImported   = "downloadFolderImported"
)

// Client talks to one Sonarr or Radarr instance.
type Client struct {
	BaseURL  string // no trailing slash; may include a path prefix such as /sonarr
	APIKey   string
	HTTP     *http.Client
	PageSize int // records per page for history and queue; 0 means 1000
}

// Get requests path and decodes the JSON response into out. It does not change q.
func (c *Client) Get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return httpjson.Get(ctx, c.HTTP, u, http.Header{"X-Api-Key": {c.APIKey}}, out)
}

// Tags returns the tag labels by tag ID.
func (c *Client) Tags(ctx context.Context) (map[int]string, error) {
	var raw []struct {
		ID    int    `json:"id"`
		Label string `json:"label"`
	}
	if err := c.Get(ctx, "/api/v3/tag", nil, &raw); err != nil {
		return nil, err
	}
	out := make(map[int]string, len(raw))
	for _, t := range raw {
		out[t.ID] = t.Label
	}
	return out, nil
}

// TagLabels turns tag IDs into labels, skipping IDs that are not known.
func TagLabels(byID map[int]string, ids []int) []string {
	var out []string
	for _, id := range ids {
		if label, ok := byID[id]; ok {
			out = append(out, label)
		}
	}
	return out
}

// IDString formats a numeric external ID. Zero means unknown and gives an empty string.
func IDString(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// RootFolders returns the library folders with their free space.
func (c *Client) RootFolders(ctx context.Context) ([]domain.RootFolder, error) {
	var raw []struct {
		Path      string `json:"path"`
		FreeSpace int64  `json:"freeSpace"`
	}
	if err := c.Get(ctx, "/api/v3/rootfolder", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]domain.RootFolder, 0, len(raw))
	for _, f := range raw {
		out = append(out, domain.RootFolder{Path: f.Path, FreeBytes: f.FreeSpace})
	}
	return out, nil
}

// Status returns the app's name and version, for example "Sonarr 4.0.9.2244".
func (c *Client) Status(ctx context.Context) (string, error) {
	var raw struct {
		AppName string `json:"appName"`
		Version string `json:"version"`
	}
	if err := c.Get(ctx, "/api/v3/system/status", nil, &raw); err != nil {
		return "", err
	}
	return strings.TrimSpace(raw.AppName + " " + raw.Version), nil
}

// page is one page of a paged resource.
type page[T any] struct {
	TotalRecords int `json:"totalRecords"`
	Records      []T `json:"records"`
}

// eachPage reads every page of a paged resource and calls fn for each record, in order.
// It sets the page and pageSize parameters on q.
func eachPage[T any](ctx context.Context, c *Client, path string, q url.Values, fn func(T)) error {
	size := c.PageSize
	if size <= 0 {
		size = defaultPageSize
	}
	q.Set("pageSize", strconv.Itoa(size))
	for pageNo := 1; ; pageNo++ {
		q.Set("page", strconv.Itoa(pageNo))
		var p page[T]
		if err := c.Get(ctx, path, q, &p); err != nil {
			return err
		}
		for _, r := range p.Records {
			fn(r)
		}
		if len(p.Records) == 0 || pageNo*size >= p.TotalRecords {
			return nil
		}
	}
}

type historyRecord struct {
	EventType  string         `json:"eventType"`
	DownloadID string         `json:"downloadId"`
	MovieID    int            `json:"movieId"`
	EpisodeID  int            `json:"episodeId"`
	Data       map[string]any `json:"data"`
}

type queueRecord struct {
	DownloadID string `json:"downloadId"`
}

// LinkOptions adapts Links to Sonarr or Radarr.
type LinkOptions struct {
	// Episodes is true for Sonarr, whose history items are episodes, and false for Radarr,
	// whose history items are movies.
	Episodes bool
	// CurrentFiles maps an item ID to the ID of its current library file. It is used only
	// when import events do not report the file ID.
	CurrentFiles map[int]int
}

// Links reads the whole history and the queue and works out which download each library
// file came from. Download IDs are lower-cased so they compare equal to the download
// client's IDs.
func (c *Client) Links(ctx context.Context, opt LinkOptions) (domain.Links, error) {
	links := domain.Links{Grabbed: map[string][]int{}, FileSource: map[int]string{}, Queue: map[string]bool{}}

	type importInfo struct {
		download  string
		hasFileID bool
	}
	newestImport := map[int]importInfo{} // item ID -> its most recent import event
	grabbed := map[string]map[int]bool{}

	history := url.Values{"sortKey": {"date"}, "sortDirection": {"descending"}}
	err := eachPage(ctx, c, "/api/v3/history", history, func(r historyRecord) {
		item := r.MovieID
		if opt.Episodes {
			item = r.EpisodeID
		}
		if item == 0 {
			return
		}
		download := normalizeID(r.DownloadID)
		if download != "" {
			if grabbed[download] == nil {
				grabbed[download] = map[int]bool{}
			}
			if !grabbed[download][item] {
				grabbed[download][item] = true
				links.Grabbed[download] = append(links.Grabbed[download], item)
			}
		}
		if r.EventType != eventImported {
			return
		}
		fileID, hasFileID := intFrom(r.Data, "fileId")
		if _, seen := newestImport[item]; !seen {
			newestImport[item] = importInfo{download: download, hasFileID: hasFileID}
		}
		if hasFileID && download != "" {
			if _, set := links.FileSource[fileID]; !set {
				links.FileSource[fileID] = download
			}
		}
	})
	if err != nil {
		return domain.Links{}, fmt.Errorf("history: %w", err)
	}

	// Some versions do not report the file ID on import events. For those, the most recent
	// import of an item is taken as the source of the item's current file. A manual import
	// has no download ID, so it links nothing.
	for item, file := range opt.CurrentFiles {
		if _, ok := links.FileSource[file]; ok {
			continue
		}
		if imp, ok := newestImport[item]; ok && !imp.hasFileID && imp.download != "" {
			links.FileSource[file] = imp.download
		}
	}

	unknown := "includeUnknownMovieItems"
	if opt.Episodes {
		unknown = "includeUnknownSeriesItems"
	}
	err = eachPage(ctx, c, "/api/v3/queue", url.Values{unknown: {"true"}}, func(r queueRecord) {
		if download := normalizeID(r.DownloadID); download != "" {
			links.Queue[download] = true
		}
	})
	if err != nil {
		return domain.Links{}, fmt.Errorf("queue: %w", err)
	}
	return links, nil
}

func normalizeID(id string) string { return strings.ToLower(strings.TrimSpace(id)) }

// intFrom reads an integer stored under key, ignoring the key's letter case. The apps send
// the value as a string; a JSON number is accepted too.
func intFrom(data map[string]any, key string) (int, bool) {
	for k, v := range data {
		if !strings.EqualFold(k, key) {
			continue
		}
		switch x := v.(type) {
		case string:
			n, err := strconv.Atoi(strings.TrimSpace(x))
			return n, err == nil
		case float64:
			return int(x), true
		}
	}
	return 0, false
}
