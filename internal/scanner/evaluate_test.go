package scanner

import (
	"errors"
	"reflect"
	"testing"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
)

var errTest = errors.New("test failure")

// fetched reads the small library of world() the way a scan does.
func fetched(t *testing.T) Fetched {
	t.Helper()
	p, _ := world()
	return Read(ctx, p, withUser(), nil)
}

func TestEvaluateGivesWhatComputeGives(t *testing.T) {
	f := fetched(t)
	summary, candidates := Compute(withUser(), now, f)
	e := Evaluate(withUser(), now, f)
	if !reflect.DeepEqual(e.Summary, summary) || !reflect.DeepEqual(e.Candidates, candidates) {
		t.Errorf("Evaluate = %+v / %+v, want Compute's %+v / %+v", e.Summary, e.Candidates, summary, candidates)
	}
}

func TestEvaluateKeepsTheRowsWithWhatDeleteTouches(t *testing.T) {
	e := Evaluate(withUser(), now, fetched(t))
	byKey := map[string]scan.Row{}
	for _, r := range e.Rows {
		byKey[r.Candidate.Key] = r
	}
	movie, series := byKey["tmdb:603"], byKey["tvdb:100"]
	if movie.Manager != scan.ManagerMovies || movie.ManagerID != 1 || !reflect.DeepEqual(movie.FileIDs, []int{10}) {
		t.Errorf("movie row = %+v", movie)
	}
	if series.Manager != scan.ManagerSeries || series.ManagerID != 1 || !reflect.DeepEqual(series.ItemIDs, []int{11}) {
		t.Errorf("series row = %+v", series)
	}
	if len(e.Fetched.Downloads.Value) != 2 {
		t.Errorf("Fetched holds %d downloads, want the 2 that were read", len(e.Fetched.Downloads.Value))
	}
}

func TestEvaluateKeepsNoRowsOfAListItCannotProduce(t *testing.T) {
	f := fetched(t)
	f.Series.Err = errTest
	e := Evaluate(withUser(), now, f)
	if len(e.Rows) != 0 {
		t.Errorf("rows = %+v, want none: neither watched list can be produced", e.Rows)
	}
}

func TestEvaluateKeepsHistoryApartFromContentLinks(t *testing.T) {
	f := fetched(t)
	// The series file was imported by hand from "orphan1": only its size says so.
	f.Series.Value.Series[0].Episodes[0].File.Size = scan.MinContentSize
	f.Files = map[string][]domain.DownloadFile{"orphan1": {{Name: "ep.mkv", Size: scan.MinContentSize}}}
	e := Evaluate(withUser(), now, f)
	if got := e.Index.Associated(scan.ManagerSeries, []int{11}, []int{11}); !reflect.DeepEqual(got, []string{"orphan1"}) {
		t.Errorf("Index.Associated = %v, want [orphan1] by content", got)
	}
	if got := e.History.Associated(scan.ManagerSeries, []int{11}, []int{11}); len(got) != 0 {
		t.Errorf("History.Associated = %v, want none: history does not know the file", got)
	}
	if got := e.History.Associated(scan.ManagerMovies, []int{1}, []int{10}); !reflect.DeepEqual(got, []string{"h1"}) {
		t.Errorf("History.Associated for the movie = %v, want [h1]", got)
	}
}

func TestReadTellsItsPhases(t *testing.T) {
	p, _ := world()
	var phases []string
	Read(ctx, p, withUser(), func(s string) { phases = append(phases, s) })
	want := []string{"reading the download client", "reading the movie manager", "reading the series manager",
		"matching downloads to library files", "reading the media server"}
	if !reflect.DeepEqual(phases, want) {
		t.Errorf("phases = %v, want %v", phases, want)
	}
}
