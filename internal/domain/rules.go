package domain

import "strings"

const (
	WatchedAll = "all"
	WatchedAny = "any"

	UnitSeries = "series"
	UnitSeason = "season"
)

// Rules are the user-adjustable settings that decide what is suggested (spec 5.6).
type Rules struct {
	CountingUsers        []string `json:"countingUsers"` // media server user IDs
	WatchedMode          string   `json:"watchedMode"`   // WatchedAll or WatchedAny
	GraceDays            int      `json:"graceDays"`
	SeriesUnit           string   `json:"seriesUnit"` // UnitSeries or UnitSeason
	ProtectFavorites     bool     `json:"protectFavorites"`
	ProtectionTag        string   `json:"protectionTag"`
	MinRatio             float64  `json:"minRatio"`    // 0 means off
	MinSeedDays          float64  `json:"minSeedDays"` // 0 means off
	ExcludedCategories   []string `json:"excludedCategories"`
	ExcludedTags         []string `json:"excludedTags"`
	AddExclusionOnDelete bool     `json:"addExclusionOnDelete"`
}

// DefaultRules returns the rules a fresh install starts with.
func DefaultRules() Rules {
	return Rules{
		CountingUsers:      []string{},
		WatchedMode:        WatchedAll,
		SeriesUnit:         UnitSeries,
		ProtectFavorites:   true,
		ProtectionTag:      "keep",
		ExcludedCategories: []string{},
		ExcludedTags:       []string{},
	}
}

// FieldError reports an invalid value for one named setting.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// Validate checks the rules. It returns a *FieldError naming the first invalid field.
func (r Rules) Validate() error {
	if r.WatchedMode != WatchedAll && r.WatchedMode != WatchedAny {
		return &FieldError{Field: "watchedMode", Message: `must be "all" or "any"`}
	}
	if r.SeriesUnit != UnitSeries && r.SeriesUnit != UnitSeason {
		return &FieldError{Field: "seriesUnit", Message: `must be "series" or "season"`}
	}
	if r.GraceDays < 0 {
		return &FieldError{Field: "graceDays", Message: "must be 0 or more"}
	}
	if r.MinRatio < 0 {
		return &FieldError{Field: "minRatio", Message: "must be 0 or more"}
	}
	if r.MinSeedDays < 0 {
		return &FieldError{Field: "minSeedDays", Message: "must be 0 or more"}
	}
	for _, u := range r.CountingUsers {
		if strings.TrimSpace(u) == "" {
			return &FieldError{Field: "countingUsers", Message: "must not contain empty user IDs"}
		}
	}
	return nil
}
