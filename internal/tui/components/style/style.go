// Package style holds shared lipgloss styles for the TUI lists and panels.
package style

import (
	"charm.land/lipgloss/v2"
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

	// listTitleUnfocusedFg dims the unfocused pane's title text in
	// dual-pane mode so the focused pane reads at a glance.
	listTitleUnfocusedFg = lipgloss.Color("#999999")

	StatusMessageStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#04B575"))

	// StatusErrorStyle renders the status bar's error chip.
	StatusErrorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#ffffff")).
				Background(lipgloss.Color("#cc0000")).
				Padding(0, 1)
)

// ListTitleStyle is the title-bar chip shared by all four lists (profiles,
// buckets, objects, local), matching the status bar's profile chip so the
// theme keys title_fg/title_bg restyle both (TitleStyle is read live, after
// Apply). The unfocused dual-pane title keeps the background but dims the
// text.
func ListTitleStyle(focused bool) lipgloss.Style {
	if focused {
		return TitleStyle
	}
	return TitleStyle.Foreground(listTitleUnfocusedFg)
}
