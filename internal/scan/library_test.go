package scan

import (
	"reflect"
	"testing"

	"github.com/removarr/removarr/internal/domain"
)

func TestMovieFiles(t *testing.T) {
	s := domain.MovieSnapshot{Movies: []domain.Movie{
		{ID: 1, File: &domain.MediaFile{ID: 10, Size: 700}},
		{ID: 2}, // no file
		{ID: 3, File: &domain.MediaFile{ID: 30, Size: 5}},
	}}
	want := []LibraryFile{{ID: 10, Size: 700, Items: []int{1}}, {ID: 30, Size: 5, Items: []int{3}}}
	if got := MovieFiles(s); !reflect.DeepEqual(got, want) {
		t.Errorf("MovieFiles = %+v, want %+v", got, want)
	}
}

func TestEpisodeFilesGivesASharedFileEveryEpisode(t *testing.T) {
	shared := &domain.MediaFile{ID: 50, Size: 900}
	s := domain.SeriesSnapshot{Series: []domain.Series{
		{ID: 1, Episodes: []domain.Episode{
			{ID: 11, File: shared},
			{ID: 12, File: shared},
			{ID: 13, File: &domain.MediaFile{ID: 51, Size: 400}},
		}},
		{ID: 2, Episodes: []domain.Episode{{ID: 21, File: shared}}},
	}}
	want := []LibraryFile{
		{ID: 50, Size: 900, Items: []int{11, 12, 21}},
		{ID: 51, Size: 400, Items: []int{13}},
	}
	if got := EpisodeFiles(s); !reflect.DeepEqual(got, want) {
		t.Errorf("EpisodeFiles = %+v, want %+v", got, want)
	}
}

func TestEpisodeFilesSkipsAnEpisodeWithoutAFile(t *testing.T) {
	s := domain.SeriesSnapshot{Series: []domain.Series{{ID: 1, Episodes: []domain.Episode{
		{ID: 11},
		{ID: 12, File: &domain.MediaFile{ID: 60, Size: 10}},
	}}}}
	want := []LibraryFile{{ID: 60, Size: 10, Items: []int{12}}}
	if got := EpisodeFiles(s); !reflect.DeepEqual(got, want) {
		t.Errorf("EpisodeFiles = %+v, want %+v", got, want)
	}
}
