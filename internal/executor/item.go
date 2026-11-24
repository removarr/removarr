// Package executor carries out a confirmed plan: the only code in removarr that changes
// anything in an app (spec 6.8, 6.11).
package executor

import (
	"context"
	"fmt"
	"sort"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/providers"
	"github.com/removarr/removarr/internal/scan"
)

// Providers are the apps an execution reaches. A nil field means the app is not configured.
type Providers struct {
	Movies    providers.MovieWriter
	Series    providers.SeriesWriter
	Downloads providers.DownloadClient // read right before an item's torrents
	Remover   providers.DownloadRemover
}

// run is one execution in progress.
type run struct {
	p       Providers
	dryRun  bool
	removed map[string]bool // torrents this execution removed, or would have in a dry run
}

func newRun(p Providers, dryRun bool) *run {
	return &run{p: p, dryRun: dryRun, removed: map[string]bool{}}
}

// item runs one plan item (spec 6.8, rules 3 and 4; 6.11): its media manager steps in plan
// order, then its torrents, stopping at the first failure. It returns the item's record
// without ID, plan and time, and the stored rows it deleted: none in a dry run.
func (r *run) item(ctx context.Context, it domain.PlanItem) (domain.Action, []domain.RowRef) {
	act := domain.Action{DryRun: r.dryRun, Rows: []domain.ActionRow{}, Steps: []domain.StepResult{}}
	for _, row := range it.Rows {
		c := row.Row
		act.Rows = append(act.Rows, domain.ActionRow{List: c.List, Key: c.Key, Title: c.Title, Season: c.Season, Size: c.Size, Added: row.Added})
		act.Size += c.Size
	}
	failed := false
	for i := range it.Steps {
		s := it.Steps[i]
		res := domain.StepResult{Manager: &s}
		switch {
		case failed:
			res.Result = domain.ResultSkipped
		default:
			if err := r.step(ctx, s); err != nil {
				res.Result, res.Error, failed = domain.ResultFailed, err.Error(), true
			} else if r.dryRun {
				res.Result = domain.ResultDryRun
			} else {
				res.Result = domain.ResultDone
			}
		}
		act.Steps = append(act.Steps, res)
	}
	if len(it.Torrents) > 0 {
		failed = r.torrents(ctx, it.Torrents, failed, &act)
	}
	switch {
	case failed:
		act.Outcome = domain.OutcomeFailed
	case r.dryRun:
		act.Outcome = domain.OutcomeDryRun
	default:
		act.Outcome = domain.OutcomeDone
	}
	if r.dryRun {
		return act, nil
	}
	return act, drops(it, act)
}

// step checks that the step's app is configured and the step is known, and sends it unless
// this is a dry run.
func (r *run) step(ctx context.Context, s domain.ManagerStep) error {
	var send func() error
	switch s.Manager {
	case scan.ManagerMovies:
		if r.p.Movies == nil {
			return fmt.Errorf("the movie manager: %w", providers.ErrNotConfigured)
		}
		switch s.Action {
		case domain.StepRemoveMovie:
			send = func() error { return r.p.Movies.RemoveMovie(ctx, s.ItemID, s.AddExclusion) }
		default:
			return fmt.Errorf("unknown step %q for the movie manager", s.Action)
		}
	case scan.ManagerSeries:
		if r.p.Series == nil {
			return fmt.Errorf("the series manager: %w", providers.ErrNotConfigured)
		}
		switch s.Action {
		case domain.StepRemoveSeries:
			send = func() error { return r.p.Series.RemoveSeries(ctx, s.ItemID, s.AddExclusion) }
		case domain.StepDeleteEpisodeFiles:
			send = func() error { return r.p.Series.DeleteEpisodeFiles(ctx, s.FileIDs) }
		case domain.StepUnmonitorEpisodes:
			send = func() error { return r.p.Series.UnmonitorEpisodes(ctx, s.EpisodeIDs) }
		case domain.StepUnmonitorSeasons:
			send = func() error { return r.p.Series.UnmonitorSeasons(ctx, s.ItemID, s.Seasons) }
		default:
			return fmt.Errorf("unknown step %q for the series manager", s.Action)
		}
	default:
		return fmt.Errorf("unknown media manager %q", s.Manager)
	}
	if r.dryRun {
		return nil
	}
	return send()
}

