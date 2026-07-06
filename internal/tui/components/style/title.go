package style

import (
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// listTitleReserve is the number of columns the bubbles list's title bar
// consumes around the title text: 2 cols of TitleBar left padding, 2 cols
// of the Title style's horizontal padding, and 1 col reserved for the
// spinner. bubbles' own titleView truncation only accounts for the
// spinner, not the TitleBar padding, so an over-long title renders a bar
// wider than the list — lipgloss then word-wraps it onto a second line,
// pushing every following row down (visibly misaligning the two panes in
// dual-pane mode).
const listTitleReserve = 5

// FitListTitle middle-truncates a composed list title so the rendered
// title bar always fits on one line of a bubbles list sized to listWidth
// (the list's INNER width, i.e. list.Model.Width()). Middle truncation
// keeps both the head (the s3:// bucket / path root) and the tail (the
// basename and the "[name ↑]" sort suffix) readable. A non-positive
// listWidth (list not sized yet) returns the title unchanged.
func FitListTitle(title string, listWidth int) string {
	if listWidth <= 0 {
		return title
	}
	return TruncateMiddle(title, max(listWidth-listTitleReserve, 1))
}

// TruncateMiddle shortens s to fit within width cells, keeping the head
// and tail and replacing the middle with "…" when truncation is needed.
// When width is large enough the original string is returned unchanged.
func TruncateMiddle(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := lipgloss.Width(s)
	if w <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	keep := width - 1
	head := keep / 2
	tail := keep - head
	// TruncateLeft keeps a wide grapheme straddling the cut, which can
	// overshoot the tail budget; advance the cut until the tail fits.
	cut := w - tail
	right := ansi.TruncateLeft(s, cut, "")
	for lipgloss.Width(right) > tail && cut < w {
		cut++
		right = ansi.TruncateLeft(s, cut, "")
	}
	return ansi.Truncate(s, head, "") + "…" + right
}
