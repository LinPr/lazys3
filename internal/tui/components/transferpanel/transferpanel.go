// Package transferpanel renders a compact, always-on panel at the bottom of
// the TUI that tracks in-flight and recently completed file operations
// (downloads, uploads, deletes, copies, renames, bucket make/remove).
//
// The panel is driven by messages emitted by the file-op tea.Cmds in
// objectlist/ops.go:
//
//   - TransferAddMsg        — queue a new transfer (status=queued)
//   - TransferStartMsg      — mark a transfer as running
//   - TransferProgressMsg   — update done/total bytes
//   - TransferDoneMsg       — mark a transfer as done/failed/canceled
//   - TickMsg               — refresh rows from their shared Progress
//     counters while any transfer is running (re-armed every 200ms)
//
// Byte progress flows through a shared *Progress per transfer: the storage
// progress callback (running on a worker goroutine) writes atomically, and
// the panel's tick loop reads it into the row for rendering.
package transferpanel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Op is the kind of operation a Transfer represents.
type Op string

const (
	OpDownload     Op = "download"
	OpUpload       Op = "upload"
	OpDelete       Op = "delete"
	OpCopy         Op = "copy"
	OpRename       Op = "rename"
	OpMakeBucket   Op = "mb"
	OpDeleteBucket Op = "rb"
	OpSync         Op = "sync"
	OpVersioning   Op = "versioning"
)

// Status is the lifecycle state of a Transfer.
type Status string

const (
	StatusQueued   Status = "queued"
	StatusRunning  Status = "running"
	StatusDone     Status = "done"
	StatusFailed   Status = "failed"
	StatusCanceled Status = "canceled"
)

// Progress is a goroutine-safe transferred/total byte pair shared between a
// storage progress callback and the panel's tick loop. transferred is kept
// max-so-far because an upload over a plain HTTP endpoint may reset the
// byte count once when the SDK re-reads the body for payload hashing.
// total == -1 means unknown (indeterminate).
type Progress struct {
	transferred atomic.Int64
	total       atomic.Int64
}

// NewProgress returns a Progress with an unknown total (-1).
func NewProgress() *Progress {
	p := &Progress{}
	p.total.Store(-1)
	return p
}

// Report records a progress callback observation. Safe for concurrent use;
// it matches storage.ProgressFunc so it can be passed as the callback.
func (p *Progress) Report(transferred, total int64) {
	for {
		cur := p.transferred.Load()
		if transferred <= cur || p.transferred.CompareAndSwap(cur, transferred) {
			break
		}
	}
	p.total.Store(total)
}

// Load returns the current (max-so-far transferred, total) pair.
func (p *Progress) Load() (transferred, total int64) {
	return p.transferred.Load(), p.total.Load()
}

// Transfer is a single tracked operation. The ID is assigned by the handler
// (NewID) and is used by the file-op tea.Cmds to address progress/done
// updates. Progress and Cancel are optional: Progress feeds the tick loop
// with live byte counts; Cancel aborts the operation's context.
type Transfer struct {
	ID         string
	Op         Op
	Label      string
	Total      int64
	Done       int64
	Status     Status
	Err        error
	Note       string
	Progress   *Progress
	Cancel     context.CancelFunc
	StartedAt  time.Time
	FinishedAt time.Time
}

// TransferAddMsg queues a new transfer. Emitted as a tea.Cmd by the modal
// onConfirm callbacks so the row is created on the live model (never on a
// stale captured copy), and only after the user confirms.
type TransferAddMsg struct {
	Transfer Transfer
}

// TransferStartMsg marks the named transfer as running.
type TransferStartMsg struct {
	ID string
}

// TransferProgressMsg updates byte progress imperatively.
type TransferProgressMsg struct {
	ID    string
	Done  int64
	Total int64
}

// TransferDoneMsg marks the named transfer as completed. Err is nil on
// success; a context.Canceled error marks the row canceled instead of
// failed.
type TransferDoneMsg struct {
	ID  string
	Err error
	// Op and Label are echoed back so the handler can decide which list to
	// refresh without tracking the ID → transfer mapping itself.
	Op    Op
	Label string
	// Note, when non-empty, replaces the row's note — a fast sync can
	// finish before its first 200ms poll, so the final summary must not
	// depend on the poll loop having observed anything.
	Note string
	// Local marks an op that ran on the local filesystem (the dual-pane
	// local delete): the remote listing is untouched, so the TUI refreshes
	// the local pane instead of re-fetching the remote list.
	Local bool
}

