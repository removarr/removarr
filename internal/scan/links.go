package scan

import "github.com/removarr/removarr/internal/domain"

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

// ref names a library file or a media item. Its ID means nothing without its manager:
// both media managers number from 1.
type ref struct {
	manager string
	id      int
}

// LinkIndex answers questions about downloads across all media managers (spec 6.6).
type LinkIndex struct {
	queued  map[string]bool
	known   map[string]bool
	backing map[string]int   // download ID -> number of current library files imported from it
	bytes   map[string]int64 // download ID -> total size of those files
	source  map[ref]string   // current library file -> the download it was imported from
	backs   map[string][]ref // download ID -> the current library files imported from it
	grabbed map[ref][]string // media item -> the downloads associated with it
}

// NewLinkIndex builds the index from every media manager that answered.
func NewLinkIndex(managers ...ManagerFiles) *LinkIndex {
	x := &LinkIndex{
		queued:  map[string]bool{},
		known:   map[string]bool{},
		backing: map[string]int{},
		bytes:   map[string]int64{},
		source:  map[ref]string{},
		backs:   map[string][]ref{},
		grabbed: map[ref][]string{},
	}
	for _, m := range managers {
		for id := range m.Links.Queue {
			if id != "" {
				x.queued[id] = true
				x.known[id] = true
			}
		}
		for id, items := range m.Links.Grabbed {
			if id == "" {
				continue
			}
			x.known[id] = true
			for _, item := range items {
				x.grabbed[ref{m.Manager, item}] = append(x.grabbed[ref{m.Manager, item}], id)
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
				f := ref{m.Manager, file.ID}
				x.source[f] = id
				x.backs[id] = append(x.backs[id], f)
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
