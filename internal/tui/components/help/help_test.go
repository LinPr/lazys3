package help

import (
	"fmt"
	"strings"
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

// TestViewFitsWithoutScrolling pins the tall-terminal case: everything is
// rendered (first and last binding visible) and no scroll footer appears.
func TestViewFitsWithoutScrolling(t *testing.T) {
	m := NewModel()
	m.SetSize(120, 80)
	m.Show()
	out := m.View()
	if !strings.Contains(out, "lazys3 — keybindings") {
		t.Fatal("view missing the title")
	}
	if !strings.Contains(out, "close the overlay") {
		t.Fatal("view missing the last binding (content cut off despite fitting)")
	}
	if strings.Contains(out, "j/k scroll") {
		t.Fatal("scroll footer rendered although the content fits")
	}
	// Scroll keys are clamped no-ops while the content fits.
	m.HandleKey("j")
	m.HandleKey("G")
	if m.offset != 0 {
		t.Fatalf("offset = %d after scrolling a fitting view, want 0", m.offset)
	}
}

// TestScrollWindowAtSmallHeight pins the window slicing on a 15-row
// terminal: only the first page renders, the footer shows the position,
// and G jumps to the bottom.
func TestScrollWindowAtSmallHeight(t *testing.T) {
	m := NewModel()
	m.SetSize(120, 15)
	m.Show()
	total := m.lineCount()
	page := m.contentHeight() - 1 // footer row
	out := m.View()
	if !strings.Contains(out, "lazys3 — keybindings") {
		t.Fatal("first page missing the title")
	}
	if strings.Contains(out, "close the overlay") {
		t.Fatal("last binding visible on the first page of a 15-row terminal")
	}
	// The full footer carries the position, the continues-below indicator
	// (only ↓ on the first page) and the key hints.
	wantFooter := fmt.Sprintf("1-%d of %d ↓ · j/k scroll · ?/esc close", page, total)
	if !strings.Contains(out, wantFooter) {
		t.Fatalf("footer %q not found in:\n%s", wantFooter, out)
	}

	m.HandleKey("G")
	out = m.View()
	if !strings.Contains(out, "close the overlay") {
		t.Fatal("G did not scroll the last binding into view")
	}
	// Bottom page: only the continues-above indicator.
	wantFooter = fmt.Sprintf("%d-%d of %d ↑ · j/k scroll · ?/esc close", total-page+1, total, total)
	if !strings.Contains(out, wantFooter) {
		t.Fatalf("bottom footer %q not found in:\n%s", wantFooter, out)
	}
	// The box never exceeds the terminal height.
	if h := lipgloss.Height(out); h != 15 {
		t.Fatalf("view height = %d, want 15", h)
	}
}

// TestScrollClampsAtBothEnds pins the offset clamping: k/pgup at the top
// stay at 0; j/pgdown at the bottom stay at the max offset.
func TestScrollClampsAtBothEnds(t *testing.T) {
	m := NewModel()
	m.SetSize(120, 15)
	m.Show()
	m.HandleKey("k")
	m.HandleKey("pgup")
	if m.offset != 0 {
		t.Fatalf("offset = %d after scrolling up at the top, want 0", m.offset)
	}
	m.HandleKey("G")
	max := m.offset
	if max <= 0 {
		t.Fatalf("G offset = %d, want > 0 on a 15-row terminal", max)
	}
	m.HandleKey("j")
	m.HandleKey("pgdown")
	if m.offset != max {
		t.Fatalf("offset = %d after scrolling down at the bottom, want %d", m.offset, max)
	}
	m.HandleKey("g")
	if m.offset != 0 {
		t.Fatalf("offset = %d after g, want 0", m.offset)
	}
}

// TestTinyTerminalNeverOverflows pins the degenerate heights: when the box
// fits one content line or less, the footer is dropped and the rendered
// view never exceeds the terminal height.
func TestTinyTerminalNeverOverflows(t *testing.T) {
	for h := 5; h <= 7; h++ {
		m := NewModel()
		m.SetSize(80, h)
		m.Show()
		out := m.View()
		if got := lipgloss.Height(out); got > h {
			t.Fatalf("height %d: view is %d rows tall, exceeds the terminal", h, got)
		}
		if h-boxStyle.GetVerticalFrameSize() < 2 && strings.Contains(out, "j/k scroll") {
			t.Fatalf("height %d: footer rendered although no row is left for it", h)
		}
		// Scrolling still reaches the bottom line.
		m.HandleKey("G")
		if !strings.Contains(m.View(), "close the overlay") {
			t.Fatalf("height %d: G did not scroll the last binding into view", h)
		}
	}
}

// TestOpenResetsScroll pins that every open starts at the top, whether via
// Show or Toggle.
func TestOpenResetsScroll(t *testing.T) {
	m := NewModel()
	m.SetSize(120, 15)
	m.Show()
	m.HandleKey("G")
	if m.offset == 0 {
		t.Fatal("G did not move the offset")
	}
	m.Hide()
	m.Show()
	if m.offset != 0 {
		t.Fatalf("Show left offset = %d, want 0", m.offset)
	}
	m.HandleKey("G")
	m.Toggle() // close
	m.Toggle() // reopen
	if m.offset != 0 {
		t.Fatalf("Toggle reopen left offset = %d, want 0", m.offset)
	}
}

// helpBoxWidth extracts the rendered box's outer width from a full-canvas
// view: the top border line ("╭───╮"), stripped of ANSI and the centering
// whitespace around it.
func helpBoxWidth(t *testing.T, view string) int {
	t.Helper()
	for _, line := range strings.Split(stripANSI(view), "\n") {
		if strings.Contains(line, "╭") {
			return lipgloss.Width(strings.TrimSpace(line))
		}
	}
	t.Fatal("no top border line found in the help view")
	return 0
}

// stripANSI removes escape sequences so structural checks see plain text.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
		case r == '\x1b':
			inEsc = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TestBoxWidthConstantWhileScrolling pins ISSUE 4: the box border must not
// resize as the user pages through the content — its width is computed once
// from the longest line of the FULL content, not the visible window.
func TestBoxWidthConstantWhileScrolling(t *testing.T) {
	for _, size := range []struct{ w, h int }{{120, 15}, {100, 12}, {80, 20}} {
		m := NewModel()
		m.SetSize(size.w, size.h)
		m.Show()
		want := helpBoxWidth(t, m.View())
		step := 0
		check := func(key string) {
			m.HandleKey(key)
			step++
			if got := helpBoxWidth(t, m.View()); got != want {
				t.Fatalf("%dx%d: box width = %d at step %d (key %q, offset %d), want constant %d",
					size.w, size.h, got, step, key, m.offset, want)
			}
		}
		// Walk every offset to the bottom, then page and jump around.
		m2 := m
		m2.HandleKey("G")
		bottom := m2.offset
		for i := 0; i < bottom; i++ {
			check("j")
		}
		check("G")
		check("g")
		check("pgdown")
		check("pgdown")
		check("pgup")
		check("G")
	}
}

// TestBoxWidthConstantFits80Cols pins that the fixed-width box still fits a
// narrow terminal: on 80 cols every rendered line stays within 80 cells at
// every scroll position.
func TestBoxWidthConstantFits80Cols(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 15)
	m.Show()
	keys := []string{"", "j", "j", "pgdown", "G", "g"}
	for _, k := range keys {
		if k != "" {
			m.HandleKey(k)
		}
		for i, line := range strings.Split(m.View(), "\n") {
			if w := lipgloss.Width(line); w > 80 {
				t.Fatalf("after %q: line %d width = %d, exceeds 80", k, i, w)
			}
		}
	}
}

// Test80ColWidthFit pins the horizontal fit: on an 80-col terminal every
// line is truncated into the box, so the rendered canvas is exactly 80
// cells wide.
func Test80ColWidthFit(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 40)
	m.Show()
	out := m.View()
	if w := lipgloss.Width(out); w != 80 {
		t.Fatalf("view width = %d, want exactly 80", w)
	}
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > 80 {
			t.Fatalf("line %d width = %d, exceeds 80", i, w)
		}
	}
}