// TickMsg drives the panel's progress refresh loop. It is re-armed by
// Update while any transfer with a Progress counter is still running.
type TickMsg struct{}

func tickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return TickMsg{} })
}

// maxHistory bounds the transfer history: once the slice exceeds it, the
// oldest finished (done/failed/canceled) rows are evicted so a long
// session's tick loop and row scans stay O(maxHistory).
const maxHistory = 100

// Model is the transfer panel model.
type Model struct {
	transfers  []Transfer
	visible    bool
	ticking    bool
	frame      int
	width      int
	height     int
	maxVisible int
	idCounter  uint64
}

// NewModel returns a transfer panel model. The panel starts hidden and is
// revealed by Toggle or by sending a TransferAddMsg.
func NewModel() Model {
	return Model{
		visible:    false,
		maxVisible: 5,
	}
}

func (m Model) Init() tea.Cmd { return nil }

// Update handles transfer-related messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case TransferAddMsg:
		t := msg.Transfer
		if t.ID == "" {
			m.idCounter++
			t.ID = fmt.Sprintf("t%d", m.idCounter)
		}
		if t.Status == "" {
			t.Status = StatusQueued
		}
		if t.Status == StatusRunning && t.StartedAt.IsZero() {
			t.StartedAt = time.Now()
		}
		m.transfers = append(m.transfers, t)
		m.pruneFinished()
		m.visible = true
		if t.Progress != nil && !m.ticking {
			m.ticking = true
			return m, tickCmd()
		}
	case TransferStartMsg:
		m.updateStatus(msg.ID, StatusRunning, nil)
	case TransferProgressMsg:
		m.updateProgress(msg.ID, msg.Done, msg.Total)
	case TransferDoneMsg:
		status := StatusDone
		if msg.Err != nil {
			if errors.Is(msg.Err, context.Canceled) {
				status = StatusCanceled
			} else {
				status = StatusFailed
			}
		}
		// Apply the final note before the row turns terminal: SetNote
		// ignores updates to terminal rows (so stale poll snapshots can't
		// overwrite the summary), which would also swallow this one.
		if msg.Note != "" {
			m.SetNote(msg.ID, msg.Note)
		}
		m.updateStatus(msg.ID, status, msg.Err)
	case TickMsg:
		m.frame++
		active := false
		for i := range m.transfers {
			t := &m.transfers[i]
			if t.Progress == nil {
				continue
			}
			if t.Status == StatusRunning || t.Status == StatusQueued {
				t.Done, t.Total = t.Progress.Load()
				active = true
			}
		}
		if active {
			return m, tickCmd()
		}
		m.ticking = false
	}
	return m, nil
}

func (m *Model) updateStatus(id string, status Status, err error) {
	for i := range m.transfers {
		t := &m.transfers[i]
		if t.ID != id {
			continue
		}
		// A row the user already canceled stays canceled, even when the
		// op goroutine later reports the ctx error as done/failed.
		if t.Status == StatusCanceled {
			status = StatusCanceled
		}
		t.Status = status
		t.Err = err
		switch status {
		case StatusDone, StatusFailed, StatusCanceled:
			t.FinishedAt = time.Now()
			if t.Cancel != nil {
				t.Cancel()
				t.Cancel = nil
			}
			if t.Progress != nil {
				t.Done, t.Total = t.Progress.Load()
			}
			if status == StatusDone && t.Total > 0 {
				t.Done = t.Total
			}
		case StatusRunning:
			if t.StartedAt.IsZero() {
				t.StartedAt = time.Now()
			}
		}
		return
	}
}

func (m *Model) updateProgress(id string, done, total int64) {
	for i := range m.transfers {
		if m.transfers[i].ID == id {
			m.transfers[i].Done = done
			m.transfers[i].Total = total
			return
		}
	}
}

