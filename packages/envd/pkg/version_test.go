package pkg

import (
	"strings"
	"testing"

	"golang.org/x/mod/semver"
)

// TestVersionFormat ensures the Version literal stays valid SemVer 2.0 so the
// shared snapshot-compatibility check (MinEnvdVersionForSnapshot) keeps working.
func TestVersionFormat(t *testing.T) {
	v := Version
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		t.Fatalf("Version %q is not valid SemVer 2.0 (must satisfy semver.IsValid after `v`-prefix)", Version)
	}
}

// TestVersionGTEMinSnapshot ensures any bump stays >= 0.5.0 so we don't break
// the snapshot-creation guard in packages/shared/pkg/utils.
func TestVersionGTEMinSnapshot(t *testing.T) {
	v := Version
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if semver.Compare(v, "v0.5.0") < 0 {
		t.Fatalf("Version %q < v0.5.0 — would break CheckEnvdVersionForSnapshot", Version)
	}
}

// TestVersionNotE2BBase guards against accidentally regressing to the upstream
// e2b base "0.5.18" (bugbot-forensik P2 2026-05-14: GetVersion was reporting
// the upstream value while binary contained MaxiCore content).
func TestVersionNotE2BBase(t *testing.T) {
	if Version == "0.5.18" {
		t.Fatalf("Version regressed to upstream e2b base %q — bump it to reflect MaxiCore build "+
			"(see pkg/version.go + Makefile LDFLAGS injection)", Version)
	}
}
