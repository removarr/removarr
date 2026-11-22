package scanner

import (
	"errors"
	"strings"
	"testing"

	"github.com/removarr/removarr/internal/domain"
)

// When no download needs its files, a scan still read the file lists: fetch leaves an empty
// map, not nil, so Guard 1 does not fail the lists.
func TestRunKeepsTheFileMapNonNilWhenNoDownloadNeededItsFiles(t *testing.T) {
	check := func(name string, p Providers) {
		t.Helper()
		f := fetchedFrom(p)
		if f.Files == nil {
			t.Errorf("%s: Files is nil after fetch, want an empty map", name)
		}
		st := run(t, p, withUser())
		for _, list := range []domain.ListKind{domain.ListMovies, domain.ListSeries, domain.ListTorrents} {
			if got := st.summary.Lists[list]; strings.Contains(got.Error, "file lists were not read") || got.Error != "" {
				t.Errorf("%s: %s error %q, want the list produced", name, list, got.Error)
			}
		}
	}

	// History links every download: h1 to the movie's file.
	p, _ := world()
	p.Downloads.(*fakeDownloads).list = []domain.Download{{ID: "h1", Name: "The.Matrix"}}
	check("every download linked", p)

	p, _ = world()
	p.Downloads.(*fakeDownloads).list = nil
	check("no downloads", p)
}

// A manager whose settings are half entered is configured and fails every read.
func TestRunNamesAHalfConfiguredSecondManager(t *testing.T) {
	p, _ := world()
	p.Series = &fakeSeries{err: errors.New("its connection settings are incomplete")}
	st := run(t, p, withUser())

	expect(t, st, domain.ListMovies, 0, "The series manager could not be read: its connection settings are incomplete")
}
