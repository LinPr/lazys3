package bucketlist

import (
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/LinPr/lazys3/internal/tui/components/style"
)

func NewCustomItemStyles(isDark bool) (s list.DefaultItemStyles) {
	lightDark := lipgloss.LightDark(isDark)

	s.NormalTitle = lipgloss.NewStyle().
		Foreground(lightDark(lipgloss.Color("#1a1a1a"), lipgloss.Color("#dddddd"))).
		Padding(0, 0, 0, 2) //nolint:mnd
		// Border(lipgloss.NormalBorder(), true, true, true, true)

	s.NormalDesc = s.NormalTitle.
		Foreground(lightDark(lipgloss.Color("#A49FA5"), lipgloss.Color("#777777")))

	s.SelectedTitle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lightDark(lipgloss.Color("#f3ec38ff"), lipgloss.Color("#f3ec38ff"))).
		Foreground(lightDark(lipgloss.Color("#f3ec38ff"), lipgloss.Color("#f3ec38ff"))).
		Padding(0, 0, 0, 1)

	s.SelectedDesc = s.SelectedTitle.
		Foreground(lightDark(lipgloss.Color("#F793FF"), lipgloss.Color("#AD58B4")))

	// Theme override for the highlighted row (style.Apply runs before the
	// components are constructed).
	if c := style.SelectedItemFg; c != nil {
		s.SelectedTitle = s.SelectedTitle.Foreground(c).BorderForeground(c)
	}

	s.DimmedTitle = lipgloss.NewStyle().
		Foreground(lightDark(lipgloss.Color("#A49FA5"), lipgloss.Color("#777777"))).
		Padding(0, 0, 0, 2) //nolint:mnd

	s.DimmedDesc = s.DimmedTitle.
		Foreground(lightDark(lipgloss.Color("#C2B8C2"), lipgloss.Color("#4D4D4D")))

	s.FilterMatch = lipgloss.NewStyle().Underline(true)

	return s
}

const (
	bullet   = "•"
	ellipsis = "…"
)

func CustomStyle(isDark bool) (s list.Styles) {
	lightDark := lipgloss.LightDark(isDark)

	verySubduedColor := lightDark(lipgloss.Color("#DDDADA"), lipgloss.Color("#3C3C3C"))
	subduedColor := lightDark(lipgloss.Color("#9B9B9B"), lipgloss.Color("#5C5C5C"))

	s.TitleBar = lipgloss.NewStyle().
		Padding(0, 0, 1, 2) //nolint:mnd

	s.Title = lipgloss.NewStyle().
		Background(lipgloss.Color("62")).
		Foreground(lipgloss.Color("230")).
		Padding(0, 1)

	s.Spinner = lipgloss.NewStyle().
		Foreground(lightDark(lipgloss.Color("#8E8E8E"), lipgloss.Color("#747373")))

	prompt := lipgloss.NewStyle().
		Foreground(lightDark(lipgloss.Color("#04B575"), lipgloss.Color("#ECFD65")))
	s.Filter = textinput.DefaultStyles(isDark)
	s.Filter.Cursor.Color = lightDark(lipgloss.Color("#EE6FF8"), lipgloss.Color("#EE6FF8"))
	s.Filter.Blurred.Prompt = prompt
	s.Filter.Focused.Prompt = prompt

	s.DefaultFilterCharacterMatch = lipgloss.NewStyle().Underline(true)

	s.StatusBar = lipgloss.NewStyle().
		Foreground(lightDark(lipgloss.Color("#A49FA5"), lipgloss.Color("#777777"))).
		Padding(0, 0, 1, 2) //nolint:mnd

	s.StatusEmpty = lipgloss.NewStyle().Foreground(subduedColor)

	s.StatusBarActiveFilter = lipgloss.NewStyle().
		Foreground(lightDark(lipgloss.Color("#1a1a1a"), lipgloss.Color("#dddddd")))

	s.StatusBarFilterCount = lipgloss.NewStyle().Foreground(verySubduedColor)

	s.NoItems = lipgloss.NewStyle().
		Foreground(lightDark(lipgloss.Color("#909090"), lipgloss.Color("#626262")))

	s.ArabicPagination = lipgloss.NewStyle().Foreground(subduedColor)

	s.PaginationStyle = lipgloss.NewStyle().PaddingLeft(2) //nolint:mnd

	s.HelpStyle = lipgloss.NewStyle().Padding(1, 0, 0, 2) //nolint:mnd

	s.ActivePaginationDot = lipgloss.NewStyle().
		Foreground(lightDark(lipgloss.Color("#847A85"), lipgloss.Color("#979797"))).
		SetString(bullet)

	s.InactivePaginationDot = lipgloss.NewStyle().
		Foreground(verySubduedColor).
		SetString(bullet)

	s.DividerDot = lipgloss.NewStyle().
		Foreground(verySubduedColor).
		SetString(" " + bullet + " ")

	return s
}
