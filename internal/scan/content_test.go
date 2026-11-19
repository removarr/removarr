package scan

import (
	"reflect"
	"testing"

	"github.com/removarr/removarr/internal/domain"
)

const mb = int64(1) << 20

// sized returns a download's file list holding files of the given sizes.
func sized(sizes ...int64) []domain.DownloadFile {
	out := make([]domain.DownloadFile, 0, len(sizes))
	for _, s := range sizes {
		out = append(out, domain.DownloadFile{Name: "file", Size: s})
	}
	return out
}

func TestContentLinks(t *testing.T) {
	movie := LibraryFile{ID: 10, Size: 700 * mb, Items: []int{1}}
	tests := []struct {
		name       string
		history    func(l *domain.Links)
		library    []LibraryFile
		files      map[string][]domain.DownloadFile
		wantSource map[int]string
		wantItems  map[string][]int
	}{
		{
			name:       "a download's file with exactly the library file's size backs it",
			library:    []LibraryFile{movie},
			files:      map[string][]domain.DownloadFile{"pack": sized(700 * mb)},
			wantSource: map[int]string{10: "pack"},
			wantItems:  map[string][]int{"pack": {1}},
		},
		{
			name:       "one byte off is not a match",
			library:    []LibraryFile{movie},
			files:      map[string][]domain.DownloadFile{"pack": sized(700*mb + 1)},
			wantSource: map[int]string{},
			wantItems:  map[string][]int{},
		},
		{
			name:       "files under 50 MB are not compared",
			library:    []LibraryFile{{ID: 10, Size: 50*mb - 1, Items: []int{1}}},
			files:      map[string][]domain.DownloadFile{"pack": sized(50*mb - 1)},
			wantSource: map[int]string{},
			wantItems:  map[string][]int{},
		},
		{
			name:       "a file of exactly 50 MB is compared",
			library:    []LibraryFile{{ID: 10, Size: 50 * mb, Items: []int{1}}},
			files:      map[string][]domain.DownloadFile{"pack": sized(50 * mb)},
			wantSource: map[int]string{10: "pack"},
			wantItems:  map[string][]int{"pack": {1}},
		},
		{
			name: "history names the source, so content does not replace it",
			history: func(l *domain.Links) {
				l.FileSource[10] = "auto"
				l.Grabbed["auto"] = []int{1}
			},
			library:    []LibraryFile{movie},
			files:      map[string][]domain.DownloadFile{"crossseed": sized(700 * mb)},
			wantSource: map[int]string{10: "auto"},
			wantItems:  map[string][]int{"auto": {1}},
		},
		{
			name:       "a file of several episodes associates every one of them",
			library:    []LibraryFile{{ID: 20, Size: 900 * mb, Items: []int{11, 12}}},
			files:      map[string][]domain.DownloadFile{"double": sized(900 * mb)},
			wantSource: map[int]string{20: "double"},
			wantItems:  map[string][]int{"double": {11, 12}},
		},
		{
			name:       "a pack backs every library file it holds",
			library:    []LibraryFile{movie, {ID: 11, Size: 800 * mb, Items: []int{2}}},
			files:      map[string][]domain.DownloadFile{"pack": sized(700*mb, 4096, 800*mb)},
			wantSource: map[int]string{10: "pack", 11: "pack"},
			wantItems:  map[string][]int{"pack": {1, 2}},
		},
		{
			name:       "of two downloads holding the same file, the one whose ID sorts first backs it",
			library:    []LibraryFile{movie},
			files:      map[string][]domain.DownloadFile{"bbb": sized(700 * mb), "aaa": sized(700 * mb)},
			wantSource: map[int]string{10: "aaa"},
			wantItems:  map[string][]int{"aaa": {1}},
		},
		{
			name:       "a download history already associates with the item keeps it once",
			history:    func(l *domain.Links) { l.Grabbed["pack"] = []int{1} },
			library:    []LibraryFile{movie},
			files:      map[string][]domain.DownloadFile{"pack": sized(700 * mb)},
			wantSource: map[int]string{10: "pack"},
			wantItems:  map[string][]int{"pack": {1}},
		},
		{
			name:       "a download without an ID links nothing",
			library:    []LibraryFile{movie},
			files:      map[string][]domain.DownloadFile{"": sized(700 * mb)},
			wantSource: map[int]string{},
			wantItems:  map[string][]int{},
		},
		{
			name:       "without file lists nothing changes",
			history:    func(l *domain.Links) { l.Grabbed["old"] = []int{1} },
			library:    []LibraryFile{movie},
			wantSource: map[int]string{},
			wantItems:  map[string][]int{"old": {1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := links()
			if tt.history != nil {
				tt.history(&l)
			}
			got := ContentLinks(l, tt.library, tt.files)
			if !reflect.DeepEqual(got.FileSource, tt.wantSource) {
				t.Errorf("FileSource = %v, want %v", got.FileSource, tt.wantSource)
			}
			if !reflect.DeepEqual(got.Grabbed, tt.wantItems) {
				t.Errorf("Grabbed = %v, want %v", got.Grabbed, tt.wantItems)
			}
		})
	}
}

func TestContentLinksLeavesItsInputAlone(t *testing.T) {
	l := links()
	l.Grabbed["old"] = []int{1}
	l.Queue["queued"] = true

	got := ContentLinks(l, []LibraryFile{{ID: 10, Size: 700 * mb, Items: []int{1, 2}}},
		map[string][]domain.DownloadFile{"old": sized(700 * mb)})

	if len(l.FileSource) != 0 || !reflect.DeepEqual(l.Grabbed, map[string][]int{"old": {1}}) {
		t.Errorf("the input changed: FileSource %v, Grabbed %v", l.FileSource, l.Grabbed)
	}
	if !reflect.DeepEqual(got.Grabbed["old"], []int{1, 2}) || got.FileSource[10] != "old" || !got.Queue["queued"] {
		t.Errorf("result = %+v, want old backing file 10 for items 1 and 2, and the queue kept", got)
	}
}

func TestNeedsContentCheck(t *testing.T) {
	l := links()
	l.Queue["queued"] = true
	l.Grabbed["linked"] = []int{1}
	l.FileSource[10] = "linked"
	l.Grabbed["replaced"] = []int{1}
	x := NewLinkIndex(ManagerFiles{Links: l, FileIDs: []int{10}})

	r := domain.DefaultRules()
	r.ExcludedCategories = []string{"games"}
	r.ExcludedTags = []string{"keep"}

	tests := []struct {
		name string
		d    domain.Download
		want bool
	}{
		{"a download no manager knows", dl("stray"), true},
		{"a download history knows but that backs nothing", dl("replaced"), true},
		{"a download history shows backing a file", dl("linked"), false},
		{"a queued download", dl("queued"), false},
		{"a download in an excluded category", dl("stray", func(d *domain.Download) { d.Category = "games" }), false},
		{"a download with an excluded tag", dl("stray", func(d *domain.Download) { d.Tags = []string{"keep"} }), false},
		{"a download without an ID", dl(""), false},
	}
	for _, tt := range tests {
		if got := NeedsContentCheck(r, x, tt.d); got != tt.want {
			t.Errorf("%s: NeedsContentCheck = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// A torrent linked by content must behave like one linked by history everywhere else: it
// is not listed as an orphan, and a deletion that leaves it backing nothing removes it.
func TestAContentLinkedDownloadIsNotListedAndGoesWithItsItems(t *testing.T) {
	library := []LibraryFile{{ID: 10, Size: 700 * mb, Items: []int{1}}}
	linked := ContentLinks(links(), library, map[string][]domain.DownloadFile{
		"pack":  sized(700 * mb),
		"stray": sized(650 * mb),
	})
	x := NewLinkIndex(ManagerFiles{Links: linked, FileIDs: []int{10}})

	res := Torrents(TorrentsInput{
		Rules:     domain.DefaultRules(),
		Downloads: []domain.Download{dl("pack"), dl("stray")},
		Index:     x,
	})
	if len(res.Candidates) != 1 || res.Candidates[0].Key != "hash:stray" || res.Candidates[0].Reason != domain.ReasonOrphan {
		t.Errorf("listed %+v, want only the stray torrent, as an orphan", res.Candidates)
	}

	if got := x.Removable(linked, []int{1}, []int{10}); !reflect.DeepEqual(got, []string{"pack"}) {
		t.Errorf("deleting the movie and its file removes %v, want [pack]", got)
	}
	if got := x.Removable(linked, []int{1}, nil); got != nil {
		t.Errorf("while the file stays, a deletion removes %v, want nothing", got)
	}
}
