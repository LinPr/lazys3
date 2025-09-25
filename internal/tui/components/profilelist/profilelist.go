package profilelist

import (
	"log"

	"github.com/LinPr/lazys3/internal/tui/components/style"
	"github.com/charmbracelet/bubbles/v2/list"
	tea "github.com/charmbracelet/bubbletea/v2"
)

const ProfileListTitle = "AWS Profiles"

type size struct {
	width  int
	height int
}
type Model struct {
	profileList         list.Model
	keyBindings         *KeyBindings
	delegateKeyBindings *delegateKeyBindings
	size
}

func NewModel() Model {
	items := make([]list.Item, 0)
	delegate := list.NewDefaultDelegate()
	// delegate.Styles.NormalTitle = delegate.Styles.NormalTitle.MaxHeight(1)
	// delegate.Styles.NormalDesc = delegate.Styles.NormalDesc.MaxHeight(1)
	// delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.MaxHeight(1)
	// delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.MaxHeight(1)
	profileList := list.New(items, delegate, 0, 0)
	profileList.Title = ProfileListTitle
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
	case tea.KeyMsg:
		if msg.String() == "esc" {
			// return m, nil
		}
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

func (m Model) View() string {
	w, _ := m.GetSize()
	return style.ProfileListStyle.
		Width(w).
		Render(m.profileList.View())
}

func (m Model) GetSelectedProfile() *Profile {
	if item, ok := m.profileList.SelectedItem().(Profile); ok {
		return &item
	}
	return nil
}

func (m *Model) SetSize(width, height int) {
	m.profileList.SetSize(width, height)
	delegate := list.NewDefaultDelegate()
	// delegate.Styles.NormalTitle = delegate.Styles.NormalTitle.MaxHeight(1).MaxWidth(width)
	// delegate.Styles.NormalDesc = delegate.Styles.NormalDesc.MaxHeight(1).MaxWidth(width)
	// delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.MaxHeight(1).MaxWidth(width)
	// delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.MaxHeight(1).MaxWidth(width)
	m.profileList.SetDelegate(delegate)
}

func (m *Model) GetSize() (width, height int) {
	return m.profileList.Width(), m.profileList.Height()
}
