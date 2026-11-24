package planner

import (
	"fmt"
	"sort"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
)

// changedReason is the reason for an item whose new read would remove a torrent the latest
// scan did not name for its rows (spec 6.10, two reads).
func changedReason(name string) string {
	return fmt.Sprintf("It changed since the scan: it would now remove the torrent %q, which the scan did not show. Scan again.", name)
}

// staysReason is the reason for an item whose new read would keep a torrent, still in the
// client, that the latest scan named for its rows (spec 6.10, two reads).
func staysReason(name string) string {
	return fmt.Sprintf("It changed since the scan: the torrent %q that the scan showed would now stay. Scan again.", name)
}

// removedWith is the reason for a torrent row whose torrent another item removes.
func removedWith(title string) string {
	return fmt.Sprintf("Its torrent is removed with %q anyway.", title)
}

// addGroup plans a movie or series row together with the rows it is tied to (spec 6.3).
func (m *maker) addGroup(id rowID, c domain.Candidate) {
	members := []domain.Candidate{c}
	for _, ref := range c.TiedWith {
		o, ok := m.fresh[rowID{ref.List, ref.Key}]
		if !ok || o.Waiting {
			// WholeTorrents ties listed rows only; a plan does not build on anything else.
			m.skip(id, ReasonNoLonger)
			return
		}
		members = append(members, o)
	}
	for _, o := range members {
		// Holds.Reason has checked this already; an ignored row is never deleted with others.
		if m.in.Ignores.Hides(o.List, o.Key) {
			m.skip(id, fmt.Sprintf("It is tied to %q, which is ignored.", o.Title))
			return
		}
	}
	sort.Slice(members, func(i, j int) bool {
		if a, b := listRank(members[i].List), listRank(members[j].List); a != b {
			return a < b
		}
		return members[i].Key < members[j].Key
	})
	if reason := m.changedSinceScan(members); reason != "" {
		m.skip(id, reason)
		return
	}

	selected := refOf(c)
	item := domain.PlanItem{Rows: []domain.PlanRow{}, Steps: m.steps(members), Torrents: m.torrents(members), Kept: m.kept(members)}
	for _, o := range members {
		r := domain.PlanRow{Row: o, Shares: shares(o, members)}
		if o.List != c.List || o.Key != c.Key {
			with := selected
			r.Added, r.AddedWith = true, &with
		}
		item.Rows = append(item.Rows, r)
	}
	m.push(item)
}

// changedSinceScan returns why the two reads disagree about the torrents of an item's rows,
// or "" when they agree (spec 6.10, two reads). History is read before the queue, so one read
// alone can take a download that was just imported for one that backs nothing. The reads
// disagree when the new read would remove a torrent that the latest scan did not name for
// any of the rows, and when the latest scan named a torrent for one of the rows that is
// still in the client and that the new read names for none of them: the user selected the
// rows expecting it to go. A torrent that is gone from the client needs no agreement.
func (m *maker) changedSinceScan(members []domain.Candidate) string {
	scanned := map[string]bool{}
	for _, o := range members {
		for _, t := range m.latest[rowID{o.List, o.Key}].Torrents {
			scanned[t.ID] = true
		}
	}
	named := map[string]bool{}
	for _, o := range members {
		for _, t := range o.Torrents {
			named[t.ID] = true
			if !scanned[t.ID] {
				return changedReason(t.Name)
			}
		}
	}
	for _, o := range members {
		for _, t := range m.latest[rowID{o.List, o.Key}].Torrents {
			if _, inClient := m.downloads[t.ID]; inClient && !named[t.ID] {
				return staysReason(t.Name)
			}
		}
	}
	return ""
}

// shares returns the torrents a row shares with the other rows of its item, sorted.
func shares(o domain.Candidate, members []domain.Candidate) []string {
	mine := map[string]bool{}
	for _, t := range o.Torrents {
		mine[t.ID] = true
	}
	set := map[string]bool{}
	for _, p := range members {
		if p.List == o.List && p.Key == o.Key {
			continue
		}
		for _, t := range p.Torrents {
			if mine[t.ID] {
				set[t.ID] = true
			}
		}
	}
	var out []string
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// steps returns the media manager steps of an item's rows (spec 6.7). A library file that two
// rows of the item delete is deleted once.
func (m *maker) steps(members []domain.Candidate) []domain.ManagerStep {
	steps := []domain.ManagerStep{}
	deleted := map[int]bool{} // episode files already in a step of this item
	for _, c := range members {
		row := m.rows[rowID{c.List, c.Key}]
		switch {
		case c.List == domain.ListMovies:
			steps = append(steps, domain.ManagerStep{
				Manager: scan.ManagerMovies, Action: domain.StepRemoveMovie, ItemID: row.ManagerID,
				AddExclusion: m.in.Rules.AddExclusionOnDelete,
			})
		case c.Scope == domain.ScopeSeries && !c.StillAiring:
			steps = append(steps, domain.ManagerStep{
				Manager: scan.ManagerSeries, Action: domain.StepRemoveSeries, ItemID: row.ManagerID,
				AddExclusion: m.in.Rules.AddExclusionOnDelete,
			})
		default:
			// A season row, or the row of a series that is still airing: the series stays.
			var files []int
			for _, f := range sortedInts(row.FileIDs) {
				if !deleted[f] {
					deleted[f] = true
					files = append(files, f)
				}
			}
			if len(files) > 0 {
				steps = append(steps, domain.ManagerStep{Manager: scan.ManagerSeries, Action: domain.StepDeleteEpisodeFiles, ItemID: row.ManagerID, FileIDs: files})
			}
			if episodes := sortedInts(row.ItemIDs); len(episodes) > 0 {
				steps = append(steps, domain.ManagerStep{Manager: scan.ManagerSeries, Action: domain.StepUnmonitorEpisodes, ItemID: row.ManagerID, EpisodeIDs: episodes})
			}
			if seasons := m.finishedSeasons(row); len(seasons) > 0 {
				steps = append(steps, domain.ManagerStep{Manager: scan.ManagerSeries, Action: domain.StepUnmonitorSeasons, ItemID: row.ManagerID, Seasons: seasons})
			}
		}
	}
	return steps
}

// finishedSeasons returns the regular seasons of a row's episodes that have finished airing:
// only seasons whose files the row deletes are unmonitored (spec 6.7).
func (m *maker) finishedSeasons(row scan.Row) []int {
	s := m.series[row.ManagerID]
	seasonOf := map[int]int{}
	for _, e := range s.Episodes {
		seasonOf[e.ID] = e.Season
	}
	set := map[int]bool{}
	for _, item := range row.ItemIDs {
		if n := seasonOf[item]; n > 0 {
			set[n] = true
		}
	}
	var out []int
	for n := range set {
		if scan.SeasonFinished(s, n, m.in.Now) {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

func sortedInts(xs []int) []int {
	out := append([]int(nil), xs...)
	sort.Ints(out)
	return out
}
