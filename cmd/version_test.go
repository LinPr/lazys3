package cmd

import (
	"runtime/debug"
	"strings"
	"testing"
)

// TestResolvedVersionPrefersLdflags pins the precedence: a build that passed
// -X ...cmd.Version always wins, so release binaries report the exact tag
// the workflow injected rather than whatever the VCS metadata says.
func TestResolvedVersionPrefersLdflags(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })

	Version = "v1.2.3"
	if got := resolvedVersion(); got != "v1.2.3" {
		t.Fatalf("resolvedVersion() = %q, want the ldflags value", got)
	}
}

// TestResolvedVersionFallsBackToBuildInfo pins the case that matters for
// `go install pkg@version`: the toolchain cannot pass ldflags, so without a
// fallback a tagged install reports "dev". The module version recorded in
// build info is the tag that was actually installed.
func TestResolvedVersionFallsBackToBuildInfo(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })

	// Whatever this test binary was built from, the fallback must produce a
	// usable string: either the module version or the documented "dev".
	for _, sentinel := range []string{"dev", ""} {
		Version = sentinel
		got := resolvedVersion()
		if got == "" {
			t.Fatalf("resolvedVersion() with Version=%q returned empty", sentinel)
		}
		bi, ok := debug.ReadBuildInfo()
		if !ok {
			continue
		}
		want := bi.Main.Version
		if want == "" || want == "(devel)" {
			want = "dev"
		}
		if got != want {
			t.Errorf("resolvedVersion() = %q, want %q from build info", got, want)
		}
	}
}

// TestResolvedVersionNeverReturnsDirtyDevel pins that the raw Go sentinel
// "(devel)" never reaches the user: it is meaningless in `--version` output.
func TestResolvedVersionNeverReturnsDirtyDevel(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })
	Version = "dev"
	if got := resolvedVersion(); strings.Contains(got, "(devel)") {
		t.Fatalf("resolvedVersion() leaked the Go sentinel: %q", got)
	}
}
