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
	width  int
	height int
}

func NewModel() Model {
	items := make([]list.Item, 0)
	delegate := list.NewDefaultDelegate()
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
	return style.ProfileListStyle.
		Width(m.width).
		Height(m.height).
		Render(m.profileList.View())
}

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
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	fh, fv := style.ProfileListStyle.GetFrameSize()
	m.profileList.SetSize(max(width-fh, 0), max(height-fv, 0))
}

// GetSize returns the outer dimensions from SetSize.
func (m *Model) GetSize() (width, height int) {
	return m.width, m.height
}
