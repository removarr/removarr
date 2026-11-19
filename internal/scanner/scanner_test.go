package scanner

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

var (
	ctx = context.Background()
	now = time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
)

type fakeServer struct {
	lib   domain.Library
	err   error
	calls int
}

func (f *fakeServer) Users(context.Context) ([]domain.User, error) {
	return []domain.User{{ID: "a", Name: "Alice"}}, f.err
}

func (f *fakeServer) Library(context.Context, []string) (domain.Library, error) {
	f.calls++
	return f.lib, f.err
}

type fakeMovies struct {
	snap domain.MovieSnapshot
	err  error
}

func (f *fakeMovies) Movies(context.Context) (domain.MovieSnapshot, error) { return f.snap, f.err }

type fakeSeries struct {
	snap domain.SeriesSnapshot
	err  error
}

func (f *fakeSeries) Series(context.Context) (domain.SeriesSnapshot, error) { return f.snap, f.err }

type fakeDownloads struct {
	list      []domain.Download
	err       error
	files     map[string][]domain.DownloadFile // download ID -> its files
	filesErr  error
	fileCalls []string      // the download IDs Files was asked for, in order
	entered   chan struct{} // when set, receives once Downloads has been called
	release   chan struct{} // when set, Downloads waits until it is closed
}

func (f *fakeDownloads) Files(_ context.Context, id string) ([]domain.DownloadFile, error) {
	f.fileCalls = append(f.fileCalls, id)
	return f.files[id], f.filesErr
}

func (f *fakeDownloads) Downloads(context.Context) ([]domain.Download, error) {
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.release != nil {
		<-f.release
	}
	return f.list, f.err
}

type fakeStore struct {
	begun, finished int
	summary         domain.ScanSummary
	candidates      []domain.Candidate
}

func (f *fakeStore) BeginScan(context.Context, time.Time) (int64, error) {
	f.begun++
	return int64(f.begun), nil
}

func (f *fakeStore) FinishScan(_ context.Context, _ int64, _ time.Time, s domain.ScanSummary, c []domain.Candidate) error {
	f.finished++
	f.summary, f.candidates = s, c
	return nil
}

// world returns providers for a small library: one watched movie backed by torrent h1, one
// watched ended series, and one torrent no media manager knows.
func world() (Providers, *fakeServer) {
	seen := map[string]domain.UserState{"a": {Played: true}}
	server := &fakeServer{lib: domain.Library{
		Movies:   []domain.ServerMovie{{ID: "m1", IDs: domain.ExternalIDs{TMDB: "603"}, States: seen}},
		Series:   []domain.ServerSeries{{ID: "s1", IDs: domain.ExternalIDs{TVDB: "100"}}},
		Episodes: []domain.ServerEpisode{{SeriesID: "s1", Season: 1, Episode: 1, States: seen}},
	}}
	movies := &fakeMovies{snap: domain.MovieSnapshot{
		Movies: []domain.Movie{{
			ID: 1, Title: "The Matrix", IDs: domain.ExternalIDs{TMDB: "603"},
			File: &domain.MediaFile{ID: 10, Size: 5000},
		}},
		Links: domain.Links{
			Grabbed:    map[string][]int{"h1": {1}},
			FileSource: map[int]string{10: "h1"},
			Queue:      map[string]bool{},
		},
		RootFolders: []domain.RootFolder{{Path: "/media/movies", FreeBytes: 100}},
	}}
	series := &fakeSeries{snap: domain.SeriesSnapshot{
		Series: []domain.Series{{
			ID: 1, Title: "The Show", Ended: true, IDs: domain.ExternalIDs{TVDB: "100"},
			Episodes: []domain.Episode{{ID: 11, Season: 1, Number: 1, File: &domain.MediaFile{ID: 11, Size: 900}}},
		}},
		Links:       domain.Links{Grabbed: map[string][]int{}, FileSource: map[int]string{}, Queue: map[string]bool{}},
		RootFolders: []domain.RootFolder{{Path: "/media/tv", FreeBytes: 200}, {Path: "/media/movies", FreeBytes: 100}},
	}}
	downloads := &fakeDownloads{list: []domain.Download{{ID: "h1", Name: "The.Matrix"}, {ID: "orphan1", Name: "Unknown"}}}
	return Providers{MediaServer: server, Movies: movies, Series: series, Downloads: downloads}, server
}

