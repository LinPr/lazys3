package preview

import (
	"log"
	"reflect"

	"github.com/charmbracelet/bubbles/v2/list"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
)

type PreviewItem interface {
	list.Item
	GetPreviewContent() string
	// GetPreviewTitle() string
}

// PreviewModel 独立的预览面板模型
type PreviewModel struct {
	title   string
	content string
	width   int
	height  int
	visible bool
}

// NewPreviewModel 创建新的预览模型
func NewPreviewModel() PreviewModel {
	return PreviewModel{
		title:   "Preview",
		content: "No item selected",
		visible: false,
	}
}

// SetContent 设置预览内容
func (pm *PreviewModel) SetContent(item PreviewItem) {
	// TODO: content 可能是个值
	log.Printf("reflect.TypeOf(item): %v\n", reflect.TypeOf(item))
	if item != nil {
		pm.content = item.GetPreviewContent()
	} else {
		pm.title = "Preview"
		pm.content = "No item selected"
	}
}

// SetDimensions 设置预览面板尺寸
func (pm *PreviewModel) SetSize(width, height int) {
	pm.width = width
	pm.height = height
}

// Show 显示预览面板
func (pm *PreviewModel) Show() {
	pm.visible = true
}

// Hide 隐藏预览面板
func (pm *PreviewModel) Hide() {
	pm.visible = false
}

// Toggle 切换预览面板显示状态
func (pm *PreviewModel) Toggle() {
	pm.visible = !pm.visible
}

// IsVisible 返回预览面板是否可见
func (pm *PreviewModel) IsVisible() bool {
	return pm.visible
}

func (pm PreviewModel) Init() tea.Cmd {
	return nil
}

// Update 更新预览面板（预览面板通常是静态的，不需要处理输入）
func (pm PreviewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return pm, nil
}

// View 渲染预览面板
func (pm PreviewModel) View() string {
	if !pm.visible {
		return ""
	}

	// 标题样式
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("25")).
		Bold(true).
		Border(lipgloss.NormalBorder(), true, true, true, true).
		Padding(0, 0, 1, 0)

	// 组合内容
	content := titleStyle.Render(pm.title) + "\n\n" + pm.content

	// 预览面板样式
	previewStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("6"))
		// Padding(1, 2).
		// Width(pm.width - 4).
		// Height(pm.height - 4).
		// Align(lipgloss.Right)
		// MarginLeft(lipgloss.Width(" ") * 2)
		// 使用AbsolutePosition可以让面板固定在右侧（需要lipgloss v0.9.0+）
		// 你可以根据终端宽度动态计算left偏移
		// 这里假设屏幕宽度为80
		// .AbsolutePosition(80-pm.width, 0)
		// 如果没有AbsolutePosition，可以用MarginLeft模拟

	return previewStyle.Width(pm.width).
		Height(pm.height - 5).
		MaxHeight(pm.height - 5).
		Render(content)
}
