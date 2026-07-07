package tui

import (
	"os"
	"runtime"
	"testing"
)

// TestTermLacksREP pins the TERM classification driving the renderer
// workaround. It must mirror cellbuf's own base-name matching: only the
// terminals cellbuf treats as xterm-like but that don't implement REP
// (CSI Ps b) need the standard renderer.
func TestTermLacksREP(t *testing.T) {
	cases := []struct {
		term string
		want bool
	}{
		{"screen", true},
		{"screen-256color", true},
		{"linux", true},
		// cellbuf does not classify "screen.xterm" as xterm-like, so it
		// never emits REP there — no workaround needed.
		{"screen.xterm-256color", false},
		{"xterm", false},
		{"xterm-256color", false},
		{"tmux-256color", false},
		{"alacritty", false},
		{"foot", false},
		{"", false},
		{"dumb", false},
	}
	for _, c := range cases {
		if got := termLacksREP(c.term); got != c.want {
			t.Errorf("termLacksREP(%q) = %v, want %v", c.term, got, c.want)
		}
	}
}

// TestNeedsStandardRenderer pins the GOOS/TERM matrix: Windows always
// forces the standard renderer (conhost's partial VT support and beta1's
// missing resize events make the cursed renderer unsafe there), unix only
// when $TERM lacks REP.
func TestNeedsStandardRenderer(t *testing.T) {
	cases := []struct {
		goos string
		term string
		want bool
	}{
		{"windows", "", true},
		{"windows", "xterm-256color", true}, // e.g. MSYS/Cygwin shells
		{"linux", "xterm-256color", false},
		{"linux", "screen-256color", true},
		{"darwin", "screen", true},
		{"darwin", "xterm-256color", false},
		{"linux", "", false},
	}
	for _, c := range cases {
		if got := needsStandardRenderer(c.goos, c.term); got != c.want {
			t.Errorf("needsStandardRenderer(%q, %q) = %v, want %v", c.goos, c.term, got, c.want)
		}
	}
}

// TestEnsureCompatRenderer pins the env contract: under a REP-less TERM
// the standard renderer is forced, elsewhere the env is left unset, and
// an explicit user setting is never overridden. (Runs on the host GOOS —
// non-windows in CI — so the Windows branch is pinned by
// TestNeedsStandardRenderer instead.)
func TestEnsureCompatRenderer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows forces the standard renderer regardless of TERM")
	}
	cases := []struct {
		name    string
		term    string
		preset  *string // pre-existing TEA_STANDARD_RENDERER value
		wantSet bool
		wantVal string
	}{
		{name: "screen forces standard renderer", term: "screen-256color", wantSet: true, wantVal: "1"},
		{name: "linux console forces standard renderer", term: "linux", wantSet: true, wantVal: "1"},
		{name: "xterm keeps default renderer", term: "xterm-256color", wantSet: false},
		{name: "explicit user opt-out wins", term: "screen-256color", preset: strPtr("0"), wantSet: true, wantVal: "0"},
		{name: "explicit user opt-in preserved", term: "xterm-256color", preset: strPtr("true"), wantSet: true, wantVal: "true"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("TERM", c.term)
			// t.Setenv registers cleanup; start from a known-unset state.
			t.Setenv(teaStandardRendererEnv, "")
			os.Unsetenv(teaStandardRendererEnv) //nolint:errcheck
			if c.preset != nil {
				t.Setenv(teaStandardRendererEnv, *c.preset)
			}

			ensureCompatRenderer()

			got, set := os.LookupEnv(teaStandardRendererEnv)
			if set != c.wantSet {
				t.Fatalf("%s set = %v, want %v", teaStandardRendererEnv, set, c.wantSet)
			}
			if set && got != c.wantVal {
				t.Errorf("%s = %q, want %q", teaStandardRendererEnv, got, c.wantVal)
			}
		})
	}
}

func strPtr(s string) *string { return &s }