// Add queues a transfer and returns the assigned ID. Retained for callers
// that hold the model directly (tests).
func (m *Model) Add(t Transfer) string {
	m.idCounter++
	if t.ID == "" {
		t.ID = fmt.Sprintf("t%d", m.idCounter)
	}
	if t.Status == "" {
		t.Status = StatusQueued
	}
	m.transfers = append(m.transfers, t)
	m.pruneFinished()
	m.visible = true
	return t.ID
}

// pruneFinished evicts the oldest finished rows once the history exceeds
// maxHistory. Active (queued/running) rows are never evicted, so the slice
// may temporarily exceed the cap while that many transfers are in flight.
func (m *Model) pruneFinished() {
	excess := len(m.transfers) - maxHistory
	if excess <= 0 {
		return
	}
	n := len(m.transfers)
	kept := m.transfers[:0]
	for _, t := range m.transfers {
		if excess > 0 {
			switch t.Status {
			case StatusDone, StatusFailed, StatusCanceled:
				excess--
				continue
			}
		}
		kept = append(kept, t)
	}
	// Zero the compacted-away tail so evicted rows release their Err,
	// Progress, and label references.
	for i := len(kept); i < n; i++ {
		m.transfers[i] = Transfer{}
	}
	m.transfers = kept
}

// UpdateProgress is the imperative form of TransferProgressMsg, used by
// callers that hold the model directly (the sync poll loop). An empty
// status leaves the row's status unchanged.
func (m *Model) UpdateProgress(id string, done, total int64, status Status, err error) {
	for i := range m.transfers {
		t := &m.transfers[i]
		if t.ID != id {
			continue
		}
		// A terminal row keeps its final Done/Total: a stale in-flight
		// poll dequeued after TransferDoneMsg must not overwrite them.
		if t.Status == StatusDone || t.Status == StatusFailed || t.Status == StatusCanceled {
			return
		}
		t.Done = done
		t.Total = total
		if status != "" && t.Status != StatusCanceled {
			t.Status = status
		}
		if err != nil {
			t.Err = err
		}
		if status == StatusDone || status == StatusFailed {
			t.FinishedAt = time.Now()
		} else if status == StatusRunning && t.StartedAt.IsZero() {
			t.StartedAt = time.Now()
		}
		return
	}
}

// SetNote attaches a short free-form annotation to a row (e.g. the sync
// loop's "3 file(s) done · path" detail). Rendered after the status.
func (m *Model) SetNote(id, note string) {
	for i := range m.transfers {
		if m.transfers[i].ID == id {
			switch m.transfers[i].Status {
			case StatusDone, StatusFailed, StatusCanceled:
				// Keep the final note; ignore stale in-flight updates.
				return
			}
			m.transfers[i].Note = note
			return
		}
	}
}

// Transfer returns a copy of the named transfer's row. Used by the TUI to
// snapshot the final row state (bytes, StartedAt/FinishedAt, note) when a
// transfer turns terminal, e.g. for the persistent history file.
func (m Model) Transfer(id string) (Transfer, bool) {
	for i := range m.transfers {
		if m.transfers[i].ID == id {
			return m.transfers[i], true
		}
	}
	return Transfer{}, false
}

// Status returns the named transfer's status.
func (m Model) Status(id string) (Status, bool) {
	for i := range m.transfers {
		if m.transfers[i].ID == id {
			return m.transfers[i].Status, true
		}
	}
	return "", false
}

// CancelLatest cancels the most recently queued/running transfer that has
// a cancelable context. Returns the canceled ID and true on success.
func (m *Model) CancelLatest() (string, bool) {
	for i := len(m.transfers) - 1; i >= 0; i-- {
		t := &m.transfers[i]
		if (t.Status == StatusRunning || t.Status == StatusQueued) && t.Cancel != nil {
			t.Cancel()
			t.Cancel = nil
			t.Status = StatusCanceled
			t.FinishedAt = time.Now()
			m.visible = true
			return t.ID, true
		}
	}
	return "", false
}

// Active returns copies of the queued/running rows. Used by the quit path
// to snapshot in-flight transfers before CancelAll marks them canceled.
func (m Model) Active() []Transfer {
	var active []Transfer
	for _, t := range m.transfers {
		if t.Status == StatusRunning || t.Status == StatusQueued {
			active = append(active, t)
		}
	}
	return active
}

