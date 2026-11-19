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
	// by download ID. It is nil when no file list was read.
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
	if f.Downloads.ok() && (f.Movies.ok() || f.Series.ok()) {
		s.setPhase("matching downloads to library files")
		f.Files, f.Downloads.Err = readFiles(ctx, p.Downloads, rules, f)
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
	// A download ID means the same download in both managers; file IDs do not, so each
	// manager's links are matched against its own files and the results added.
	sourced := map[string]int64{}
	add := func(bytes map[string]int64) {
		for id, n := range bytes {
			sourced[id] += n
		}
	}
	if f.Movies.ok() {
		add(scan.SourcedBytes(f.Movies.Value.Links, movieFiles(f.Movies.Value)))
	}
	if f.Series.ok() {
		add(scan.SourcedBytes(f.Series.Value.Links, episodeFiles(f.Series.Value)))
	}
	files := map[string][]domain.DownloadFile{}
	for _, d := range f.Downloads.Value {
		if !scan.NeedsContentCheck(rules, history, d, sourced[d.ID]) {
			continue
		}
		list, err := client.Files(ctx, d.ID)
		if err != nil {
			return nil, err
		}
		files[d.ID] = list
	}
	return files, nil
}

// linkData returns the link data of every media manager that answered. With files given,
// each manager's links are first extended by content (spec 6.6) and stored back into f, so
// the list rules see the same links the index is built from.
func linkData(f *Fetched, files map[string][]domain.DownloadFile) []scan.ManagerFiles {
	var managers []scan.ManagerFiles
	if f.Movies.ok() {
		library := movieFiles(f.Movies.Value)
		if files != nil {
			f.Movies.Value.Links = scan.ContentLinks(f.Movies.Value.Links, library, files)
		}
		managers = append(managers, scan.ManagerFiles{Links: f.Movies.Value.Links, FileIDs: fileIDs(library)})
	}
	if f.Series.ok() {
		library := episodeFiles(f.Series.Value)
		if files != nil {
			f.Series.Value.Links = scan.ContentLinks(f.Series.Value.Links, library, files)
		}
		managers = append(managers, scan.ManagerFiles{Links: f.Series.Value.Links, FileIDs: fileIDs(library)})
	}
	return managers
}

// movieFiles lists the current movie files with the movie each belongs to.
func movieFiles(s domain.MovieSnapshot) []scan.LibraryFile {
	var out []scan.LibraryFile
	for _, m := range s.Movies {
		if m.File != nil {
			out = append(out, scan.LibraryFile{ID: m.File.ID, Size: m.File.Size, Items: []int{m.ID}})
		}
	}
	return out
}

// episodeFiles lists the current episode files with the episodes each belongs to. The
// episodes of a multi-episode file share one file.
func episodeFiles(s domain.SeriesSnapshot) []scan.LibraryFile {
	var out []scan.LibraryFile
	at := map[int]int{} // file ID -> its position in out
	for _, show := range s.Series {
		for _, e := range show.Episodes {
			if e.File == nil {
				continue
			}
			i, seen := at[e.File.ID]
			if !seen {
				i = len(out)
				at[e.File.ID] = i
				out = append(out, scan.LibraryFile{ID: e.File.ID, Size: e.File.Size})
			}
			out[i].Items = append(out[i].Items, e.ID)
		}
	}
	return out
}

func fileIDs(files []scan.LibraryFile) []int {
	ids := make([]int, 0, len(files))
	for _, f := range files {
		ids = append(ids, f.ID)
	}
	return ids
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
	index := scan.NewLinkIndex(linkData(&f, f.Files)...)

	add := func(list domain.ListKind, problem string, compute func() scan.ListResult) {
		if problem != "" {
			summary.Lists[list] = domain.ListSummary{Error: problem}
			return
		}
		res := compute()
		summary.Lists[list] = domain.ListSummary{Candidates: len(res.Candidates), Protected: res.Protected, Unmatched: res.Unmatched}
		candidates = append(candidates, res.Candidates...)
	}

	noUsers := ""
	if len(rules.CountingUsers) == 0 {
		noUsers = "No counting users are selected."
	}
	watchedProblem := func(manager string) string {
		return first(manager, f.Downloads.problem(roleDownloads), f.Library.problem(roleMediaServer), noUsers)
	}

	add(domain.ListMovies, watchedProblem(f.Movies.problem(roleMovies)), func() scan.ListResult {
		return scan.Movies(scan.MoviesInput{
			Rules: rules, Now: now, Users: f.Users, Library: f.Library.Value,
			Snapshot: f.Movies.Value, Downloads: f.Downloads.Value, Index: index,
		})
	})
	add(domain.ListSeries, watchedProblem(f.Series.problem(roleSeries)), func() scan.ListResult {
		return scan.Series(scan.SeriesInput{
			Rules: rules, Now: now, Users: f.Users, Library: f.Library.Value,
			Snapshot: f.Series.Value, Downloads: f.Downloads.Value, Index: index,
		})
	})
	add(domain.ListTorrents, torrentsProblem(f), func() scan.ListResult {
		return scan.Torrents(scan.TorrentsInput{Rules: rules, Downloads: f.Downloads.Value, Index: index})
	})
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

// first returns the first non-empty text.
func first(texts ...string) string {
	for _, t := range texts {
		if t != "" {
			return t
		}
	}
	return ""
}