// torrents removes an item's torrents after its media manager steps (spec 6.11) and reports
// whether the item failed. failed says whether a manager step failed already: then every
// torrent is skipped and the download list is not read.
func (r *run) torrents(ctx context.Context, planned []domain.PlanTorrent, failed bool, act *domain.Action) bool {
	var live map[string]domain.Download
	if !failed {
		var err error
		if live, err = r.read(ctx); err != nil {
			for i := range planned {
				t := planned[i]
				act.Steps = append(act.Steps, domain.StepResult{Torrent: &t.Torrent, KeepData: t.KeepData, Result: domain.ResultFailed, Error: err.Error()})
			}
			return true
		}
	}
	// Longer content paths first, so a torrent whose directory holds another's goes after it
	// and takes the directory as a whole.
	order := append([]domain.PlanTorrent(nil), planned...)
	pathLen := func(t domain.PlanTorrent) int { return len(scan.NormalPath(live[t.Torrent.ID].ContentPath)) }
	sort.SliceStable(order, func(i, j int) bool {
		if a, b := pathLen(order[i]), pathLen(order[j]); a != b {
			return a > b
		}
		return order[i].Torrent.ID < order[j].Torrent.ID
	})
	for i := range order {
		t := order[i]
		res := domain.StepResult{Torrent: &t.Torrent, KeepData: t.KeepData}
		d, there := live[t.Torrent.ID]
		switch {
		case failed:
			res.Result = domain.ResultSkipped
		case !there:
			res.Result = domain.ResultGone
		default:
			keep := t.KeepData || scan.KeepsData(d.ContentPath, otherPaths(live, d.ID))
			// The flag sent, or that would be: the history says what the client was asked,
			// also when the call failed after the client may have acted on it.
			res.KeepData = keep
			if r.dryRun {
				res.Result = domain.ResultDryRun
			} else if err := r.p.Remover.RemoveDownload(ctx, d.ID, !keep); err != nil {
				res.Result, res.Error, failed = domain.ResultFailed, err.Error(), true
			} else {
				res.Result = domain.ResultDone
			}
			if !failed {
				// Gone from the client, or would be: later torrents no longer share its data.
				r.removed[d.ID] = true
				delete(live, d.ID)
			}
		}
		act.Steps = append(act.Steps, res)
	}
	return failed
}

// otherPaths returns the content paths of every download in live except id.
func otherPaths(live map[string]domain.Download, id string) []string {
	out := make([]string, 0, len(live))
	for other, d := range live {
		if other != id {
			out = append(out, d.ContentPath)
		}
	}
	return out
}

// read returns the downloads in the client now, without those this execution removed or
// would have in a dry run.
func (r *run) read(ctx context.Context) (map[string]domain.Download, error) {
	if r.p.Downloads == nil || r.p.Remover == nil {
		return nil, fmt.Errorf("the download client: %w", providers.ErrNotConfigured)
	}
	list, err := r.p.Downloads.Downloads(ctx)
	if err != nil {
		return nil, err
	}
	live := make(map[string]domain.Download, len(list))
	for _, d := range list {
		if !r.removed[d.ID] {
			live[d.ID] = d
		}
	}
	return live, nil
}

// drops returns the stored rows an item deleted, each once (spec 6.11, stored lists): its
// rows in plan order, then the torrent-list row of each torrent it removed or found gone.
// A media row goes when every step of its own was done, or, with no step of its own, when
// every manager step of the item was done.
func drops(it domain.PlanItem, act domain.Action) []domain.RowRef {
	allDone := true
	ownDone := map[domain.RowRef]bool{} // a row with steps of its own -> all of them done
	removed := map[string]bool{}        // torrent-list keys of the torrents done or gone
	for _, s := range act.Steps {
		switch {
		case s.Manager != nil:
			ref := domain.RowRef{List: domain.ListKind(s.Manager.Manager), Key: s.Manager.RowKey}
			done, seen := ownDone[ref]
			ownDone[ref] = (done || !seen) && s.Result == domain.ResultDone
			if s.Result != domain.ResultDone {
				allDone = false
			}
		case s.Torrent != nil && (s.Result == domain.ResultDone || s.Result == domain.ResultGone):
			removed[domain.TorrentKey(s.Torrent.ID)] = true
		}
	}
	out := []domain.RowRef{}
	seen := map[domain.RowRef]bool{}
	add := func(ref domain.RowRef) {
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	for _, row := range it.Rows {
		ref := domain.RowRef{List: row.Row.List, Key: row.Row.Key}
		if ref.List == domain.ListTorrents {
			if removed[ref.Key] {
				add(ref)
			}
			continue
		}
		if done, own := ownDone[ref]; done || !own && allDone {
			add(ref)
		}
	}
	for _, t := range it.Torrents {
		if key := domain.TorrentKey(t.Torrent.ID); removed[key] {
			add(domain.RowRef{List: domain.ListTorrents, Key: key})
		}
	}
	return out
}
