// Package transferview renders a full-screen overlay listing this
// session's transfers, newest first. It follows the versionview overlay
// pattern: the TUI toggles it on 't', swallows every other key while it is
// visible, closes it on esc/'t', and routes 'x' to cancel the HIGHLIGHTED
// transfer (outside the overlay 'x' keeps its cancel-latest meaning).
//
// The overlay owns no transfer state: View renders from the transferpanel
// Model's Rows() snapshot each frame, so the panel's message loop
// (TransferAddMsg/TickMsg/SyncPollMsg...) keeps driving live progress while
// the overlay is open. The indeterminate bar's bounce position derives from
// wall-clock time (not the panel's tick frame, which only advances for
// Progress-carrying transfers), so any 200ms repaint — tick or sync poll —
// animates it.
//
// Terminal rows render their FINAL state, never a frozen tick value: a done
// transfer with a known total shows a full bar + 100%; done with an unknown
// total shows just the done marker (no bouncing bar); failed shows the
// error snippet; canceled its marker. Status glyphs fall back to ASCII
// ("ok"/"x"/"-") when nerd_font is off, matching the status bar tallies.
package transferview

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/lazys3/internal/tui/components/style"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
)

// Model is the transfers overlay state (visibility, cursor, scroll window).
type Model struct {
	visible bool
	cursor  int
	offset  int
	width   int
	height  int
}

// NewModel returns a hidden transfers overlay.
func NewModel() Model { return Model{} }

// Init is a no-op; the overlay renders from live transferpanel rows.
func (m Model) Init() tea.Cmd { return nil }

// Show opens the overlay with the cursor on the newest transfer. Hide
// closes it; Toggle flips between the two.
func (m *Model) Show() {
	m.visible = true
	m.cursor = 0
	m.offset = 0
}

func (m *Model) Hide() { m.visible = false }

func (m *Model) Toggle() {
	if m.visible {
		m.Hide()
		return
	}
	m.Show()
}

// IsVisible reports whether the overlay is shown.
func (m Model) IsVisible() bool { return m.visible }

// SetSize sets the overlay's full-canvas dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Cursor returns the highlighted row index (into the newest-first Rows()
// snapshot); the TUI maps it to a transfer ID for the 'x' cancel.
func (m Model) Cursor() int { return m.cursor }

// HandleKey moves the cursor (j/k, arrows, pgup/pgdown, g/G) over total
// rows. Unrecognised keys are swallowed by design — the TUI forwards
// nothing else while the overlay is visible.
func (m *Model) HandleKey(key string, total int) {
	page := m.pageSize()
	switch key {
	case "j", "down":
		m.cursor++
	case "k", "up":
		m.cursor--
	case "pgdown":
		m.cursor += page
	case "pgup":
		m.cursor -= page
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = total - 1
	}
	m.clamp(total)
}

