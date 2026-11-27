package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/removarr/removarr/internal/api"
	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/executor"
	"github.com/removarr/removarr/internal/gate"
	"github.com/removarr/removarr/internal/planner"
	"github.com/removarr/removarr/internal/providers"
	"github.com/removarr/removarr/internal/scanner"
	"github.com/removarr/removarr/internal/store"
	"github.com/removarr/removarr/internal/webui"
)

const (
	kib = int64(1) << 10
	mib = kib << 10
	gib = mib << 10

	movieTorrent  = "1111111111111111111111111111111111111111"
	orphanTorrent = "2222222222222222222222222222222222222222"
)

// library is the fake Jellyfin, Radarr, Sonarr and qBittorrent in one: it answers the reads
// from its fixture, and the writes change it, so the next scan reflects a deletion. It never
// opens a network connection.
type library struct {
	mu        sync.Mutex
	users     []domain.User
	server    domain.Library
	movies    domain.MovieSnapshot
	series    domain.SeriesSnapshot
	downloads []domain.Download
	files     map[string][]domain.DownloadFile
	calls     []string // the writes received, in order
}

// newLibrary returns the fixture library, its watch dates relative to now.
func newLibrary(now time.Time) *library {
	ago := func(days int) *time.Time {
		t := now.AddDate(0, 0, -days)
		return &t
	}
	played := func(at *time.Time) map[string]domain.UserState {
		return map[string]domain.UserState{"u1": {Played: true, LastPlayed: at}}
	}
	ratio := 2.0
	seeding := 59 * 24 * time.Hour
	orphanRatio := 0.4
	orphanSeeding := 10 * 24 * time.Hour

	l := &library{
		users: []domain.User{{ID: "u1", Name: "viewer"}},
		server: domain.Library{
			Movies: []domain.ServerMovie{
				{ID: "j-movie-1", Title: "Fake Movie One", IDs: domain.ExternalIDs{TMDB: "101"}, States: played(ago(30))},
				{ID: "j-movie-2", Title: "Fake Movie Two", IDs: domain.ExternalIDs{TMDB: "102"}, States: played(nil)},
			},
			Series: []domain.ServerSeries{{ID: "j-series-1", Title: "Fake Series", IDs: domain.ExternalIDs{TVDB: "201"}}},
		},
		movies: domain.MovieSnapshot{
			Movies: []domain.Movie{
				{ID: 1, Title: "Fake Movie One", Year: 2020, IDs: domain.ExternalIDs{TMDB: "101"}, File: &domain.MediaFile{ID: 11, Size: 2 * gib}},
				// A few MiB more than 2 GiB: a file of exactly the size of Fake Movie One's would
				// be linked by content to its torrent (spec 6.6), and tie the two movies.
				{ID: 2, Title: "Fake Movie Two", Year: 2021, IDs: domain.ExternalIDs{TMDB: "102"}, File: &domain.MediaFile{ID: 12, Size: 2*gib + 3*mib}},
			},
			Links: domain.Links{
				Grabbed:    map[string][]int{movieTorrent: {1}},
				FileSource: map[int]string{11: movieTorrent},
				Queue:      map[string]bool{},
			},
			RootFolders: []domain.RootFolder{{Path: "/media/movies", FreeBytes: 500 * gib}},
		},
		series: domain.SeriesSnapshot{
			Links:       domain.Links{Grabbed: map[string][]int{}, FileSource: map[int]string{}, Queue: map[string]bool{}},
			RootFolders: []domain.RootFolder{{Path: "/media/tv", FreeBytes: 400 * gib}},
		},
		downloads: []domain.Download{
			{
				ID: movieTorrent, Name: "Fake.Movie.One.2020.1080p", Size: 3 * gib, State: "uploading", Category: "radarr",
				ContentPath: "/downloads/Fake.Movie.One.2020.1080p", AddedAt: now.AddDate(0, 0, -60),
				Ratio: &ratio, SeedingTime: &seeding, Tracker: "tracker.fake.invalid",
			},
			{
				ID: orphanTorrent, Name: "fake.orphan.torrent", Size: 500 * mib, State: "uploading",
				ContentPath: "/downloads/fake.orphan.torrent", AddedAt: now.AddDate(0, 0, -20),
				Ratio: &orphanRatio, SeedingTime: &orphanSeeding, Tracker: "tracker.fake.invalid",
			},
		},
		// No file of these matches a library file by size except the movie itself, so linking
		// by content adds nothing to what history says.
		files: map[string][]domain.DownloadFile{
			movieTorrent: {
				{Name: "Fake.Movie.One.2020.1080p/Fake.Movie.One.mkv", Size: 2 * gib},
				{Name: "Fake.Movie.One.2020.1080p/Extras/Extras.mkv", Size: gib - mib},
				{Name: "Fake.Movie.One.2020.1080p/Fake.Movie.One.nfo", Size: mib},
			},
			orphanTorrent: {{Name: "fake.orphan.torrent", Size: 500 * mib}},
		},
	}

	series := domain.Series{ID: 1, Title: "Fake Series", Year: 2019, IDs: domain.ExternalIDs{TVDB: "201"}, Ended: true}
	for season := 1; season <= 2; season++ {
		for number := 1; number <= 2; number++ {
			id := season*100 + number
			series.Episodes = append(series.Episodes, domain.Episode{
				ID: id, Season: season, Number: number, AirDate: ago(400 - id),
				File: &domain.MediaFile{ID: 1000 + id, Size: gib},
			})
			l.server.Episodes = append(l.server.Episodes, domain.ServerEpisode{
				SeriesID: "j-series-1", Season: season, Episode: number, States: played(ago(10 + id%100)),
			})
		}
	}
	l.series.Series = []domain.Series{series}
	return l
}

