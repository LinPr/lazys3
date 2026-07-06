// Package historyview renders a full-screen overlay listing the persistent
// transfer history (internal/history), newest first. It follows the help
// overlay pattern: the TUI toggles it on 'T', swallows every other key
// while it is visible (except ctrl+c and the j/k/pgup/pgdown scrolling
// handled here), and closes it on esc/'T'.
//
// The records are re-read from the state file each time the overlay opens
// (LoadCmd, run as a tea.Cmd so the read never blocks Update); a loading
// line is shown until LoadedMsg arrives.
package historyview

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/lazys3/internal/history"
	"github.com/LinPr/lazys3/internal/strutil"
)

// loadLimit caps how many records the overlay reads from the state file.
const loadLimit = 500

// LoadedMsg carries the records read by LoadCmd (newest first).
type LoadedMsg struct {
	Records []history.Record
	Err     error
}

// LoadCmd reads the newest loadLimit records off the Update goroutine.
func LoadCmd(store *history.Store) tea.Cmd {
	return func() tea.Msg {
		recs, err := store.Load(loadLimit)
		return LoadedMsg{Records: recs, Err: err}
	}
}

// Model is the history overlay state.
type Model struct {
	visible bool
	loading bool
	loadErr error
	records []history.Record
	offset  int
	width   int
	height  int
}

// NewModel returns a hidden history overlay.
func NewModel() Model { return Model{} }

// Init is a no-op; loading is kicked off by the TUI when the overlay opens.
func (m Model) Init() tea.Cmd { return nil }

// Update consumes LoadedMsg; everything else passes through unchanged.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if lm, ok := msg.(LoadedMsg); ok {
		m.loading = false
		m.loadErr = lm.Err
		m.records = lm.Records
		m.offset = 0
	}
	return m, nil
}

// Show opens the overlay in its loading state (the TUI pairs it with
// LoadCmd). Hide closes it.
func (m *Model) Show() {
	m.visible = true
	m.loading = true
	m.offset = 0
}

func (m *Model) Hide() { m.visible = false }

// IsVisible reports whether the overlay is shown.
func (m Model) IsVisible() bool { return m.visible }

// SetSize sets the overlay's full-canvas dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// HandleKey scrolls the table. Unrecognised keys are swallowed by design
// (the TUI forwards nothing else while the overlay is visible).
func (m *Model) HandleKey(key string) {
	page := m.pageSize()
	switch key {
	case "j", "down":
		m.offset++
	case "k", "up":
		m.offset--
	case "pgdown":
		m.offset += page
	case "pgup":
		m.offset -= page
	}
	max := len(m.records) - page
	if m.offset > max {
		m.offset = max
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

var boxStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("#3b82f6")).
	Padding(0, 1)

// pageSize is how many record rows fit in the box: total height minus the
// border frame, title, header, and footer lines.
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

// fmtTime renders the record's RFC3339 timestamp as "01-02 15:04" in local
// time; unparseable timestamps fall back to the raw string, truncated.
func fmtTime(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return ansi.Truncate(s, 11, "…")
	}
	return t.Local().Format("01-02 15:04")
}

// fmtBytes renders the transferred byte count, "-" when unknown.
func fmtBytes(b int64) string {
	if b < 0 {
		return "-"
	}
	return strutil.HumanizeBytes(b)
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

// renderRow renders one record fitted to inner cells:
//
//	01-02 15:04  download  done      1.2M  label…  note-or-error
//
// The fixed columns come first so the variable label/detail tail can be
// truncated ansi-aware without disturbing the table alignment.
func renderRow(r history.Record, inner int) string {
	const (
		timeW   = 11
		opW     = 10
		statusW = 8
		sizeW   = 7
	)
	detail := r.Note
	if r.Error != "" {
		detail = r.Error
	}
	if detail != "" {
		detail = "  " + ansi.Truncate(detail, 30, "…")
	}
	fixed := timeW + 2 + opW + 2 + statusW + 2 + sizeW + 2
	labelW := inner - fixed - ansi.StringWidth(detail)
	if labelW < 8 {
		labelW = 8
	}
	row := fmt.Sprintf("%s  %s  %s  %s  %s%s",
		pad(fmtTime(r.Time), timeW),
		pad(r.Op, opW),
		pad(r.Status, statusW),
		pad(fmtBytes(r.Bytes), sizeW),
		pad(r.Label, labelW),
		detail,
	)
	return ansi.Truncate(row, inner, "")
}

// View renders the overlay: a full-canvas bordered box with a title,
// column header, the visible page of records, and a scroll footer.
func (m Model) View() string {
	if !m.visible {
		return ""
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#e39f00ff")).
		Background(lipgloss.Color("#444745ff")).
		Padding(0, 1)
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#3b82f6"))
	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#aaaaaa"))

	inner := m.innerWidth()
	page := m.pageSize()

	lines := []string{
		titleStyle.Render("lazys3 — transfer history"),
		headerStyle.Render(ansi.Truncate(fmt.Sprintf("%s  %s  %s  %s  %s",
			pad("time", 11), pad("op", 10), pad("status", 8), pad("size", 7), "label"), inner, "")),
	}

	switch {
	case m.loading:
		lines = append(lines, dimStyle.Render("loading history…"))
	case m.loadErr != nil:
		lines = append(lines, dimStyle.Render(ansi.Truncate("history unavailable: "+m.loadErr.Error(), inner, "…")))
	case len(m.records) == 0:
		lines = append(lines, dimStyle.Render("no transfers recorded yet — completed transfers will show up here"))
	default:
		end := m.offset + page
		if end > len(m.records) {
			end = len(m.records)
		}
		for _, r := range m.records[m.offset:end] {
			lines = append(lines, renderRow(r, inner))
		}
	}

	footer := fmt.Sprintf("%d record(s)", len(m.records))
	if len(m.records) > page {
		footer = fmt.Sprintf("%d-%d of %d", m.offset+1, min(m.offset+page, len(m.records)), len(m.records))
	}
	footer += " · j/k pgup/pgdn scroll · T/esc close"
	lines = append(lines, dimStyle.Render(ansi.Truncate(footer, inner, "…")))

	box := boxStyle.Width(m.width)
	if m.height > 0 {
		box = box.Height(m.height)
	}
	return box.Render(strings.Join(lines, "\n"))
}
