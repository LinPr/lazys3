package keybinding

import "testing"

func TestKeyString(t *testing.T) {
	cases := map[string]string{
		"shift+b":   "B",
		"shift+d":   "D",
		"shift+o":   "O",
		"B":         "B",
		"b":         "b",
		"space":     "space",
		"ctrl+c":    "ctrl+c",
		"shift+tab": "shift+tab",
		"esc":       "esc",
		"?":         "?",
	}
	for in, want := range cases {
		if got := KeyString(in); got != want {
			t.Errorf("KeyString(%q) = %q, want %q", in, got, want)
		}
	}
}
