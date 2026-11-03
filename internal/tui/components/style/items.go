package style

import (
	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
)

// ItemStyles returns the list item styles shared by every pane, derived
// from the active palette. Using bubbles' own NewDefaultItemStyles would
// paint the cursor row in its built-in magenta/cyan, ignoring the theme.
func ItemStyles() (s list.DefaultItemStyles) {
	s.NormalTitle = lipgloss.NewStyle().
		Foreground(TextFg).
		Padding(0, 0, 0, 2) //nolint:mnd
		// Border(lipgloss.NormalBorder(), true, true, true, true)

	s.NormalDesc = s.NormalTitle.
		Foreground(DimFg)

	// Cursor-row highlight. NOTE: lipgloss.Color only parses #RGB/#RRGGBB —
	// the previous "#f3ec38ff" (8-digit hex) silently became NoColor, which
	// is how the cursor row lost its highlight.
	s.SelectedTitle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(CursorHighlightFg).
		Foreground(CursorHighlightFg).
		Padding(0, 0, 0, 1)

	s.SelectedDesc = s.SelectedTitle.
		Foreground(WarningFg)

	// Theme override for the highlighted row (style.Apply runs before the
	// components are constructed).
	if c := SelectedItemFg; c != nil {
		s.SelectedTitle = s.SelectedTitle.Foreground(c).BorderForeground(c)
	}

	s.DimmedTitle = lipgloss.NewStyle().
		Foreground(MutedFg).
		Padding(0, 0, 0, 2) //nolint:mnd

	s.DimmedDesc = s.DimmedTitle.
		Foreground(MutedFg)

	s.FilterMatch = lipgloss.NewStyle().Underline(true)

	return s
}