// Counts tallies the panel's rows for the status bar's transfer summary:
// running includes queued rows, failed includes canceled ones.
func (m Model) Counts() (running, done, failed int) {
	for _, t := range m.transfers {
		switch t.Status {
		case StatusRunning, StatusQueued:
			running++
		case StatusDone:
			done++
		case StatusFailed, StatusCanceled:
			failed++
		}
	}
	return running, done, failed
}

// CancelAll cancels every outstanding transfer context. Called on quit so
// no goroutine outlives the TUI.
func (m *Model) CancelAll() {
	for i := range m.transfers {
		t := &m.transfers[i]
		if (t.Status == StatusRunning || t.Status == StatusQueued) && t.Cancel != nil {
			t.Cancel()
			t.Cancel = nil
			t.Status = StatusCanceled
			t.FinishedAt = time.Now()
		}
	}
}

// Toggle shows/hides the panel.
func (m *Model) Toggle() { m.visible = !m.visible }

// IsVisible reports whether the panel is visible.
func (m Model) IsVisible() bool { return m.visible }

// SetSize sets the panel dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// SetMaxVisible sets the row cap so a configured transfer_panel_height can
// actually show more (or fewer) rows than the default. Values below 1 are
// clamped.
func (m *Model) SetMaxVisible(n int) {
	if n < 1 {
		n = 1
	}
	m.maxVisible = n
}

// NextID returns the next transfer ID without queuing anything. Used by
// handlers that need an ID before queuing (e.g. to pass into a tea.Cmd).
func (m *Model) NextID() string {
	m.idCounter++
	return fmt.Sprintf("t%d", m.idCounter)
}

var idSeq uint64

// NewID returns a globally-unique transfer ID. Used by handlers that build
// a TransferAddMsg/TransferStartMsg pair without holding the model pointer
// (the ID is matched when the messages arrive).
func NewID() string {
	return fmt.Sprintf("t%d", atomic.AddUint64(&idSeq, 1))
}

// opIcon returns a short glyph for the operation kind.
func opIcon(op Op) string {
	switch op {
	case OpDownload:
		return "↓"
	case OpUpload:
		return "↑"
	case OpDelete:
		return "✕"
	case OpCopy:
		return "⎘"
	case OpRename:
		return "⟳"
	case OpMakeBucket:
		return "▣"
	case OpDeleteBucket:
		return "⊟"
	case OpSync:
		return "⇅"
	case OpVersioning:
		return "◈"
	default:
		return "•"
	}
}

// statusIcon returns a short glyph for the status.
func statusIcon(s Status) string {
	switch s {
	case StatusQueued:
		return "…"
	case StatusRunning:
		return "▸"
	case StatusDone:
		return "✓"
	case StatusFailed:
		return "✗"
	case StatusCanceled:
		return "⊘"
	default:
		return " "
	}
}

// bar renders a 10-cell progress bar. total > 0 renders a determinate
// fill; total <= 0 renders an indeterminate 3-cell block that bounces with
// the tick frame.
func bar(done, total int64, frame int) string {
	const width = 10
	if total <= 0 {
		const block = 3
		span := width - block
		pos := frame % (2 * span)
		if pos > span {
			pos = 2*span - pos
		}
		return strings.Repeat(" ", pos) + strings.Repeat("█", block) +
			strings.Repeat(" ", width-block-pos)
	}
	pct := float64(done) / float64(total)
	if pct < 0 {
		pct = 0
	} else if pct > 1 {
		pct = 1
	}
	filled := int(pct * float64(width))
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat(" ", width-filled)
}

// percent renders " NN%" when the total is known, else "".
func percent(done, total int64) string {
	if total <= 0 {
		return ""
	}
	pct := 100 * float64(done) / float64(total)
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}
	return fmt.Sprintf(" %3.0f%%", pct)
}

