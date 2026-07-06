package style

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func plainBg(w, h int) string {
	line := strings.Repeat("0123456789", (w+9)/10)[:w]
	lines := make([]string, h)
	for i := range lines {
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// assertRect asserts every line of s has display width w and there are h lines.
func assertRect(t *testing.T, s string, w, h int) {
	t.Helper()
	lines := strings.Split(s, "\n")
	if len(lines) != h {
		t.Fatalf("line count = %d, want %d", len(lines), h)
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got != w {
			t.Fatalf("line %d width = %d, want %d (%q)", i, got, w, l)
		}
	}
}

func TestPlaceOverlayPlainText(t *testing.T) {
	bg := plainBg(10, 4)
	out := PlaceOverlay(bg, "AB\nCD", 4, 1)
	assertRect(t, out, 10, 4)
	lines := strings.Split(out, "\n")
	want := []string{
		"0123456789",
		"0123AB6789",
		"0123CD6789",
		"0123456789",
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestPlaceOverlayCorners(t *testing.T) {
	// Top-left.
	out := PlaceOverlay(plainBg(6, 3), "XX\nXX", 0, 0)
	lines := strings.Split(out, "\n")
	if lines[0] != "XX2345" || lines[1] != "XX2345" || lines[2] != "012345" {
		t.Fatalf("top-left corner splice wrong:\n%s", out)
	}
	// Bottom-right.
	out = PlaceOverlay(plainBg(6, 3), "XX\nXX", 4, 1)
	lines = strings.Split(out, "\n")
	if lines[0] != "012345" || lines[1] != "0123XX" || lines[2] != "0123XX" {
		t.Fatalf("bottom-right corner splice wrong:\n%s", out)
	}
}

func TestPlaceOverlayClampsOversizedFg(t *testing.T) {
	bg := plainBg(8, 3)
	// fg wider and taller than bg: clamped to the bg canvas, never panics,
	// output keeps the bg geometry.
	fg := strings.Repeat("W", 20) + "\n" + strings.Repeat("W", 20) + "\n" +
		strings.Repeat("W", 20) + "\n" + strings.Repeat("W", 20) + "\n" +
		strings.Repeat("W", 20)
	out := PlaceOverlay(bg, fg, 3, 1)
	assertRect(t, out, 8, 3)
	// Negative offsets clamp to the origin.
	out = PlaceOverlay(bg, "ab", -5, -5)
	if !strings.HasPrefix(out, "ab234567") {
		t.Fatalf("negative offsets not clamped to origin:\n%s", out)
	}
	// Offsets past the bottom-right clamp back inside.
	out = PlaceOverlay(bg, "ab", 100, 100)
	lines := strings.Split(out, "\n")
	if lines[2] != "012345ab" {
		t.Fatalf("overshooting offsets not clamped: %q", lines[2])
	}
}

func TestPlaceOverlayANSIBackground(t *testing.T) {
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000"))
	row := red.Render(strings.Repeat("r", 12))
	bg := strings.Join([]string{row, row, row}, "\n")
	out := PlaceOverlay(bg, "AB\nCD", 5, 1)
	assertRect(t, out, 12, 3)
	lines := strings.Split(out, "\n")
	stripped := strings.Split(ansi.Strip(out), "\n")
	if stripped[0] != "rrrrrrrrrrrr" {
		t.Fatalf("untouched bg row corrupted: %q", stripped[0])
	}
	if stripped[1] != "rrrrrABrrrrr" || stripped[2] != "rrrrrCDrrrrr" {
		t.Fatalf("spliced rows wrong: %q / %q", stripped[1], stripped[2])
	}
	// A reset must sit between the bg's left part and the fg so the red
	// foreground does not bleed into the box.
	if !strings.Contains(lines[1], resetStyle+"AB") {
		t.Fatalf("no reset before the fg splice: %q", lines[1])
	}
	// The right remainder re-establishes its own SGR state (TruncateLeft
	// preserves the leading escape sequences), so the trailing cells stay
	// red rather than inheriting whatever the fg left behind.
	afterFg := lines[1][strings.Index(lines[1], "AB")+2:]
	if !strings.Contains(afterFg, "38;2;255;0;0") && !strings.Contains(afterFg, "\x1b[") {
		t.Fatalf("right remainder lost its color state: %q", afterFg)
	}
}

func TestPlaceOverlayCJKSpliceColumns(t *testing.T) {
	// "你好世界你好" is 6 wide runes = 12 cells. Splice "XX" at x=1: the
	// left cut splits 你 (cells 0-1) and the right cut at cell 3 splits 好
	// (cells 2-3). Both split runes must be replaced by padding spaces so
	// the total width stays 12.
	bg := strings.Join([]string{"你好世界你好", "你好世界你好", "你好世界你好"}, "\n")
	out := PlaceOverlay(bg, "XX", 1, 1)
	assertRect(t, out, 12, 3)
	lines := strings.Split(out, "\n")
	if lines[1] != " XX 世界你好" {
		t.Fatalf("CJK splice line = %q, want %q", lines[1], " XX 世界你好")
	}
	if lines[0] != "你好世界你好" || lines[2] != "你好世界你好" {
		t.Fatalf("untouched CJK rows corrupted:\n%s", out)
	}

	// Aligned splice (x=2, box width 4): no rune is split; the remainder
	// starts exactly at cell 6.
	out = PlaceOverlay(bg, "ABCD", 2, 1)
	assertRect(t, out, 12, 3)
	if got := strings.Split(out, "\n")[1]; got != "你ABCD界你好" {
		t.Fatalf("aligned CJK splice = %q, want %q", got, "你ABCD界你好")
	}
}

func TestPlaceOverlayEmptyFgIsNoop(t *testing.T) {
	bg := plainBg(5, 2)
	if out := PlaceOverlay(bg, "", 1, 1); out != bg {
		t.Fatalf("empty fg changed the bg:\n%s", out)
	}
}

func TestPlaceOverlayRaggedFgPadsToBlockWidth(t *testing.T) {
	// A ragged fg block ("AAAA" over "BB") must occupy its full block
	// width on every row, so the bg never peeks through the box interior.
	out := PlaceOverlay(plainBg(10, 2), "AAAA\nBB", 3, 0)
	lines := strings.Split(out, "\n")
	if lines[0] != "012AAAA789" || lines[1] != "012BB  789" {
		t.Fatalf("ragged fg splice wrong:\n%s", out)
	}
}
