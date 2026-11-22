package scan

import (
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

// links returns empty links with non-nil maps.
func links() domain.Links {
	return domain.Links{Grabbed: map[string][]int{}, FileSource: map[int]string{}, Queue: map[string]bool{}}
}

func TestLinkIndex(t *testing.T) {
	movies := links()
	movies.Grabbed["aaa"] = []int{1}
	movies.Grabbed["old"] = []int{1}
	movies.FileSource[10] = "aaa"
	movies.Queue["qqq"] = true

	series := links()
	series.Grabbed["pack"] = []int{101, 102}
	series.FileSource[20] = "pack"
	series.FileSource[21] = "pack"
	series.FileSource[99] = "gone" // file 99 is not a current file

	x := NewLinkIndex(
		ManagerFiles{Links: movies, FileIDs: []int{10}},
		ManagerFiles{Links: series, FileIDs: []int{20, 21, 21}},
	)

	backing := map[string]int{"aaa": 1, "pack": 2, "old": 0, "gone": 0, "zzz": 0}
	for id, want := range backing {
		if got := x.Backing(id); got != want {
			t.Errorf("Backing(%q) = %d, want %d", id, got, want)
		}
	}
	known := map[string]bool{"aaa": true, "old": true, "pack": true, "qqq": true, "zzz": false}
	for id, want := range known {
		if got := x.Known(id); got != want {
			t.Errorf("Known(%q) = %v, want %v", id, got, want)
		}
	}
	if !x.Queued("qqq") || x.Queued("aaa") {
		t.Error("only qqq is queued")
	}
}

func TestRemovable(t *testing.T) {
	l := links()
	l.Grabbed["new"] = []int{1}     // current release of movie 1
	l.Grabbed["old"] = []int{1}     // replaced by an upgrade
	l.Grabbed["pack"] = []int{2, 3} // collection pack behind movies 2 and 3
	l.Grabbed["queued"] = []int{1}
	l.Grabbed["other"] = []int{4}
	l.FileSource[10] = "new"
	l.FileSource[20] = "pack"
	l.FileSource[30] = "pack"
	l.FileSource[40] = "other"
	l.Queue["queued"] = true
	x := NewLinkIndex(ManagerFiles{Links: l, FileIDs: []int{10, 20, 30, 40}})

	tests := []struct {
		name         string
		items, files []int
		want         []string
	}{
		{"movie with an old and a new release", []int{1}, []int{10}, []string{"new", "old"}},
		{"one movie of a pack keeps the pack", []int{2}, []int{20}, nil},
		{"both movies of a pack free the pack", []int{2, 3}, []int{20, 30}, []string{"pack"}},
		{"a file listed twice is counted once", []int{2}, []int{20, 20}, nil},
		{"nothing associated", []int{99}, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := x.Removable(l, tt.items, tt.files); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Removable = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRemovableCountsFilesInOtherManagers(t *testing.T) {
	a := links()
	a.Grabbed["shared"] = []int{1}
	a.FileSource[10] = "shared"
	b := links()
	b.Grabbed["shared"] = []int{7}
	b.FileSource[70] = "shared"
	x := NewLinkIndex(ManagerFiles{Links: a, FileIDs: []int{10}}, ManagerFiles{Links: b, FileIDs: []int{70}})

	if got := x.Removable(a, []int{1}, []int{10}); got != nil {
		t.Errorf("a download that still backs a file in another manager must stay, got %v", got)
	}
}

func TestSeedBelowMin(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	d := func(days float64) *time.Duration {
		x := time.Duration(days * 24 * float64(time.Hour))
		return &x
	}
	tests := []struct {
		name              string
		minRatio, minDays float64
		ratio             *float64
		seeded            *time.Duration
		want              bool
	}{
		{"both off", 0, 0, f(0.1), d(1), false},
		{"ratio only, below", 1, 0, f(0.5), d(100), true},
		{"ratio only, met", 1, 0, f(1.0), d(0), false},
		{"time only, below", 0, 14, f(5), d(13.9), true},
		{"time only, met", 0, 14, f(0), d(14), false},
		{"both set, both below", 1, 14, f(0.5), d(3), true},
		{"both set, ratio met", 1, 14, f(1.2), d(3), false},
		{"both set, time met", 1, 14, f(0.2), d(20), false},
		{"client does not seed", 1, 14, nil, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := domain.DefaultRules()
			r.MinRatio, r.MinSeedDays = tt.minRatio, tt.minDays
			got := SeedBelowMin(r, domain.Download{Ratio: tt.ratio, SeedingTime: tt.seeded})
			if got != tt.want {
				t.Errorf("SeedBelowMin = %v, want %v", got, tt.want)
			}
		})
	}
}
