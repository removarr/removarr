package scan

import (
	"sort"

	"github.com/removarr/removarr/internal/domain"
)

// Row is a movie, series or season that qualifies, with what Delete would touch.
type Row struct {
	Candidate domain.Candidate // everything except Torrents, BelowSeedMin and TiedWith
	Manager   string           // ManagerMovies or ManagerSeries: whose IDs the next two fields hold
	ItemIDs   []int            // the movies or episodes Delete affects
	FileIDs   []int            // the library files Delete removes
}

// WholeTorrents applies the whole-torrent rule to the rows of the watched lists. It fills
// Candidates and Waiting of each list from its Rows.
//
// Rows whose files are backed by the same torrent form a group, directly or through a chain
// of torrents. A torrent blocks its group when it is queued or backs a library file that no
// row of the group deletes; a blocked group waits and none of its rows is listed. The rows
// of a group that does not wait are listed and tied to each other (spec 6.3). Only downloads
// that are in the download client count for any of this.
func WholeTorrents(r domain.Rules, x *LinkIndex, downloads []domain.Download, lists ...*ListResult) {
	inClient := downloadsByID(downloads)
	present := func(id string) bool {
		_, ok := inClient[id]
		return id != "" && ok
	}

	type entry struct {
		list *ListResult
		row  *Row
	}
	var rows []entry
	for _, l := range lists {
		l.Candidates, l.Waiting = nil, 0
		for i := range l.Rows {
			rows = append(rows, entry{l, &l.Rows[i]})
		}
	}

	// Group the rows through the downloads that back their files.
	parent := make([]int, len(rows))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	firstRow := map[string]int{} // download ID -> the first row it backs a file of
	for i, e := range rows {
		for _, file := range e.row.FileIDs {
			id := x.source[ref{e.row.Manager, file}]
			if !present(id) {
				continue
			}
			if j, ok := firstRow[id]; ok {
				parent[find(i)] = find(j)
			} else {
				firstRow[id] = i
			}
		}
	}
	groups := map[int][]int{}
	for i := range rows {
		root := find(i)
		groups[root] = append(groups[root], i)
	}

	for _, group := range groups {
		deleted := map[ref]bool{}
		for _, i := range group {
			for _, file := range rows[i].row.FileIDs {
				deleted[ref{rows[i].row.Manager, file}] = true
			}
		}
		// stays reports whether a download outlives the group's deletion: it is queued, or it
		// backs a library file that no row of the group deletes.
		stays := func(id string) bool {
			if x.queued[id] {
				return true
			}
			for _, f := range x.backs[id] {
				if !deleted[f] {
					return true
				}
			}
			return false
		}

		waits := false
		for _, i := range group {
			for _, file := range rows[i].row.FileIDs {
				if id := x.source[ref{rows[i].row.Manager, file}]; present(id) && stays(id) {
					waits = true
				}
			}
		}
		if waits {
			for _, i := range group {
				rows[i].list.Waiting++
			}
			continue
		}

		for _, i := range group {
			row := rows[i].row
			ids := map[string]bool{}
			for _, file := range row.FileIDs {
				ids[x.source[ref{row.Manager, file}]] = true
			}
			for _, item := range row.ItemIDs {
				for _, id := range x.grabbed[ref{row.Manager, item}] {
					ids[id] = true
				}
			}
			var named []string
			for id := range ids {
				if present(id) && !stays(id) {
					named = append(named, id)
				}
			}
			sort.Strings(named)

			c := row.Candidate
			c.Torrents, c.BelowSeedMin = torrentInfos(r, inClient, named)
			c.TiedWith = nil
			for _, j := range group {
				if j == i {
					continue
				}
				o := rows[j].row.Candidate
				c.TiedWith = append(c.TiedWith, domain.RowRef{List: o.List, Key: o.Key, Title: o.Title, Season: o.Season})
			}
			sort.Slice(c.TiedWith, func(a, b int) bool {
				if c.TiedWith[a].List != c.TiedWith[b].List {
					return c.TiedWith[a].List < c.TiedWith[b].List
				}
				return c.TiedWith[a].Key < c.TiedWith[b].Key
			})
			rows[i].list.Candidates = append(rows[i].list.Candidates, c)
		}
	}
	for _, l := range lists {
		sortByKey(l.Candidates)
	}
}

// sortRows orders rows by key so results are deterministic.
func sortRows(rows []Row) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].Candidate.Key < rows[j].Candidate.Key })
}
