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
// total shows just the done word (no bouncing bar); failed shows the
// error snippet; canceled its word. Status glyphs fall back to ASCII
// ("ok"/"x"/"-") when nerd_font is off, matching the status bar tallies.
//
// Rows are a table under a dim header, columns in this order:
// [cursor marker (2, fixed)] [op] [file] [progress] [status glyph] [note].
// Column widths are DYNAMIC: each is max(header, longest cell among the
// VISIBLE rows), clamped to a per-column cap (op 12, file 60, note 40;
// progress is fixed bar+percent), recomputed every render. When the
// assembled row overflows the terminal, ←/→ shift a horizontal offset over
// the columns (the marker stays put); the slicing is ANSI-safe and pads a
// split CJK rune with a space (the PlaceOverlay trick) so columns never
// drift.
//
// Pressing enter on a sync row opens a per-file detail mode inside the
// overlay (esc/enter returns): one row per planned file with its own
// status glyph, relative path, live bar+percent and size, backed by
// syncmodal.PerFile — live from the poll registry while the sync runs, and
// from syncmodal's completed-plan cache (small, capped) after the row goes
// terminal, so the detail stays inspectable for recent finished syncs.
package transferview

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/lazys3/internal/tui/components/style"
	"github.com/LinPr/lazys3/internal/tui/components/syncmodal"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
)

// perFileFn is the detail view's data source; a package variable so tests
// can stub the per-file snapshots without running a real sync.
var perFileFn = syncmodal.PerFile

