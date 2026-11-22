package scan

import "github.com/removarr/removarr/internal/domain"

// MovieFiles lists the current movie files with the movie each belongs to.
func MovieFiles(s domain.MovieSnapshot) []LibraryFile {
	var out []LibraryFile
	for _, m := range s.Movies {
		if m.File != nil {
			out = append(out, LibraryFile{ID: m.File.ID, Size: m.File.Size, Items: []int{m.ID}})
		}
	}
	return out
}

// EpisodeFiles lists the current episode files with the episodes each belongs to. The
// episodes of a multi-episode file share one file.
func EpisodeFiles(s domain.SeriesSnapshot) []LibraryFile {
	var out []LibraryFile
	at := map[int]int{} // file ID -> its position in out
	for _, show := range s.Series {
		for _, e := range show.Episodes {
			if e.File == nil {
				continue
			}
			i, seen := at[e.File.ID]
			if !seen {
				i = len(out)
				at[e.File.ID] = i
				out = append(out, LibraryFile{ID: e.File.ID, Size: e.File.Size})
			}
			out[i].Items = append(out[i].Items, e.ID)
		}
	}
	return out
}
