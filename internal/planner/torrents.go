package planner

import (
	"sort"

	"github.com/removarr/removarr/internal/domain"
)

// torrents returns the torrents an item removes: those its rows name, each once, without the
// ones an earlier item of the plan removes already.
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
			out = append(out, domain.PlanTorrent{Torrent: t})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Torrent.ID < out[j].Torrent.ID })
	return out
}

// kept returns the torrents associated with an item's rows that stay. Task 5 fills it.
func (m *maker) kept(members []domain.Candidate) []domain.KeptTorrent {
	return []domain.KeptTorrent{}
}

// finish completes what needs the whole plan. Task 5 fills it.
func (m *maker) finish() {}
