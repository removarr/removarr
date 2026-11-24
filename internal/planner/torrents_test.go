package planner

import (
	"reflect"
	"strings"
	"testing"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
)

func TestMakeMarksATorrentLinkedOnlyByFileSize(t *testing.T) {
	f := world()
	// Movie 2's file was imported by hand from c2: only its size ties them.
	size := scan.MinContentSize + 7
	f.Movies.Value.Movies[1].File.Size = size
	f.Downloads.Value = append(f.Downloads.Value, domain.Download{ID: "c2", Name: "Two.Torrent", Size: size, ContentPath: "/dl/two"})
	f.Files = map[string][]domain.DownloadFile{"c2": {{Name: "two.mkv", Size: size}}}
	items, skipped := same(t, rules(), f, nil, movieOne, movieTwo)
	if len(items) != 2 || len(skipped) != 0 {
		t.Fatalf("items = %+v, skipped = %+v", items, skipped)
	}
	if got := items[0].Torrents; len(got) != 1 || got[0].LinkedBySize {
		t.Errorf("One: torrents = %+v, want m1, linked by history", got)
	}
	if got := items[1].Torrents; len(got) != 1 || got[0].Torrent.ID != "c2" || !got[0].LinkedBySize {
		t.Errorf("Two: torrents = %+v, want c2, linked by size", got)
	}
}

