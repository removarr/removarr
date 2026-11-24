package executor

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
)

func TestAnItemRunsItsManagerStepsBeforeItsTorrents(t *testing.T) {
	a := newApps(t, dl("h1", "/dl/one"))
	act, drop := newRun(a.providers(), false).item(ctx, movieItem(7, "tmdb:1", 700, torrent("h1")))
	want := []string{"remove movie 7 exclusion=false", "read downloads", "remove torrent h1 data=true"}
	if !reflect.DeepEqual(a.log, want) {
		t.Errorf("calls = %q, want %q", a.log, want)
	}
	if act.Outcome != domain.OutcomeDone || act.Size != 700 || len(act.Rows) != 1 {
		t.Errorf("record = %+v", act)
	}
	wantDrop := []domain.RowRef{{List: domain.ListMovies, Key: "tmdb:1"}, {List: domain.ListTorrents, Key: "hash:h1"}}
	if !reflect.DeepEqual(drop, wantDrop) {
		t.Errorf("drop = %+v, want %+v", drop, wantDrop)
	}
}

func TestAFailedStepStopsTheItemAndKeepsItsTorrents(t *testing.T) {
	a := newApps(t, dl("h1", "/dl/one"))
	a.fail["unmonitor seasons 1 [1]"] = errors.New("unexpected status 500")
	it := domain.PlanItem{
		Rows: []domain.PlanRow{{Row: domain.Candidate{List: domain.ListSeries, Key: "tvdb:100:s1"}}},
		Steps: []domain.ManagerStep{
			{Manager: "series", Action: domain.StepUnmonitorEpisodes, ItemID: 1, EpisodeIDs: []int{11}, RowKey: "tvdb:100:s1"},
			{Manager: "series", Action: domain.StepUnmonitorSeasons, ItemID: 1, Seasons: []int{1}, RowKey: "tvdb:100:s1"},
			{Manager: "series", Action: domain.StepDeleteEpisodeFiles, ItemID: 1, FileIDs: []int{111}, RowKey: "tvdb:100:s1"},
		},
		Torrents: []domain.PlanTorrent{torrent("h1")},
	}
	act, drop := newRun(a.providers(), false).item(ctx, it)
	if want := []string{"unmonitor episodes [11]", "unmonitor seasons 1 [1]"}; !reflect.DeepEqual(a.log, want) {
		t.Errorf("calls = %q, want %q: nothing after the failure", a.log, want)
	}
	want := []string{domain.ResultDone, domain.ResultFailed, domain.ResultSkipped, domain.ResultSkipped}
	if !reflect.DeepEqual(results(act), want) || act.Outcome != domain.OutcomeFailed {
		t.Errorf("results = %q, outcome %s; want %q, failed", results(act), act.Outcome, want)
	}
	if !strings.Contains(act.Steps[1].Error, "500") {
		t.Errorf("error = %q, want the app's answer", act.Steps[1].Error)
	}
	if len(drop) != 0 {
		t.Errorf("drop = %+v, want nothing: the row was not deleted", drop)
	}
}

func TestATiedRowWhoseStepsSucceededIsDroppedWhenAnotherFails(t *testing.T) {
	a := newApps(t)
	a.fail["remove series 2 exclusion=false"] = errors.New("unexpected status 404")
	it := domain.PlanItem{
		Rows: []domain.PlanRow{
			{Row: domain.Candidate{List: domain.ListMovies, Key: "tmdb:1"}},
			{Row: domain.Candidate{List: domain.ListSeries, Key: "tvdb:2"}, Added: true},
		},
		Steps: []domain.ManagerStep{
			{Manager: "movies", Action: domain.StepRemoveMovie, ItemID: 1, RowKey: "tmdb:1"},
			{Manager: "series", Action: domain.StepRemoveSeries, ItemID: 2, RowKey: "tvdb:2"},
		},
		Torrents: []domain.PlanTorrent{torrent("h1")},
	}
	_, drop := newRun(a.providers(), false).item(ctx, it)
	if want := []domain.RowRef{{List: domain.ListMovies, Key: "tmdb:1"}}; !reflect.DeepEqual(drop, want) {
		t.Errorf("drop = %+v, want only the movie", drop)
	}
	for _, c := range a.log {
		if strings.HasPrefix(c, "remove torrent") || c == "read downloads" {
			t.Errorf("the shared torrent was touched: %q", a.log)
		}
	}
}

