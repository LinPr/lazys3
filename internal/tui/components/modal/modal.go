// Package modal provides a tiny text-input / confirm modal for the lazys3
// TUI. It is used by the file-op key branches (handler.go) to prompt for
// download paths, upload sources, rename targets, copy destinations, and
// confirmations on destructive operations.
//
// The modal stores an onConfirm callback that returns a tea.Cmd. When the
// user confirms an input-mode modal, the current input value is passed to
// onConfirm and the resulting Cmd is returned by Update. For confirm-mode
// (yes/no) modals, onConfirm is called with the empty string on "yes".
//
// While the modal is visible, the TUI's Update must dispatch key events to
// the modal first (see handler.go): when m.modal.IsVisible(), forward the
// msg to modal.Update and skip the list/preview dispatch.
package modal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/v2/textinput"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
)

// Mode selects the modal interaction style.
type Mode int

const (
	// ModeInput prompts for a free-form text value (e.g. a path or s3
	// URI). Enter submits; Esc cancels.
	ModeInput Mode = iota
	// ModeConfirm renders a body and asks yes/no. 'y' confirms; any other
	// key (except Esc which also cancels) is ignored.
	ModeConfirm
)

// Model is the modal model. It owns a textinput for input mode and a stored
// onConfirm callback. Track D's statusbar/help overlay can hook in by
// listening for the modal's tea.Cmd results.
type Model struct {
	visible     bool
	mode        Mode
	title       string
	body        string
	yesLabel    string
	noLabel     string
	placeholder string
	input       textinput.Model
	onConfirm   func(string) tea.Cmd
	onCancel    func() tea.Cmd
	width       int
	height      int
}

// NewModel returns a hidden modal. Show it with Show or ShowConfirm.
func NewModel() Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = ""
	ti.CharLimit = 0
	return Model{
		yesLabel: "y",
		noLabel:  "N",
		input:    ti,
	}
}

func (m Model) Init() tea.Cmd { return nil }

// Update handles key events while the modal is visible.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.visible {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.Hide()
			if m.onCancel != nil {
				return m, m.onCancel()
			}
			return m, nil
		case "enter":
			if m.mode == ModeInput {
				val := m.input.Value()
				if val == "" {
					// Fall back to the displayed default so hitting
					// enter without typing submits the placeholder.
					val = m.placeholder
				}
				cb := m.onConfirm
				m.Hide()
				if cb != nil {
					return m, cb(val)
				}
				return m, nil
			}
			// ModeConfirm: enter defaults to no.
			m.Hide()
			if m.onCancel != nil {
				return m, m.onCancel()
			}
			return m, nil
		case "y":
			if m.mode == ModeConfirm {
				cb := m.onConfirm
				m.Hide()
				if cb != nil {
					return m, cb("")
				}
				return m, nil
			}
			// In input mode, 'y' is a regular character.
		case "n":
			if m.mode == ModeConfirm {
				m.Hide()
				if m.onCancel != nil {
					return m, m.onCancel()
				}
				return m, nil
			}
		}
	}
	// Otherwise forward to the textinput in input mode.
	if m.mode == ModeInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// Show opens an input modal. onConfirm receives the typed value, or the
// placeholder default when the user confirms without typing.
func (m *Model) Show(title, placeholder string, onConfirm func(string) tea.Cmd) {
	m.visible = true
	m.mode = ModeInput
	m.title = title
	m.body = ""
	m.placeholder = placeholder
	m.input.Reset()
	m.input.Placeholder = placeholder
	m.input.Focus()
	m.onConfirm = onConfirm
	m.onCancel = nil
}

// ShowConfirm opens a yes/no modal. onConfirm is called with "" on "yes".
func (m *Model) ShowConfirm(title, body string, onConfirm func() tea.Cmd) {
	m.visible = true
	m.mode = ModeConfirm
	m.title = title
	m.body = body
	m.onConfirm = func(string) tea.Cmd {
		if onConfirm != nil {
			return onConfirm()
		}
		return nil
	}
	m.onCancel = nil
}

// ShowConfirmWithCancel is like ShowConfirm but also lets the caller hook
// the cancel path (e.g. to clear a transient selection).
func (m *Model) ShowConfirmWithCancel(title, body string, onConfirm func() tea.Cmd, onCancel func() tea.Cmd) {
	m.ShowConfirm(title, body, onConfirm)
	m.onCancel = onCancel
}

// Hide closes the modal and clears state.
func (m *Model) Hide() {
	m.visible = false
	m.title = ""
	m.body = ""
	m.onConfirm = nil
	m.onCancel = nil
	m.input.Reset()
}

// IsVisible reports whether the modal is currently shown.
func (m Model) IsVisible() bool { return m.visible }

// Value returns the current input value (input mode only).
func (m Model) Value() string { return m.input.Value() }

// SetSize sets the modal's rendering dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	if w > 4 {
		m.input.SetWidth(w - 4)
	}
}

// View renders the modal. It is the caller's responsibility (tui.go) to
// overlay the rendered string on top of the rest of the UI.
func (m Model) View() string {
	if !m.visible {
		return ""
	}
	width := m.width
	if width < 40 {
		width = 40
	}
	if width > 80 {
		width = 80
	}

	var content string
	switch m.mode {
	case ModeInput:
		content = m.input.View()
	case ModeConfirm:
		content = m.body
	}

	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#e39f00ff")).
		Background(lipgloss.Color("#444745ff")).
		Padding(0, 1).
		Render(m.title)

	bodyStyle := lipgloss.NewStyle().Padding(0, 1)
	body := bodyStyle.Render(content)

	hint := ""
	if m.mode == ModeConfirm {
		hint = fmt.Sprintf("[%s] confirm  [%s/%s] cancel", m.yesLabel, m.noLabel, "esc")
	} else {
		hint = "[enter] confirm  [esc] cancel"
	}
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		Padding(0, 1).
		Render(hint)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3b82f6")).
		Width(width)

	rendered := box.Render(
		lipgloss.JoinVertical(lipgloss.Left, header, body, hintStyle),
	)

	// Center on the screen. The overlay is rendered on a full-canvas
	// background so the TUI's View() can swap it in for the whole layout
	// when the modal is visible (see tui.go).
	if m.height > 0 {
		dimBg := lipgloss.NewStyle().Background(lipgloss.Color("#1a1a1a"))
		rendered = lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			rendered,
			lipgloss.WithWhitespaceStyle(dimBg),
		)
	}
	return rendered
}

// Title returns the current modal title (used by tests / debugging).
func (m Model) Title() string { return m.title }

// Mode returns the current modal mode.
func (m Model) Mode() Mode { return m.mode }

// Body returns the rendered body for confirm modals (input modals return
// the current input value via Value()).
func (m Model) Body() string { return m.body }

// Reset is a small helper that resets the input field without closing the
// modal. Useful when the caller wants to reuse the same Show for multiple
// inputs.
func (m *Model) Reset() {
	m.input.Reset()
}

// HasMultilineBody reports whether the body contains a newline, used by the
// handler to decide on layout adjustments. Kept simple to avoid pulling in
// more state.
func (m Model) HasMultilineBody() bool {
	return strings.Contains(m.body, "\n")
}
