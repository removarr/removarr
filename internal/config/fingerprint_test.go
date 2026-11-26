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

func TestFingerprintIsStable(t *testing.T) {
	svc, _ := service(nil)
	a, _ := svc.Fingerprint(ctx)
	b, _ := svc.Fingerprint(ctx)
	if a != b {
		t.Error("two reads of unchanged settings differ")
	}
}
