package tui

import (
	"os"
	"strings"
)

// teaStandardRendererEnv is bubbletea v2's escape hatch: when it parses as
// a true boolean, tea.Program uses the legacy line-diff ("standard")
// renderer instead of the default cell-diff ("cursed") renderer.
const teaStandardRendererEnv = "TEA_STANDARD_RENDERER"

// termLacksREP reports whether the terminal identified by term is treated
// as xterm-like by bubbletea's cell renderer but does not implement the
// REP escape sequence (CSI Ps b, "repeat preceding character").
//
// bubbletea v2.0.0-beta1's default renderer (cellbuf v0.0.13) emits REP
// for any run of >= 5 identical cells on a repainted line — e.g. the
// padding spaces after a short row like a directory entry, a status-bar
// fill, or the help overlay's dimmed background. GNU screen and the Linux
// console silently ignore REP, so those cells are never painted: the
// previous frame's text stays on screen while the renderer's model thinks
// the cells were updated, leaving permanent artifacts ("stale tails")
// that even cursor movement doesn't clear.
//
// Upstream fixed this after cellbuf v0.0.13 by dropping the REP
// capability for exactly these terminals (see cellbuf's xtermCaps:
// `case "screen": v = allCaps; v &^= capREP` and the reduced capability
// set for "linux"), but that cellbuf requires a newer bubbletea than the
// beta1 this project pins. Until the dependency is upgraded, we match
// cellbuf's own TERM classification here: it takes the part of $TERM
// before the first '-' ("screen-256color" -> "screen"). Any other value —
// including "screen.xterm-256color", which cellbuf does not classify as
// xterm-like and therefore never sends REP to — is safe.
func termLacksREP(term string) bool {
	base, _, _ := strings.Cut(term, "-")
	switch base {
	case "screen", "linux":
		return true
	}
	return false
}

// ensureCompatRenderer selects bubbletea's legacy standard renderer when
// $TERM identifies a terminal that the default cell renderer corrupts
// (see termLacksREP). The standard renderer repaints whole changed lines
// and erases to end-of-line — sequences every terminal supports — so it
// renders correctly (if slightly less efficiently) under GNU screen.
//
// It must run before tea.Program.Run decides on a renderer; calling it
// from NewLazyS3Model guarantees that. An explicit TEA_STANDARD_RENDERER
// set by the user always wins, whatever its value.
func ensureCompatRenderer() {
	if _, explicit := os.LookupEnv(teaStandardRendererEnv); explicit {
		return
	}
	if termLacksREP(os.Getenv("TERM")) {
		os.Setenv(teaStandardRendererEnv, "1") //nolint:errcheck
	}
}
