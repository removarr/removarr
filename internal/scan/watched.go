// Package scan holds the rules that turn snapshots of the apps into suggestion lists
// (spec section 6). It does no I/O.
package scan

import (
	"time"

	"github.com/removarr/removarr/internal/domain"
)

// Verdict is the result of applying the watched rule (spec 6.1) to one movie or episode.
type Verdict struct {
	Watched bool
	At      *time.Time // latest known play date among the counting users who played; nil when unknown
	By      []string   // IDs of the counting users who played, in CountingUsers order
}

// Evaluate applies the watched rule to the per-user states of one item.
func Evaluate(r domain.Rules, states map[string]domain.UserState) Verdict {
	var v Verdict
	for _, id := range r.CountingUsers {
		st, ok := states[id]
		if !ok || !st.Played {
			continue
		}
		v.By = append(v.By, id)
		v.At = later(v.At, st.LastPlayed)
	}
	if r.WatchedMode == domain.WatchedAny {
		v.Watched = len(v.By) > 0
	} else {
		v.Watched = len(r.CountingUsers) > 0 && len(v.By) == len(r.CountingUsers)
	}
	return v
}

// GraceOK reports whether the grace period has passed since the watched date.
// An unknown date satisfies it.
func GraceOK(r domain.Rules, at *time.Time, now time.Time) bool {
	if at == nil || r.GraceDays <= 0 {
		return true
	}
	return now.Sub(*at) >= time.Duration(r.GraceDays)*24*time.Hour
}

// mergeStates combines the states of several media server items that stand for the same
// movie or episode: a user has played or favourited it when that holds for any of the items.
func mergeStates(a, b map[string]domain.UserState) map[string]domain.UserState {
	if a == nil && b == nil {
		return nil
	}
	out := make(map[string]domain.UserState, len(a)+len(b))
	for id, st := range a {
		out[id] = st
	}
	for id, st := range b {
		cur, ok := out[id]
		if !ok {
			out[id] = st
			continue
		}
		cur.Played = cur.Played || st.Played
		cur.Favorite = cur.Favorite || st.Favorite
		cur.LastPlayed = later(cur.LastPlayed, st.LastPlayed)
		out[id] = cur
	}
	return out
}

// later returns the later of two optional times.
func later(a, b *time.Time) *time.Time {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	case b.After(*a):
		return b
	default:
		return a
	}
}
