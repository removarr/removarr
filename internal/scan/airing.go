package scan

import (
	"time"

	"github.com/removarr/removarr/internal/domain"
)

// SeasonFinished reports whether a season has finished airing (spec 6.3): the series has
// ended, or a later season exists and every episode of the season aired before now. An
// episode without an air date has not aired, so the newest season of a series that has not
// ended never counts as finished.
func SeasonFinished(s domain.Series, season int, now time.Time) bool {
	if s.Ended {
		return true
	}
	later, episodes := false, 0
	for _, e := range s.Episodes {
		if e.Season > season {
			later = true
		}
		if e.Season != season {
			continue
		}
		episodes++
		if e.AirDate == nil || !e.AirDate.Before(now) {
			return false
		}
	}
	return later && episodes > 0
}