func withUser() domain.Rules {
	r := domain.DefaultRules()
	r.CountingUsers = []string{"a"}
	return r
}

func service(p Providers, r domain.Rules, st *fakeStore) *Service {
	return &Service{
		Build: func(context.Context) (Providers, error) { return p, nil },
		Rules: func(context.Context) (domain.Rules, error) { return r, nil },
		Store: st,
		Now:   func() time.Time { return now },
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// run performs one scan and returns what was stored.
func run(t *testing.T, p Providers, r domain.Rules) *fakeStore {
	t.Helper()
	st := &fakeStore{}
	if err := service(p, r, st).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if st.begun != 1 || st.finished != 1 {
		t.Fatalf("scan recorded %d starts and %d finishes, want 1 and 1", st.begun, st.finished)
	}
	return st
}

// expect checks one list's summary: its row count when it was produced, or that its error
// mentions wantError when it was not.
func expect(t *testing.T, st *fakeStore, list domain.ListKind, rows int, wantError string) {
	t.Helper()
	got := st.summary.Lists[list]
	if wantError == "" {
		if got.Error != "" || got.Candidates != rows {
			t.Errorf("%s: %d rows, error %q; want %d rows and no error", list, got.Candidates, got.Error, rows)
		}
		return
	}
	if !strings.Contains(got.Error, wantError) || got.Candidates != 0 {
		t.Errorf("%s: %d rows, error %q; want no rows and an error mentioning %q", list, got.Candidates, got.Error, wantError)
	}
	for _, c := range st.candidates {
		if c.List == list {
			t.Errorf("%s: row %s stored although the list failed", list, c.Key)
		}
	}
}

func TestRunProducesAllThreeLists(t *testing.T) {
	p, _ := world()
	st := run(t, p, withUser())

	expect(t, st, domain.ListMovies, 1, "")
	expect(t, st, domain.ListSeries, 1, "")
	expect(t, st, domain.ListTorrents, 1, "")

	keys := map[string]bool{}
	for _, c := range st.candidates {
		keys[c.Key] = true
	}
	for _, want := range []string{"tmdb:603", "tvdb:100", "hash:orphan1"} {
		if !keys[want] {
			t.Errorf("row %s missing; stored rows: %v", want, keys)
		}
	}
	if len(st.candidates) != 3 {
		t.Errorf("%d rows stored, want 3", len(st.candidates))
	}
	if len(st.summary.RootFolders) != 2 {
		t.Errorf("RootFolders = %+v, want /media/movies and /media/tv once each", st.summary.RootFolders)
	}
}

func TestRunWithoutCountingUsers(t *testing.T) {
	p, server := world()
	st := run(t, p, domain.DefaultRules())

	expect(t, st, domain.ListMovies, 0, "No counting users")
	expect(t, st, domain.ListSeries, 0, "No counting users")
	expect(t, st, domain.ListTorrents, 1, "")
	if server.calls != 0 {
		t.Errorf("the media server library was read %d times, want 0", server.calls)
	}
}

func TestRunWhenAMediaManagerFails(t *testing.T) {
	p, _ := world()
	p.Movies = &fakeMovies{err: errors.New("timeout")}
	st := run(t, p, withUser())

	expect(t, st, domain.ListMovies, 0, "movie manager could not be read: timeout")
	expect(t, st, domain.ListSeries, 1, "")
	// h1 belongs to a movie. With the movie manager down it would look like an orphan.
	expect(t, st, domain.ListTorrents, 0, "movie manager could not be read")
}

func TestRunWithOnlyOneMediaManagerConfigured(t *testing.T) {
	p, _ := world()
	p.Movies = nil
	st := run(t, p, withUser())

	expect(t, st, domain.ListMovies, 0, "movie manager is not configured")
	expect(t, st, domain.ListSeries, 1, "")
	expect(t, st, domain.ListTorrents, 2, "") // nothing manages h1 any more
}

func TestRunWithoutAnyMediaManager(t *testing.T) {
	p, _ := world()
	p.Movies, p.Series = nil, nil
	st := run(t, p, withUser())

	expect(t, st, domain.ListTorrents, 0, "No media manager is configured")
}

func TestRunWhenTheDownloadClientFails(t *testing.T) {
	p, _ := world()
	p.Downloads = &fakeDownloads{err: errors.New("connection refused")}
	st := run(t, p, withUser())

	for _, list := range []domain.ListKind{domain.ListMovies, domain.ListSeries, domain.ListTorrents} {
		expect(t, st, list, 0, "download client could not be read: connection refused")
	}
}

func TestRunWithoutAMediaServer(t *testing.T) {
	p, _ := world()
	p.MediaServer = nil
	st := run(t, p, withUser())

	expect(t, st, domain.ListMovies, 0, "media server is not configured")
	expect(t, st, domain.ListSeries, 0, "media server is not configured")
	expect(t, st, domain.ListTorrents, 1, "")
}

func TestRunWhenTheMediaServerFails(t *testing.T) {
	p, server := world()
	server.err = errors.New("401 Unauthorized")
	st := run(t, p, withUser())

	expect(t, st, domain.ListMovies, 0, "media server could not be read: 401 Unauthorized")
	expect(t, st, domain.ListTorrents, 1, "")
}

func waitIdle(t *testing.T, s *Service) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for s.Status().Running {
		if time.Now().After(deadline) {
			t.Fatal("the scan did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestOnlyOneScanAtATime(t *testing.T) {
	p, _ := world()
	dl := &fakeDownloads{entered: make(chan struct{}), release: make(chan struct{})}
	p.Downloads = dl
	st := &fakeStore{}
	svc := service(p, withUser(), st)

	if err := svc.Start(); err != nil {
		t.Fatal(err)
	}
	<-dl.entered // the scan is now waiting inside the download client

	if status := svc.Status(); !status.Running || status.Phase == "" || status.StartedAt == nil {
		t.Errorf("status during a scan = %+v", status)
	}
	if err := svc.Start(); !errors.Is(err, ErrRunning) {
		t.Errorf("second Start = %v, want ErrRunning", err)
	}
	if err := svc.Run(ctx); !errors.Is(err, ErrRunning) {
		t.Errorf("Run during a scan = %v, want ErrRunning", err)
	}

	close(dl.release)
	waitIdle(t, svc)
	if st.finished != 1 {
		t.Errorf("%d scans finished, want 1", st.finished)
	}
	if status := svc.Status(); status.Running || status.LastError != "" {
		t.Errorf("status after a scan = %+v", status)
	}

	dl.entered, dl.release = nil, nil
	if err := svc.Run(ctx); err != nil {
		t.Errorf("a new scan after the first finished: %v", err)
	}
}

func TestAScanThatCannotStartIsReported(t *testing.T) {
	p, _ := world()
	st := &fakeStore{}
	svc := service(p, withUser(), st)
	svc.Rules = func(context.Context) (domain.Rules, error) { return domain.Rules{}, errors.New("database is locked") }

	if err := svc.Run(ctx); err == nil {
		t.Fatal("want an error")
	}
	if st.begun != 0 {
		t.Error("no scan must be recorded when the rules cannot be loaded")
	}
	status := svc.Status()
	if status.Running || !strings.Contains(status.LastError, "database is locked") {
		t.Errorf("status = %+v, want idle with the error kept", status)
	}
}

const mb = int64(1) << 20

// manualWorld is world() after a manual import: history no longer links the movie's file
// to torrent h1, but h1 still holds a file of exactly the movie file's size.
func manualWorld() (Providers, *fakeDownloads) {
	p, _ := world()
	movies := p.Movies.(*fakeMovies)
	movies.snap.Movies[0].File = &domain.MediaFile{ID: 10, Size: 700 * mb}
	movies.snap.Links = domain.Links{Grabbed: map[string][]int{}, FileSource: map[int]string{}, Queue: map[string]bool{}}
	downloads := p.Downloads.(*fakeDownloads)
	downloads.files = map[string][]domain.DownloadFile{
		"h1":      {{Name: "The.Matrix.mkv", Size: 700 * mb}, {Name: "The.Matrix.nfo", Size: 900}},
		"orphan1": {{Name: "Unknown.mkv", Size: 650 * mb}},
	}
	return p, downloads
}

func TestRunLinksATorrentToAManuallyImportedFileByContent(t *testing.T) {
	p, downloads := manualWorld()
	st := run(t, p, withUser())

	expect(t, st, domain.ListMovies, 1, "")
	expect(t, st, domain.ListTorrents, 1, "")
	sawMovie := false
	for _, c := range st.candidates {
		switch c.Key {
		case "hash:h1":
			t.Errorf("h1 is listed as %s although it holds the movie's file", c.Reason)
		case "tmdb:603":
			sawMovie = true
			if len(c.Torrents) != 1 || c.Torrents[0].ID != "h1" {
				t.Errorf("deleting the movie would remove torrents %+v, want h1", c.Torrents)
			}
		}
	}
	if !sawMovie {
		t.Error("the movie row is missing")
	}
	if !reflect.DeepEqual(downloads.fileCalls, []string{"h1", "orphan1"}) {
		t.Errorf("files were read for %v, want h1 and orphan1: history links neither", downloads.fileCalls)
	}
}

func TestRunReadsFilesOnlyForDownloadsHistoryDoesNotLink(t *testing.T) {
	p, _ := world() // history links h1 to the movie's file
	// h1 is larger than the movie's file, but by less than 50 MB: it holds no further
	// library file, so it is not asked.
	p.Movies.(*fakeMovies).snap.Movies[0].File = &domain.MediaFile{ID: 10, Size: 700 * mb}
	downloads := p.Downloads.(*fakeDownloads)
	downloads.list[0].Size = 750*mb - 1
	downloads.list = append(downloads.list,
		domain.Download{ID: "queued1", Name: "Still.Importing"},
		domain.Download{ID: "game1", Name: "A.Game", Category: "games"},
	)
	p.Series.(*fakeSeries).snap.Links.Queue["queued1"] = true
	r := withUser()
	r.ExcludedCategories = []string{"games"}
	run(t, p, r)

	if !reflect.DeepEqual(downloads.fileCalls, []string{"orphan1"}) {
		t.Errorf("files were read for %v, want only orphan1", downloads.fileCalls)
	}
}

// mixedPackWorld returns providers for an ended two-season series held by one pack. History
// links the pack to season 1's file only; season 2's file was imported from the pack by
// hand, so history names no source for it. unwatched lists the seasons nobody has played.
func mixedPackWorld(unwatched ...int) (Providers, *fakeDownloads) {
	states := func(season int) map[string]domain.UserState {
		for _, s := range unwatched {
			if s == season {
				return map[string]domain.UserState{"a": {}}
			}
		}
		return map[string]domain.UserState{"a": {Played: true}}
	}
	server := &fakeServer{lib: domain.Library{
		Series: []domain.ServerSeries{{ID: "s1", IDs: domain.ExternalIDs{TVDB: "200"}}},
		Episodes: []domain.ServerEpisode{
			{SeriesID: "s1", Season: 1, Episode: 1, States: states(1)},
			{SeriesID: "s1", Season: 2, Episode: 1, States: states(2)},
		},
	}}
	series := &fakeSeries{snap: domain.SeriesSnapshot{
		Series: []domain.Series{{
			ID: 7, Title: "The Pack Show", Ended: true, IDs: domain.ExternalIDs{TVDB: "200"},
			Episodes: []domain.Episode{
				{ID: 21, Season: 1, Number: 1, File: &domain.MediaFile{ID: 31, Size: 800 * mb}},
				{ID: 22, Season: 2, Number: 1, File: &domain.MediaFile{ID: 32, Size: 900 * mb}},
			},
		}},
		Links: domain.Links{
			Grabbed:    map[string][]int{"pack": {21}},
			FileSource: map[int]string{31: "pack"},
			Queue:      map[string]bool{},
		},
	}}
	downloads := &fakeDownloads{
		list: []domain.Download{{ID: "pack", Name: "The.Pack.Show.S01-S02", Size: 1700*mb + 4096}},
		files: map[string][]domain.DownloadFile{"pack": {
			{Name: "S01E01.mkv", Size: 800 * mb},
			{Name: "S02E01.mkv", Size: 900 * mb},
			{Name: "pack.nfo", Size: 4096},
		}},
	}
	return Providers{MediaServer: server, Series: series, Downloads: downloads}, downloads
}

func TestRunAsksALinkedPackThatHoldsMoreThanHistorySays(t *testing.T) {
	p, downloads := mixedPackWorld()
	st := run(t, p, withUser())

	if !reflect.DeepEqual(downloads.fileCalls, []string{"pack"}) {
		t.Errorf("files were read for %v, want the pack: it is 900 MB larger than the file history links to it", downloads.fileCalls)
	}
	expectRow(t, st.candidates, domain.SeriesKey("200"), "pack")
	expectNoTorrentRows(t, st.summary, st.candidates)
}

func TestRunKeepsAMixedPackWhileItBacksAnotherSeason(t *testing.T) {
	p, _ := mixedPackWorld(2)
	r := withUser()
	r.SeriesUnit = domain.UnitSeason
	st := run(t, p, r)

	// The pack still holds season 2's file, which was imported from it by hand.
	expectRow(t, st.candidates, domain.SeasonKey("200", 1))
	expectNoTorrentRows(t, st.summary, st.candidates)
}

// A file list that cannot be read is an incomplete answer from the download client
// (spec 6.8, rule 5): no list may be produced from it.
func TestRunFailsEveryListWhenFilesCannotBeRead(t *testing.T) {
	p, downloads := manualWorld()
	downloads.filesErr = errors.New("boom")
	st := run(t, p, withUser())

	for _, list := range []domain.ListKind{domain.ListMovies, domain.ListSeries, domain.ListTorrents} {
		expect(t, st, list, 0, "download client could not be read: boom")
	}
	if len(downloads.fileCalls) != 1 {
		t.Errorf("files were read %d times, want the scan to stop asking after the first failure", len(downloads.fileCalls))
	}
}

func TestRunDoesNotReadFilesWhenNoMediaManagerAnswered(t *testing.T) {
	p, downloads := manualWorld()
	p.Movies = &fakeMovies{err: errors.New("timeout")}
	p.Series = nil
	run(t, p, withUser())

	if len(downloads.fileCalls) != 0 {
		t.Errorf("files were read for %v, want none: without a media manager there is nothing to match", downloads.fileCalls)
	}
}

func TestComputeLinksAMultiEpisodeFileByContent(t *testing.T) {
	file := &domain.MediaFile{ID: 50, Size: 900 * mb}
	f := Fetched{
		Series: Source[domain.SeriesSnapshot]{Configured: true, Value: domain.SeriesSnapshot{
			Series: []domain.Series{{
				ID: 1, Title: "The Show", Ended: true, IDs: domain.ExternalIDs{TVDB: "100"},
				Episodes: []domain.Episode{
					{ID: 11, Season: 1, Number: 1, File: file},
					{ID: 12, Season: 1, Number: 2, File: file},
				},
			}},
			Links: domain.Links{Grabbed: map[string][]int{}, FileSource: map[int]string{}, Queue: map[string]bool{}},
		}},
		Downloads: Source[[]domain.Download]{Configured: true, Value: []domain.Download{{ID: "double", Name: "The.Show.S01E01E02"}}},
		Files:     map[string][]domain.DownloadFile{"double": {{Name: "The.Show.S01E01E02.mkv", Size: 900 * mb}}},
	}

	summary, candidates := Compute(domain.DefaultRules(), now, f)

	if got := summary.Lists[domain.ListTorrents]; got.Error != "" || got.Candidates != 0 || len(candidates) != 0 {
		t.Errorf("torrent list = %+v with rows %+v, want none: the torrent holds the episodes' file", got, candidates)
	}
	if len(f.Series.Value.Links.FileSource) != 0 || len(f.Series.Value.Links.Grabbed) != 0 {
		t.Errorf("Compute changed the links it was given: %+v", f.Series.Value.Links)
	}
}

// noLinks returns links in which history knows nothing.
func noLinks() domain.Links {
	return domain.Links{Grabbed: map[string][]int{}, FileSource: map[int]string{}, Queue: map[string]bool{}}
}

// crossWorld is fetched data in which the movie manager and the series manager number their
// files and items from 1 alike: movie 1 has file 10, and episode 1 has a file 10 too. History
// names no source for any library file. Torrent "movie" holds the movie's file; "pack" holds
// every file of the two-season series, including the file that episodes 2 and 3 share.
// unwatched lists the seasons whose episodes nobody has played.
func crossWorld(unwatched ...int) Fetched {
	played := map[string]domain.UserState{"a": {Played: true}}
	states := func(season int) map[string]domain.UserState {
		for _, s := range unwatched {
			if s == season {
				return map[string]domain.UserState{"a": {}}
			}
		}
		return played
	}
	shared := &domain.MediaFile{ID: 11, Size: 900 * mb}
	return Fetched{
		Users: []domain.User{{ID: "a", Name: "Alice"}},
		Library: Source[domain.Library]{Configured: true, Value: domain.Library{
			Movies: []domain.ServerMovie{{ID: "m1", IDs: domain.ExternalIDs{TMDB: "603"}, States: played}},
			Series: []domain.ServerSeries{{ID: "s1", IDs: domain.ExternalIDs{TVDB: "100"}}},
			Episodes: []domain.ServerEpisode{
				{SeriesID: "s1", Season: 1, Episode: 1, States: states(1)},
				{SeriesID: "s1", Season: 1, Episode: 2, EpisodeEnd: 3, States: states(1)},
				{SeriesID: "s1", Season: 2, Episode: 1, States: states(2)},
			},
		}},
		Movies: Source[domain.MovieSnapshot]{Configured: true, Value: domain.MovieSnapshot{
			Movies: []domain.Movie{{
				ID: 1, Title: "The Matrix", IDs: domain.ExternalIDs{TMDB: "603"},
				File: &domain.MediaFile{ID: 10, Size: 700 * mb},
			}},
			Links: noLinks(),
		}},
		Series: Source[domain.SeriesSnapshot]{Configured: true, Value: domain.SeriesSnapshot{
			Series: []domain.Series{{
				ID: 5, Title: "The Show", Ended: true, IDs: domain.ExternalIDs{TVDB: "100"},
				Episodes: []domain.Episode{
					{ID: 1, Season: 1, Number: 1, File: &domain.MediaFile{ID: 10, Size: 800 * mb}},
					{ID: 2, Season: 1, Number: 2, File: shared},
					{ID: 3, Season: 1, Number: 3, File: shared},
					{ID: 4, Season: 2, Number: 1, File: &domain.MediaFile{ID: 12, Size: 1000 * mb}},
				},
			}},
			Links: noLinks(),
		}},
		Downloads: Source[[]domain.Download]{Configured: true, Value: []domain.Download{
			{ID: "movie", Name: "The.Matrix", Size: 700 * mb},
			{ID: "pack", Name: "The.Show.S01-S02", Size: 2700*mb + 4096},
		}},
		Files: map[string][]domain.DownloadFile{
			"movie": {{Name: "The.Matrix.mkv", Size: 700 * mb}},
			"pack": {
				{Name: "S01E01.mkv", Size: 800 * mb},
				{Name: "S01E02E03.mkv", Size: 900 * mb},
				{Name: "S02E01.mkv", Size: 1000 * mb},
				{Name: "pack.nfo", Size: 4096},
			},
		},
	}
}

// rowsByKey returns the rows by key.
func rowsByKey(candidates []domain.Candidate) map[string]domain.Candidate {
	out := map[string]domain.Candidate{}
	for _, c := range candidates {
		out[c.Key] = c
	}
	return out
}

// expectRow checks that the row with key exists and names exactly the torrents want.
func expectRow(t *testing.T, candidates []domain.Candidate, key string, want ...string) {
	t.Helper()
	c, found := rowsByKey(candidates)[key]
	if !found {
		t.Errorf("row %s is missing; rows: %v", key, rowsByKey(candidates))
		return
	}
	got := []string{}
	for _, torrent := range c.Torrents {
		got = append(got, torrent.ID)
	}
	if want == nil {
		want = []string{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("row %s names torrents %v, want %v", key, got, want)
	}
}

// expectNoTorrentRows checks that the torrent list was produced and lists nothing.
func expectNoTorrentRows(t *testing.T, summary domain.ScanSummary, candidates []domain.Candidate) {
	t.Helper()
	if got := summary.Lists[domain.ListTorrents]; got.Error != "" || got.Candidates != 0 {
		t.Errorf("torrent list = %+v, want no rows and no error", got)
	}
	for _, c := range candidates {
		if c.List == domain.ListTorrents {
			t.Errorf("torrent %s is listed as %s although it holds library files", c.Key, c.Reason)
		}
	}
}

func TestComputeLinksByContentPerManagerWhenIDsAreShared(t *testing.T) {
	summary, candidates := Compute(withUser(), now, crossWorld())

	expectRow(t, candidates, "tmdb:603", "movie")
	expectRow(t, candidates, domain.SeriesKey("100"), "pack")
	expectNoTorrentRows(t, summary, candidates)
}

func TestComputeKeepsAPackThatStillBacksAnotherSeason(t *testing.T) {
	r := withUser()
	r.SeriesUnit = domain.UnitSeason
	summary, candidates := Compute(r, now, crossWorld(2))

	expectRow(t, candidates, domain.SeasonKey("100", 1))
	if _, found := rowsByKey(candidates)[domain.SeasonKey("100", 2)]; found {
		t.Error("season 2 has a row although nobody watched it")
	}
	expectRow(t, candidates, "tmdb:603", "movie")
	expectNoTorrentRows(t, summary, candidates)
}

// The episodes of one file are normally in one season, so every row that deletes the file
// holds all of them. This file spans two seasons, so the season 2 row holds only the file's
// second episode: the torrent goes with that row only if content associates it with that
// episode too.
func TestComputeAssociatesEveryEpisodeOfASharedFileByContent(t *testing.T) {
	shared := &domain.MediaFile{ID: 30, Size: 900 * mb}
	f := Fetched{
		Users: []domain.User{{ID: "a", Name: "Alice"}},
		Library: Source[domain.Library]{Configured: true, Value: domain.Library{
			Series: []domain.ServerSeries{{ID: "s1", IDs: domain.ExternalIDs{TVDB: "100"}}},
			Episodes: []domain.ServerEpisode{
				{SeriesID: "s1", Season: 1, Episode: 1, States: map[string]domain.UserState{"a": {}}},
				{SeriesID: "s1", Season: 2, Episode: 1, States: map[string]domain.UserState{"a": {Played: true}}},
			},
		}},
		Series: Source[domain.SeriesSnapshot]{Configured: true, Value: domain.SeriesSnapshot{
			Series: []domain.Series{{
				ID: 5, Title: "The Show", Ended: true, IDs: domain.ExternalIDs{TVDB: "100"},
				Episodes: []domain.Episode{
					{ID: 1, Season: 1, Number: 1, File: shared},
					{ID: 2, Season: 2, Number: 1, File: shared},
				},
			}},
			Links: noLinks(),
		}},
		Downloads: Source[[]domain.Download]{Configured: true, Value: []domain.Download{{ID: "double", Name: "The.Show.Double"}}},
		Files:     map[string][]domain.DownloadFile{"double": {{Name: "double.mkv", Size: 900 * mb}}},
	}
	r := withUser()
	r.SeriesUnit = domain.UnitSeason
	_, candidates := Compute(r, now, f)

	expectRow(t, candidates, domain.SeasonKey("100", 2), "double")
}
