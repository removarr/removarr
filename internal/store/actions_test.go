package store

import (
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

func action(planID string, at time.Time) domain.Action {
	return domain.Action{
		PlanID: planID, At: at, Outcome: domain.OutcomeDone, Size: 700,
		Rows: []domain.ActionRow{{List: domain.ListMovies, Key: "tmdb:1", Title: "One", Size: 700}},
		Steps: []domain.StepResult{
			{Manager: &domain.ManagerStep{Manager: "movies", Action: domain.StepRemoveMovie, ItemID: 1, RowKey: "tmdb:1"}, Result: domain.ResultDone},
			{Torrent: &domain.TorrentInfo{ID: "h1", Name: "One.Torrent"}, Result: domain.ResultDone},
		},
	}
}

func TestRecordActionStoresItAndDropsTheDeletedRowsFromTheLatestScan(t *testing.T) {
	s := open(t)
	at := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	id, err := s.BeginScan(ctx, at)
	if err != nil {
		t.Fatal(err)
	}
	rows := []domain.Candidate{
		{List: domain.ListMovies, Key: "tmdb:1", Torrents: []domain.TorrentInfo{}},
		{List: domain.ListMovies, Key: "tmdb:2", Torrents: []domain.TorrentInfo{}},
		{List: domain.ListTorrents, Key: "hash:h1", Torrents: []domain.TorrentInfo{}},
	}
	if err := s.FinishScan(ctx, id, at, domain.ScanSummary{}, rows); err != nil {
		t.Fatal(err)
	}
	a := action("p1", at)
	got, err := s.RecordAction(ctx, a, []domain.RowRef{{List: domain.ListMovies, Key: "tmdb:1"}, {List: domain.ListTorrents, Key: "hash:h1"}})
	if err != nil {
		t.Fatal(err)
	}
	latest, err := s.ReadLatest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(latest.Rows) != 1 || latest.Rows[0].Key != "tmdb:2" {
		t.Errorf("rows left = %+v, want only tmdb:2", latest.Rows)
	}
	list, err := s.Actions(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	a.ID = got
	if len(list) != 1 || !reflect.DeepEqual(list[0], a) {
		t.Errorf("history = %+v, want %+v", list, a)
	}
}

func TestRecordActionWithoutAScanStillRecords(t *testing.T) {
	s := open(t)
	if _, err := s.RecordAction(ctx, action("p1", time.Now().UTC()), []domain.RowRef{{List: domain.ListMovies, Key: "tmdb:1"}}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT COUNT(*) FROM actions`); n != 1 {
		t.Errorf("%d records, want 1", n)
	}
}

func TestActionsAreNewestFirstAndPaged(t *testing.T) {
	s := open(t)
	at := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	var ids []int64
	for i := 0; i < 5; i++ {
		id, err := s.RecordAction(ctx, action("p1", at.Add(time.Duration(i)*time.Minute)), nil)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	page, err := s.Actions(ctx, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page[0].ID != ids[4] || page[1].ID != ids[3] {
		t.Fatalf("first page = %v, want the two newest", idsOf(page))
	}
	page, _ = s.Actions(ctx, page[1].ID, 2)
	if len(page) != 2 || page[0].ID != ids[2] || page[1].ID != ids[1] {
		t.Errorf("second page = %v, want the next two", idsOf(page))
	}
	if empty, _ := s.Actions(ctx, ids[0], 2); empty == nil || len(empty) != 0 {
		t.Errorf("after the oldest = %#v, want an empty list", empty)
	}
}

func idsOf(as []domain.Action) []int64 {
	var out []int64
	for _, a := range as {
		out = append(out, a.ID)
	}
	return out
}
