package scan

import (
	"fmt"

	"github.com/removarr/removarr/internal/domain"
)

// IgnoreSet holds the user's ignores by key (spec 6.5).
type IgnoreSet map[string]domain.Ignore

// NewIgnoreSet indexes ignores by key.
func NewIgnoreSet(ignores []domain.Ignore) IgnoreSet {
	set := make(IgnoreSet, len(ignores))
	for _, i := range ignores {
		set[i.Key] = i
	}
	return set
}

// Keys returns the ignored keys, in the form SeriesInput.Ignored takes.
func (s IgnoreSet) Keys() map[string]bool {
	out := make(map[string]bool, len(s))
	for key := range s {
		out[key] = true
	}
	return out
}

// Hides reports whether an ignore hides the row with key in list: the row's own ignore, or
// for a season row the ignore of its series. A season ignore never hides a series row, so it
// has no effect in whole-series mode (spec 6.5).
func (s IgnoreSet) Hides(list domain.ListKind, key string) bool {
	if i, ok := s[key]; ok && i.List() == list {
		return true
	}
	if list != domain.ListSeries {
		return false
	}
	series, _, ok := domain.ParseSeasonKey(key)
	if !ok {
		return false
	}
	i, ok := s[series]
	return ok && i.Kind == domain.IgnoreSeries
}

// ListView is one list as the user sees it with the current ignores.
type ListView struct {
	Shown   []domain.Candidate // listed rows that are neither hidden nor tied to a hidden row
	Ignored []domain.Candidate // the ignored view: one entry per ignore of the list
	Waiting int                // rows held back by the whole-torrent rule or by a season ignore made during the scan, or tied to a row that is hidden or held back; hidden rows are not counted
}

// View applies the ignores to the rows the latest scan stored for a list, listed and held
// back alike (spec 6.3, 6.5). An ignored row is hidden and counted nowhere. A series row the
// per-season merge produced over a season that is now ignored waits: the ignore came during
// the scan, and the next scan lists the seasons instead. A row tied to a hidden row or to
// such a series row, in any list, waits: its group lost a row, and the torrent or file the
// two shared now backs something that stays. An ignore whose row the scan did not store is
// shown in the ignored view by its key and title. Both slices are sorted by key and never
// nil.
//
// View relies on TiedWith naming every other row of a row's group, across chains, as
// WholeTorrents fills it. With direct neighbours only, a hidden row would not hold back the
// far end of a chain.
func View(list domain.ListKind, rows []domain.Candidate, ignores IgnoreSet) ListView {
	v := ListView{Shown: []domain.Candidate{}, Ignored: []domain.Candidate{}}
	e := newEffects(rows, ignores)
	stored := map[string]domain.Candidate{}
	for _, c := range rows {
		if c.List != list {
			continue
		}
		stored[c.Key] = c
		switch {
		case ignores.Hides(list, c.Key):
		case c.Waiting || e.heldBack(c) || e.tiedToOut(c):
			v.Waiting++
		default:
			v.Shown = append(v.Shown, c)
		}
	}
	for key, i := range ignores {
		if i.List() != list {
			continue
		}
		c, ok := stored[key]
		if !ok {
			c = domain.Candidate{List: list, Key: key, Title: i.Title, Torrents: []domain.TorrentInfo{}, NotInScan: true}
			if list == domain.ListSeries {
				c.Scope = domain.ScopeSeries
				if _, season, ok := domain.ParseSeasonKey(key); ok {
					c.Scope, c.Season = domain.ScopeSeason, season
				}
			}
		}
		at := i.CreatedAt
		c.IgnoredAt = &at
		v.Ignored = append(v.Ignored, c)
	}
	sortByKey(v.Shown)
	sortByKey(v.Ignored)
	return v
}

// rowID names a stored row.
type rowID struct {
	list domain.ListKind
	key  string
}

// effects decides which stored rows the current ignores take out of their groups.
type effects struct {
	ignores       IgnoreSet
	seasonIgnored map[string]bool // keys of the series with at least one ignored season
	rows          map[rowID]domain.Candidate
}

func newEffects(rows []domain.Candidate, ignores IgnoreSet) effects {
	e := effects{ignores: ignores, seasonIgnored: map[string]bool{}, rows: make(map[rowID]domain.Candidate, len(rows))}
	for key, i := range ignores {
		if i.Kind != domain.IgnoreSeason {
			continue
		}
		if series, _, ok := domain.ParseSeasonKey(key); ok {
			e.seasonIgnored[series] = true
		}
	}
	for _, c := range rows {
		e.rows[rowID{c.List, c.Key}] = c
	}
	return e
}

// heldBack reports whether c is a series row the per-season merge produced over a season
// that is now ignored (spec 6.5). The user did not hide it, so it waits.
func (e effects) heldBack(c domain.Candidate) bool {
	return c.Merged && e.seasonIgnored[c.Key]
}

// out reports whether c leaves its group: an ignore hides it, or it is held back.
func (e effects) out(c domain.Candidate) bool {
	return e.ignores.Hides(c.List, c.Key) || e.heldBack(c)
}

// tiedToOut reports whether a row c is tied to leaves its group. A row the scan did not store
// is judged by its list and key alone.
func (e effects) tiedToOut(c domain.Candidate) bool {
	for _, ref := range c.TiedWith {
		r, ok := e.rows[rowID{ref.List, ref.Key}]
		if !ok {
			r = domain.Candidate{List: ref.List, Key: ref.Key}
		}
		if e.out(r) {
			return true
		}
	}
	return false
}

// Why the current ignores, or the whole-torrent rule, keep a row from being deleted (spec
// 6.10). Rows tied to a row that is out give the texts of Holds.Reason.
const (
	HoldIgnored = "It is ignored. Un-ignore it before deleting it."
	HoldWaiting = "It waits: it shares a torrent or a library file with something that stays."
	HoldMerged  = "It was merged from its seasons while one of them was ignored. Scan again."
)

// Holds answers, for the rows of one scan and the current ignores, why a row cannot be
// deleted. It applies the same effects as View.
type Holds struct{ e effects }

// NewHolds prepares the answers for rows, which are every row of one scan, listed and held.
func NewHolds(rows []domain.Candidate, ignores IgnoreSet) Holds {
	return Holds{newEffects(rows, ignores)}
}

// Reason returns why c cannot be deleted now, or "" when nothing holds it: it is hidden by
// an ignore, held back by the whole-torrent rule or by a season ignore made during the scan,
// or tied to a row that is.
func (h Holds) Reason(c domain.Candidate) string {
	switch {
	case h.e.ignores.Hides(c.List, c.Key):
		return HoldIgnored
	case c.Waiting && c.WaitReason != "":
		return c.WaitReason
	case c.Waiting:
		return HoldWaiting // a scan from before stage 2c stored no reason
	case h.e.heldBack(c):
		return HoldMerged
	}
	for _, ref := range c.TiedWith {
		r, ok := h.e.rows[rowID{ref.List, ref.Key}]
		if !ok {
			r = domain.Candidate{List: ref.List, Key: ref.Key}
		}
		switch {
		case h.e.ignores.Hides(r.List, r.Key):
			return fmt.Sprintf("It is tied to %q, which is ignored.", ref.Title)
		case h.e.heldBack(r):
			return fmt.Sprintf("It is tied to %q, which waits for the next scan.", ref.Title)
		}
	}
	return ""
}