func TestATorrentNoLongerInTheClientIsGone(t *testing.T) {
	a := newApps(t) // the client holds nothing
	act, drop := newRun(a.providers(), false).item(ctx, movieItem(7, "tmdb:1", 0, torrent("h1")))
	if got := results(act); !reflect.DeepEqual(got, []string{domain.ResultDone, domain.ResultGone}) || act.Outcome != domain.OutcomeDone {
		t.Errorf("results = %q, outcome %s", got, act.Outcome)
	}
	for _, c := range a.log {
		if strings.HasPrefix(c, "remove torrent") {
			t.Errorf("a removal was sent for a torrent that is gone: %q", a.log)
		}
	}
	if len(drop) != 2 {
		t.Errorf("drop = %+v, want the movie and the torrent row", drop)
	}
}

func TestADownloadListThatCannotBeReadFailsTheTorrents(t *testing.T) {
	a := newApps(t, dl("h1", "/dl/one"))
	a.readErr = errors.New("connection refused")
	act, _ := newRun(a.providers(), false).item(ctx, movieItem(7, "tmdb:1", 0, torrent("h1"), torrent("h2")))
	want := []string{domain.ResultDone, domain.ResultFailed, domain.ResultFailed}
	if !reflect.DeepEqual(results(act), want) || act.Outcome != domain.OutcomeFailed || !strings.Contains(act.Steps[1].Error, "connection refused") {
		t.Errorf("record = %+v", act)
	}
}

func TestTheLastOfTwoTorrentsWithOneContentPathTakesTheData(t *testing.T) {
	a := newApps(t, dl("h1", "/dl/one"), dl("h2", "/dl/one"))
	newRun(a.providers(), false).item(ctx, movieItem(7, "tmdb:1", 0, torrent("h1"), torrent("h2")))
	want := []string{"remove movie 7 exclusion=false", "read downloads", "remove torrent h1 data=false", "remove torrent h2 data=true"}
	if !reflect.DeepEqual(a.log, want) {
		t.Errorf("calls = %q, want %q", a.log, want)
	}
}

func TestATorrentInsideAnotherGoesFirstAndTheDirectoryGoesWhole(t *testing.T) {
	a := newApps(t, dl("aa", "/dl/pack"), dl("zz", "/dl/pack/one.mkv"))
	newRun(a.providers(), false).item(ctx, movieItem(7, "tmdb:1", 0, torrent("aa"), torrent("zz")))
	want := []string{"remove movie 7 exclusion=false", "read downloads", "remove torrent zz data=false", "remove torrent aa data=true"}
	if !reflect.DeepEqual(a.log, want) {
		t.Errorf("calls = %q, want %q", a.log, want)
	}
}

func TestDataSharedAcrossItemsGoesWithTheLastAndStaysWhenTheFirstFails(t *testing.T) {
	// Both removals succeed: the first keeps the data, the second takes it.
	a := newApps(t, dl("h1", "/dl/one"), dl("h2", "/dl/one"))
	r := newRun(a.providers(), false)
	r.item(ctx, movieItem(1, "tmdb:1", 0, torrent("h1")))
	r.item(ctx, movieItem(2, "tmdb:2", 0, torrent("h2")))
	if !contains(a.log, "remove torrent h1 data=false") || !contains(a.log, "remove torrent h2 data=true") {
		t.Errorf("calls = %q", a.log)
	}

	// The first removal fails: h1 stays, so h2 must keep the data.
	a = newApps(t, dl("h1", "/dl/one"), dl("h2", "/dl/one"))
	a.fail["remove torrent h1 data=false"] = errors.New("unexpected status 500")
	r = newRun(a.providers(), false)
	r.item(ctx, movieItem(1, "tmdb:1", 0, torrent("h1")))
	r.item(ctx, movieItem(2, "tmdb:2", 0, torrent("h2")))
	if !contains(a.log, "remove torrent h2 data=false") {
		t.Errorf("calls = %q, want h2 removed without its data", a.log)
	}
}

func TestThePlansKeepDataHoldsWhenNothingSharesItNow(t *testing.T) {
	a := newApps(t, dl("h1", "/dl/one"))
	keep := domain.PlanTorrent{Torrent: domain.TorrentInfo{ID: "h1"}, KeepData: true}
	act, _ := newRun(a.providers(), false).item(ctx, movieItem(7, "tmdb:1", 0, keep))
	if !contains(a.log, "remove torrent h1 data=false") || !act.Steps[1].KeepData {
		t.Errorf("calls = %q, record %+v: the user confirmed keeping the data", a.log, act.Steps[1])
	}
}

