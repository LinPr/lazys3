package bucketlist

import (
	// "github.com/LinPr/lazys3/internal/tui/components/style"
	"fmt"

	"github.com/LinPr/lazys3/internal/tui/components/style"
	"github.com/charmbracelet/bubbles/v2/list"
	tea "github.com/charmbracelet/bubbletea/v2"
)

type Model struct {
	Title      string
	Option     *Option
	bucketlist list.Model
}

func NewModel() Model {

	items := make([]list.Item, 0)

	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.Styles = NewCustomItemStyles(true)

	bucketlist := list.New(items, delegate, 0, 0)
	bucketlist.Styles = CustomStyle(true)
	return Model{
		bucketlist: bucketlist,
	}
}

func (m *Model) Init() tea.Cmd {

	return nil
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {

	var cmd tea.Cmd

	m.bucketlist, cmd = m.bucketlist.Update(msg)

	m.SetTitle(fmt.Sprintf("S3 Buckets (%s)", m.Option.Profile))
	return m, cmd
}

func (m Model) View() string {
	w, _ := m.GetSize()
	return style.BucketListStyle.
		Width(w).
		Render(m.bucketlist.View())
}

func (m *Model) SetTitle(title string) {
	m.Title = title
}

func (m *Model) SetOption(opt *Option) {
	m.Option = opt
}

func (m *Model) SetBuckets(items []Bucket) {
	listItems := make([]list.Item, 0)
	for _, b := range items {
		listItems = append(listItems, b)
	}
	m.bucketlist.SetItems(listItems)
}

func (m *Model) GetSelectedBucket() *Bucket {
	if item, ok := m.bucketlist.SelectedItem().(Bucket); ok {
		return &item
	}
	return nil
}

func (m *Model) SetSize(width, height int) {
	m.bucketlist.SetSize(width, height)
}

func (m *Model) GetSize() (width, height int) {
	return m.bucketlist.Width(), m.bucketlist.Height()
}
