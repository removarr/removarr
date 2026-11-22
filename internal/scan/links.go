package scan

import (
	"sort"

	"github.com/removarr/removarr/internal/domain"
)

// The media managers a ManagerFiles can come from. Library file IDs are only unique within
// one manager.
const (
	ManagerMovies = "movies" // the movie manager
	ManagerSeries = "series" // the series manager
)

// ManagerFiles is one media manager's links with its current library files.
type ManagerFiles struct {
	Manager string // ManagerMovies or ManagerSeries
	Links   domain.Links
	Files   []LibraryFile
}

// LinkIndex answers questions about downloads across all media managers (spec 6.6).
type LinkIndex struct {
	queued  map[string]bool
	known   map[string]bool
	backing map[string]int   // download ID -> number of current library files imported from it
	bytes   map[string]int64 // download ID -> total size of those files
}

// NewLinkIndex builds the index from every media manager that answered.
func NewLinkIndex(managers ...ManagerFiles) *LinkIndex {
	x := &LinkIndex{queued: map[string]bool{}, known: map[string]bool{}, backing: map[string]int{}, bytes: map[string]int64{}}
	for _, m := range managers {
		for id := range m.Links.Queue {
			if id != "" {
				x.queued[id] = true
				x.known[id] = true
			}
		}
		for id := range m.Links.Grabbed {
			if id != "" {
				x.known[id] = true
			}
		}
		seen := map[int]bool{}
		for _, file := range m.Files {
			if seen[file.ID] {
				continue
			}
			seen[file.ID] = true
			if id := m.Links.FileSource[file.ID]; id != "" {
				x.backing[id]++
				x.bytes[id] += file.Size
				x.known[id] = true
			}
		}
	}
	return x
}

// Queued reports whether the download is in a media manager's queue.
func (x *LinkIndex) Queued(id string) bool { return x.queued[id] }

// Known reports whether any media manager's history or queue mentions the download.
func (x *LinkIndex) Known(id string) bool { return x.known[id] }

// Backing returns how many current library files were imported from the download.
func (x *LinkIndex) Backing(id string) int { return x.backing[id] }

// BackedBytes returns the total size of the current library files the download backs, over
// every media manager.
func (x *LinkIndex) BackedBytes(id string) int64 { return x.bytes[id] }

// Removable returns the downloads a deletion would leave with nothing to back (spec 6.7):
// those associated in links with one of itemIDs, not queued, and backing no library file
// once deletedFileIDs are gone. links must belong to the manager the deletion happens in.
// The result is sorted, and nil when empty.
func (x *LinkIndex) Removable(links domain.Links, itemIDs, deletedFileIDs []int) []string {
	items := make(map[int]bool, len(itemIDs))
	for _, id := range itemIDs {
		items[id] = true
	}
	freed := map[string]int{}
	seen := map[int]bool{}
	for _, file := range deletedFileIDs {
		if seen[file] {
			continue
		}
		seen[file] = true
		if id := links.FileSource[file]; id != "" {
			freed[id]++
		}
	}
	var out []string
	for id, grabbedFor := range links.Grabbed {
		if id == "" || x.queued[id] {
			continue
		}
		associated := false
		for _, item := range grabbedFor {
			if items[item] {
				associated = true
				break
			}
		}
		if !associated || x.backing[id]-freed[id] > 0 {
			continue
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// SeedBelowMin reports whether a download is below the seeding minimum (spec 6.6).
// With both minimums set, it is below only when it is below both.
func SeedBelowMin(r domain.Rules, d domain.Download) bool {
	ratioSet, timeSet := r.MinRatio > 0, r.MinSeedDays > 0
	belowRatio := ratioSet && d.Ratio != nil && *d.Ratio < r.MinRatio
	belowTime := timeSet && d.SeedingTime != nil && d.SeedingTime.Hours() < r.MinSeedDays*24
	switch {
	case ratioSet && timeSet:
		return belowRatio && belowTime
	case ratioSet:
		return belowRatio
	case timeSet:
		return belowTime
	default:
		return false
	}
}
