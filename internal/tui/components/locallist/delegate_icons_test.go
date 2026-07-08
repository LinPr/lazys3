package locallist

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/LinPr/lazys3/internal/tui/components/style"
	"github.com/charmbracelet/x/ansi"
)

// withNerdFont enables icons for one test and restores the previous state.
func withNerdFont(t *testing.T) {
	t.Helper()
	prev := style.NerdFontEnabled()
	style.SetNerdFont(true)
	t.Cleanup(func() { style.SetNerdFont(prev) })
}

// underlinedRunes extracts the characters rendered with the underline SGR
// attribute (the FilterMatch style). Mirrors objectlist's helper.
func underlinedRunes(s string) string {
	var out []rune
	underline := false
	rs := []rune(s)
	for i := 0; i < len(rs); {
		if rs[i] == 0x1b && i+1 < len(rs) && rs[i+1] == '[' {
			j := i + 2
			for j < len(rs) && !(rs[j] >= 0x40 && rs[j] <= 0x7e) {
				j++
			}
			if j < len(rs) && rs[j] == 'm' {
				params := strings.Split(string(rs[i+2:j]), ";")
				for k := 0; k < len(params); k++ {
					switch params[k] {
					case "", "0":
						underline = false
					case "4":
						underline = true
					case "24":
						underline = false
					case "38", "48", "58":
						// Color introducer: skip its arguments so an rgb
						// component of 4 is not read as underline.
						if k+1 < len(params) && params[k+1] == "2" {
							k += 4
						} else if k+1 < len(params) && params[k+1] == "5" {
							k += 2
						}
					}
				}
			}
			i = j + 1
			continue
		}
		if underline {
			out = append(out, rs[i])
		}
		i++
	}
	return string(out)
}

// TestIconViewLinesFitWidth renders the full view with icons enabled at
// the dual-pane widths: no line may exceed the width even with the extra
// icon column, and dirs/files carry their glyphs.
func TestIconViewLinesFitWidth(t *testing.T) {
	withNerdFont(t)
	for _, width := range []int{40, 80} {
		m := NewModel()
		m.SetSize(width, 20)
		m = load(t, m, sampleDir(t))
		out := m.View()
		for i, line := range strings.Split(ansi.Strip(out), "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("width %d: line %d is %d cells wide:\n%q", width, i, w, line)
			}
		}
		dirGlyph, _ := style.IconFor("data/", true, false)
		txtGlyph, _ := style.IconFor("zeta.txt", false, false)
		stripped := ansi.Strip(out)
		if !strings.Contains(stripped, dirGlyph+" data/") {
			t.Errorf("width %d: expected dir icon before data/, got:\n%s", width, stripped)
		}
		if !strings.Contains(stripped, txtGlyph+" zeta.txt") {
			t.Errorf("width %d: expected txt icon before zeta.txt, got:\n%s", width, stripped)
		}
	}
}

// TestIconFilterHighlightLandsOnName pins the filter-highlight offset with
// the icon column enabled: the underlined runes must be exactly the
// matched part of the NAME, not shifted into the icon or marker columns.
func TestIconFilterHighlightLandsOnName(t *testing.T) {
	withNerdFont(t)
	for _, width := range []int{40, 80} {
		m := NewModel()
		m.SetSize(width, 20)
		m = load(t, m, sampleDir(t))
		m = press(m, tea.Key{Code: '/', Text: "/"})
		m = typeString(m, "zeta")

		d := newSelectDelegate(&m.selected)
		items := m.list.VisibleItems()
		if len(items) != 1 {
			t.Fatalf("width %d: want 1 visible item, got %d", width, len(items))
		}
		var row strings.Builder
		d.Render(&row, m.list, 0, items[0])
		if got := underlinedRunes(row.String()); got != "zeta" {
			t.Errorf("width %d: underlined runes = %q, want %q\nrow: %q", width, got, "zeta", row.String())
		}
	}
}

// TestOffRenderHasNoIcon guards the opt-in: with nerd_font off (the
// default), the rendered row contains no icon column at all — the marker
// column sits directly before the name.
func TestOffRenderHasNoIcon(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	m = load(t, m, sampleDir(t))
	m.ToggleSelected() // cursor on data/ (dirs sort first)
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "✔ data/") {
		t.Errorf("nerd_font off: expected ✔ marker directly before selected name, got:\n%s", out)
	}
	if !strings.Contains(out, "  zeta.txt") || strings.Contains(out, "✔ zeta.txt") {
		t.Errorf("nerd_font off: expected blank 2-cell marker before unselected name, got:\n%s", out)
	}
}