// truncateLabel shortens a label to fit within width cells, replacing the
// middle with "…" when truncated. This keeps the table layout stable even
// with very long s3://bucket/long/prefix/key labels.
func truncateLabel(s string, width int) string {
	if width <= 0 {
		return ""
	}
	sw := ansi.StringWidth(s)
	if sw <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	// Keep the head and tail; the middle is the most variable part of an
	// s3 path and is least useful for identification. Truncation is
	// cell-based so multi-byte (CJK) keys are never split mid-rune.
	head := (width - 1) / 2
	tail := width - 1 - head
	headStr := ansi.Truncate(s, head, "")
	tailStr := ansi.TruncateLeft(s, sw-tail, "")
	// A wide rune straddling the cut can leave the tail a cell over
	// budget; trim whole runes from the left until the label fits.
	// (ansi.TruncateLeft by one cell cannot split a two-cell rune and
	// would loop forever.)
	for tailStr != "" && ansi.StringWidth(headStr)+1+ansi.StringWidth(tailStr) > width {
		_, size := utf8.DecodeRuneInString(tailStr)
		tailStr = tailStr[size:]
	}
	return headStr + "…" + tailStr
}

// panelStyle is the panel's bordered box. Rows are budgeted against its
// frame so the rendered block never exceeds the width from SetSize.
var panelStyle = lipgloss.NewStyle().
	Border(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("#3b82f6")).
	Padding(0, 1)

// innerWidth returns the row budget: the outer width minus the panel's
// border and padding.
func (m Model) innerWidth() int {
	return m.width - panelStyle.GetHorizontalFrameSize()
}

// renderRow renders one transfer line fitted to the panel's inner width.
// The label truncation accounts for the bar, percentage, status word and
// note (capped) so they stay visible; whatever still doesn't fit is
// clipped at the right edge as a last resort.
func (m Model) renderRow(t Transfer) string {
	inner := m.innerWidth()
	// icon + " " + icon + "  " ahead of the label.
	const prefixW = 5
	tail := fmt.Sprintf("  [%s]%s %s",
		bar(t.Done, t.Total, m.frame),
		percent(t.Done, t.Total),
		t.Status,
	)
	if t.Note != "" {
		tail += "  " + truncateLabel(t.Note, 40)
	}
	if t.Status == StatusFailed && t.Err != nil {
		errMsg := t.Err.Error()
		if len(errMsg) > 30 {
			errMsg = errMsg[:27] + "..."
		}
		tail += "  " + errMsg
	}
	labelW := inner - prefixW - ansi.StringWidth(tail)
	if labelW < 8 {
		labelW = 8
	}
	row := fmt.Sprintf("%s %s  %s%s",
		opIcon(t.Op),
		statusIcon(t.Status),
		truncateLabel(t.Label, labelW),
		tail,
	)
	if inner > 0 {
		row = ansi.Truncate(row, inner, "")
	}
	return row
}

// View renders the panel. When hidden it returns "" so the layout
// collapses. When visible it renders a bordered box with one row per
// transfer, capped at what fits in the height from SetSize (and at most
// maxVisible). Overflow is indicated by a "+N more" footer.
func (m Model) View() string {
	if !m.visible || len(m.transfers) == 0 {
		return ""
	}

	// The border eats 2 lines and the title 1, leaving m.height-3 body
	// lines for rows plus the overflow footer.
	maxRows := m.maxVisible
	if m.height > 0 {
		if avail := m.height - panelStyle.GetVerticalFrameSize() - 1; avail < maxRows {
			maxRows = avail
		}
	}
	if maxRows < 1 {
		maxRows = 1
	}

	visible := m.transfers
	overflow := 0
	if len(visible) > maxRows {
		// Reserve one body line for the "+N more" footer.
		overflow = len(visible) - (maxRows - 1)
		visible = visible[len(visible)-(maxRows-1):]
	}

	lines := make([]string, 0, len(visible)+1)
	for _, t := range visible {
		lines = append(lines, m.renderRow(t))
	}
	if overflow > 0 {
		lines = append(lines, fmt.Sprintf("+%d more", overflow))
	}

	// lipgloss v2's Width includes the border frame, so the rendered block
	// is exactly m.width wide; rows were budgeted against innerWidth.
	style := panelStyle.Width(m.width)
	if m.height > 0 {
		style = style.MaxHeight(m.height)
	}

	return style.Render("Transfers\n" + strings.Join(lines, "\n"))
}
