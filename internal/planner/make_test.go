package planner

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
)

func TestMakeReturnsEmptyListsForNoSelection(t *testing.T) {
	items, skipped := same(t, rules(), world(), nil)
	if items == nil || skipped == nil || len(items)+len(skipped) != 0 {
		t.Errorf("items = %v, skipped = %v, want two empty non-nil lists", items, skipped)
	}
}

func TestMakeSkipsEveryRowOfAListThatCannotBeProduced(t *testing.T) {
	f := world()
	latest := scanned(rules(), f)
	f.Movies.Err = errors.New("timeout")
	items, skipped := plan(rules(), f, latest, nil, movieOne, theShow, orphan)
	got := skips(skipped)
	for _, key := range []string{"tmdb:1", "tvdb:100", "hash:orphan"} {
		if !strings.Contains(got[key], "movie manager could not be read") {
			t.Errorf("%s: reason = %q, want the movie manager's error", key, got[key])
		}
	}
	if len(items) != 0 {
		t.Errorf("items = %v, want none", items)
	}
}

func TestMakeSkipsARowThatIsNotInTheLatestScan(t *testing.T) {
	items, skipped := plan(rules(), world(), nil, nil, movieOne)
	if len(items) != 0 || skips(skipped)["tmdb:1"] != ReasonNotInScan {
		t.Errorf("items = %v, skipped = %v, want tmdb:1 skipped as not in the scan", items, skipped)
	}
	if skipped[0].Row.List != domain.ListMovies {
		t.Errorf("skipped row = %+v, want its list kept", skipped[0].Row)
	}
}

func TestMakeSkipsAnIgnoredRow(t *testing.T) {
	ign := []domain.Ignore{{Kind: domain.IgnoreMovie, Key: "tmdb:1", Title: "One"}}
	items, skipped := same(t, rules(), world(), ign, movieOne)
	if len(items) != 0 || skips(skipped)["tmdb:1"] != scan.HoldIgnored {
		t.Errorf("items = %v, skipped = %v, want tmdb:1 skipped as ignored", items, skipped)
	}
}

func TestMakeSkipsAnIgnoredRowAsIgnoredAlsoWhenItNoLongerQualifies(t *testing.T) {
	f := world()
	latest := scanned(rules(), f)
	f.Library.Value.Movies[0].States = nil // the new read no longer produces the row
	ign := []domain.Ignore{{Kind: domain.IgnoreMovie, Key: "tmdb:1", Title: "One"}}
	items, skipped := plan(rules(), f, latest, ign, movieOne)
	if len(items) != 0 || skips(skipped)["tmdb:1"] != scan.HoldIgnored {
		t.Errorf("items = %v, skipped = %v, want tmdb:1 skipped as ignored", items, skipped)
	}
}

func TestMakeSkipsAMovieThatNoLongerQualifies(t *testing.T) {
	f := world()
	latest := scanned(rules(), f)
	f.Library.Value.Movies[0].States = nil // nobody has watched it any more
	_, skipped := plan(rules(), f, latest, nil, movieOne)
	if got := skips(skipped)["tmdb:1"]; got != ReasonNoLonger {
		t.Errorf("reason = %q, want %q", got, ReasonNoLonger)
	}
	if skipped[0].Row.Title != "One" {
		t.Errorf("skipped row = %+v, want its title from the latest scan", skipped[0].Row)
	}
}

