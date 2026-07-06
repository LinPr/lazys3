package objectlist

import (
	"strings"
	"testing"
	"time"

	"github.com/LinPr/lazys3/internal/tui/components/style"
	tea "github.com/charmbracelet/bubbletea/v2"
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
// attribute (the FilterMatch style), so tests can pin exactly which cells
// the filter highlight lands on.
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
// the pane widths used by the dual layout: no line may exceed the width
// even with the extra icon column.
func TestIconViewLinesFitWidth(t *testing.T) {
	withNerdFont(t)
	for _, width := range []int{40, 80} {
		m := NewModel()
		m.SetSize(width, 20)
		m.SetObjects([]Object{
			{name: "dir-with-a-fairly-long-name/", isDir: true},
			{
				name:         "a-very-long-object-name-that-used-to-wrap-at-eighty-columns.txt",
				size:         123456789,
				modTime:      time.Date(2024, 3, 1, 10, 30, 0, 0, time.UTC),
				storageClass: "STANDARD",
			},
			{name: "main.go", size: 42, modTime: time.Date(2024, 3, 2, 8, 0, 0, 0, time.UTC)},
		})
		out := m.View()
		for i, line := range strings.Split(ansi.Strip(out), "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("width %d: line %d is %d cells wide:\n%q", width, i, w, line)
			}
		}
		// The icon column actually rendered.
		goGlyph, _ := style.IconFor("main.go", false, false)
		if !strings.Contains(ansi.Strip(out), goGlyph+" main.go") {
			t.Errorf("width %d: expected icon before main.go, got:\n%s", width, ansi.Strip(out))
		}
	}
}

// TestIconRowWidthDirEqualsFile pins the aligned-row invariant with the
// icon column present: dir and file rows still render at the same width.
func TestIconRowWidthDirEqualsFile(t *testing.T) {
	withNerdFont(t)
	for _, width := range []int{40, 80} {
		m := NewModel()
		m.SetSize(width, 20)
		m.SetObjects([]Object{
			{name: "hello/", isDir: true},
			{
				name:         "LICENSE",
				size:         1024,
				modTime:      time.Date(2025, 2, 25, 9, 49, 0, 0, time.UTC),
				storageClass: "STANDARD",
			},
		})
		d := newSelectDelegate(&m.selected)
		items := m.objectlist.VisibleItems()
		var dir, file strings.Builder
		d.Render(&dir, m.objectlist, 0, items[0])
		d.Render(&file, m.objectlist, 1, items[1])
		dw, fw := ansi.StringWidth(dir.String()), ansi.StringWidth(file.String())
		if dw != fw {
			t.Errorf("width %d: dir row is %d cells, file row is %d cells:\ndir:  %q\nfile: %q",
				width, dw, fw, dir.String(), file.String())
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
		m.SetObjects([]Object{
			{name: "one.txt", size: 10, modTime: time.Date(2024, 3, 1, 10, 30, 0, 0, time.UTC)},
			{name: "two.txt", size: 20, modTime: time.Date(2024, 3, 1, 11, 30, 0, 0, time.UTC)},
		})
		m = press(m, tea.Key{Code: '/', Text: "/"})
		m = typeString(m, "one")

		d := newSelectDelegate(&m.selected)
		items := m.objectlist.VisibleItems()
		if len(items) != 1 {
			t.Fatalf("width %d: want 1 visible item, got %d", width, len(items))
		}
		var row strings.Builder
		d.Render(&row, m.objectlist, 0, items[0])
		if got := underlinedRunes(row.String()); got != "one" {
			t.Errorf("width %d: underlined runes = %q, want %q\nrow: %q", width, got, "one", row.String())
		}
	}
}

// TestOffRenderHasNoIcon guards the opt-in: with nerd_font off (the
// default), the rendered row contains no icon column at all.
func TestOffRenderHasNoIcon(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	m.SetObjects([]Object{{name: "main.go", size: 42}})
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "[ ] main.go") {
		t.Errorf("nerd_font off: expected marker directly before name, got:\n%s", out)
	}
}
