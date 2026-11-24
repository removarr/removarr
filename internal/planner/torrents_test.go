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