func TestMakeSkipsATorrentRowWithWhatChanged(t *testing.T) {
	tests := []struct {
		name   string
		change func(f *scannerFetched)
		want   string
	}{
		{"gone from the client", func(f *scannerFetched) { f.Downloads.Value = f.Downloads.Value[:2] }, ReasonGone},
		{"in a queue now", func(f *scannerFetched) { f.Movies.Value.Links.Queue["orphan"] = true }, ReasonQueuedNow},
		{"backs a library file now", func(f *scannerFetched) { f.Movies.Value.Links.FileSource[20] = "orphan" }, ReasonLinkedNow},
		{"excluded now", func(f *scannerFetched) { f.Downloads.Value[2].Category = "music" }, ReasonExcludedNow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := rules()
			r.ExcludedCategories = []string{"music"}
			f := world()
			latest := scanned(r, f)
			tt.change(&f)
			_, skipped := plan(r, f, latest, nil, orphan)
			if got := skips(skipped)["hash:orphan"]; got != tt.want {
				t.Errorf("reason = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMakeSkipsAWaitingRowWithWhatItWaitsFor(t *testing.T) {
	f := world()
	f.Movies.Value.Links.FileSource[20] = "m1" // m1 also backs movie 2's file
	f.Library.Value.Movies[1].States = nil     // and movie 2 is not watched
	_, skipped := same(t, rules(), f, nil, movieOne)
	if got := skips(skipped)["tmdb:1"]; got != scan.WaitTorrentStays {
		t.Errorf("reason = %q, want %q", got, scan.WaitTorrentStays)
	}
}

func TestMakeSkipsARowTiedToAnIgnoredRow(t *testing.T) {
	f := world()
	f.Movies.Value.Links.FileSource[20] = "m1" // m1 backs both movies: they are tied
	ign := []domain.Ignore{{Kind: domain.IgnoreMovie, Key: "tmdb:2", Title: "Two"}}
	items, skipped := same(t, rules(), f, ign, movieOne)
	if len(items) != 0 || skips(skipped)["tmdb:1"] != `It is tied to "Two", which is ignored.` {
		t.Errorf("items = %v, skipped = %v, want tmdb:1 skipped, naming Two", items, skipped)
	}
}

func TestMakeTurnsATorrentRowIntoAnItemOfItsOwn(t *testing.T) {
	items, skipped := same(t, rules(), world(), nil, orphan)
	if len(skipped) != 0 || len(items) != 1 {
		t.Fatalf("items = %v, skipped = %v, want one item", items, skipped)
	}
	item := items[0]
	if !reflect.DeepEqual(rowKeys(item), []string{"torrents/hash:orphan"}) || !reflect.DeepEqual(torrentIDs(item), []string{"orphan"}) {
		t.Errorf("item = %+v, want the torrent row with its torrent", item)
	}
	if item.Steps == nil || len(item.Steps) != 0 || item.Kept == nil {
		t.Errorf("steps = %v, kept = %v, want empty non-nil lists", item.Steps, item.Kept)
	}
}

func TestMakeTakesEachSelectedRowOnceAndOrdersTheItems(t *testing.T) {
	items, _ := same(t, rules(), world(), nil, orphan, theShow, movieTwo, movieOne, movieOne)
	var got []string
	for _, it := range items {
		got = append(got, rowKeys(it)...)
	}
	want := []string{"movies/tmdb:1", "movies/tmdb:2", "series/tvdb:100", "torrents/hash:orphan"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

func TestMakeSkipsARowThatWaitedInTheLatestScan(t *testing.T) {
	f := world()
	f.Movies.Value.Links.FileSource[20] = "m1" // m1 also backs movie 2's file
	f.Library.Value.Movies[1].States = nil     // and movie 2 is not watched: movie 1 waits
	latest := scanned(rules(), f)
	if len(latest) == 0 || latest[0].Key != "tmdb:1" || !latest[0].Waiting {
		t.Fatalf("latest = %+v, want movie 1 stored as waiting", latest)
	}
	// Since the scan, movie 2 was watched: both movies are listed now, tied through m1.
	f.Library.Value.Movies[1].States = played()
	items, skipped := plan(rules(), f, latest, nil, movieOne, movieTwo)
	if len(items) != 0 {
		t.Fatalf("items = %+v, want none", items)
	}
	got := skips(skipped)
	if got["tmdb:1"] != ReasonWaitedInScan {
		t.Errorf("tmdb:1: reason = %q, want %q", got["tmdb:1"], ReasonWaitedInScan)
	}
	if got["tmdb:2"] != ReasonNotInScan {
		t.Errorf("tmdb:2: reason = %q, want %q", got["tmdb:2"], ReasonNotInScan)
	}
}

func TestMakeSkipsARowTiedToARowThatWaitedInTheLatestScan(t *testing.T) {
	f := world()
	// Movie 2's file comes from m2, which is in a queue: movie 2 waits, movie 1 is listed.
	f.Downloads.Value = append(f.Downloads.Value, domain.Download{ID: "m2", Name: "Two.Torrent", Size: 2000, ContentPath: "/dl/two"})
	f.Movies.Value.Links.FileSource[20] = "m2"
	f.Movies.Value.Links.Queue["m2"] = true
	latest := scanned(rules(), f)
	// Since the scan, m2 left the queue and movie 2's file comes from m1: the movies are tied.
	f.Movies.Value.Links.Queue["m2"] = false
	f.Movies.Value.Links.FileSource[20] = "m1"
	items, skipped := plan(rules(), f, latest, nil, movieOne, movieTwo)
	if len(items) != 0 {
		t.Fatalf("items = %+v, want none", items)
	}
	got := skips(skipped)
	if want := `It is tied to "Two", which waited in the latest scan. Scan again.`; got["tmdb:1"] != want {
		t.Errorf("tmdb:1: reason = %q, want %q", got["tmdb:1"], want)
	}
	if got["tmdb:2"] != ReasonWaitedInScan {
		t.Errorf("tmdb:2: reason = %q, want %q", got["tmdb:2"], ReasonWaitedInScan)
	}
}

func TestMakeDoesNotAddARowThatWaitedInTheLatestScanToAnItem(t *testing.T) {
	f := world()
	// Movie 1's torrent m1 is in a queue: movie 1 waits. Movie 2 is listed with m2.
	f.Downloads.Value = append(f.Downloads.Value, domain.Download{ID: "m2", Name: "Two.Torrent", Size: 2000, ContentPath: "/dl/two"})
	f.Movies.Value.Links.FileSource[20] = "m2"
	f.Movies.Value.Links.Queue["m1"] = true
	latest := scanned(rules(), f)
	// Since the scan, m1 left the queue and history no longer names it; movie 1's file comes
	// from m2: the movies are tied.
	f.Movies.Value.Links.Queue["m1"] = false
	delete(f.Movies.Value.Links.Grabbed, "m1")
	f.Movies.Value.Links.FileSource[10] = "m2"
	items, skipped := plan(rules(), f, latest, nil, movieOne, movieTwo)
	if len(items) != 0 {
		t.Fatalf("items = %+v, want none", items)
	}
	got := skips(skipped)
	if got["tmdb:1"] != ReasonWaitedInScan {
		t.Errorf("tmdb:1: reason = %q, want %q", got["tmdb:1"], ReasonWaitedInScan)
	}
	if want := `It is tied to "One", which waited in the latest scan. Scan again.`; got["tmdb:2"] != want {
		t.Errorf("tmdb:2: reason = %q, want %q", got["tmdb:2"], want)
	}
}
