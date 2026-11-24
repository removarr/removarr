package domain

import (
	"encoding/json"
	"testing"
	"time"
)

// Stored scans and the API depend on these names.
func TestJSONNamesOfTheTiedAndOutsideAndWaitingFields(t *testing.T) {
	keys := func(v any) map[string]json.RawMessage {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	has := func(m map[string]json.RawMessage, name string, want bool) {
		t.Helper()
		if _, ok := m[name]; ok != want {
			t.Errorf("key %q present = %v, want %v", name, ok, want)
		}
	}

	has(keys(Candidate{TiedWith: []RowRef{{List: ListSeries, Key: "tvdb:1"}}}), "tiedWith", true)
	has(keys(Candidate{}), "tiedWith", false)

	has(keys(TorrentInfo{OutsideLibrary: 1 << 30}), "outsideLibrary", true)
	has(keys(TorrentInfo{}), "outsideLibrary", false)

	has(keys(ListSummary{Waiting: 2}), "waiting", true)
	has(keys(ListSummary{}), "waiting", true)

	at := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	has(keys(Candidate{Waiting: true, IgnoredAt: &at, NotInScan: true}), "waiting", true)
	has(keys(Candidate{Waiting: true, IgnoredAt: &at, NotInScan: true}), "ignoredAt", true)
	has(keys(Candidate{Waiting: true, IgnoredAt: &at, NotInScan: true}), "notInScan", true)
	has(keys(Candidate{}), "waiting", false)
	has(keys(Candidate{}), "ignoredAt", false)
	has(keys(Candidate{}), "notInScan", false)

	has(keys(Candidate{Merged: true}), "merged", true)
	has(keys(Candidate{}), "merged", false)

	has(keys(Candidate{Waiting: true, WaitReason: "It waits."}), "waitReason", true)
	has(keys(Candidate{}), "waitReason", false)
}
