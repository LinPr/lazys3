// Package statusbar renders a single-line bar at the bottom of the lazys3
// TUI that surfaces the current context (profile, focused pane, selection
// count, transfer tallies) and the most recent info note or error.
//
// The bar is driven by two messages delivered by the TUI's Update loop:
//
//   - types.StatusUpdateMsg — refreshes the profile, the focused-pane
//     indicator, the selection count and the transfer tallies. The TUI
//     emits this after dispatching to the active list so the bar always
//     reflects the current state.
//   - types.ErrMsg — sets lastError so failures from file-op Cmds surface
//     visibly. The error is dismissable (DismissError).
//
// Layout (one line, always):
//
//	[profile] [pane] [N selected] [▶R ✓D ✗F] [info] [error] … [? help]
//
// The pane indicator appears only in dual-pane mode, the selection count
// only when >0, and the transfer summary only when any transfer rows
// exist. The transient info and the error share the remaining middle
// width (ansi-aware middle truncation); "? help" is right-aligned.
package statusbar

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/lazys3/internal/tui/components/style"
	"github.com/LinPr/lazys3/internal/tui/types"
)

// Model is the status bar state.
type Model struct {
	profile          string
	bucket           string
	prefix           string
	selectedCount    int
	pane             string
	transfersRunning int
	transfersDone    int
	transfersFailed  int
	lastError        string
	info             string
	width            int
	height           int
	dismissedError   string // snapshot of the dismissed error so we don't reshow
}

// NewModel returns a fresh, empty status bar.
func NewModel() Model {
	return Model{}
}

// Init is a no-op; the bar has no async work of its own.
func (m Model) Init() tea.Cmd { return nil }

// SetSize sets the bar's allocated dimensions. The bar always renders as a
// single line, so height is informational (used by the layout to reserve
// rows).
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// SetError displays an error message on the bar. Empty messages clear the
// error. Reissuing the same error string that the user just dismissed is
// ignored so the bar doesn't flicker the same error back.
func (m *Model) SetError(msg string) {
	if msg == "" {
		m.lastError = ""
		m.dismissedError = ""
		return
	}
	if msg == m.dismissedError {
		return
	}
	m.lastError = msg
}

// DismissError clears the current error and remembers it so a reissued
// SetError with the same string does not resurface it.
func (m *Model) DismissError() {
	if m.lastError != "" {
		m.dismissedError = m.lastError
	}
	m.lastError = ""
}

// SetInfo displays a transient informational note (e.g. "presigned URL
// copied to clipboard" when the result modal could not be shown). It is
// cleared by the next StatusUpdateMsg, i.e. the next navigation or
// selection change.
func (m *Model) SetInfo(msg string) { m.info = msg }

// Info returns the current informational note.
func (m Model) Info() string { return m.info }

// SetProfile / SetBucket / SetPrefix / SetSelectedCount / SetPane /
// SetTransferCounts are imperative setters used by the TUI (and tests) to
// keep the bar in sync without round-tripping through a message.
func (m *Model) SetProfile(p string)    { m.profile = p }
func (m *Model) SetBucket(b string)     { m.bucket = b }
func (m *Model) SetPrefix(p string)     { m.prefix = p }
func (m *Model) SetSelectedCount(n int) { m.selectedCount = n }
func (m *Model) SetPane(p string)       { m.pane = p }
func (m *Model) SetTransferCounts(running, done, failed int) {
	m.transfersRunning = running
	m.transfersDone = done
	m.transfersFailed = failed
}

// Profile / Bucket / Prefix / SelectedCount / Pane / LastError are read
// accessors used by the TUI and by tests.
func (m Model) Profile() string    { return m.profile }
func (m Model) Bucket() string     { return m.bucket }
func (m Model) Prefix() string     { return m.prefix }
func (m Model) SelectedCount() int { return m.selectedCount }
func (m Model) Pane() string       { return m.pane }
func (m Model) LastError() string  { return m.lastError }

// Update handles types.StatusUpdateMsg and types.ErrMsg. All other
// messages are ignored; the bar is passive.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch tmsg := msg.(type) {
	case types.StatusUpdateMsg:
		m.profile = tmsg.Profile
		m.bucket = tmsg.Bucket
		m.prefix = tmsg.Prefix
		m.selectedCount = tmsg.SelectedCount
		m.pane = tmsg.Pane
		m.transfersRunning = tmsg.TransfersRunning
		m.transfersDone = tmsg.TransfersDone
		m.transfersFailed = tmsg.TransfersFailed
		// The info note is dismissed only on navigation-ish changes
		// (ClearInfo); transfer-tally-only refreshes leave it readable.
		if tmsg.ClearInfo {
			m.info = ""
		}
	case types.ErrMsg:
		m.SetError(tmsg.Err.Error())
	}
	return m, nil
}

