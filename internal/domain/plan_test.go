package domain

import (
	"testing"
	"time"
)

func TestPlanExpiresTenMinutesAfterItWasBuilt(t *testing.T) {
	built := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	p := Plan{CreatedAt: built, ExpiresAt: built.Add(PlanLifetime)}
	if PlanLifetime != 10*time.Minute {
		t.Errorf("PlanLifetime = %v, want 10 minutes", PlanLifetime)
	}
	if p.Expired(built.Add(PlanLifetime - time.Nanosecond)) {
		t.Error("expired before ten minutes")
	}
	if !p.Expired(built.Add(PlanLifetime)) {
		t.Error("not expired at exactly ten minutes")
	}
}
