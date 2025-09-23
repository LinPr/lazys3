package objectlist

import (
	// "github.com/LinPr/lazys3/internal/tui/components/style"

	"github.com/LinPr/lazys3/internal/tui/components/style"
	"github.com/charmbracelet/bubbles/v2/list"
	tea "github.com/charmbracelet/bubbletea/v2"
)

type Model struct {
	Title      string
	objectlist list.Model
}

func NewModel() Model {

	items := make([]list.Item, 0)

	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false

	objectlist := list.New(items, delegate, 0, 0)

	return Model{
		objectlist: objectlist,
	}
}

func (m Model) Init() tea.Cmd {

	return nil
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	newObjectListModel, cmd := m.objectlist.Update(msg)
	m.objectlist = newObjectListModel
	return m, cmd
}

func (m Model) View() string {
	w, _ := m.GetSize()
	return style.ObjectListStyle.
		Width(w).
		Render(m.objectlist.View())
}

func (m *Model) SetTitle(title string) {
	// m.Title = title
	m.objectlist.Title = title
}

func (m *Model) SetObjects(items []Object) {
	listItems := make([]list.Item, 0)
	for _, o := range items {
		listItems = append(listItems, o)
	}
	m.objectlist.SetItems(listItems)
}

func (m *Model) GetSelectedObject() *Object {
	if item, ok := m.objectlist.SelectedItem().(Object); ok {
		return &item
	}
	return nil
}

func (m *Model) SetSize(width, height int) {
	m.objectlist.SetSize(width, height)
}

func (m *Model) GetSize() (width, height int) {
	return m.objectlist.Width(), m.objectlist.Height()
}
