package domain

import (
	"encoding/json"
	"testing"
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
}