func TestADryRunSendsNothingAndDecidesAsARealRun(t *testing.T) {
	a := newApps(t, dl("h1", "/dl/one"), dl("h2", "/dl/one"))
	a.noWrites = true
	r := newRun(a.providers(), true)
	first, drop := r.item(ctx, movieItem(1, "tmdb:1", 0, torrent("h1")))
	second, _ := r.item(ctx, movieItem(2, "tmdb:2", 0, torrent("h2")))
	if first.Outcome != domain.OutcomeDryRun || !first.DryRun || results(first)[0] != domain.ResultDryRun {
		t.Errorf("first = %+v", first)
	}
	if !first.Steps[1].KeepData || second.Steps[1].KeepData {
		t.Errorf("keepData = %v then %v, want true then false as a real run would decide", first.Steps[1].KeepData, second.Steps[1].KeepData)
	}
	if len(drop) != 0 {
		t.Errorf("drop = %+v, want none in a dry run", drop)
	}
}

func TestAStepOfAnAppThatIsNotConfiguredFailsTheItem(t *testing.T) {
	for _, dry := range []bool{false, true} {
		a := newApps(t)
		p := a.providers()
		p.Movies = nil
		act, _ := newRun(p, dry).item(ctx, movieItem(7, "tmdb:1", 0))
		if act.Outcome != domain.OutcomeFailed || !strings.Contains(act.Steps[0].Error, "not configured") {
			t.Errorf("dry run %v: record = %+v, want failed as not configured", dry, act)
		}
	}
}

