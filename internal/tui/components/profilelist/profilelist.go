// Package profilelist renders the AWS shared-config profile picker and
// loads profiles from ~/.aws/credentials and ~/.aws/config.
package profilelist

import (
	"log"

	"github.com/LinPr/lazys3/internal/tui/components/filter"
	"github.com/LinPr/lazys3/internal/tui/components/style"
	"github.com/charmbracelet/bubbles/v2/key"
	"github.com/charmbracelet/bubbles/v2/list"
	tea "github.com/charmbracelet/bubbletea/v2"
)

const ProfileListTitle = "AWS Profiles"

type Model struct {
	profileList list.Model

	// Outer dimensions (including the border frame) from SetSize.
	width   int
	height  int
	focused bool
}

func NewModel() Model {
	items := make([]list.Item, 0)
	delegate := list.NewDefaultDelegate()
	// Theme override for the highlighted row (style.Apply runs before the
	// components are constructed). This delegate renders descriptions, so
	// the desc line's left border must follow too or the two-line selection
	// bar renders two different colors.
	if c := style.SelectedItemFg; c != nil {
		delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(c).BorderForeground(c)
		delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.BorderForeground(c)
	}
	profileList := list.New(items, delegate, 0, 0)
	profileList.Filter = filter.Substring
	profileList.Title = ProfileListTitle
	profileList.DisableQuitKeybindings()
	// Narrow paging to pgup/pgdown so the default bindings (right/l/d/f,
	// left/h/b/u) don't shadow the global navigation and file-op keys.
	profileList.KeyMap.PrevPage = key.NewBinding(
		key.WithKeys("pgup"), key.WithHelp("pgup", "prev page"))
	profileList.KeyMap.NextPage = key.NewBinding(
		key.WithKeys("pgdown"), key.WithHelp("pgdn", "next page"))
	return Model{
		profileList: profileList,
		focused:     true,
	}
}

func (m Model) Init() tea.Cmd {
	return ReadAwsConfigProfileListCmd()
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case ReadAwsConfigResult:
		if msg.Err != nil {
			log.Println("read aws config error:", msg.Err)
		}
		items := make([]list.Item, 0)
		for _, profile := range msg.Profiles {
			items = append(items, profile)
		}
		m.profileList.SetItems(items)
		// bubbles' SetItems recomputes the page size against the
		// pagination line's PREVIOUS height, so a listing that crosses the
		// one-page boundary renders one row too many until the next
		// SetSize. Re-apply the size to converge (see locallist.setEntries).
		m.SetSize(m.width, m.height)
		return m, nil
	}

	newProfileListModel, cmd := m.profileList.Update(msg)
	m.profileList = newProfileListModel
	return m, cmd
}

// View renders the bordered list at exactly the outer size from SetSize.
// lipgloss v2's Width/Height include the border frame, so the style gets
// the outer dimensions while the wrapped list was sized to the inner ones.
func (m Model) View() string {
	border := style.FocusedBorderColor
	if !m.focused {
		border = style.UnfocusedBorderColor
	}
	return style.ProfileListStyle.
		BorderForeground(border).
		Width(m.width).
		Height(m.height).
		Render(m.profileList.View())
}

// SetFocused marks the pane as owning list-navigation keys (dual-pane
// mode); View picks the border color from it. Constructors default to
// focused so single-pane rendering is unchanged.
func (m *Model) SetFocused(v bool) { m.focused = v }

// Focused reports whether the pane is focused.
func (m Model) Focused() bool { return m.focused }

// Filtering reports whether the list's filter input is focused. The parent
// Update must skip global hotkey handling while this is true.
func (m Model) Filtering() bool {
	return m.profileList.SettingFilter()
}

func (m Model) GetSelectedProfile() *Profile {
	if item, ok := m.profileList.SelectedItem().(Profile); ok {
		return &item
	}
	return nil
}

// SetSize sets the component's outer dimensions. The wrapped list gets the
// inner size (outer minus the border frame) so rows never overflow the box.
// The title is re-fit to the new width so it never wraps at narrow widths.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	fh, fv := style.ProfileListStyle.GetFrameSize()
	m.profileList.SetSize(max(width-fh, 0), max(height-fv, 0))
	// bubbles sizes its help to the full list width but renders it inside
	// HelpStyle's 2-col left padding, so a footer of exactly that width
	// wraps onto a second line at narrow pane widths. Shrink the help
	// budget by the style's frame so the footer truncates ("…") instead.
	m.profileList.Help.Width = max(m.profileList.Width()-m.profileList.Styles.HelpStyle.GetHorizontalFrameSize(), 0)
	m.profileList.Title = style.FitListTitle(ProfileListTitle, m.profileList.Width())
}

// GetSize returns the outer dimensions from SetSize.
func (m *Model) GetSize() (width, height int) {
	return m.width, m.height
}
