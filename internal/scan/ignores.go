package scan

import "github.com/removarr/removarr/internal/domain"

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
	Waiting int                // rows held back by the whole-torrent rule or tied to a hidden row; hidden rows are not counted
}

// View applies the ignores to the rows the latest scan stored for a list, listed and held
// back alike (spec 6.3, 6.5). An ignored row is hidden and counted nowhere. A row tied to a
// hidden row, in any list, waits: its group lost a row, and the torrent or file the two
// shared now backs something that stays. An ignore whose row the scan did not store is
// shown in the ignored view by its key and title. Both slices are sorted by key and never
// nil.
func View(list domain.ListKind, rows []domain.Candidate, ignores IgnoreSet) ListView {
	v := ListView{Shown: []domain.Candidate{}, Ignored: []domain.Candidate{}}
	stored := map[string]domain.Candidate{}
	for _, c := range rows {
		if c.List != list {
			continue
		}
		stored[c.Key] = c
		switch {
		case ignores.Hides(list, c.Key):
		case c.Waiting || tiedToHidden(c, ignores):
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

// tiedToHidden reports whether a row the row is tied to is hidden by an ignore.
func tiedToHidden(c domain.Candidate, ignores IgnoreSet) bool {
	for _, ref := range c.TiedWith {
		if ignores.Hides(ref.List, ref.Key) {
			return true
		}
	}
	return false
}
