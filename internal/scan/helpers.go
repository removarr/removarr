package scan

import (
	"strings"

	"github.com/removarr/removarr/internal/domain"
)

// userNames maps user IDs to display names.
func userNames(users []domain.User) map[string]string {
	out := make(map[string]string, len(users))
	for _, u := range users {
		out[u.ID] = u.Name
	}
	return out
}

// namesOf returns the display names of the given user IDs, falling back to the ID itself.
func namesOf(names map[string]string, ids []string) []string {
	var out []string
	for _, id := range ids {
		if n := names[id]; n != "" {
			out = append(out, n)
		} else {
			out = append(out, id)
		}
	}
	return out
}

// hasTag reports whether tags contains tag, ignoring case. An empty tag matches nothing.
func hasTag(tags []string, tag string) bool {
	if tag == "" {
		return false
	}
	for _, t := range tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

// anyFavorite reports whether a counting user marked the item a favourite.
func anyFavorite(r domain.Rules, states map[string]domain.UserState) bool {
	for _, id := range r.CountingUsers {
		if states[id].Favorite {
			return true
		}
	}
	return false
}

// torrentInfos returns the details of those downloads that exist in the client, and whether
// any of them is below the seeding minimum. The returned slice is never nil.
func torrentInfos(r domain.Rules, byID map[string]domain.Download, ids []string) ([]domain.TorrentInfo, bool) {
	infos := []domain.TorrentInfo{}
	below := false
	for _, id := range ids {
		d, ok := byID[id]
		if !ok {
			continue
		}
		info := torrentInfo(r, d)
		infos = append(infos, info)
		below = below || info.BelowSeedMin
	}
	return infos, below
}
