// Package keybinding holds global key bindings for the TUI.
//
// Keys already claimed by the global dispatcher (tui.go): q, ctrl+c,
// enter, right, backspace, left, p, t, ?, space, a, d, u, D, r, c, B, s.
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
)
