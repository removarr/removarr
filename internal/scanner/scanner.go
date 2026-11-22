// Package scanner runs one scan: it reads every configured app, applies the rules and
// stores the result.
package scanner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/providers"
	"github.com/removarr/removarr/internal/scan"
)

// Providers are the apps a scan reads. A nil field means the app is not configured.
type Providers struct {
	MediaServer providers.MediaServer
	Movies      providers.MovieManager
	Series      providers.SeriesManager
	Downloads   providers.DownloadClient
}

// Store is the persistence a scan needs. *store.Store satisfies it.
type Store interface {
	BeginScan(ctx context.Context, startedAt time.Time) (int64, error)
	FinishScan(ctx context.Context, id int64, finishedAt time.Time, summary domain.ScanSummary, candidates []domain.Candidate) error
}

// ErrRunning is returned while a scan is in progress.
var ErrRunning = errors.New("a scan is already running")

const defaultTimeout = 30 * time.Minute

// Status describes the scan in progress, if any.
type Status struct {
	Running   bool       `json:"running"`
	Phase     string     `json:"phase,omitempty"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	LastError string     `json:"lastError,omitempty"` // why the most recent scan could not be completed
}

// Service runs scans, one at a time.
type Service struct {
	Build   func(ctx context.Context) (Providers, error)    // builds providers from the current settings
	Rules   func(ctx context.Context) (domain.Rules, error) // loads the current rules
	Store   Store
	Now     func() time.Time
	Log     *slog.Logger
	Timeout time.Duration // limit for one background scan; 0 means 30 minutes

	mu     sync.Mutex
	status Status
}

// Start begins a scan in the background. It returns ErrRunning when one is in progress.
func (s *Service) Start() error {
	if err := s.begin(); err != nil {
		return err
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		s.finish(s.run(ctx))
	}()
	return nil
}

// Run performs a scan and waits for it. It returns ErrRunning when one is in progress.
func (s *Service) Run(ctx context.Context) error {
	if err := s.begin(); err != nil {
		return err
	}
	err := s.run(ctx)
	s.finish(err)
	return err
}

// Status returns what the scanner is doing.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Service) begin() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.Running {
		return ErrRunning
	}
	started := s.Now()
	s.status = Status{Running: true, Phase: "starting", StartedAt: &started}
	return nil
}

func (s *Service) finish(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = Status{}
	if err != nil {
		s.status.LastError = err.Error()
		s.Log.Error("scan failed", "error", err)
	}
}

func (s *Service) setPhase(phase string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Phase = phase
}

func (s *Service) run(ctx context.Context) error {
	started := s.Now()
	rules, err := s.Rules(ctx)
	if err != nil {
		return fmt.Errorf("load rules: %w", err)
	}
	p, err := s.Build(ctx)
	if err != nil {
		return fmt.Errorf("load connections: %w", err)
	}
	id, err := s.Store.BeginScan(ctx, started)
	if err != nil {
		return fmt.Errorf("record scan: %w", err)
	}

	fetched := s.fetch(ctx, p, rules)
	s.setPhase("applying the rules")
	summary, candidates := Compute(rules, s.Now(), fetched)
	s.setPhase("saving")
	if err := s.Store.FinishScan(ctx, id, s.Now(), summary, candidates); err != nil {
		return fmt.Errorf("save scan: %w", err)
	}
	s.Log.Info("scan finished",
		"movies", summary.Lists[domain.ListMovies].Candidates,
		"series", summary.Lists[domain.ListSeries].Candidates,
		"torrents", summary.Lists[domain.ListTorrents].Candidates)
	return nil
}

// Source is what a scan got from one app.
type Source[T any] struct {
	Configured bool
	Err        error
	Value      T
}

func (s Source[T]) ok() bool { return s.Configured && s.Err == nil }

// problem explains why the app's data cannot be used, or returns "" when it can.
func (s Source[T]) problem(role string) string {
	switch {
	case !s.Configured:
		return "The " + role + " is not configured."
	case s.Err != nil:
		return "The " + role + " could not be read: " + s.Err.Error()
	default:
		return ""
	}
}

// Fetched is everything one scan read from the apps.
type Fetched struct {
	Users     []domain.User
	Library   Source[domain.Library]
	Movies    Source[domain.MovieSnapshot]
	Series    Source[domain.SeriesSnapshot]
	Downloads Source[[]domain.Download]
	// Files holds the files of the downloads that needed the content check (spec 6.6),
	// by download ID. nil means the content check did not run; an empty map means it ran
	// and no download needed its files.
	Files map[string][]domain.DownloadFile
}

// fetch reads the apps one after another. The media server's library is read only when
// counting users are selected, because without them no watched list can be produced. Once
// the download client and at least one media manager have answered, it reads the file
// lists linking by content needs (spec 6.6): those of the downloads history does not link
// to a library file, and those of the downloads at least 50 MB larger than the library
// files history names them the source of.
func (s *Service) fetch(ctx context.Context, p Providers, rules domain.Rules) Fetched {
	var f Fetched
	if p.Downloads != nil {
		s.setPhase("reading the download client")
		f.Downloads.Configured = true
		f.Downloads.Value, f.Downloads.Err = p.Downloads.Downloads(ctx)
	}
	if p.Movies != nil {
		s.setPhase("reading the movie manager")
		f.Movies.Configured = true
		f.Movies.Value, f.Movies.Err = p.Movies.Movies(ctx)
	}
	if p.Series != nil {
		s.setPhase("reading the series manager")
		f.Series.Configured = true
		f.Series.Value, f.Series.Err = p.Series.Series(ctx)
	}
	// Without a media manager's answer there is no library file to match against, and no
	// list that could use the result.
	// When only one of two configured media managers answered, the other manager's downloads
	// look unlinked, so their file lists are read too; the torrent list is not produced in
	// that case.
	if f.Downloads.ok() && (f.Movies.ok() || f.Series.ok()) {
		s.setPhase("matching downloads to library files")
		f.Files, f.Downloads.Err = readFiles(ctx, p.Downloads, rules, f)
		if f.Downloads.Err == nil {
			s.Log.Info("read file lists for linking by content", "downloads", len(f.Files))
		}
	}
	if p.MediaServer != nil {
		f.Library.Configured = true
		if len(rules.CountingUsers) > 0 {
			s.setPhase("reading the media server")
			f.Users, f.Library.Err = p.MediaServer.Users(ctx)
			if f.Library.Err == nil {
				f.Library.Value, f.Library.Err = p.MediaServer.Library(ctx, rules.CountingUsers)
			}
		}
	}
	return f
}

// readFiles asks the download client for the files of every download that history does
// not link to a library file, and of every download that is at least 50 MB larger than the
// library files history names it the source of (spec 6.6). The first failure is returned
// and nothing else: with a partial answer, a download that holds library files would look
// unlinked.
func readFiles(ctx context.Context, client providers.DownloadClient, rules domain.Rules, f Fetched) (map[string][]domain.DownloadFile, error) {
	history := scan.NewLinkIndex(linkData(&f, nil)...)
	files := map[string][]domain.DownloadFile{}
	for _, d := range f.Downloads.Value {
		if !scan.NeedsContentCheck(rules, history, d, history.BackedBytes(d.ID)) {
			continue
		}
		list, err := client.Files(ctx, d.ID)
		if err != nil {
			label := d.Name
			if label == "" {
				label = d.ID
			}
			return nil, fmt.Errorf("%w (download %q)", err, label)
		}
		files[d.ID] = list
	}
	return files, nil
}

// linkData returns the link data of every media manager that answered. With files given,
// each manager's links are first extended by content (spec 6.6) and stored back into f. The
// list rules Movies and Series do not read the links: the index is built from them, and
// WholeTorrents and the torrent list use the index.
func linkData(f *Fetched, files map[string][]domain.DownloadFile) []scan.ManagerFiles {
	var managers []scan.ManagerFiles
	if f.Movies.ok() {
		library := scan.MovieFiles(f.Movies.Value)
		if files != nil {
			f.Movies.Value.Links = scan.ContentLinks(f.Movies.Value.Links, library, files)
		}
		managers = append(managers, scan.ManagerFiles{Manager: scan.ManagerMovies, Links: f.Movies.Value.Links, Files: library})
	}
	if f.Series.ok() {
		library := scan.EpisodeFiles(f.Series.Value)
		if files != nil {
			f.Series.Value.Links = scan.ContentLinks(f.Series.Value.Links, library, files)
		}
		managers = append(managers, scan.ManagerFiles{Manager: scan.ManagerSeries, Links: f.Series.Value.Links, Files: library})
	}
	return managers
}

const (
	roleMediaServer = "media server"
	roleMovies      = "movie manager"
	roleSeries      = "series manager"
	roleDownloads   = "download client"
)

// Compute applies the rules to what was fetched. A list is produced only when everything
// it depends on was read completely (spec 6.8, rule 5); otherwise its summary carries the
// reason and it has no rows.
func Compute(rules domain.Rules, now time.Time, f Fetched) (domain.ScanSummary, []domain.Candidate) {
	summary := domain.ScanSummary{Lists: map[domain.ListKind]domain.ListSummary{}, RootFolders: []domain.RootFolder{}}
	candidates := []domain.Candidate{}

	seenFolder := map[string]bool{}
	addFolders := func(folders []domain.RootFolder) {
		for _, folder := range folders {
			if !seenFolder[folder.Path] {
				seenFolder[folder.Path] = true
				summary.RootFolders = append(summary.RootFolders, folder)
			}
		}
	}
	if f.Movies.ok() {
		addFolders(f.Movies.Value.RootFolders)
	}
	if f.Series.ok() {
		addFolders(f.Series.Value.RootFolders)
	}
	// f is this function's own copy, so extending its links by content stays local.
	// Without the file lists, links come from history alone, which misses files imported
	// by hand (spec 6.8, rule 5).
	if f.Downloads.ok() && (f.Movies.ok() || f.Series.ok()) && f.Files == nil {
		f.Downloads.Err = errors.New("its file lists were not read")
	}
	index := scan.NewLinkIndex(linkData(&f, f.Files)...)

	// add writes a list's summary and appends its candidates. res is nil when the list
	// cannot be produced.
	add := func(list domain.ListKind, problem string, res *scan.ListResult) {
		if problem != "" {
			summary.Lists[list] = domain.ListSummary{Error: problem}
			return
		}
		summary.Lists[list] = domain.ListSummary{
			Candidates: len(res.Candidates), Protected: res.Protected, Unmatched: res.Unmatched, Waiting: res.Waiting,
		}
		candidates = append(candidates, res.Candidates...)
	}

	noUsers := ""
	if len(rules.CountingUsers) == 0 {
		noUsers = "No counting users are selected."
	}
	// A watched list needs both media managers when both are configured: a download that
	// backs an item of the other manager would look unlinked otherwise. A manager that is
	// not configured blocks nothing.
	watchedProblem := func(own string, other string) string {
		return first(own, other, f.Downloads.problem(roleDownloads), f.Library.problem(roleMediaServer), noUsers)
	}

	moviesProblem := watchedProblem(f.Movies.problem(roleMovies), failure(f.Series.Configured, f.Series.Err, roleSeries))
	seriesProblem := watchedProblem(f.Series.problem(roleSeries), failure(f.Movies.Configured, f.Movies.Err, roleMovies))
	// The rows of both watched lists go through the whole-torrent rule together: a torrent
	// can back a movie file and an episode file (spec 6.3).
	var movies, series *scan.ListResult
	var watched []*scan.ListResult
	if moviesProblem == "" {
		res := scan.Movies(scan.MoviesInput{
			Rules: rules, Now: now, Users: f.Users, Library: f.Library.Value, Snapshot: f.Movies.Value,
		})
		movies = &res
		watched = append(watched, movies)
	}
	if seriesProblem == "" {
		res := scan.Series(scan.SeriesInput{
			Rules: rules, Now: now, Users: f.Users, Library: f.Library.Value, Snapshot: f.Series.Value,
		})
		series = &res
		watched = append(watched, series)
	}
	scan.WholeTorrents(rules, index, f.Downloads.Value, watched...)

	add(domain.ListMovies, moviesProblem, movies)
	add(domain.ListSeries, seriesProblem, series)
	torrents := scan.ListResult{}
	problem := torrentsProblem(f)
	if problem == "" {
		torrents = scan.Torrents(scan.TorrentsInput{Rules: rules, Downloads: f.Downloads.Value, Index: index})
	}
	add(domain.ListTorrents, problem, &torrents)
	return summary, candidates
}

// torrentsProblem explains why the orphan torrent list cannot be produced, or returns "".
// A configured media manager that could not be read blocks the list, because all of its
// torrents would look like orphans.
func torrentsProblem(f Fetched) string {
	if p := f.Downloads.problem(roleDownloads); p != "" {
		return p
	}
	if !f.Movies.Configured && !f.Series.Configured {
		return "No media manager is configured."
	}
	if f.Movies.Configured && f.Movies.Err != nil {
		return f.Movies.problem(roleMovies)
	}
	if f.Series.Configured && f.Series.Err != nil {
		return f.Series.problem(roleSeries)
	}
	return ""
}

// failure explains why a configured app could not be read, or returns "" when the app is
// not configured or answered.
func failure(configured bool, err error, role string) string {
	if !configured || err == nil {
		return ""
	}
	return "The " + role + " could not be read: " + err.Error()
}

// first returns the first non-empty text.
func first(texts ...string) string {
	for _, t := range texts {
		if t != "" {
			return t
		}
	}
	return ""
}
