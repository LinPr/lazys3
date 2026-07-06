// Package keybinding holds global key bindings for the TUI.
//
// Keys already claimed by the global dispatcher (tui.go): q, ctrl+c,
// enter, right, backspace, left, p, t, T, ?, space, a, d, u, D, r, c, B,
// s, x, y, Y, v, V, l, tab.
// The object-list component additionally claims the keys below; they are
// exported so the help overlay and the component reference one source of
// truth.
package keybinding

import "strings"

// KeyString canonicalizes a key press string for binding lookups.
// Depending on the terminal, bubbletea v2 reports a shifted letter
// either as the uppercase letter itself ("B") or as "shift+b"; fold the
// latter into the former so bindings can be declared as the character
// the user typed.
func KeyString(s string) string {
	if r, ok := strings.CutPrefix(s, "shift+"); ok && len(r) == 1 && r[0] >= 'a' && r[0] <= 'z' {
		return strings.ToUpper(r)
	}
	return s
}

const (
	// Filter is the bubbles/list built-in key that starts filtering on
	// the focused list.
	Filter = "/"

	// ObjectSortCycle cycles the object list sort field
	// (name -> size -> time).
	ObjectSortCycle = "o"

	// ObjectSortReverse toggles the object list sort direction
	// (ascending <-> descending).
	ObjectSortReverse = "O"

	// TransferCancel cancels the most recent running transfer (global,
	// handled in tui.go).
	TransferCancel = "x"

	// YankURI ("yank") copies the highlighted item's identifier to the
	// clipboard: the s3:// URI in the remote panes (bucket or object;
	// directories yield their prefix URI), the absolute path in the
	// dual-pane local pane.
	YankURI = "y"

	// Presign (shift+y) generates a presigned share URL for the
	// highlighted object (files only, remote pane).
	Presign = "Y"

	// TransfersToggle opens/closes the live transfers overlay (global,
	// handled in tui.go).
	TransfersToggle = "t"

	// HistoryToggle (shift+t) opens/closes the persistent transfer-history
	// overlay (global, handled in tui.go).
	HistoryToggle = "T"

	// VersionsToggle opens/closes the object-versions overlay for the
	// highlighted file (global, handled in tui.go).
	VersionsToggle = "v"

	// VersioningToggle (shift+v) toggles bucket versioning
	// (Enabled <-> Suspended) on the highlighted bucket.
	VersioningToggle = "V"

	// DualPaneToggle enters/exits the dual-pane (local ⇄ remote) layout
	// (global, handled in tui.go; mnemonic: local).
	DualPaneToggle = "l"

	// PaneSwitch moves focus between the remote and local panes while
	// dual-pane mode is active (global, handled in tui.go).
	PaneSwitch = "tab"
)
