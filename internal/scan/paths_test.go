package scan

import "testing"

func TestKeepsDataWhenUnsure(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		others []string
		want   bool
	}{
		{"nothing else stays", "/dl/one", nil, false},
		{"its own path is unknown", "", []string{"/dl/two"}, true},
		{"a path of separators only is unknown", "//", nil, true},
		{"another path is unknown", "/dl/one", []string{""}, true},
		{"the same path", "/dl/one", []string{"/dl/one"}, true},
		{"the same path in another case", "/dl/One", []string{"/DL/one/"}, true},
		{"the same path with Windows separators", `C:\dl\one`, []string{"c:/dl//one"}, true},
		{"inside another", "/dl/pack/one.mkv", []string{"/dl/pack"}, true},
		{"holding another", "/dl/pack", []string{"/dl/pack/one.mkv"}, true},
		{"only the same letters at the start", "/dl/one", []string{"/dl/one-two"}, false},
	}
	for _, c := range cases {
		if got := KeepsData(c.path, c.others); got != c.want {
			t.Errorf("%s: KeepsData(%q, %q) = %v, want %v", c.name, c.path, c.others, got, c.want)
		}
	}
}
