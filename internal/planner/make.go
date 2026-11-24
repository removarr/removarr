// Package planner builds the plan for a deletion from live data (spec 6.7, 6.8, 6.10). It
// reads and evaluates the apps through scanner, so a plan decides exactly as a scan does.
// Nothing here changes anything in an app.
package planner

import (
	"sort"
	"strings"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
	"github.com/removarr/removarr/internal/scanner"
)

// Input is everything a plan is made from.
type Input struct {
	Rules    domain.Rules
	Now      time.Time
	Eval     scanner.Evaluation // the planner's own read, evaluated as a scan
	Ignores  scan.IgnoreSet     // the current ignores
	Latest   []domain.Candidate // every row of the latest scan, listed and held
	Selected []domain.RowRef    // the rows the user selected, by list and key
}

// Why a selected row is skipped. Other reasons come from the rules: a list's error, what a
// row waits for (scan.Wait…, scan.Hold…), or a change since the scan.
const (
	ReasonNotInScan   = "It is not a row of the latest scan. Scan again."
	ReasonNoLonger    = "It no longer qualifies. Scan again to see why."
	ReasonGone        = "It is no longer in the download client."
	ReasonQueuedNow   = "It is in a media manager's queue now."
	ReasonLinkedNow   = "It backs a library file now."
	ReasonExcludedNow = "Its category or tag is excluded now."
)

// rowID names a row.
type rowID struct {
	list domain.ListKind
	key  string
}

// Make builds the items and the skipped rows of a plan (spec 6.7, 6.10). Both are never nil.
// Items come in the order movies, series, torrents, each by key, so a torrent row is reached
// after any movie or series that removes its torrent anyway.
func Make(in Input) ([]domain.PlanItem, []domain.SkippedRow) {
	m := newMaker(in)
	for _, ref := range selection(in.Selected) {
		m.add(ref)
	}
	m.finish()
	return m.items, m.skipped
}

// maker holds what Make looks things up in, and what it has built so far.
type maker struct {
	in        Input
	latest    map[rowID]domain.Candidate // the latest scan
	fresh     map[rowID]domain.Candidate // the planner's read, listed and held
	rows      map[rowID]scan.Row         // the planner's read: what Delete touches
	series    map[int]domain.Series      // by ID in the series manager
	downloads map[string]domain.Download // the download client's answer
	holds     scan.Holds
	items     []domain.PlanItem
	inItem    map[rowID]int  // row -> its item
	owner     map[string]int // torrent ID -> the item that removes it
	skipped   []domain.SkippedRow
}

func newMaker(in Input) *maker {
	m := &maker{
		in:        in,
		latest:    map[rowID]domain.Candidate{},
		fresh:     map[rowID]domain.Candidate{},
		rows:      map[rowID]scan.Row{},
		series:    map[int]domain.Series{},
		downloads: map[string]domain.Download{},
		holds:     scan.NewHolds(in.Eval.Candidates, in.Ignores),
		items:     []domain.PlanItem{},
		inItem:    map[rowID]int{},
		owner:     map[string]int{},
		skipped:   []domain.SkippedRow{},
	}
	for _, c := range in.Latest {
		m.latest[rowID{c.List, c.Key}] = c
	}
	for _, c := range in.Eval.Candidates {
		m.fresh[rowID{c.List, c.Key}] = c
	}
	for _, r := range in.Eval.Rows {
		m.rows[rowID{r.Candidate.List, r.Candidate.Key}] = r
	}
	for _, s := range in.Eval.Fetched.Series.Value.Series {
		m.series[s.ID] = s
	}
	for _, d := range in.Eval.Fetched.Downloads.Value {
		m.downloads[d.ID] = d
	}
	return m
}

// listRank orders the lists in a plan.
func listRank(l domain.ListKind) int {
	switch l {
	case domain.ListMovies:
		return 0
	case domain.ListSeries:
		return 1
	default:
		return 2
	}
}