// writes returns the writes received so far.
func (l *library) writes() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.calls)
}

func (l *library) record(call string) {
	l.calls = append(l.calls, call)
}

// The reads return copies, so a write never changes what a running scan works with.

func (l *library) Users(context.Context) ([]domain.User, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.users), nil
}

func (l *library) Library(context.Context, []string) (domain.Library, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return domain.Library{
		Movies:   slices.Clone(l.server.Movies),
		Series:   slices.Clone(l.server.Series),
		Episodes: slices.Clone(l.server.Episodes),
	}, nil
}

func (l *library) Movies(context.Context) (domain.MovieSnapshot, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	movies := make([]domain.Movie, len(l.movies.Movies))
	for i, m := range l.movies.Movies {
		if m.File != nil {
			f := *m.File
			m.File = &f
		}
		movies[i] = m
	}
	return domain.MovieSnapshot{Movies: movies, Links: cloneLinks(l.movies.Links), RootFolders: slices.Clone(l.movies.RootFolders)}, nil
}

func (l *library) Series(context.Context) (domain.SeriesSnapshot, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	series := make([]domain.Series, len(l.series.Series))
	for i, s := range l.series.Series {
		episodes := make([]domain.Episode, len(s.Episodes))
		for j, e := range s.Episodes {
			if e.File != nil {
				f := *e.File
				e.File = &f
			}
			episodes[j] = e
		}
		s.Episodes = episodes
		series[i] = s
	}
	return domain.SeriesSnapshot{Series: series, Links: cloneLinks(l.series.Links), RootFolders: slices.Clone(l.series.RootFolders)}, nil
}

func (l *library) Downloads(context.Context) ([]domain.Download, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.downloads), nil
}

func (l *library) Files(_ context.Context, id string) ([]domain.DownloadFile, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.files[id]), nil
}

func cloneLinks(in domain.Links) domain.Links {
	out := domain.Links{Grabbed: map[string][]int{}, FileSource: map[int]string{}, Queue: map[string]bool{}}
	for k, v := range in.Grabbed {
		out.Grabbed[k] = slices.Clone(v)
	}
	for k, v := range in.FileSource {
		out.FileSource[k] = v
	}
	for k, v := range in.Queue {
		out.Queue[k] = v
	}
	return out
}

