package help

import (
	"testing"

	"github.com/charmbracelet/lipgloss/v2"
)

func TestPadRightUsesDisplayWidth(t *testing.T) {
	cases := []string{"enter / →", "backspace / ←", "↑ / k, ↓ / j", "q"}
	const w = 20
	for _, s := range cases {
		got := lipgloss.Width(padRight(s, w))
		if got != w {
			t.Errorf("padRight(%q, %d) width = %d, want %d", s, w, got, w)
		}
	}
}
