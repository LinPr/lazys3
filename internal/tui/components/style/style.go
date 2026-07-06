// Package style holds shared lipgloss styles for the TUI lists and panels.
package style

import (
	"github.com/charmbracelet/lipgloss/v2"
)

var (
	// FocusedBorderColor is the border color of the pane that owns
	// list-navigation keys; UnfocusedBorderColor marks the other pane in
	// dual-pane mode. Single-pane lists stay focused, keeping the
	// original border color.
	FocusedBorderColor   = lipgloss.Color("#20e71cff")
	UnfocusedBorderColor = lipgloss.Color("#555555")

	AppStyle = lipgloss.NewStyle()

	ErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("rgba(255, 0, 0, 1)")).
			Border(lipgloss.NormalBorder()).
			Bold(true)

	ProfileListStyle = lipgloss.NewStyle().
		// Padding(1, 2).
		// Margin(1, 2).
		// Width(30).
		// Background(lipgloss.Color("#000000ff")).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#20e71cff"))

		// Margin(1, 2)

	BucketListStyle = lipgloss.NewStyle().
		// Padding(1, 2).
		// Margin(1, 2).
		// Width(30).
		// Background(lipgloss.Color("#000000ff")).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#20e71cff"))

	ObjectListStyle = lipgloss.NewStyle().
		// Padding(1, 2).
		// Margin(1, 2).
		// Width(30).
		// Background(lipgloss.Color("#000000ff")).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#20e71cff"))

	LocalListStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(FocusedBorderColor)

	PreviewStyle = lipgloss.NewStyle().
		// Width(30).Height(8).
		Background(lipgloss.Color("#FF6B6B")).
		Foreground(lipgloss.Color("#FFFFFF")).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#FFFFFF"))
		// Padding(1, 2)

	TitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#e39f00ff")).
			Background(lipgloss.Color("#444745ff")).
			Padding(0, 1)

	StatusMessageStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#04B575"))
)