// RemoveMovie removes the movie from the movie manager, and from the media server as its
// next library scan would.
func (l *library) RemoveMovie(_ context.Context, id int, addExclusion bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.record(fmt.Sprintf("remove movie %d exclusion=%v", id, addExclusion))
	i := slices.IndexFunc(l.movies.Movies, func(m domain.Movie) bool { return m.ID == id })
	if i < 0 {
		return fmt.Errorf("movie %d not found", id)
	}
	m := l.movies.Movies[i]
	l.movies.Movies = slices.Delete(l.movies.Movies, i, i+1)
	if m.File != nil {
		delete(l.movies.Links.FileSource, m.File.ID)
	}
	for dl, items := range l.movies.Links.Grabbed {
		if items = slices.DeleteFunc(items, func(item int) bool { return item == id }); len(items) == 0 {
			delete(l.movies.Links.Grabbed, dl)
		} else {
			l.movies.Links.Grabbed[dl] = items
		}
	}
	l.server.Movies = slices.DeleteFunc(l.server.Movies, func(s domain.ServerMovie) bool { return s.IDs.TMDB == m.IDs.TMDB })
	return nil
}

// RemoveSeries removes the series from the series manager and from the media server.
func (l *library) RemoveSeries(_ context.Context, id int, addExclusion bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.record(fmt.Sprintf("remove series %d exclusion=%v", id, addExclusion))
	i := slices.IndexFunc(l.series.Series, func(s domain.Series) bool { return s.ID == id })
	if i < 0 {
		return fmt.Errorf("series %d not found", id)
	}
	s := l.series.Series[i]
	l.series.Series = slices.Delete(l.series.Series, i, i+1)
	var serverIDs []string
	for _, ss := range l.server.Series {
		if ss.IDs.TVDB == s.IDs.TVDB {
			serverIDs = append(serverIDs, ss.ID)
		}
	}
	l.server.Series = slices.DeleteFunc(l.server.Series, func(ss domain.ServerSeries) bool { return ss.IDs.TVDB == s.IDs.TVDB })
	l.server.Episodes = slices.DeleteFunc(l.server.Episodes, func(e domain.ServerEpisode) bool { return slices.Contains(serverIDs, e.SeriesID) })
	return nil
}

// DeleteEpisodeFiles removes the files from their episodes.
func (l *library) DeleteEpisodeFiles(_ context.Context, fileIDs []int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.record(fmt.Sprintf("delete episode files %v", fileIDs))
	for i := range l.series.Series {
		for j := range l.series.Series[i].Episodes {
			e := &l.series.Series[i].Episodes[j]
			if e.File != nil && slices.Contains(fileIDs, e.File.ID) {
				delete(l.series.Links.FileSource, e.File.ID)
				e.File = nil
			}
		}
	}
	return nil
}

func (l *library) UnmonitorEpisodes(_ context.Context, episodeIDs []int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.record(fmt.Sprintf("unmonitor episodes %v", episodeIDs))
	return nil
}

func (l *library) UnmonitorSeasons(_ context.Context, seriesID int, seasons []int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.record(fmt.Sprintf("unmonitor seasons %d %v", seriesID, seasons))
	return nil
}

// RemoveDownload removes the torrent from the download client.
func (l *library) RemoveDownload(_ context.Context, id string, deleteData bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.record(fmt.Sprintf("remove torrent %s data=%v", id, deleteData))
	i := slices.IndexFunc(l.downloads, func(d domain.Download) bool { return d.ID == id })
	if i < 0 {
		return fmt.Errorf("torrent %s not found", id)
	}
	l.downloads = slices.Delete(l.downloads, i, i+1)
	delete(l.files, id)
	return nil
}

var (
	_ providers.MediaServer     = (*library)(nil)
	_ providers.MovieManager    = (*library)(nil)
	_ providers.SeriesManager   = (*library)(nil)
	_ providers.DownloadClient  = (*library)(nil)
	_ providers.MovieWriter     = (*library)(nil)
	_ providers.SeriesWriter    = (*library)(nil)
	_ providers.DownloadRemover = (*library)(nil)
)

// fakeApps stands in for app.Apps: every app tests as "fake", and the media server is the
// fake library.
type fakeApps struct {
	lib *library
}

type fakeTester struct{}

func (fakeTester) Test(context.Context) (string, error) { return "fake", nil }

