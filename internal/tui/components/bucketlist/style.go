package bucketlist

import (
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/LinPr/lazys3/internal/tui/components/style"
)

const (
	bullet   = "•"
	ellipsis = "…"
)

func CustomStyle(isDark bool) (s list.Styles) {
	verySubduedColor := style.MutedFg
	subduedColor := style.DimFg

	s.TitleBar = lipgloss.NewStyle().
		Padding(0, 0, 1, 2) //nolint:mnd

	// The title chip is shared with the other lists (and the status bar's
	// profile chip) so the theme keys title_fg/title_bg restyle them all.
	s.Title = style.ListTitleStyle(true)

	s.Spinner = lipgloss.NewStyle().
		Foreground(style.MutedFg)

	prompt := lipgloss.NewStyle().
		Foreground(style.SuccessFg)
	s.Filter = textinput.DefaultStyles(isDark)
	s.Filter.Cursor.Color = style.AccentFg
	s.Filter.Blurred.Prompt = prompt
	s.Filter.Focused.Prompt = prompt

	s.DefaultFilterCharacterMatch = lipgloss.NewStyle().Underline(true)

	s.StatusBar = lipgloss.NewStyle().
		Foreground(style.DimFg).
		Padding(0, 0, 1, 2) //nolint:mnd

	s.StatusEmpty = lipgloss.NewStyle().Foreground(subduedColor)

	s.StatusBarActiveFilter = lipgloss.NewStyle().
		Foreground(style.TextFg)

	s.StatusBarFilterCount = lipgloss.NewStyle().Foreground(verySubduedColor)

	s.NoItems = lipgloss.NewStyle().
		Foreground(style.MutedFg)

	s.ArabicPagination = lipgloss.NewStyle().Foreground(subduedColor)

	s.PaginationStyle = lipgloss.NewStyle().PaddingLeft(2) //nolint:mnd

	s.HelpStyle = lipgloss.NewStyle().Padding(1, 0, 0, 2) //nolint:mnd

	s.ActivePaginationDot = lipgloss.NewStyle().
		Foreground(style.DimFg).
		SetString(bullet)

	s.InactivePaginationDot = lipgloss.NewStyle().
		Foreground(verySubduedColor).
		SetString(bullet)

	s.DividerDot = lipgloss.NewStyle().
		Foreground(verySubduedColor).
		SetString(" " + bullet + " ")

	return s
}
