package planner

import (
	"sort"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
)

// torrents returns the torrents an item removes: those its rows name, each once, without the
// ones an earlier item of the plan removes already. A torrent that history does not associate
// with any of the rows is marked as linked by file size (spec 6.7).
func (m *maker) torrents(members []domain.Candidate) []domain.PlanTorrent {
	out := []domain.PlanTorrent{}
	seen := map[string]bool{}
	for _, o := range members {
		for _, t := range o.Torrents {
			if seen[t.ID] {
				continue
			}
			seen[t.ID] = true
			if _, removed := m.owner[t.ID]; removed {
				continue
			}
			out = append(out, domain.PlanTorrent{Torrent: t, LinkedBySize: !m.byHistory(members, t.ID)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Torrent.ID < out[j].Torrent.ID })
	return out
}

// byHistory reports whether history alone associates the download with one of the rows.
func (m *maker) byHistory(members []domain.Candidate, id string) bool {
	for _, o := range members {
		row := m.rows[rowID{o.List, o.Key}]
		for _, h := range m.in.Eval.History.Associated(row.Manager, row.ItemIDs, row.FileIDs) {
			if h == id {
				return true
			}
		}
	}
	return false
}

// kept returns the downloads in the client that are associated with an item's rows and that
// the rows do not name: they are in a queue, or they back a library file that stays (spec
// 6.10).
func (m *maker) kept(members []domain.Candidate) []domain.KeptTorrent {
	named := map[string]bool{}
	for _, o := range members {
		for _, t := range o.Torrents {
			named[t.ID] = true
		}
	}
	out := []domain.KeptTorrent{}
	seen := map[string]bool{}
	for _, o := range members {
		row := m.rows[rowID{o.List, o.Key}]
		for _, id := range m.in.Eval.Index.Associated(row.Manager, row.ItemIDs, row.FileIDs) {
			d, ok := m.downloads[id]
			// finish also drops every torrent another item of the plan removes.
			if !ok || named[id] || seen[id] {
				continue
			}
			seen[id] = true
			reason := domain.KeptBacksLibrary
			if m.in.Eval.Index.Queued(id) {
				reason = domain.KeptQueued
			}
			out = append(out, domain.KeptTorrent{Torrent: scan.TorrentDetails(m.in.Rules, d), Reason: reason})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Torrent.ID < out[j].Torrent.ID })
	return out
}

// finish completes what needs the whole plan. It decides which torrents keep their data:
// those whose content path another torrent that stays uses too, and those whose path is
// unknown (spec 6.7, shared data). And it drops from each item's kept torrents those that
// another item of the plan removes: they do not stay (spec 6.10).
func (m *maker) finish() {
	removed := map[string]bool{}
	for _, it := range m.items {
		for _, t := range it.Torrents {
			removed[t.Torrent.ID] = true
		}
	}
	for i := range m.items {
		for j := range m.items[i].Torrents {
			t := &m.items[i].Torrents[j]
			t.KeepData = m.sharesData(t.Torrent.ID, removed)
		}
		stay := []domain.KeptTorrent{}
		for _, k := range m.items[i].Kept {
			if !removed[k.Torrent.ID] {
				stay = append(stay, k)
			}
		}
		m.items[i].Kept = stay
	}
}

// sharesData reports whether a removed download's data must stay. Unsure means keep: a
// removed download whose path is unknown keeps its data, and so does every removed download
// while a download that stays has an unknown path, since that one may use any data.
func (m *maker) sharesData(id string, removed map[string]bool) bool {
	var others []string
	for _, d := range m.in.Eval.Fetched.Downloads.Value {
		if !removed[d.ID] {
			others = append(others, d.ContentPath)
		}
	}
	return scan.KeepsData(m.downloads[id].ContentPath, others)
}