// transferSummary renders the "▶R ✓D ✗F" tallies (running incl. queued /
// done / failed incl. canceled), zero segments omitted. ASCII fallbacks
// (">R okD xF") are used when nerd_font is off. Empty when no transfer
// rows exist at all.
func (m Model) transferSummary() string {
	if m.transfersRunning+m.transfersDone+m.transfersFailed == 0 {
		return ""
	}
	run, ok, fail := "▶", "✓", "✗"
	if !style.NerdFontEnabled() {
		run, ok, fail = ">", "ok", "x"
	}
	runStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#3b82f6"))
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
	failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444"))
	var parts []string
	if m.transfersRunning > 0 {
		parts = append(parts, runStyle.Render(fmt.Sprintf("%s%d", run, m.transfersRunning)))
	}
	if m.transfersDone > 0 {
		parts = append(parts, okStyle.Render(fmt.Sprintf("%s%d", ok, m.transfersDone)))
	}
	if m.transfersFailed > 0 {
		parts = append(parts, failStyle.Render(fmt.Sprintf("%s%d", fail, m.transfersFailed)))
	}
	return strings.Join(parts, " ")
}

// View renders the bar as a single styled line:
//
//	[profile] [pane] [N selected] [transfer summary] [info] [error] … [? help]
//
// The info/error middle section is ansi-truncated (from the middle) to
// whatever width the fixed segments leave over; "? help" is right-aligned.
// The bar always occupies exactly one terminal line.
func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}

	// The profile chip and the error chip pick up theme overrides via the
	// shared style vars (title_fg/title_bg, status_error_fg).
	profileStyle := style.TitleStyle
	paneStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#a78bfa"))
	selStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#3b82f6"))
	infoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#22c55e"))
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#777777"))
	errStyle := style.StatusErrorStyle

	const sep = "  "
	segs := []string{profileStyle.Render(profileLabel(m.profile))}
	if m.pane != "" {
		segs = append(segs, paneStyle.Render(m.pane))
	}
	if m.selectedCount > 0 {
		segs = append(segs, selStyle.Render(fmt.Sprintf("%d selected", m.selectedCount)))
	}
	if ts := m.transferSummary(); ts != "" {
		segs = append(segs, ts)
	}
	left := strings.Join(segs, sep)
	helpBlock := helpStyle.Render("? help")

	// The middle (info + error) gets whatever the fixed segments leave
	// over. The error is budgeted first so it is never squeezed out by a
	// long info note; both are middle-truncated (CJK-safe).
	avail := m.width - lipgloss.Width(left) - lipgloss.Width(helpBlock) - 2*len(sep)
	var middle []string
	errW := 0
	if m.lastError != "" && avail > 0 {
		errText := truncateMiddle("err: "+m.lastError, avail)
		errW = lipgloss.Width(errText) + len(sep)
		middle = append(middle, errStyle.Render(errText))
	}
	if m.info != "" && avail-errW > 0 {
		infoBlock := infoStyle.Render(truncateMiddle(m.info, avail-errW))
		middle = append([]string{infoBlock}, middle...)
	}

	line := left
	if len(middle) > 0 {
		line += sep + strings.Join(middle, sep)
	}
	if pad := m.width - lipgloss.Width(line) - lipgloss.Width(helpBlock); pad > 0 {
		line += strings.Repeat(" ", pad) + helpBlock
	}
	// Hard-clip as a last resort so a too-narrow bar truncates instead of
	// wrapping (MaxHeight would otherwise clip the wrapped tail invisibly).
	line = ansi.Truncate(line, m.width, "")

	bar := lipgloss.NewStyle().
		Background(lipgloss.Color("#222222")).
		Width(m.width).
		MaxHeight(1)
	return bar.Render(line)
}

// profileLabel returns the profile name or a placeholder when none is set.
func profileLabel(p string) string {
	if p == "" {
		return "no profile"
	}
	return p
}

// truncateMiddle shortens s to fit within width cells, keeping the head and
// tail and replacing the middle with "…" when truncation is needed. It is
// shared with the list titles as style.TruncateMiddle.
func truncateMiddle(s string, width int) string {
	return style.TruncateMiddle(s, width)
}
