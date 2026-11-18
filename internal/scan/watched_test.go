package scan

import (
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

// tp parses an RFC 3339 timestamp for tests.
func tp(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return &t
}

// rules returns the default rules with the given watched mode and counting users.
func rules(mode string, users ...string) domain.Rules {
	r := domain.DefaultRules()
	r.WatchedMode = mode
	r.CountingUsers = users
	return r
}

// played returns a played state; at may be empty for an unknown date.
func played(at string) domain.UserState {
	st := domain.UserState{Played: true}
	if at != "" {
		st.LastPlayed = tp(at)
	}
	return st
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func TestEvaluate(t *testing.T) {
	jan, feb := "2026-01-10T20:00:00Z", "2026-02-10T20:00:00Z"
	type states = map[string]domain.UserState
	tests := []struct {
		name    string
		rules   domain.Rules
		states  states
		watched bool
		at      *time.Time
		by      []string
	}{
		{"all: everyone played", rules("all", "a", "b"), states{"a": played(jan), "b": played(feb)}, true, tp(feb), []string{"a", "b"}},
		{"all: one has not played", rules("all", "a", "b"), states{"a": played(jan), "b": {}}, false, tp(jan), []string{"a"}},
		{"all: one has no state", rules("all", "a", "b"), states{"a": played(jan)}, false, tp(jan), []string{"a"}},
		{"any: one played", rules("any", "a", "b"), states{"b": played(feb)}, true, tp(feb), []string{"b"}},
		{"any: nobody played", rules("any", "a", "b"), states{"a": {}}, false, nil, nil},
		{"no counting users", rules("all"), states{"a": played(jan)}, false, nil, nil},
		{"other users are ignored", rules("all", "a"), states{"a": played(jan), "z": played(feb)}, true, tp(jan), []string{"a"}},
		{"played without a date", rules("all", "a"), states{"a": played("")}, true, nil, []string{"a"}},
		{"nil states", rules("all", "a"), nil, false, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Evaluate(tt.rules, tt.states)
			if v.Watched != tt.watched {
				t.Errorf("Watched = %v, want %v", v.Watched, tt.watched)
			}
			if !sameTime(v.At, tt.at) {
				t.Errorf("At = %v, want %v", v.At, tt.at)
			}
			if !reflect.DeepEqual(v.By, tt.by) {
				t.Errorf("By = %v, want %v", v.By, tt.by)
			}
		})
	}
}

func TestGraceOK(t *testing.T) {
	now := *tp("2026-03-10T12:00:00Z")
	r := domain.DefaultRules()
	if !GraceOK(r, tp("2026-03-10T11:59:00Z"), now) {
		t.Error("a grace period of 0 days must always be satisfied")
	}
	r.GraceDays = 7
	if GraceOK(r, tp("2026-03-03T12:00:01Z"), now) {
		t.Error("one second short of 7 days must not satisfy a 7 day grace period")
	}
	if !GraceOK(r, tp("2026-03-03T12:00:00Z"), now) {
		t.Error("exactly 7 days must satisfy a 7 day grace period")
	}
	if !GraceOK(r, nil, now) {
		t.Error("an unknown watched date must satisfy the grace period")
	}
}

func TestMergeStates(t *testing.T) {
	a := map[string]domain.UserState{"u": {Favorite: true}}
	b := map[string]domain.UserState{"u": played("2026-01-01T00:00:00Z"), "v": {}}
	got := mergeStates(a, b)
	if !got["u"].Played || !got["u"].Favorite || got["u"].LastPlayed == nil {
		t.Errorf("merged state for u = %+v, want played, favourite and dated", got["u"])
	}
	if _, ok := got["v"]; !ok {
		t.Error("user v must be kept")
	}
	if a["u"].Played {
		t.Error("mergeStates must not change its arguments")
	}
	if mergeStates(nil, nil) != nil {
		t.Error("merging nothing must give nil")
	}
}