func TestATorrentRowIsDroppedWhenItsTorrentGoes(t *testing.T) {
	a := newApps(t, dl("h9", "/dl/orphan"))
	it := domain.PlanItem{
		Rows:     []domain.PlanRow{{Row: domain.Candidate{List: domain.ListTorrents, Key: "hash:h9", Size: 50}}},
		Steps:    []domain.ManagerStep{},
		Torrents: []domain.PlanTorrent{torrent("h9")},
	}
	act, drop := newRun(a.providers(), false).item(ctx, it)
	if act.Outcome != domain.OutcomeDone || act.Size != 50 {
		t.Errorf("record = %+v", act)
	}
	if want := []domain.RowRef{{List: domain.ListTorrents, Key: "hash:h9"}}; !reflect.DeepEqual(drop, want) {
		t.Errorf("drop = %+v, want %+v (once)", drop, want)
	}
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

func TestManagerNamesAreListNames(t *testing.T) {
	if scan.ManagerMovies != string(domain.ListMovies) || scan.ManagerSeries != string(domain.ListSeries) {
		t.Fatal("drops matches a step's manager to a row's list by name")
	}
}

func TestAnUnknownStepFailsTheItemWithoutACall(t *testing.T) {
	for _, s := range []domain.ManagerStep{
		{Manager: "music", Action: domain.StepRemoveMovie, ItemID: 1, RowKey: "tmdb:1"},
		{Manager: "movies", Action: domain.StepRemoveSeries, ItemID: 1, RowKey: "tmdb:1"},
		{Manager: "series", Action: domain.StepRemoveMovie, ItemID: 1, RowKey: "tmdb:1"},
	} {
		a := newApps(t, dl("h1", "/dl/one"))
		it := movieItem(1, "tmdb:1", 0, torrent("h1"))
		it.Steps = []domain.ManagerStep{s}
		act, drop := newRun(a.providers(), false).item(ctx, it)
		if want := []string{domain.ResultFailed, domain.ResultSkipped}; !reflect.DeepEqual(results(act), want) || act.Outcome != domain.OutcomeFailed {
			t.Errorf("%s %s: results = %q, outcome %s", s.Manager, s.Action, results(act), act.Outcome)
		}
		if len(a.log) != 0 || len(drop) != 0 {
			t.Errorf("%s %s: calls = %q, drop = %+v, want none", s.Manager, s.Action, a.log, drop)
		}
	}
}

func TestTorrentsFailWhenTheDownloadClientIsNotConfigured(t *testing.T) {
	for _, nilDownloads := range []bool{true, false} {
		a := newApps(t, dl("h1", "/dl/one"))
		p := a.providers()
		if nilDownloads {
			p.Downloads = nil
		} else {
			p.Remover = nil
		}
		act, drop := newRun(p, false).item(ctx, movieItem(7, "tmdb:1", 0, torrent("h1")))
		if act.Outcome != domain.OutcomeFailed || act.Steps[1].Result != domain.ResultFailed || !strings.Contains(act.Steps[1].Error, "the download client: not configured") {
			t.Errorf("nil downloads %v: record = %+v", nilDownloads, act)
		}
		if want := []string{"remove movie 7 exclusion=false"}; !reflect.DeepEqual(a.log, want) {
			t.Errorf("calls = %q, want %q", a.log, want)
		}
		if want := []domain.RowRef{{List: domain.ListMovies, Key: "tmdb:1"}}; !reflect.DeepEqual(drop, want) {
			t.Errorf("drop = %+v, want the movie only: its torrent stays", drop)
		}
	}
}

func TestAFailedTorrentRemovalSkipsTheRestOfTheItem(t *testing.T) {
	// h3 stays and shares h1's data, so h1 is sent without its data; the plan said with.
	a := newApps(t, dl("h1", "/dl/one/longer"), dl("h2", "/dl/two"), dl("h3", "/dl/one/longer"))
	a.fail["remove torrent h1 data=false"] = errors.New("unexpected status 500")
	act, drop := newRun(a.providers(), false).item(ctx, movieItem(7, "tmdb:1", 0, torrent("h1"), torrent("h2")))
	want := []string{domain.ResultDone, domain.ResultFailed, domain.ResultSkipped}
	if !reflect.DeepEqual(results(act), want) || act.Outcome != domain.OutcomeFailed {
		t.Errorf("results = %q, outcome %s; want %q, failed", results(act), act.Outcome, want)
	}
	if act.Steps[1].KeepData {
		t.Errorf("a failed removal records the plan's keepData, got %+v", act.Steps[1])
	}
	if !contains(a.log, "remove torrent h1 data=false") || contains(a.log, "remove torrent h2 data=true") {
		t.Errorf("calls = %q: nothing after the failure", a.log)
	}
	if want := []domain.RowRef{{List: domain.ListMovies, Key: "tmdb:1"}}; !reflect.DeepEqual(drop, want) {
		t.Errorf("drop = %+v, want the movie only", drop)
	}
}

func TestARowWithoutStepsOfItsOwnGoesOnlyWhenEveryStepOfTheItemWasDone(t *testing.T) {
	item := func() domain.PlanItem {
		return domain.PlanItem{
			Rows: []domain.PlanRow{
				{Row: domain.Candidate{List: domain.ListSeries, Key: "tvdb:1"}},
				{Row: domain.Candidate{List: domain.ListSeries, Key: "tvdb:1:s1"}, Added: true},
			},
			Steps: []domain.ManagerStep{
				{Manager: "series", Action: domain.StepUnmonitorEpisodes, ItemID: 1, EpisodeIDs: []int{11}, RowKey: "tvdb:1"},
				{Manager: "series", Action: domain.StepDeleteEpisodeFiles, ItemID: 1, FileIDs: []int{111}, RowKey: "tvdb:1"},
			},
		}
	}
	a := newApps(t)
	act, drop := newRun(a.providers(), false).item(ctx, item())
	want := []domain.RowRef{{List: domain.ListSeries, Key: "tvdb:1"}, {List: domain.ListSeries, Key: "tvdb:1:s1"}}
	if !reflect.DeepEqual(drop, want) || !act.Rows[1].Added {
		t.Errorf("drop = %+v, want %+v; rows %+v", drop, want, act.Rows)
	}

	a = newApps(t)
	a.fail["delete files [111]"] = errors.New("unexpected status 500")
	if _, drop := newRun(a.providers(), false).item(ctx, item()); len(drop) != 0 {
		t.Errorf("drop = %+v, want nothing when a step failed", drop)
	}
}

func TestADryRunStillReadsTheDownloadList(t *testing.T) {
	a := newApps(t, dl("h1", "/dl/one"))
	a.noWrites = true
	act, _ := newRun(a.providers(), true).item(ctx, movieItem(7, "tmdb:1", 0, torrent("h1")))
	if !reflect.DeepEqual(a.log, []string{"read downloads"}) || !reflect.DeepEqual(results(act), []string{domain.ResultDryRun, domain.ResultDryRun}) {
		t.Errorf("calls = %q, results = %q", a.log, results(act))
	}
}
