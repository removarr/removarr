package scan

import (
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

func TestSeasonFinished(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	aired := func(id, season int, at *time.Time) domain.Episode {
		return domain.Episode{ID: id, Season: season, Number: id, AirDate: at}
	}
	tests := []struct {
		name   string
		series domain.Series
		season int
		want   bool
	}{
		{"an ended series has finished every season", domain.Series{Ended: true, Episodes: []domain.Episode{aired(1, 1, &future)}}, 1, true},
		{"aired, with a later season", domain.Series{Episodes: []domain.Episode{aired(1, 1, &past), aired(2, 2, &future)}}, 1, true},
		{"the newest season of a running series", domain.Series{Episodes: []domain.Episode{aired(1, 1, &past)}}, 1, false},
		{"an episode that has not aired yet", domain.Series{Episodes: []domain.Episode{aired(1, 1, &past), aired(2, 1, &future), aired(3, 2, nil)}}, 1, false},
		{"an episode without an air date", domain.Series{Episodes: []domain.Episode{aired(1, 1, nil), aired(3, 2, &past)}}, 1, false},
		{"an episode airing right now has not aired", domain.Series{Episodes: []domain.Episode{aired(1, 1, &now), aired(3, 2, &past)}}, 1, false},
		{"a season with no episodes", domain.Series{Episodes: []domain.Episode{aired(3, 2, &past)}}, 1, false},
	}
	for _, tt := range tests {
		if got := SeasonFinished(tt.series, tt.season, now); got != tt.want {
			t.Errorf("%s: SeasonFinished = %v, want %v", tt.name, got, tt.want)
		}
	}
}
