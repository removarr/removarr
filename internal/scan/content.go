package scan

import (
	"sort"

	"github.com/removarr/removarr/internal/domain"
)

// MinContentSize is the smallest file that linking by content compares: 50 MB (spec 6.6).
// Smaller files, such as subtitles and samples, share a size by chance too easily.
const MinContentSize int64 = 50 << 20

// LibraryFile is one current library file and the items it belongs to: one movie, or
// every episode of the file.
type LibraryFile struct {
	ID    int
	Size  int64
	Items []int
}

// NeedsContentCheck reports whether a download's files are needed for linking by content
// (spec 6.6): the rules do not exclude the download, it is not in a queue, and either
// history shows it backing no library file, or the download is at least MinContentSize
// larger than the files history names it the source of, so it may hold a file imported by
// hand as well. sourced is the total size of the library files that history names d as the
// source of, summed over every media manager that answered. x must be built from history
// alone.
func NeedsContentCheck(r domain.Rules, x *LinkIndex, d domain.Download, sourced int64) bool {
	if d.ID == "" || excluded(r, d) || x.Queued(d.ID) {
		return false
	}
	return x.Backing(d.ID) == 0 || d.Size-sourced >= MinContentSize
}

// SourcedBytes returns, per download, the total size of the library files that links
// names the download as the source of.
func SourcedBytes(links domain.Links, library []LibraryFile) map[string]int64 {
	out := map[string]int64{}
	seen := map[int]bool{}
	for _, f := range library {
		if seen[f.ID] {
			continue
		}
		seen[f.ID] = true
		if id := links.FileSource[f.ID]; id != "" {
			out[id] += f.Size
		}
	}
	return out
}

// ContentLinks returns links extended by content (spec 6.6). A download backs a library
// file when one of the download's files has exactly the library file's size, that size is
// at least MinContentSize, and links names no source for the library file. The download
// is then also associated with the file's items. History takes precedence: a source that
// links names is never replaced. When files of several downloads match one library file,
// the download whose ID sorts first backs it. links itself is not modified.
func ContentLinks(links domain.Links, library []LibraryFile, files map[string][]domain.DownloadFile) domain.Links {
	out := domain.Links{
		Grabbed:    make(map[string][]int, len(links.Grabbed)),
		FileSource: make(map[int]string, len(links.FileSource)),
		Queue:      make(map[string]bool, len(links.Queue)),
	}
	for id, items := range links.Grabbed {
		out.Grabbed[id] = append([]int(nil), items...)
	}
	for file, id := range links.FileSource {
		out.FileSource[file] = id
	}
	for id, queued := range links.Queue {
		out.Queue[id] = queued
	}

	// The library files history names no source for, by size.
	unsourced := map[int64][]LibraryFile{}
	for _, f := range library {
		if f.Size >= MinContentSize && links.FileSource[f.ID] == "" {
			unsourced[f.Size] = append(unsourced[f.Size], f)
		}
	}
	if len(unsourced) == 0 {
		return out
	}

	ids := make([]string, 0, len(files))
	for id := range files {
		if id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		for _, file := range files[id] {
			for _, match := range unsourced[file.Size] {
				if out.FileSource[match.ID] != "" {
					continue // a download that sorts earlier already backs it
				}
				out.FileSource[match.ID] = id
				out.Grabbed[id] = addItems(out.Grabbed[id], match.Items)
			}
		}
	}
	return out
}

// addItems appends the items that are not in have yet, keeping the order.
func addItems(have, items []int) []int {
	for _, item := range items {
		found := false
		for _, h := range have {
			if h == item {
				found = true
				break
			}
		}
		if !found {
			have = append(have, item)
		}
	}
	return have
}