func (a *fakeApps) Tester(_ context.Context, name string) (providers.Tester, error) {
	if !slices.Contains(config.Apps, name) {
		return nil, providers.ErrNotConfigured
	}
	return fakeTester{}, nil
}

func (a *fakeApps) MediaServer(context.Context) (providers.MediaServer, error) { return a.lib, nil }

func (a *fakeApps) providers(context.Context) (scanner.Providers, error) {
	return scanner.Providers{MediaServer: a.lib, Movies: a.lib, Series: a.lib, Downloads: a.lib}, nil
}

func (a *fakeApps) writers(context.Context) (executor.Providers, error) {
	return executor.Providers{Movies: a.lib, Series: a.lib, Downloads: a.lib, Remover: a.lib}, nil
}

// world is the wired fake server.
type world struct {
	http  *http.Server
	store *store.Store
	lib   *library
	apps  *fakeApps
	scans *scanner.Service
	plans *planner.Service
	exec  *executor.Service
	stop  chan struct{}
}

// Close tells the executor to start no further item, waits for a running execution and
// closes the database. It does not stop the HTTP server.
func (w *world) Close() error {
	close(w.stop)
	w.exec.Wait(time.Minute)
	return w.store.Close()
}

// fakeEnv is the environment the fake server runs with: every app has a URL and a key,
// so the status reports them configured. Nothing ever connects to them.
func fakeEnv(dir string, port int) (config.Env, error) {
	vars := map[string]string{
		"REMOVARR_CONFIG_DIR":           dir,
		"REMOVARR_DRY_RUN":              "false",
		"REMOVARR_JELLYFIN_URL":         "http://fake.invalid",
		"REMOVARR_JELLYFIN_API_KEY":     "fake-key",
		"REMOVARR_SONARR_URL":           "http://fake.invalid",
		"REMOVARR_SONARR_API_KEY":       "fake-key",
		"REMOVARR_RADARR_URL":           "http://fake.invalid",
		"REMOVARR_RADARR_API_KEY":       "fake-key",
		"REMOVARR_QBITTORRENT_URL":      "http://fake.invalid",
		"REMOVARR_QBITTORRENT_USERNAME": "fake",
		"REMOVARR_QBITTORRENT_PASSWORD": "fake-password",
	}
	if port > 0 {
		vars["REMOVARR_PORT"] = strconv.Itoa(port)
	}
	return config.LoadEnv(func(k string) string { return vars[k] })
}

// wire builds removarr's API server as cmd/removarr does, on the fake library, with its
// database in dir. The executor runs with dry run off: the fakes record the writes. The
// server listens on nothing yet; its Addr is 127.0.0.1:port.
func wire(dir string, port int, log *slog.Logger) (*world, error) {
	env, err := fakeEnv(dir, port)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create the directory: %w", err)
	}
	st, err := store.Open(filepath.Join(dir, "removarr.db"))
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	cfg := config.NewService(env, st)
	ctx := context.Background()
	rules, err := cfg.Rules(ctx)
	if err == nil {
		rules.CountingUsers = []string{"u1"}
		err = cfg.SetRules(ctx, rules)
	}
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("save the rules: %w", err)
	}

	lib := newLibrary(time.Now())
	apps := &fakeApps{lib: lib}
	g := &gate.Gate{}
	stop := make(chan struct{})
	w := &world{
		store: st, lib: lib, apps: apps, stop: stop,
		scans: &scanner.Service{Gate: g, Build: apps.providers, Rules: cfg.Rules, Fingerprint: cfg.Fingerprint, Store: st, Now: time.Now, Log: log},
		plans: &planner.Service{Gate: g, Build: apps.providers, Rules: cfg.Rules, Store: st, Now: time.Now},
		exec:  &executor.Service{Gate: g, Build: apps.writers, Store: st, DryRun: env.DryRun, Now: time.Now, Stop: stop, Log: log},
	}
	server := &api.Server{Config: cfg, Store: st, Scanner: w.scans, Apps: apps, Planner: w.plans, Executor: w.exec, Log: log, UI: webui.Handler(webui.Files())}
	w.http = &http.Server{
		Addr:              fmt.Sprintf("127.0.0.1:%d", port),
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return w, nil
}