// clamp keeps the cursor inside the listing and the scroll window around
// the cursor.
func (m *Model) clamp(total int) {
	if m.cursor >= total {
		m.cursor = total - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	page := m.pageSize()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+page {
		m.offset = m.cursor - page + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

var boxStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("#3b82f6")).
	Padding(0, 1)

// pageSize is how many transfer rows fit in the box: total height minus
// the border frame, title and footer lines.
func (m Model) pageSize() int {
	page := m.height - boxStyle.GetVerticalFrameSize() - 2
	if page < 1 {
		page = 1
	}
	return page
}

func (m Model) innerWidth() int {
	inner := m.width - boxStyle.GetHorizontalFrameSize()
	if inner < 20 {
		inner = 20
	}
	return inner
}

// statusGlyph maps a status to its marker; ASCII fallbacks when nerd_font
// is off (matching the status bar's ">"/"ok"/"x" tallies).
func statusGlyph(s transferpanel.Status) string {
	if style.NerdFontEnabled() {
		switch s {
		case transferpanel.StatusQueued:
			return "…"
		case transferpanel.StatusRunning:
			return "▸"
		case transferpanel.StatusDone:
			return "✓"
		case transferpanel.StatusFailed:
			return "✗"
		case transferpanel.StatusCanceled:
			return "⊘"
		}
		return " "
	}
	switch s {
	case transferpanel.StatusQueued:
		return "."
	case transferpanel.StatusRunning:
		return ">"
	case transferpanel.StatusDone:
		return "ok"
	case transferpanel.StatusFailed:
		return "x"
	case transferpanel.StatusCanceled:
		return "-"
	}
	return " "
}

// barWidth is the progress bar cell budget.
const barWidth = 10

// animFrame derives the indeterminate bar's bounce position from wall-clock
// time. The panel's tick frame only advances for Progress-carrying
// transfers, so a sync (which reports via SyncPollMsg, no Progress counter)
// would freeze a frame-driven bar; time keeps it moving on every 200ms
// repaint regardless of what armed the repaint.
func animFrame() int {
	return int(time.Now().UnixMilli() / 200)
}

// bar renders a barWidth-cell progress bar: a determinate fill when total
// is known, an indeterminate 3-cell block bouncing with the frame
// otherwise.
func bar(done, total int64, frame int) string {
	if total <= 0 {
		const block = 3
		span := barWidth - block
		pos := frame % (2 * span)
		if pos > span {
			pos = 2*span - pos
		}
		return strings.Repeat(" ", pos) + strings.Repeat("█", block) +
			strings.Repeat(" ", barWidth-block-pos)
	}
	pct := float64(done) / float64(total)
	if pct < 0 {
		pct = 0
	} else if pct > 1 {
		pct = 1
	}
	filled := int(pct * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	return strings.Repeat("█", filled) + strings.Repeat(" ", barWidth-filled)
}

// percent renders "NN%" clamped to 0..100.
func percent(done, total int64) string {
	pct := 100 * float64(done) / float64(total)
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}
	return fmt.Sprintf("%3.0f%%", pct)
}

// progressCell renders the bar/percent-or-status column for one row. A
// terminal row always renders its final state: done with a known total is
// a FULL bar + 100% by definition (never the frozen last tick value); done
// with an unknown total drops the bar entirely.
func progressCell(t transferpanel.Transfer, frame int) string {
	switch t.Status {
	case transferpanel.StatusDone:
		if t.Total > 0 {
			return fmt.Sprintf("[%s] 100%% done", strings.Repeat("█", barWidth))
		}
		return "done"
	case transferpanel.StatusFailed:
		return "failed"
	case transferpanel.StatusCanceled:
		return "canceled"
	case transferpanel.StatusQueued:
		return "queued"
	default: // running: live bar (max-so-far), bouncing when indeterminate
		if t.Total > 0 {
			return fmt.Sprintf("[%s] %s running", bar(t.Done, t.Total, frame), percent(t.Done, t.Total))
		}
		return fmt.Sprintf("[%s] running", bar(t.Done, t.Total, frame))
	}
}

// pad pads or truncates s to exactly w display cells.
func pad(s string, w int) string {
	if sw := ansi.StringWidth(s); sw > w {
		return ansi.Truncate(s, w, "…")
	} else if sw < w {
		return s + strings.Repeat(" ", w-sw)
	}
	return s
}

// renderRow renders one transfer fitted to inner cells:
//
//	▸ ✓   download  s3://b/key -> ./key  [██████████] 100% done  note
func renderRow(t transferpanel.Transfer, selected bool, frame, inner int) string {
	marker := "  "
	if selected {
		marker = "▸ "
	}
	const opW = 10
	tail := "  " + progressCell(t, frame)
	if t.Note != "" {
		tail += "  " + ansi.Truncate(t.Note, 40, "…")
	}
	if t.Status == transferpanel.StatusFailed && t.Err != nil {
		tail += "  " + ansi.Truncate(t.Err.Error(), 30, "…")
	}
	// marker(2) + glyph(2) + " " + op(opW) + "  " ahead of the label.
	fixed := 2 + 2 + 1 + opW + 2
	labelW := inner - fixed - ansi.StringWidth(tail)
	if labelW < 8 {
		labelW = 8
	}
	row := fmt.Sprintf("%s%s %s  %s%s",
		marker,
		pad(statusGlyph(t.Status), 2),
		pad(string(t.Op), opW),
		ansi.Truncate(t.Label, labelW, "…"),
		tail,
	)
	return ansi.Truncate(row, inner, "")
}

// View renders the overlay from the live rows snapshot (newest first): a
// full-canvas bordered box with a title, the visible page of rows, and a
// footer with the key legend. The scroll window is re-clamped here because
// the row count can change between HandleKey calls (new transfers,
// pruning). Indeterminate bars are positioned by animFrame (wall-clock).
func (m Model) View(rows []transferpanel.Transfer) string {
	if !m.visible {
		return ""
	}
	frame := animFrame()

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#e39f00ff")).
		Background(lipgloss.Color("#444745ff")).
		Padding(0, 1)
	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#aaaaaa"))

	inner := m.innerWidth()
	page := m.pageSize()

	cursor, offset := m.cursor, m.offset
	if cursor >= len(rows) {
		cursor = len(rows) - 1
	}
	if cursor < 0 {
		cursor = 0
	}
	if offset > cursor {
		offset = cursor
	}
	if max := len(rows) - page; offset > max {
		offset = max
	}
	if offset < 0 {
		offset = 0
	}

	lines := []string{titleStyle.Render("lazys3 — transfers (live, newest first)")}

	if len(rows) == 0 {
		lines = append(lines, dimStyle.Render("no transfers this session — d/u/c/s start one"))
	} else {
		end := offset + page
		if end > len(rows) {
			end = len(rows)
		}
		for i, r := range rows[offset:end] {
			lines = append(lines, renderRow(r, offset+i == cursor, frame, inner))
		}
	}

	footer := fmt.Sprintf("%d transfer(s)", len(rows))
	if len(rows) > page {
		footer = fmt.Sprintf("%d-%d of %d", offset+1, min(offset+page, len(rows)), len(rows))
	}
	footer += " · j/k pgup/pgdn g/G scroll · x cancel highlighted · t/esc close"
	lines = append(lines, dimStyle.Render(ansi.Truncate(footer, inner, "…")))

	box := boxStyle.Width(m.width)
	if m.height > 0 {
		box = box.Height(m.height)
	}
	return box.Render(strings.Join(lines, "\n"))
}
