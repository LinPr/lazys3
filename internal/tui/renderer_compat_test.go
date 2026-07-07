package tui

import (
	"bytes"
	"regexp"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

// repSeq matches the REP escape sequence (CSI Ps b, "repeat preceding
// character"). GNU screen and the Linux console silently ignore it, which
// under bubbletea v2.0.0-beta1's cellbuf renderer left permanent ghosting
// artifacts; this project shipped a TEA_STANDARD_RENDERER workaround
// (the since-removed renderer.go) for exactly that.
var repSeq = regexp.MustCompile(`\x1b\[[0-9]*b`)

// renderRepeatedRun renders a full-width run of identical cells (the
// pattern REP compresses) through ultraviolet's terminal renderer for the
// given TERM and returns the emitted bytes.
func renderRepeatedRun(t *testing.T, term string) string {
	t.Helper()
	var out bytes.Buffer
	r := uv.NewTerminalRenderer(&out, []string{"TERM=" + term})
	r.Resize(80, 3)
	buf := uv.NewRenderBuffer(80, 3)
	cell := uv.Cell{Content: "A", Width: 1}
	for y := 0; y < 3; y++ {
		for x := 0; x < 80; x++ {
			buf.SetCell(x, y, &cell)
		}
	}
	r.Render(buf)
	if err := r.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	return out.String()
}

// TestUltravioletOmitsREPForScreenAndLinux pins the upstream behavior that
// replaced renderer.go's TEA_STANDARD_RENDERER workaround: bubbletea
// v2.0.8's ultraviolet backend must never emit REP for terminals that do
// not implement it (GNU screen, the Linux console), per its per-terminal
// capability table (ultraviolet terminal_renderer.go, xtermCaps). If this
// test starts failing on a dependency bump, the GNU-screen ghosting bug is
// back and needs a new workaround.
//
// The empty-TERM case pins the testable half of the workaround's OTHER
// ground: renderer.go also forced the standard renderer on ALL of Windows
// because conhost (which usually runs with TERM unset) garbled cellbuf's
// optional sequences. ultraviolet's xtermCaps("") returns the zero
// capability set, so an unset TERM must produce plain output with none of
// the optional sequences — REP being the representative probe here. The
// untestable half (Windows resize delivery via console input records,
// ultraviolet terminal_reader_windows.go) needs live conhost verification.
func TestUltravioletOmitsREPForScreenAndLinux(t *testing.T) {
	for _, term := range []string{"screen", "screen-256color", "linux", ""} {
		if out := renderRepeatedRun(t, term); repSeq.MatchString(out) {
			t.Errorf("TERM=%s: renderer emitted REP, which this terminal ignores (output %q)", term, out)
		}
	}
}

// TestUltravioletEmitsREPWhereSupported is the positive control proving
// the probe above actually exercises the REP code path: on a terminal
// whose capability table includes REP (tmux), the same repeated-run frame
// must compress via REP. If upstream stops using REP entirely, the screen/
// linux assertion above becomes vacuous — surface that here instead of
// silently passing.
func TestUltravioletEmitsREPWhereSupported(t *testing.T) {
	if out := renderRepeatedRun(t, "tmux-256color"); !repSeq.MatchString(out) {
		t.Errorf("TERM=tmux-256color: expected REP in output for a repeated run, got %q", out)
	}
}
