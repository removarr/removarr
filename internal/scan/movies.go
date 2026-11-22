package scan

import (
	"time"

	"github.com/removarr/removarr/internal/domain"
)

// MoviesInput is everything the watched movie list is computed from.
type MoviesInput struct {
	Rules    domain.Rules
	Now      time.Time
	Users    []domain.User
	Library  domain.Library
	Snapshot domain.MovieSnapshot
}

// Movies returns the qualifying movies of the watched movie list (spec 6.2 to 6.4) as Rows,
// sorted by key. Candidates stays empty: WholeTorrents fills it.
func Movies(in MoviesInput) ListResult {
	var res ListResult
	names := userNames(in.Users)

	byTMDB := map[string][]domain.ServerMovie{}
	byIMDB := map[string][]domain.ServerMovie{}
	for _, m := range in.Library.Movies {
		if m.IDs.TMDB != "" {
			byTMDB[m.IDs.TMDB] = append(byTMDB[m.IDs.TMDB], m)
		}
		if m.IDs.IMDB != "" {
			byIMDB[m.IDs.IMDB] = append(byIMDB[m.IDs.IMDB], m)
		}
	}

	for _, m := range in.Snapshot.Movies {
		if m.File == nil {
			continue
		}
		var matches []domain.ServerMovie
		if m.IDs.TMDB != "" {
			matches = byTMDB[m.IDs.TMDB]
		}
		if len(matches) == 0 && m.IDs.IMDB != "" {
			matches = byIMDB[m.IDs.IMDB]
		}
		if len(matches) == 0 {
			res.Unmatched++
			continue
		}
		var states map[string]domain.UserState
		for _, match := range matches {
			states = mergeStates(states, match.States)
		}
		v := Evaluate(in.Rules, states)
		if !v.Watched || !GraceOK(in.Rules, v.At, in.Now) {
			continue
		}
		if hasTag(m.Tags, in.Rules.ProtectionTag) || (in.Rules.ProtectFavorites && anyFavorite(in.Rules, states)) {
			res.Protected++
			continue
		}
		res.Rows = append(res.Rows, Row{
			Candidate: domain.Candidate{
				List:      domain.ListMovies,
				Key:       domain.MovieKey(m.IDs),
				Title:     m.Title,
				Year:      m.Year,
				Size:      m.File.Size,
				WatchedBy: namesOf(names, v.By),
				WatchedAt: v.At,
			},
			Manager: ManagerMovies,
			ItemIDs: []int{m.ID},
			FileIDs: []int{m.File.ID},
		})
	}
	sortRows(res.Rows)
	return res
}
