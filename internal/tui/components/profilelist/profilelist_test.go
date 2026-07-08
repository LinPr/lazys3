package profilelist

import (
	"strings"
	"testing"
)

// TestCursorRowHighlighted pins the cursor-row highlight, consistent with
// the bucket picker: the highlighted profile carries the picker highlight
// foreground (style.CursorHighlightFg, #f3ec38 → SGR 38;2;243;236;56) plus
// the left-border indicator, and other rows do not.
func TestCursorRowHighlighted(t *testing.T) {
	const cursorSGR = "38;2;243;236;56"
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
