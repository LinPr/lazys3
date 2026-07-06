package style

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// resetStyle is emitted at splice boundaries so the background's SGR state
// never bleeds into the foreground box (and vice versa).
const resetStyle = "\x1b[m"

// PlaceOverlay splices the fg block into the bg string at cell (x, y),
// line by line and ANSI-aware, so a floating box can be composited on top
// of an already-rendered layout without blanking it. Both blocks are
// treated as rectangles: fg lines are padded to the block's width, and a
// wide rune (CJK) split at a splice column is replaced by a padding space
// so the columns never shift. fg positions and sizes are clamped into the
// bg canvas.
func PlaceOverlay(bg, fg string, x, y int) string {
	if fg == "" {
		return bg
	}
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")
	bgW := blockWidth(bgLines)
	fgW := blockWidth(fgLines)

	// Clamp an oversized fg into the bg canvas.
	if fgW > bgW {
		fgW = bgW
		for i, l := range fgLines {
			fgLines[i] = ansi.Truncate(l, fgW, "")
		}
	}
	if len(fgLines) > len(bgLines) {
		fgLines = fgLines[:len(bgLines)]
	}
	if x > bgW-fgW {
		x = bgW - fgW
	}
	if x < 0 {
		x = 0
	}
	if y > len(bgLines)-len(fgLines) {
		y = len(bgLines) - len(fgLines)
	}
	if y < 0 {
		y = 0
	}

	for i, fgLine := range fgLines {
		bgLines[y+i] = spliceLine(bgLines[y+i], fgLine, x, fgW)
	}
	return strings.Join(bgLines, "\n")
}

// spliceLine overwrites cells [x, x+fgW) of bgLine with fgLine.
func spliceLine(bgLine, fgLine string, x, fgW int) string {
	var b strings.Builder

	// Left segment: the first x cells. ansi.Truncate drops a wide rune
	// that straddles the boundary, so pad the missing cell(s) with spaces
	// to keep the box's left edge aligned.
	left := ansi.Truncate(bgLine, x, "")
	b.WriteString(left)
	if w := ansi.StringWidth(left); w < x {
		b.WriteString(strings.Repeat(" ", x-w))
	}
	if strings.Contains(bgLine, "\x1b") {
		b.WriteString(resetStyle)
	}

	// Foreground line, padded to the block width so the right edge stays
	// aligned even for ragged fg content.
	b.WriteString(fgLine)
	if w := ansi.StringWidth(fgLine); w < fgW {
		b.WriteString(strings.Repeat(" ", fgW-w))
	}
	if strings.Contains(fgLine, "\x1b") || strings.Contains(bgLine, "\x1b") {
		b.WriteString(resetStyle)
	}

	// Right remainder: the bg cells from x+fgW on. ansi.TruncateLeft keeps
	// a wide rune that straddles the cut whole (one cell too wide); drop it
	// and pad with a space instead, like x/ansi does elsewhere.
	cut := x + fgW
	if bgLineW := ansi.StringWidth(bgLine); bgLineW > cut {
		right := ansi.TruncateLeft(bgLine, cut, "")
		if ansi.StringWidth(right) > bgLineW-cut {
			right = " " + ansi.TruncateLeft(bgLine, cut+1, "")
		}
		b.WriteString(right)
	}
	return b.String()
}

// blockWidth is the widest display width across the block's lines.
func blockWidth(lines []string) int {
	w := 0
	for _, l := range lines {
		if lw := ansi.StringWidth(l); lw > w {
			w = lw
		}
	}
	return w
}
