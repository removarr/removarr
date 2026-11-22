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
// Rows whose files are backed by the same torrent, or that delete the same library file,
// form a group, directly or through a chain. A torrent blocks its group when it is queued
// or backs a library file that no row of the group deletes; a file blocks its group when it
// holds an item that no row of the group deletes. A blocked group waits and none of its rows
// is listed. The rows of a group that does not wait are listed and tied to each other (spec
// 6.3). Only downloads that are in the download client count for any of this.
//
// It assumes complete inputs. A download that is missing from downloads counts as gone from
// the client: it neither ties, blocks nor is named. An index built without one of the media
// managers, or from a ManagerFiles with an empty Manager, blocks nothing: files and items of
// that manager are unknown to it. So the caller must pass the complete download list and an
// index of every configured media manager.
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

	// Group the rows through the downloads that back their files and through the files
	// themselves.
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
	fileRow := map[ref]int{}     // library file -> the first row that deletes it
	for i, e := range rows {
		for _, file := range e.row.FileIDs {
			f := ref{e.row.Manager, file}
			if j, ok := fileRow[f]; ok {
				parent[find(i)] = find(j)
			} else {
				fileRow[f] = i
			}
			id := x.source[f]
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
		deleted := map[ref]bool{}      // the library files the group deletes
		deletedItems := map[ref]bool{} // the movies and episodes the group deletes
		for _, i := range group {
			for _, file := range rows[i].row.FileIDs {
				deleted[ref{rows[i].row.Manager, file}] = true
			}
			for _, item := range rows[i].row.ItemIDs {
				deletedItems[ref{rows[i].row.Manager, item}] = true
			}
		}
		// stays reports whether a download outlives the group's deletion: it is queued, or it
		// backs a library file that no row of the group deletes. The answer is the same
		// throughout a group, so it is worked out once per download: a pack is asked about
		// once for every file it backs.
		stayed := map[string]bool{}
		stays := func(id string) bool {
			if answer, known := stayed[id]; known {
				return answer
			}
			answer := x.queued[id]
			if !answer {
				for _, f := range x.backs[id] {
					if !deleted[f] {
						answer = true
						break
					}
				}
			}
			stayed[id] = answer
			return answer
		}

		waits := false
		for _, i := range group {
			manager := rows[i].row.Manager
			for _, file := range rows[i].row.FileIDs {
				if id := x.source[ref{manager, file}]; present(id) && stays(id) {
					waits = true
				}
				// A file goes as a whole: it may not take an item with it that stays.
				for _, item := range x.holds[ref{manager, file}] {
					if !deletedItems[ref{manager, item}] {
						waits = true
					}
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
			// Data outside the library is information only (spec 6.7).
			for k := range c.Torrents {
				if out := c.Torrents[k].Size - x.BackedBytes(c.Torrents[k].ID); out >= MinContentSize {
					c.Torrents[k].OutsideLibrary = out
				}
			}
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
