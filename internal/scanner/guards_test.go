package scanner

import (
	"errors"
	"reflect"
	"testing"

	"github.com/removarr/removarr/internal/domain"
)

// fetchedFrom builds the Fetched of a scan over p, as Run does.
func fetchedFrom(p Providers) Fetched {
	return service(p, withUser(), &fakeStore{}).fetch(ctx, p, withUser())
}

// A scan reads the files of a download for linking by content whenever downloads and a
// media manager answered, so a Fetched without them was assembled from history alone
// (spec 6.8, rule 5).
func TestComputeFailsEveryListWhenTheFileListsWereNotRead(t *testing.T) {
	p, _ := world()
	f := fetchedFrom(p)
	f.Files = nil
	summary, candidates := Compute(withUser(), now, f)

	const want = "The download client could not be read: its file lists were not read"
	for _, list := range []domain.ListKind{domain.ListMovies, domain.ListSeries, domain.ListTorrents} {
		if got := summary.Lists[list].Error; got != want {
			t.Errorf("%s: error %q, want %q", list, got, want)
		}
	}
	if len(candidates) != 0 {
		t.Errorf("%d rows produced, want none", len(candidates))
	}

	f.Files = map[string][]domain.DownloadFile{}
	summary, _ = Compute(withUser(), now, f)
	for _, list := range []domain.ListKind{domain.ListMovies, domain.ListSeries, domain.ListTorrents} {
		if got := summary.Lists[list]; got.Error != "" || got.Candidates == 0 {
			t.Errorf("%s with an empty file map: %+v, want rows and no error", list, got)
		}
	}
}

// A watched list needs both media managers when both are configured: a download that backs
// an item of the other manager would look unlinked otherwise (spec 6.8, rule 5).
func TestRunFailsAWatchedListWhenTheOtherMediaManagerFails(t *testing.T) {
	p, _ := world()
	p.Series = &fakeSeries{err: errors.New("timeout")}
	st := run(t, p, withUser())
	expect(t, st, domain.ListMovies, 0, "The series manager could not be read: timeout")
	expect(t, st, domain.ListSeries, 0, "The series manager could not be read: timeout")

	p, _ = world()
	p.Movies = &fakeMovies{err: errors.New("timeout")}
	st = run(t, p, withUser())
	expect(t, st, domain.ListSeries, 0, "The movie manager could not be read: timeout")
	expect(t, st, domain.ListMovies, 0, "The movie manager could not be read: timeout")
}

// The guard asks for a read of the file lists whenever one media manager answered: with only
// the movie manager configured, a Fetched without file lists still fails the lists.
func TestComputeFailsTheListsWhenOnlyOneMediaManagerIsConfiguredAndFilesWereNotRead(t *testing.T) {
	p, _ := world()
	p.Series = nil
	f := fetchedFrom(p)
	f.Files = nil
	summary, _ := Compute(withUser(), now, f)

	const want = "The download client could not be read: its file lists were not read"
	for _, list := range []domain.ListKind{domain.ListMovies, domain.ListTorrents} {
		if got := summary.Lists[list].Error; got != want {
			t.Errorf("%s: error %q, want %q", list, got, want)
		}
	}
}

// A failed media manager is named before a failed download client.
func TestRunNamesTheOtherMediaManagerBeforeTheDownloadClient(t *testing.T) {
	p, _ := world()
	p.Series = &fakeSeries{err: errors.New("series down")}
	p.Downloads = &fakeDownloads{err: errors.New("client down")}
	st := run(t, p, withUser())
	expect(t, st, domain.ListMovies, 0, "The series manager could not be read: series down")
}

func TestRunReportsTheListsOwnManagerFirst(t *testing.T) {
	p, _ := world()
	p.Movies = &fakeMovies{err: errors.New("movies down")}
	p.Series = &fakeSeries{err: errors.New("series down")}
	st := run(t, p, withUser())
	expect(t, st, domain.ListMovies, 0, "movies down")
	expect(t, st, domain.ListSeries, 0, "series down")
}

func TestRunProducesAWatchedListWhenTheOtherMediaManagerIsNotConfigured(t *testing.T) {
	p, _ := world()
	p.Series = nil
	st := run(t, p, withUser())
	expect(t, st, domain.ListMovies, 1, "")
	expect(t, st, domain.ListSeries, 0, "series manager is not configured")
}

// A download's own manager decides whether its files are asked for: file IDs of two
// managers mean different files. The movie manager's file 10 (700 MB) came from "movie1";
// the series manager's file 10 (100 MB) came from "show1", a download 660 MB larger.
// Matching the series links against the movie manager's files as well would count show1's
// source as 800 MB and hide that show1 holds more than history says.
func TestRunCountsBackedBytesPerManager(t *testing.T) {
	p, _ := world()
	downloads := p.Downloads.(*fakeDownloads)
	movies := p.Movies.(*fakeMovies)
	movies.snap.Movies[0].File = &domain.MediaFile{ID: 10, Size: 700 * mb}
	movies.snap.Links.FileSource = map[int]string{10: "movie1"}
	movies.snap.Links.Grabbed = map[string][]int{"movie1": {1}}
	series := p.Series.(*fakeSeries)
	series.snap.Series[0].Episodes[0].File = &domain.MediaFile{ID: 10, Size: 100 * mb}
	series.snap.Links.FileSource = map[int]string{10: "show1"}
	series.snap.Links.Grabbed = map[string][]int{"show1": {11}}
	downloads.list = []domain.Download{
		{ID: "movie1", Name: "Movie", Size: 700 * mb},
		{ID: "show1", Name: "Show", Size: 760 * mb},
	}
	run(t, p, withUser())

	if !reflect.DeepEqual(downloads.fileCalls, []string{"show1"}) {
		t.Errorf("files were read for %v, want only show1", downloads.fileCalls)
	}
}
