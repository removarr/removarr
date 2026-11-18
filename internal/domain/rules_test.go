package domain

import (
	"errors"
	"testing"
)

func TestDefaultRulesAreValid(t *testing.T) {
	if err := DefaultRules().Validate(); err != nil {
		t.Fatalf("default rules must validate: %v", err)
	}
}

func TestDefaultRulesMatchTheSpec(t *testing.T) {
	r := DefaultRules()
	if r.WatchedMode != WatchedAll || r.SeriesUnit != UnitSeries || r.GraceDays != 0 {
		t.Errorf("unexpected defaults: %+v", r)
	}
	if !r.ProtectFavorites || r.ProtectionTag != "keep" {
		t.Errorf("protection defaults wrong: %+v", r)
	}
	if r.MinRatio != 0 || r.MinSeedDays != 0 || r.AddExclusionOnDelete {
		t.Errorf("seeding and exclusion must default to off: %+v", r)
	}
	if r.CountingUsers == nil || r.ExcludedCategories == nil || r.ExcludedTags == nil {
		t.Errorf("list defaults must be empty, not nil, so they serialise as []: %+v", r)
	}
}

func TestRulesValidateNamesTheField(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Rules)
		field  string
	}{
		{"watched mode", func(r *Rules) { r.WatchedMode = "most" }, "watchedMode"},
		{"series unit", func(r *Rules) { r.SeriesUnit = "episode" }, "seriesUnit"},
		{"grace days", func(r *Rules) { r.GraceDays = -1 }, "graceDays"},
		{"min ratio", func(r *Rules) { r.MinRatio = -0.5 }, "minRatio"},
		{"min seed days", func(r *Rules) { r.MinSeedDays = -1 }, "minSeedDays"},
		{"empty user", func(r *Rules) { r.CountingUsers = []string{"u1", " "} }, "countingUsers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := DefaultRules()
			tt.change(&r)
			var fe *FieldError
			if err := r.Validate(); !errors.As(err, &fe) {
				t.Fatalf("want *FieldError, got %v", err)
			}
			if fe.Field != tt.field {
				t.Errorf("field = %q, want %q", fe.Field, tt.field)
			}
		})
	}
}
