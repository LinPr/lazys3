// Tests for the help-overlay key routing in tui.go's Update: scroll keys
// must be folded through keybinding.KeyString so shifted letters work on
// terminals that report them as "shift+g" rather than "G".
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea/v2"
)

// TestHelpShiftGScrollsToBottom pins that "shift+g" (the alternate report
// of a shifted 'g') jumps the help overlay to its bottom, exactly like the
// canonical "G" the help component matches on.
func TestHelpShiftGScrollsToBottom(t *testing.T) {
	m := NewLazyS3Model()
	// 15 rows: the help content overflows, so scrolling is live.
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 15})
	m = updateModel(t, m, keyPress('?'))
	if !m.help.IsVisible() {
		t.Fatal("'?' did not open the help overlay")
	}
	if strings.Contains(m.help.View(), "close the overlay") {
		t.Fatal("last binding already visible on a 15-row terminal (content should overflow)")
	}
	m = updateModel(t, m, tea.KeyPressMsg(tea.Key{Code: 'g', Mod: tea.ModShift}))
	if !strings.Contains(m.help.View(), "close the overlay") {
		t.Fatal("shift+g did not scroll the help overlay to the bottom")
	}
}