func TestMakeListsTheTorrentsThatStay(t *testing.T) {
	f := world()
	f.Library.Value.Movies[1].States = nil // movie 2 is not watched
	// Both were grabbed for movie 1: q is still in a queue, b became movie 2's file.
	f.Movies.Value.Links.Grabbed["q"] = []int{1}
	f.Movies.Value.Links.Queue["q"] = true
	f.Movies.Value.Links.Grabbed["b"] = []int{1}
	f.Movies.Value.Links.FileSource[20] = "b"
	// gone was grabbed for movie 1 and is no longer in the client: it is not mentioned.
	f.Movies.Value.Links.Grabbed["gone"] = []int{1}
	f.Downloads.Value = append(f.Downloads.Value,
		domain.Download{ID: "q", Name: "One.Queued"}, domain.Download{ID: "b", Name: "Two.Torrent"})
	item := onlyItem(t)(same(t, rules(), f, nil, movieOne))
	var got []string
	for _, k := range item.Kept {
		got = append(got, k.Torrent.ID+" "+k.Reason+" "+k.Torrent.Name)
	}
	want := []string{"b " + domain.KeptBacksLibrary + " Two.Torrent", "q " + domain.KeptQueued + " One.Queued"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("kept = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(torrentIDs(item), []string{"m1"}) {
		t.Errorf("torrents = %v, want m1 alone", torrentIDs(item))
	}
}

func TestMakeDoesNotListATorrentAsKeptThatThePlanRemoves(t *testing.T) {
	f := world()
	// b was grabbed for movie 1 and became movie 2's file. Both movies are watched and
	// selected, so b goes with movie 2: it does not stay for movie 1.
	f.Movies.Value.Links.Grabbed["b"] = []int{1}
	f.Movies.Value.Links.FileSource[20] = "b"
	f.Downloads.Value = append(f.Downloads.Value, domain.Download{ID: "b", Name: "Two.Torrent", ContentPath: "/dl/two"})
	items, skipped := same(t, rules(), f, nil, movieOne, movieTwo)
	if len(items) != 2 || len(skipped) != 0 {
		t.Fatalf("items = %+v, skipped = %+v, want the two movies", items, skipped)
	}
	if !reflect.DeepEqual(torrentIDs(items[1]), []string{"b"}) {
		t.Fatalf("Two: torrents = %v, want b", torrentIDs(items[1]))
	}
	for _, it := range items {
		if it.Kept == nil {
			t.Errorf("%v: kept is nil, want empty", rowKeys(it))
		}
		for _, k := range it.Kept {
			if k.Torrent.ID == "b" {
				t.Errorf("%v: kept lists b, which the plan removes", rowKeys(it))
			}
		}
	}
}

func TestMakeKeepsTheDataATorrentThatStaysUses(t *testing.T) {
	tests := []struct {
		name string
		path string // content path of the torrent "x", which stays
		mine string // content path of m1
		keep bool
	}{
		{"the same path", "/dl/one", "/dl/one", true},
		{"the same path with a trailing slash", "/dl/one/", "/dl/one", true},
		{"a path inside it", "/dl/one/extra.mkv", "/dl/one", true},
		{"a path around it", "/dl", "/dl/one", true},
		{"a Windows path inside it", `D:\dl\one\extra.mkv`, `D:\dl\one`, true},
		{"a path that only starts the same", "/dl/one-two", "/dl/one", false},
		{"an unrelated path", "/dl/other", "/dl/one", false},
		{"an unknown path of its own", "/dl/other", "", true},
		{"its own path with a trailing slash, around it", "/dl/one/sub", "/dl/one/", true},
		{"its own path of only a separator", "/dl/other", "/", true},
		{"its own Windows path with a trailing separator, around it", `D:\dl\one\x`, `D:\dl\one\`, true},
		{"an unknown path that stays", "", "/dl/one", true},
		{"a path of only a separator that stays", "/", "/dl/one", true},
		{"a Windows path around it in another case", `D:\DL\one`, `d:\dl\one\x`, true},
		{"a path around it, its own with a doubled separator", "/dl/one", "//dl/one/x", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := world()
			f.Downloads.Value[0].ContentPath = tt.mine
			f.Downloads.Value = append(f.Downloads.Value, domain.Download{ID: "x", Name: "Cross.Seed", ContentPath: tt.path})
			item := onlyItem(t)(same(t, rules(), f, nil, movieOne))
			if len(item.Torrents) != 1 || item.Torrents[0].KeepData != tt.keep {
				t.Errorf("torrents = %+v, want m1 with KeepData %v", item.Torrents, tt.keep)
			}
		})
	}
}

// planTorrent finds a torrent the plan removes, by ID.
func planTorrent(t *testing.T, items []domain.PlanItem, id string) domain.PlanTorrent {
	t.Helper()
	for _, it := range items {
		for _, pt := range it.Torrents {
			if pt.Torrent.ID == id {
				return pt
			}
		}
	}
	t.Fatalf("no item removes %s: %+v", id, items)
	return domain.PlanTorrent{}
}

func TestMakeKeepsTheDataOfATorrentWhosePathIsUnknown(t *testing.T) {
	t.Run("only Windows paths stay", func(t *testing.T) {
		f := world()
		f.Downloads.Value[0].ContentPath = ""
		f.Downloads.Value[1].ContentPath = `E:\elsewhere`
		f.Downloads.Value[2].ContentPath = `E:\other`
		items, _ := same(t, rules(), f, nil, movieOne)
		if !planTorrent(t, items, "m1").KeepData {
			t.Errorf("m1 deletes its data, want it kept: its path is unknown")
		}
	})
	t.Run("nothing stays", func(t *testing.T) {
		f := world()
		f.Downloads.Value[0].ContentPath = ""
		items, _ := same(t, rules(), f, nil, movieOne, theShow, orphan)
		if len(items) != 3 {
			t.Fatalf("items = %+v, want three: every download goes", items)
		}
		if !planTorrent(t, items, "m1").KeepData {
			t.Errorf("m1 deletes its data, want it kept: its path is unknown")
		}
	})
}

func TestMakeMarksOnlyTheTorrentLinkedBySizeInAnItemWithBoth(t *testing.T) {
	f := world()
	// Episode 2's file was imported by hand from c: only its size ties them. Episode 1's file
	// came from p1, which history records.
	size := scan.MinContentSize + 3
	f.Series.Value.Series[0].Episodes[1].File.Size = size
	delete(f.Series.Value.Links.FileSource, 112)
	f.Downloads.Value = append(f.Downloads.Value, domain.Download{ID: "c", Name: "Show.E02", Size: size, ContentPath: "/dl/e02"})
	f.Files = map[string][]domain.DownloadFile{"c": {{Name: "e02.mkv", Size: size}}}
	item := onlyItem(t)(same(t, rules(), f, nil, theShow))
	by := map[string]bool{}
	for _, pt := range item.Torrents {
		by[pt.Torrent.ID] = pt.LinkedBySize
	}
	if want := map[string]bool{"c": true, "p1": false}; !reflect.DeepEqual(by, want) {
		t.Errorf("linked by size = %v, want %v", by, want)
	}
}

func TestMakeDeletesSharedDataWhenEveryTorrentUsingItGoes(t *testing.T) {
	f := world()
	f.Downloads.Value[2].ContentPath = "/dl/one" // the orphan holds the same data as m1
	items, _ := same(t, rules(), f, nil, movieOne, orphan)
	if len(items) != 2 {
		t.Fatalf("items = %+v, want two", items)
	}
	for _, it := range items {
		if it.Torrents[0].KeepData {
			t.Errorf("%s keeps its data, want it deleted: no torrent using it stays", it.Torrents[0].Torrent.ID)
		}
	}
}

func TestMakeRemovesATorrentOnceWhenTwoItemsNameIt(t *testing.T) {
	f := world()
	// g was grabbed for both movies and backs nothing: both rows name it, and it is unlinked.
	f.Movies.Value.Links.Grabbed["g"] = []int{1, 2}
	f.Downloads.Value = append(f.Downloads.Value, domain.Download{ID: "g", Name: "Grabbed.Twice"})
	items, skipped := same(t, rules(), f, nil, movieOne, movieTwo, sel(domain.ListTorrents, "hash:g"))
	if len(items) != 2 {
		t.Fatalf("items = %+v, want the two movies", items)
	}
	if !reflect.DeepEqual(torrentIDs(items[0]), []string{"g", "m1"}) || len(items[1].Torrents) != 0 {
		t.Errorf("torrents = %v and %v, want g with One only", torrentIDs(items[0]), torrentIDs(items[1]))
	}
	if got := skips(skipped)["hash:g"]; !strings.Contains(got, `"One"`) {
		t.Errorf("torrent row reason = %q, want it to name One", got)
	}
}