// selection returns each selected row once, by list and key.
func selection(refs []domain.RowRef) []domain.RowRef {
	seen := map[rowID]bool{}
	out := []domain.RowRef{}
	for _, r := range refs {
		id := rowID{r.List, r.Key}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, domain.RowRef{List: r.List, Key: r.Key})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := listRank(out[i].List), listRank(out[j].List); a != b {
			return a < b
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func refOf(c domain.Candidate) domain.RowRef {
	return domain.RowRef{List: c.List, Key: c.Key, Title: c.Title, Season: c.Season}
}

// ref names a row with its title, from the planner's read or else from the latest scan.
func (m *maker) ref(id rowID) domain.RowRef {
	if c, ok := m.fresh[id]; ok {
		return refOf(c)
	}
	if c, ok := m.latest[id]; ok {
		return refOf(c)
	}
	return domain.RowRef{List: id.list, Key: id.key}
}

func (m *maker) skip(id rowID, reason string) {
	m.skipped = append(m.skipped, domain.SkippedRow{Row: m.ref(id), Reason: reason})
}

// add plans one selected row, or skips it with the reason (spec 6.10).
func (m *maker) add(ref domain.RowRef) {
	id := rowID{ref.List, ref.Key}
	if i, ok := m.inItem[id]; ok {
		m.markSelected(i, id)
		return
	}
	if problem := m.in.Eval.Summary.Lists[ref.List].Error; problem != "" {
		m.skip(id, problem)
		return
	}
	if _, ok := m.latest[id]; !ok {
		m.skip(id, ReasonNotInScan)
		return
	}
	if m.in.Ignores.Hides(ref.List, ref.Key) {
		m.skip(id, scan.HoldIgnored)
		return
	}
	c, ok := m.fresh[id]
	if !ok {
		m.skip(id, m.goneReason(id))
		return
	}
	if reason := m.holds.Reason(c); reason != "" {
		m.skip(id, reason)
		return
	}
	if ref.List == domain.ListTorrents {
		m.addTorrentRow(id, c)
		return
	}
	m.addGroup(id, c)
}

// goneReason says why the planner's read no longer produces a row of the latest scan. For a
// torrent row the download client and the links tell; for a movie or series row the rules do
// not record why, so a new scan has to show it.
func (m *maker) goneReason(id rowID) string {
	if id.list != domain.ListTorrents {
		return ReasonNoLonger
	}
	hash := strings.TrimPrefix(id.key, "hash:")
	switch {
	case m.downloads[hash].ID == "":
		return ReasonGone
	case m.in.Eval.Index.Queued(hash):
		return ReasonQueuedNow
	case m.in.Eval.Index.Backing(hash) > 0:
		return ReasonLinkedNow
	default:
		// scan.Torrents lists every other download in the client that has an ID.
		return ReasonExcludedNow
	}
}

// markSelected records that a row an earlier selection added to item i was selected too.
func (m *maker) markSelected(i int, id rowID) {
	for j := range m.items[i].Rows {
		r := &m.items[i].Rows[j]
		if r.Row.List == id.list && r.Row.Key == id.key {
			r.Added, r.AddedWith = false, nil
		}
	}
}

// addTorrentRow plans an orphan or unlinked torrent: the torrent alone, no media manager
// step (spec 6.7). A torrent a movie or series of the plan removes anyway is not planned
// twice.
func (m *maker) addTorrentRow(id rowID, c domain.Candidate) {
	if len(c.Torrents) != 1 {
		m.skip(id, ReasonNoLonger)
		return
	}
	t := c.Torrents[0]
	if i, ok := m.owner[t.ID]; ok {
		m.skip(id, removedWith(m.items[i].Rows[0].Row.Title))
		return
	}
	m.push(domain.PlanItem{
		Rows:     []domain.PlanRow{{Row: c}},
		Steps:    []domain.ManagerStep{},
		Torrents: []domain.PlanTorrent{{Torrent: t}},
		Kept:     []domain.KeptTorrent{},
	})
}

// push appends an item and records its rows and torrents.
func (m *maker) push(item domain.PlanItem) {
	i := len(m.items)
	m.items = append(m.items, item)
	for _, r := range item.Rows {
		m.inItem[rowID{r.Row.List, r.Row.Key}] = i
	}
	for _, t := range item.Torrents {
		m.owner[t.Torrent.ID] = i
	}
}
