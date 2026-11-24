package scan

import (
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

// dl returns a healthy, long-seeded download; mods adjust it.
func dl(id string, mods ...func(*domain.Download)) domain.Download {
	ratio := 2.0
	seeded := 30 * 24 * time.Hour
	d := domain.Download{ID: id, Name: "name-" + id, Size: 100, State: "uploading", Ratio: &ratio, SeedingTime: &seeded}
	for _, m := range mods {
		m(&d)
	}
	return d
}

func TestTorrents(t *testing.T) {
	l := links()
	l.Grabbed["linked"] = []int{1}
	l.FileSource[10] = "linked"
	l.Grabbed["unlinked"] = []int{1}
	l.Grabbed["queued"] = []int{2}
	l.Queue["queued"] = true
	x := NewLinkIndex(ManagerFiles{Links: l, Files: []LibraryFile{{ID: 10}}})

	r := domain.DefaultRules()
	r.ExcludedCategories = []string{"manual"}
	r.ExcludedTags = []string{"cross-seed"}
	r.MinRatio = 1
	low := 0.2

	res := Torrents(TorrentsInput{Rules: r, Index: x, Downloads: []domain.Download{
		dl("linked"),
		dl("unlinked"),
		dl("queued"),
		dl("orphan", func(d *domain.Download) { d.Ratio = &low }),
		dl("cat", func(d *domain.Download) { d.Category = "manual" }),
		dl("tag", func(d *domain.Download) { d.Tags = []string{"x", "cross-seed"} }),
		dl(""),
	}})

	if len(res.Candidates) != 2 {
		t.Fatalf("got %d candidates, want 2 (orphan and unlinked): %+v", len(res.Candidates), res.Candidates)
	}
	orphan, unlinked := res.Candidates[0], res.Candidates[1]
	if orphan.Key != "hash:orphan" || orphan.Reason != domain.ReasonOrphan {
		t.Errorf("first row = %+v, want the orphan", orphan)
	}
	if unlinked.Key != "hash:unlinked" || unlinked.Reason != domain.ReasonUnlinked {
		t.Errorf("second row = %+v, want the unlinked torrent", unlinked)
	}
	if orphan.List != domain.ListTorrents || orphan.Title != "name-orphan" || orphan.Size != 100 {
		t.Errorf("orphan row fields wrong: %+v", orphan)
	}
	if !orphan.BelowSeedMin || len(orphan.Torrents) != 1 || !orphan.Torrents[0].BelowSeedMin {
		t.Errorf("orphan must be flagged below the seeding minimum: %+v", orphan)
	}
	if unlinked.BelowSeedMin {
		t.Errorf("unlinked torrent has ratio 2 and must not be flagged: %+v", unlinked)
	}
	if got := unlinked.Torrents[0].SeedingDays; got == nil || *got != 30 {
		t.Errorf("SeedingDays = %v, want 30", got)
	}
}

func TestTorrentDetailsIsWhatATorrentRowShows(t *testing.T) {
	r := domain.DefaultRules()
	r.MinRatio = 2
	ratio := 1.0
	d := dl("abc", func(d *domain.Download) { d.Name = "Some.Torrent"; d.Ratio = &ratio })
	res := Torrents(TorrentsInput{Rules: r, Downloads: []domain.Download{d}, Index: NewLinkIndex()})
	if len(res.Candidates) != 1 || !reflect.DeepEqual(TorrentDetails(r, d), res.Candidates[0].Torrents[0]) {
		t.Errorf("TorrentDetails = %+v, want what the torrent row shows: %+v", TorrentDetails(r, d), res.Candidates)
	}
	if !TorrentDetails(r, d).BelowSeedMin {
		t.Error("a ratio of 1 against a minimum of 2 is not flagged")
	}
}
