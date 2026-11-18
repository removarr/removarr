package scan

import (
	"sort"

	"github.com/removarr/removarr/internal/domain"
)

// ListResult is one computed list plus the counts shown in the scan summary.
type ListResult struct {
	Candidates []domain.Candidate
	Protected  int // items held back by a protection rule
	Unmatched  int // items that could not be matched to the media server
}

// TorrentsInput is everything the orphan torrent list is computed from.
type TorrentsInput struct {
	Rules     domain.Rules
	Downloads []domain.Download
	Index     *LinkIndex
}

// Torrents returns the orphan and unlinked torrents (spec 6.6), sorted by key.
func Torrents(in TorrentsInput) ListResult {
	var res ListResult
	for _, d := range in.Downloads {
		if d.ID == "" || excluded(in.Rules, d) || in.Index.Queued(d.ID) || in.Index.Backing(d.ID) > 0 {
			continue
		}
		reason := domain.ReasonOrphan
		if in.Index.Known(d.ID) {
			reason = domain.ReasonUnlinked
		}
		info := torrentInfo(in.Rules, d)
		res.Candidates = append(res.Candidates, domain.Candidate{
			List:         domain.ListTorrents,
			Key:          domain.TorrentKey(d.ID),
			Title:        d.Name,
			Size:         d.Size,
			Reason:       reason,
			Torrents:     []domain.TorrentInfo{info},
			BelowSeedMin: info.BelowSeedMin,
		})
	}
	sortByKey(res.Candidates)
	return res
}

// excluded reports whether the rules leave a download out of orphan detection.
func excluded(r domain.Rules, d domain.Download) bool {
	for _, c := range r.ExcludedCategories {
		if c == d.Category {
			return true
		}
	}
	for _, want := range r.ExcludedTags {
		for _, have := range d.Tags {
			if want == have {
				return true
			}
		}
	}
	return false
}

// torrentInfo turns a download into the details a row shows.
func torrentInfo(r domain.Rules, d domain.Download) domain.TorrentInfo {
	info := domain.TorrentInfo{
		ID:           d.ID,
		Name:         d.Name,
		Size:         d.Size,
		State:        d.State,
		Category:     d.Category,
		Tracker:      d.Tracker,
		Ratio:        d.Ratio,
		AddedAt:      d.AddedAt,
		BelowSeedMin: SeedBelowMin(r, d),
	}
	if d.SeedingTime != nil {
		days := d.SeedingTime.Hours() / 24
		info.SeedingDays = &days
	}
	return info
}

// downloadsByID indexes downloads by ID.
func downloadsByID(ds []domain.Download) map[string]domain.Download {
	out := make(map[string]domain.Download, len(ds))
	for _, d := range ds {
		out[d.ID] = d
	}
	return out
}

// sortByKey orders candidates by key so results are deterministic.
func sortByKey(cs []domain.Candidate) {
	sort.Slice(cs, func(i, j int) bool { return cs[i].Key < cs[j].Key })
}
