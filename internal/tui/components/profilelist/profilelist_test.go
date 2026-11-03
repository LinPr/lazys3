package profilelist

import (
	"image/color"
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/LinPr/lazys3/internal/tui/components/style"
)

// sgrFor renders a colour through lipgloss and extracts its SGR sequence,
// so these tests follow the palette instead of pinning one hex value.
func sgrFor(t *testing.T, c color.Color) string {
	t.Helper()
	out := lipgloss.NewStyle().Foreground(c).Render("X")
	m := regexp.MustCompile(`\x1b\[([0-9;]*)m`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no SGR in %q", out)
	}
	return m[1]
}

// TestCursorRowHighlighted pins the cursor-row highlight, consistent with
// the bucket picker: the highlighted profile carries the picker highlight
// foreground (style.CursorHighlightFg, #f3ec38 → SGR 38;2;243;236;56) plus
// the left-border indicator, and other rows do not.
func TestCursorRowHighlighted(t *testing.T) {
	cursorSGR := sgrFor(t, style.CursorHighlightFg)
	m := NewModel()
	m.SetSize(60, 20)
	m, _ = m.Update(ReadAwsConfigResult{Profiles: []Profile{
		NewProfile("alpha", "https://example.com"),
		NewProfile("beta", "https://example.com"),
	}})
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, "alpha") {
			if !strings.Contains(line, cursorSGR) {
				t.Errorf("cursor row should carry the highlight fg %s:\n%q", cursorSGR, line)
			}
			if !strings.Contains(line, "│") {
				t.Errorf("cursor row should carry the left-border indicator:\n%q", line)
			}
		}
		if strings.Contains(line, "beta") && strings.Contains(line, cursorSGR) {
			t.Errorf("non-cursor row must not carry the highlight fg:\n%q", line)
		}
	}
}
