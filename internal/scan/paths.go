package scan

import "strings"

// KeepsData reports whether a download with content path path must keep its data when it is
// removed while downloads with the content paths others stay (spec 6.7, shared data). Unsure
// means keep: an unknown path, its own or one that stays, keeps the data.
func KeepsData(path string, others []string) bool {
	mine := NormalPath(path)
	if mine == "" {
		return true
	}
	for _, o := range others {
		if other := NormalPath(o); other == "" || PathsOverlap(mine, other) {
			return true
		}
	}
	return false
}

// NormalPath makes content paths comparable, in the direction that only ever keeps more data:
// two paths that might name the same place compare equal. It reads `\` as `/`, collapses
// repeated separators, drops a trailing one and lower-cases the path, so that paths that
// differ only by the client's platform, a doubled separator or case are taken as the same. A
// path of separators only becomes "", unknown.
func NormalPath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	return strings.ToLower(strings.TrimRight(p, "/"))
}

// PathsOverlap reports whether two normalised content paths are the same or one lies inside
// the other.
func PathsOverlap(a, b string) bool { return a == b || within(a, b) || within(b, a) }

// within reports whether the normalised path p lies inside the directory dir. The separator
// is part of the prefix, so /dl/one-two is not inside /dl/one.
func within(p, dir string) bool { return strings.HasPrefix(p, dir+"/") }