// Model is the transfers overlay state (visibility, cursor, scroll window,
// horizontal offset, and the per-file detail mode).
type Model struct {
	visible bool
	cursor  int
	offset  int
	hoffset int
	width   int
	height  int

	// detailID, when non-empty, switches the overlay into the per-file
	// detail mode for that sync transfer.
	detailID     string
	detailLabel  string
	detailCursor int
	detailOffset int
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
	m.hoffset = 0
	m.CloseDetail()
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

// InDetail reports whether the per-file detail mode is active; the TUI
// routes esc to CloseDetail (instead of Hide) and disables 'x' while it is.
func (m Model) InDetail() bool { return m.detailID != "" }

// CloseDetail returns from the detail mode to the transfer list.
func (m *Model) CloseDetail() {
	m.detailID = ""
	m.detailLabel = ""
	m.detailCursor = 0
	m.detailOffset = 0
}

// HandleEnter toggles the per-file detail mode: on a sync row it opens the
// detail listing, inside the detail it returns to the list, and on any
// other row it is a no-op.
func (m *Model) HandleEnter(rows []transferpanel.Transfer) {
	if m.InDetail() {
		m.CloseDetail()
		return
	}
	c := m.cursor
	if c >= len(rows) {
		c = len(rows) - 1
	}
	if c < 0 || rows[c].Op != transferpanel.OpSync {
		return
	}
	m.detailID = rows[c].ID
	m.detailLabel = rows[c].Label
	m.detailCursor = 0
	m.detailOffset = 0
}

// hStep is how many cells one ←/→ press shifts the horizontal offset.
const hStep = 8

// HandleKey moves the cursor (j/k, arrows, pgup/pgdown, g/G) over the rows
// and shifts the horizontal offset (←/→) when the table overflows. In the
// detail mode the same vertical keys move over the per-file listing
// (horizontal scroll is list-mode only — the detail truncates instead).
// Unrecognised keys are swallowed by design — the TUI forwards nothing
// else while the overlay is visible.
func (m *Model) HandleKey(key string, rows []transferpanel.Transfer) {
	if m.InDetail() {
		files, _ := perFileFn(m.detailID)
		m.detailCursor, m.detailOffset = moveCursor(
			key, m.detailCursor, m.detailOffset, len(files), m.pageSize())
		return
	}
	switch key {
	case "left":
		m.hoffset -= hStep
	case "right":
		m.hoffset += hStep
	default:
		m.cursor, m.offset = moveCursor(key, m.cursor, m.offset, len(rows), m.pageSize())
	}
	m.clampH(rows)
}

// moveCursor applies one navigation key to a cursor/offset pair over total
// rows and a page-sized window, returning the clamped pair.
func moveCursor(key string, cursor, offset, total, page int) (int, int) {
	switch key {
	case "j", "down":
		cursor++
	case "k", "up":
		cursor--
	case "pgdown":
		cursor += page
	case "pgup":
		cursor -= page
	case "g", "home":
		cursor = 0
	case "G", "end":
		cursor = total - 1
	}
	if cursor >= total {
		cursor = total - 1
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+page {
		offset = cursor - page + 1
	}
	if offset < 0 {
		offset = 0
	}
	return cursor, offset
}

// clampH clamps the horizontal offset to the current visible page's
// content overflow (0 when everything fits). View re-clamps display-side
// too, because the row set can change between keys.
func (m *Model) clampH(rows []transferpanel.Transfer) {
	w := computeWidths(m.visibleWindow(rows))
	m.hoffset = clampHOffset(m.hoffset, w.content(), m.viewportW())
}

func clampHOffset(off, content, viewport int) int {
	maxOff := content - viewport
	if maxOff < 0 {
		maxOff = 0
	}
	if off > maxOff {
		off = maxOff
	}
	if off < 0 {
		off = 0
	}
	return off
}

// visibleWindow returns the page of rows the overlay currently shows,
// mirroring View's display clamping.
func (m Model) visibleWindow(rows []transferpanel.Transfer) []transferpanel.Transfer {
	page := m.pageSize()
	_, offset := clampView(m.cursor, m.offset, len(rows), page)
	end := offset + page
	if end > len(rows) {
		end = len(rows)
	}
	return rows[offset:end]
}

// clampView is the display-side cursor/offset clamp: the row count can
// change between HandleKey calls (new transfers, pruning), so View clamps
// again without mutating the model.
func clampView(cursor, offset, total, page int) (int, int) {
	if cursor >= total {
		cursor = total - 1
	}
	if cursor < 0 {
		cursor = 0
	}
	if offset > cursor {
		offset = cursor
	}
	if max := total - page; offset > max {
		offset = max
	}
	if offset < 0 {
		offset = 0
	}
	return cursor, offset
}

var boxStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("#3b82f6")).
	Padding(0, 1)

// pageSize is how many transfer rows fit in the box: total height minus
// the border frame, title, column header and footer lines.
func (m Model) pageSize() int {
	page := m.height - boxStyle.GetVerticalFrameSize() - 3
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

// viewportW is the horizontally scrollable budget: the inner width minus
// the fixed cursor-marker column.
func (m Model) viewportW() int { return m.innerWidth() - markerW }

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

// Table layout: the cursor-marker column is fixed (never scrolled); the
// dynamic columns are joined by colGap spaces and clamped to per-column
// caps. progW fits the widest progress cell ("[██████████] 100%"):
// brackets + bar + space + 4-cell percent.
const (
	markerW = 2 // "▸ "
	colGap  = 2
	progW   = barWidth + 7
	opCap   = 12
	fileCap = 60
	noteCap = 40
)

// colWidths carries one render's dynamic column widths.
type colWidths struct {
	op, file, prog, status, note int
}

// content is the assembled row width (marker excluded): the five columns
// plus the four gaps between them.
func (w colWidths) content() int {
	return w.op + w.file + w.prog + w.status + w.note + 4*colGap
}

// computeWidths sizes each column to max(header, longest visible cell),
// clamped to its cap. Recomputed per render, over the VISIBLE rows only,
// so the table is as narrow as its current page allows.
func computeWidths(rows []transferpanel.Transfer) colWidths {
	w := colWidths{
		op:     ansi.StringWidth("op"),
		file:   ansi.StringWidth("file"),
		prog:   progW,
		status: ansi.StringWidth("st"),
		note:   ansi.StringWidth("note"),
	}
	for _, t := range rows {
		w.op = max(w.op, ansi.StringWidth(string(t.Op)))
		w.file = max(w.file, ansi.StringWidth(t.Label))
		w.status = max(w.status, ansi.StringWidth(statusGlyph(t.Status)))
		w.note = max(w.note, ansi.StringWidth(noteCell(t)))
	}
	w.op = min(w.op, opCap)
	w.file = min(w.file, fileCap)
	w.note = min(w.note, noteCap)
	return w
}

// animFrame derives the indeterminate bar's bounce position from wall-clock
// time rather than the panel's tick frame: repaints can be armed by
// SyncPollMsg independently of the panel tick, and a sync row's bar stays
// indeterminate until OnPlan delivers the totals, so it must bounce even
// before its shared Progress counter reports anything; time keeps it moving
// on every 200ms repaint regardless of what armed the repaint.
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

// progressCell renders the fixed-width bar+percent-or-status-word column.
// A terminal row always renders its final state: done with a known total is
// a FULL bar + 100% by definition (never the frozen last tick value); done
// with an unknown total drops the bar entirely (status word only), as do
// failed/canceled/queued rows.
func progressCell(t transferpanel.Transfer, frame int) string {
	switch t.Status {
	case transferpanel.StatusDone:
		if t.Total > 0 {
			return fmt.Sprintf("[%s] 100%%", strings.Repeat("█", barWidth))
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
			return fmt.Sprintf("[%s] %s", bar(t.Done, t.Total, frame), percent(t.Done, t.Total))
		}
		return fmt.Sprintf("[%s]", bar(t.Done, t.Total, frame))
	}
}

// noteCell merges the row note with, on failed rows, the error snippet.
func noteCell(t transferpanel.Transfer) string {
	note := t.Note
	if t.Status == transferpanel.StatusFailed && t.Err != nil {
		if note != "" {
			note += " · "
		}
		note += t.Err.Error()
	}
	return note
}

// pad pads or truncates s to exactly w display cells. A truncation that
// lands on a double-width rune yields w-1 cells, so the result is
// re-padded to keep the following columns aligned.
func pad(s string, w int) string {
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if sw := ansi.StringWidth(s); sw < w {
		return s + strings.Repeat(" ", w-sw)
	}
	return s
}

// hslice returns the wide-rune-safe [off, off+w) cell window of line: a
// CJK rune straddling the left cut is dropped and padded with a space (the
// PlaceOverlay splice trick), so every row keeps its columns aligned no
// matter where the offset lands.
func hslice(line string, off, w int) string {
	if w <= 0 {
		return ""
	}
	if off <= 0 {
		return ansi.Truncate(line, w, "")
	}
	lw := ansi.StringWidth(line)
	if off >= lw {
		return ""
	}
	s := ansi.TruncateLeft(line, off, "")
	if ansi.StringWidth(s) > lw-off {
		// TruncateLeft keeps a wide rune straddling the cut whole (one
		// cell too wide); drop it and pad with a space instead.
		s = " " + ansi.TruncateLeft(line, off+1, "")
	}
	return ansi.Truncate(s, w, "")
}

// gap is the inter-column spacer.
var gap = strings.Repeat(" ", colGap)

// renderRow renders one transfer's dynamic columns (marker excluded):
//
//	download  s3://b/key -> ./key  [██████████] 100%  ✓  note
func renderRow(t transferpanel.Transfer, frame int, w colWidths) string {
	return strings.Join([]string{
		pad(string(t.Op), w.op),
		pad(t.Label, w.file),
		pad(progressCell(t, frame), w.prog),
		pad(statusGlyph(t.Status), w.status),
		pad(noteCell(t), w.note),
	}, gap)
}

// headerRow renders the dim column header the rows align under.
func headerRow(w colWidths) string {
	return strings.Join([]string{
		pad("op", w.op),
		pad("file", w.file),
		pad("progress", w.prog),
		pad("st", w.status),
		pad("note", w.note),
	}, gap)
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#e39f00")).
			Background(lipgloss.Color("#444745")).
			Padding(0, 1)
	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#aaaaaa"))
)

// View renders the overlay from the live rows snapshot (newest first): a
// full-canvas bordered box with a title, the visible page of rows, and a
// footer with the key legend. The scroll window and horizontal offset are
// re-clamped here because the row count (and the dynamic column widths)
// can change between HandleKey calls. Indeterminate bars are positioned by
// animFrame (wall-clock). In detail mode the box body is the per-file
// listing instead.
func (m Model) View(rows []transferpanel.Transfer) string {
	if !m.visible {
		return ""
	}
	if m.InDetail() {
		return m.renderBox(m.detailLines())
	}
	frame := animFrame()

	inner := m.innerWidth()
	page := m.pageSize()
	cursor, offset := clampView(m.cursor, m.offset, len(rows), page)

	end := offset + page
	if end > len(rows) {
		end = len(rows)
	}
	visible := rows[offset:end]
	w := computeWidths(visible)
	viewW := m.viewportW()
	hoff := clampHOffset(m.hoffset, w.content(), viewW)

	lines := []string{
		titleStyle.Render("lazys3 — transfers (live, newest first)"),
		dimStyle.Render(strings.Repeat(" ", markerW) + hslice(headerRow(w), hoff, viewW)),
	}

	if len(rows) == 0 {
		lines = append(lines, dimStyle.Render("no transfers this session — d/u/c/s start one"))
	} else {
		for i, r := range visible {
			marker := strings.Repeat(" ", markerW)
			if offset+i == cursor {
				marker = "▸ "
			}
			lines = append(lines, marker+hslice(renderRow(r, frame, w), hoff, viewW))
		}
	}

	footer := fmt.Sprintf("%d transfer(s)", len(rows))
	if len(rows) > page {
		footer = fmt.Sprintf("%d-%d of %d", offset+1, min(offset+page, len(rows)), len(rows))
	}
	// The ←/→ hint (with the current offset over the hidden cells) comes
	// right after the count so a narrow terminal — exactly when the table
	// overflows — never truncates it away with the long key legend.
	if w.content() > viewW {
		footer += fmt.Sprintf(" · ←/→ scroll %d/%d", hoff, w.content()-viewW)
	}
	footer += " · j/k pgup/pgdn g/G scroll · enter sync detail · x cancel highlighted · t/esc close"
	lines = append(lines, dimStyle.Render(ansi.Truncate(footer, inner, "…")))
	return m.renderBox(lines)
}

// renderBox wraps the assembled lines in the overlay's full-canvas box.
func (m Model) renderBox(lines []string) string {
	box := boxStyle.Width(m.width)
	if m.height > 0 {
		box = box.Height(m.height)
	}
	return box.Render(strings.Join(lines, "\n"))
}

// Detail-mode layout: [marker][status glyph][rel path][bar+percent][size],
// the path column dynamic like the list's file column.
const detailFileCap = 60

// fileGlyph maps a planned file's state onto the row status glyphs:
// done ✓, failed ✗, started ▸, still queued ….
func fileGlyph(f syncmodal.FileProgress) string {
	switch {
	case f.Done:
		return statusGlyph(transferpanel.StatusDone)
	case f.Failed:
		return statusGlyph(transferpanel.StatusFailed)
	case f.Transferred > 0:
		return statusGlyph(transferpanel.StatusRunning)
	default:
		return statusGlyph(transferpanel.StatusQueued)
	}
}

// fileProgressCell renders a planned file's progress column: deletes get a
// word (no bytes move), failed and zero-byte files their terminal word,
// everything else a live bar+percent against the planned size.
func fileProgressCell(f syncmodal.FileProgress) string {
	switch {
	case f.Deleted:
		if f.Done {
			return "deleted"
		}
		if f.Failed {
			return "failed"
		}
		return "delete"
	case f.Failed:
		return "failed"
	case f.Done:
		if f.Size > 0 {
			return fmt.Sprintf("[%s] 100%%", strings.Repeat("█", barWidth))
		}
		return "done"
	case f.Size > 0:
		return fmt.Sprintf("[%s] %s", bar(f.Transferred, f.Size, 0), percent(f.Transferred, f.Size))
	default:
		return "queued"
	}
}

// humanBytes renders n in binary units ("1.5 KiB").
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// detailLines renders the per-file detail body: title, header, the visible
// page of planned files (live from syncmodal while the sync runs, from the
// completed-plan cache afterwards) and a footer. The listing repaints on
// the same tick/poll cadence as the list view.
func (m Model) detailLines() []string {
	title := m.detailLabel
	if !strings.HasPrefix(title, "dir:") {
		title = "dir: " + title
	}
	inner := m.innerWidth()
	lines := []string{titleStyle.Render(ansi.Truncate(title, inner-2, "…"))}

	files, ok := perFileFn(m.detailID)
	if !ok {
		lines = append(lines,
			dimStyle.Render("no per-file plan recorded for this sync"),
			dimStyle.Render("esc/enter back · t close"))
		return lines
	}

	page := m.pageSize()
	cursor, offset := clampView(m.detailCursor, m.detailOffset, len(files), page)
	end := offset + page
	if end > len(files) {
		end = len(files)
	}
	visible := files[offset:end]

	stW, fileW, sizeW := ansi.StringWidth("st"), ansi.StringWidth("file"), ansi.StringWidth("size")
	doneCount := 0
	for _, f := range files {
		if f.Done {
			doneCount++
		}
	}
	for _, f := range visible {
		stW = max(stW, ansi.StringWidth(fileGlyph(f)))
		fileW = max(fileW, ansi.StringWidth(f.Rel))
		sizeW = max(sizeW, ansi.StringWidth(humanBytes(f.Size)))
	}
	fileW = min(fileW, detailFileCap)

	header := strings.Join([]string{
		pad("st", stW), pad("file", fileW), pad("progress", progW), pad("size", sizeW),
	}, gap)
	lines = append(lines, dimStyle.Render(strings.Repeat(" ", markerW)+ansi.Truncate(header, inner-markerW, "")))

	if len(files) == 0 {
		lines = append(lines, dimStyle.Render("nothing to transfer — everything already in sync"))
	}
	for i, f := range visible {
		marker := strings.Repeat(" ", markerW)
		if offset+i == cursor {
			marker = "▸ "
		}
		row := strings.Join([]string{
			pad(fileGlyph(f), stW),
			pad(f.Rel, fileW),
			pad(fileProgressCell(f), progW),
			pad(humanBytes(f.Size), sizeW),
		}, gap)
		lines = append(lines, marker+ansi.Truncate(row, inner-markerW, ""))
	}

	footer := fmt.Sprintf("%d file(s) · %d done", len(files), doneCount)
	if len(files) > page {
		footer = fmt.Sprintf("%d-%d of %d · %d done", offset+1, end, len(files), doneCount)
	}
	footer += " · j/k scroll · esc/enter back · t close"
	lines = append(lines, dimStyle.Render(ansi.Truncate(footer, inner, "…")))
	return lines
}
