package tui

import (
	"log"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"

	"github.com/LinPr/lazys3/internal/tui/components/bucketlist"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/components/preview"
	"github.com/LinPr/lazys3/internal/tui/components/profilelist"
	"github.com/LinPr/lazys3/internal/tui/components/style"
	"github.com/LinPr/lazys3/internal/tui/state"
)

type size struct {
	width  int
	height int
}

type Model struct {
	state           state.State
	profileList     profilelist.Model
	bucketList      bucketlist.Model
	objectlist      objectlist.Model
	previewPannel   preview.PreviewModel
	selectedProfile string
	selectedBucket  string
	selectedObject  string
	size
}

func NewLazyS3Model() Model {
	return Model{
		state:         state.ActiveProfileList,
		profileList:   profilelist.NewModel(),
		bucketList:    bucketlist.NewModel(),
		objectlist:    objectlist.NewModel(),
		previewPannel: preview.NewPreviewModel(),
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	// m.state = state.ActiveProfileList
	// m.state = state.Unknown
	return tea.Batch(
		m.profileList.Init(),
		m.bucketList.Init(),
		m.objectlist.Init(),
	)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.initComponentsSize(msg)
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		// case "esc":
		// 	m.state = state.ActiveProfileList

		case "ctrl+c", "q":
			return m, tea.Quit

		case "enter", "right":
			log.Println("key string:", msg.String())

			cmds = append(cmds, m.handleForward(msg))

		case "backspace", "left":
			log.Println("key string:", msg.String())
			cmds = append(cmds, m.handleBackward())

		case "p":
			m.handlePreviewToggle()

		default:
			log.Println("key string:", msg.String())
		}
	case bucketlist.FetchBucketListResultMsg:
		buckets, err := msg.Buckets, msg.Err
		if err != nil {
			log.Println("Error fetching bucket list:", err)
			break
		}
		m.bucketList.SetBuckets(buckets)
	case objectlist.FetchObjectListResultMsg:
		objects, err := msg.Objects, msg.Err
		if err != nil {
			m.objectlist.SetObjects([]objectlist.Object{})
			log.Println("Error fetching object list:", err)
			break
		}

		log.Printf("------- objects: %#v\n", objects)
		m.objectlist.SetObjects(objects)
	}

	// dispatch message to the active component
	switch m.state {
	case state.ActiveProfileList:
		newProfileListModel, cmd := m.profileList.Update(msg)
		m.profileList = newProfileListModel
		m.previewPannel.SetContent(m.profileList.GetSelectedProfile())
		cmds = append(cmds, cmd)

	case state.ActiveBucketList:
		newBucketListModel, cmd := m.bucketList.Update(msg)
		m.bucketList = newBucketListModel
		m.previewPannel.SetContent(m.bucketList.GetSelectedBucket())
		cmds = append(cmds, cmd)

	case state.ActiveObjectList:
		newObjectListModel, cmd := m.objectlist.Update(msg)
		m.objectlist = newObjectListModel
		m.previewPannel.SetContent(m.objectlist.GetSelectedObject())
		cmds = append(cmds, cmd)

	default:
		log.Println("Unknown state:", m.state)
		// return m, nil
	}

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	switch m.state {
	case state.ActiveProfileList:
		return lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.profileList.View(),
			m.previewPannel.View(),
		)

	case state.ActiveBucketList:
		return lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.bucketList.View(),
			m.previewPannel.View(),
		)

	case state.ActiveObjectList:
		return lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.objectlist.View(),
			m.previewPannel.View(),
		)

	default:
		return style.ErrorStyle.Render("Unknown component")
	}
}
