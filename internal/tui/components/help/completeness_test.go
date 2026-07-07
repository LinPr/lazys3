package help

import (
	"strings"
	"testing"
)

// claimedKeys is the single source of truth for every key the app reacts
// to: the tui.go global switch, the file-op dispatch, the overlay scroll
// handlers, and the bubbles-list built-ins the help documents. Each entry
// lists the spellings the help content may use for it; adding a new key to
// the dispatcher without documenting it here AND in DefaultBindings makes
// this test fail loudly.
var claimedKeys = []struct {
	key       string
	spellings []string // acceptable key-column tokens; nil = the key itself
}{
	// Quit
	{key: "q"},
	{key: "ctrl+c"},
	// Navigation
	{key: "enter"},
	{key: "right", spellings: []string{"→", "right"}},
	{key: "backspace"},
	{key: "left", spellings: []string{"←", "left"}},
	{key: "p"},
	{key: "m"},
	// Selection
	{key: "space"},
	{key: "a"},
	// File ops
	{key: "d"},
	{key: "u"},
	{key: "D"},
	{key: "r"},
	{key: "c"},
	{key: "B"},
	{key: "s"},
	{key: "y"},
	{key: "Y"},
	{key: "v"},
	{key: "V"},
	// Search & sort
	{key: "/"},
	{key: "o"},
	{key: "O"},
	{key: "esc"},
	// Dual-pane
	{key: "l"},
	{key: "tab"},
	// Panels, transfers, overlays
	{key: "t"},
	{key: "T"},
	{key: "x"},
	{key: "?"},
	// Overlay scrolling
	{key: "j"},
	{key: "k"},
	{key: "g"},
	{key: "G"},
	{key: "pgup"},
	{key: "pgdn", spellings: []string{"pgdn", "pgdown"}},
}

// keyTokens splits every Binding.Key of the default groups into individual
// key tokens ("enter / →" -> "enter", "→"; "↑ / k, ↓ / j" -> "↑", "k", "↓",
// "j") so single-letter keys are matched exactly, never as substrings of
// prose.
func keyTokens(t *testing.T, groups []Group) map[string]bool {
	t.Helper()
	tokens := map[string]bool{}
	for _, g := range groups {
		for _, b := range g.Bindings {
			for _, part := range strings.FieldsFunc(b.Key, func(r rune) bool {
				return r == '/' || r == ',' || r == ' '
			}) {
				if part != "" {
					tokens[part] = true
				}
			}
		}
	}
	// "/" itself is consumed as a separator above; recover it from any
	// binding whose Key is exactly "/".
	for _, g := range groups {
		for _, b := range g.Bindings {
			if strings.TrimSpace(b.Key) == "/" {
				tokens["/"] = true
			}
		}
	}
	return tokens
}

// paneGroups are the two focus-variant groups: a file-op key whose
// behaviour depends on pane focus may be documented once in EACH of them
// (and nowhere else). Every other key must appear in exactly one group.
var paneGroups = map[string]bool{
	"Remote pane (S3)": true,
	"Local pane":       true,
}

// tokenGroups maps each key-column token to the set of group names that
// document it, using the same tokenization as keyTokens.
func tokenGroups(groups []Group) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	add := func(tok, group string) {
		if out[tok] == nil {
			out[tok] = map[string]bool{}
		}
		out[tok][group] = true
	}
	for _, g := range groups {
		for _, b := range g.Bindings {
			if strings.TrimSpace(b.Key) == "/" {
				add("/", g.Name)
				continue
			}
			for _, part := range strings.FieldsFunc(b.Key, func(r rune) bool {
				return r == '/' || r == ',' || r == ' '
			}) {
				if part != "" {
					add(part, g.Name)
				}
			}
		}
	}
	return out
}

// TestHelpDocumentsKeysExactlyOnce asserts no key is documented in more
// than one group — the redundancy the old File Ops / Dual-pane split had.
// The only allowed exception is a key appearing exactly once in each of
// the two pane groups (its per-focus variants).
func TestHelpDocumentsKeysExactlyOnce(t *testing.T) {
	for tok, groups := range tokenGroups(DefaultBindings()) {
		if len(groups) == 1 {
			continue
		}
		allPane := len(groups) <= len(paneGroups)
		for g := range groups {
			if !paneGroups[g] {
				allPane = false
			}
		}
		if !allPane {
			names := make([]string, 0, len(groups))
			for g := range groups {
				names = append(names, g)
			}
			t.Errorf("key %q is documented in %d groups %v, want exactly one (or one per pane group)",
				tok, len(groups), names)
		}
	}
}

// TestHelpDocumentsEveryClaimedKey asserts that every key the app claims
// appears in the help overlay's key column, in at least one of its
// accepted spellings.
func TestHelpDocumentsEveryClaimedKey(t *testing.T) {
	tokens := keyTokens(t, DefaultBindings())
	for _, ck := range claimedKeys {
		spellings := ck.spellings
		if spellings == nil {
			spellings = []string{ck.key}
		}
		found := false
		for _, s := range spellings {
			if tokens[s] {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("key %q is claimed by the app but missing from the help content (accepted spellings: %v)",
				ck.key, spellings)
		}
	}
}
