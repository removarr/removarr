package config

import (
	"testing"

	"github.com/removarr/removarr/internal/domain"
)

func TestFingerprintChangesWithRulesAndAddresses(t *testing.T) {
	svc, _ := service(nil)
	base, err := svc.Fingerprint(ctx)
	if err != nil || base == "" {
		t.Fatalf("Fingerprint() = %q, %v", base, err)
	}

	rules := domain.DefaultRules()
	rules.GraceDays = 7
	if err := svc.SetRules(ctx, rules); err != nil {
		t.Fatal(err)
	}
	afterRules, _ := svc.Fingerprint(ctx)
	if afterRules == base {
		t.Error("changing a rule did not change the fingerprint")
	}

	if err := svc.SetConn(ctx, field(t, AppSonarr, "url"), "http://192.0.2.1:8989"); err != nil {
		t.Fatal(err)
	}
	afterURL, _ := svc.Fingerprint(ctx)
	if afterURL == afterRules {
		t.Error("changing a URL did not change the fingerprint")
	}

	if err := svc.SetConn(ctx, field(t, AppQBittorrent, "username"), "someone"); err != nil {
		t.Fatal(err)
	}
	afterUser, _ := svc.Fingerprint(ctx)
	if afterUser == afterURL {
		t.Error("changing a user name did not change the fingerprint")
	}
}

func TestFingerprintLeavesSecretsOut(t *testing.T) {
	svc, _ := service(nil)
	before, _ := svc.Fingerprint(ctx)
	if err := svc.SetConn(ctx, field(t, AppSonarr, "apiKey"), "0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	after, _ := svc.Fingerprint(ctx)
	if after != before {
		t.Error("a secret changed the fingerprint")
	}
}

// The order of a list of users, categories or tags does not change what a scan finds, so it
// must not mark the lists out of date.
func TestFingerprintIgnoresTheOrderOfTheRuleLists(t *testing.T) {
	svc, _ := service(nil)
	rules := domain.DefaultRules()
	rules.CountingUsers = []string{"u1", "u2"}
	rules.ExcludedCategories = []string{"a", "b"}
	rules.ExcludedTags = []string{"x", "y"}
	if err := svc.SetRules(ctx, rules); err != nil {
		t.Fatal(err)
	}
	first, _ := svc.Fingerprint(ctx)

	rules.CountingUsers = []string{"u2", "u1"}
	rules.ExcludedCategories = []string{"b", "a"}
	rules.ExcludedTags = []string{"y", "x"}
	if err := svc.SetRules(ctx, rules); err != nil {
		t.Fatal(err)
	}
	reordered, _ := svc.Fingerprint(ctx)
	if reordered != first {
		t.Error("re-ordering the rule lists changed the fingerprint")
	}
	saved, err := svc.Rules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if saved.CountingUsers[0] != "u2" || saved.ExcludedCategories[0] != "b" || saved.ExcludedTags[0] != "y" {
		t.Errorf("Fingerprint changed the saved rules: %+v", saved)
	}

	for name, change := range map[string]func(*domain.Rules){
		"countingUsers":      func(r *domain.Rules) { r.CountingUsers = []string{"u1"} },
		"excludedCategories": func(r *domain.Rules) { r.ExcludedCategories = []string{"a", "c"} },
		"excludedTags":       func(r *domain.Rules) { r.ExcludedTags = []string{"x"} },
	} {
		r := rules
		change(&r)
		if err := svc.SetRules(ctx, r); err != nil {
			t.Fatal(err)
		}
		if got, _ := svc.Fingerprint(ctx); got == first {
			t.Errorf("changing %s did not change the fingerprint", name)
		}
	}
}

func TestFingerprintIsStable(t *testing.T) {
	svc, _ := service(nil)
	a, _ := svc.Fingerprint(ctx)
	b, _ := svc.Fingerprint(ctx)
	if a != b {
		t.Error("two reads of unchanged settings differ")
	}
}
