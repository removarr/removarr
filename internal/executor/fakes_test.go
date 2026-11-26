package executor

import (
	"context"
	"fmt"
	"testing"

	"github.com/removarr/removarr/internal/domain"
)

var ctx = context.Background()

// apps records every call in one log, so tests can check the order across apps.
type apps struct {
	t        *testing.T
	log      []string
	fail     map[string]error // call -> its error
	list     []domain.Download
	readErr  error
	gone     map[string]bool // removed so far
	noWrites bool            // a dry run: any write fails the test
}

func newApps(t *testing.T, list ...domain.Download) *apps {
	return &apps{t: t, fail: map[string]error{}, list: list, gone: map[string]bool{}}
}

func (a *apps) call(s string) error {
	if a.noWrites {
		a.t.Errorf("a dry run sent %q", s)
	}
	a.log = append(a.log, s)
	return a.fail[s]
}

func (a *apps) RemoveMovie(_ context.Context, id int, excl bool) error {
	return a.call(fmt.Sprintf("remove movie %d exclusion=%v", id, excl))
}
func (a *apps) RemoveSeries(_ context.Context, id int, excl bool) error {
	return a.call(fmt.Sprintf("remove series %d exclusion=%v", id, excl))
}
func (a *apps) DeleteEpisodeFiles(_ context.Context, ids []int) error {
	return a.call(fmt.Sprintf("delete files %v", ids))
}
func (a *apps) UnmonitorEpisodes(_ context.Context, ids []int) error {
	return a.call(fmt.Sprintf("unmonitor episodes %v", ids))
}
func (a *apps) UnmonitorSeasons(_ context.Context, id int, seasons []int) error {
	return a.call(fmt.Sprintf("unmonitor seasons %d %v", id, seasons))
}
func (a *apps) RemoveDownload(_ context.Context, id string, deleteData bool) error {
	s := fmt.Sprintf("remove torrent %s data=%v", id, deleteData)
	if err := a.call(s); err != nil {
		return err
	}
	a.gone[id] = true
	return nil
}
func (a *apps) Downloads(context.Context) ([]domain.Download, error) {
	a.log = append(a.log, "read downloads")
	if a.readErr != nil {
		return nil, a.readErr
	}
	var out []domain.Download
	for _, d := range a.list {
		if !a.gone[d.ID] {
			out = append(out, d)
		}
	}
	return out, nil
}
func (a *apps) Files(context.Context, string) ([]domain.DownloadFile, error) { return nil, nil }

func (a *apps) providers() Providers {
	return Providers{Movies: a, Series: a, Downloads: a, Remover: a}
}

func dl(id, path string) domain.Download { return domain.Download{ID: id, ContentPath: path} }

func torrent(id string) domain.PlanTorrent {
	return domain.PlanTorrent{Torrent: domain.TorrentInfo{ID: id}}
}

func movieItem(id int, key string, size int64, torrents ...domain.PlanTorrent) domain.PlanItem {
	return domain.PlanItem{
		Rows:     []domain.PlanRow{{Row: domain.Candidate{List: domain.ListMovies, Key: key, Title: key, Size: size}}},
		Steps:    []domain.ManagerStep{{Manager: "movies", Action: domain.StepRemoveMovie, ItemID: id, RowKey: key}},
		Torrents: torrents,
	}
}

func results(a domain.Action) []string {
	var out []string
	for _, s := range a.Steps {
		out = append(out, s.Result)
	}
	return out
}
