// Package style holds shared lipgloss styles for the TUI lists and panels.
//
// Colours come from one semantic palette (border, accent, success, …)
// rather than being written inline at each call site, so a theme switch
// cannot leave a stray hard-coded colour behind. Rounded borders are used
// throughout, matching the rest of the Charm tooling.
package style

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// palette is one complete set of semantic colours. Two variants ship
// (dark and light); ApplyDark/ApplyLight repoint every style at one.
type palette struct {
	bg        string
	bgAlt     string
	border    string // focused pane border
	borderDim string // unfocused pane border
	selected  string // selected row background
	text      string
	dim       string
	accent    string
	green     string
	red       string
	yellow    string
	cyan      string
	muted     string
}

// The dark palette is warm: amber/cream accents on a brown-tinted
// background rather than the usual blue-grey. High-chroma colours are
// avoided because a full-screen TUI shows them constantly.
var darkPalette = palette{
	bg:        "#1C1917", // warm near-black
	bgAlt:     "#262220",
	border:    "#D9A441", // amber frame
	borderDim: "#4A4238",
	selected:  "#3A332C",
	text:      "#EDE4D8", // warm off-white
	dim:       "#A89B8C",
	accent:    "#E8935A", // soft orange
	green:     "#9CC69B", // sage
	red:       "#D98A8A",
	yellow:    "#E5C07B", // cream
	cyan:      "#94C4C4", // muted teal
	muted:     "#7A6F63",
}

var lightPalette = palette{
	bg:        "#FFFBF5", // warm paper
	bgAlt:     "#F7EDE0", // cream
	border:    "#D9A441",
	borderDim: "#D8C9B6",
	selected:  "#F3E3CC",
	text:      "#3E3226",
	dim:       "#8A7A68",
	accent:    "#C2652A",
	green:     "#5E7C53",
	red:       "#A8443B",
	yellow:    "#B8873B",
	cyan:      "#4E7378",
	muted:     "#B3A697",
}

var (
	// FocusedBorderColor is the border colour of the pane that owns
	// list-navigation keys; UnfocusedBorderColor marks the other pane in
	// dual-pane mode.
	FocusedBorderColor   color.Color
	UnfocusedBorderColor color.Color

	// Semantic text colours, used by the status bar, titles and the list
	// delegates so they follow the palette instead of repeating hex codes.
	TextFg      color.Color
	DimFg       color.Color
	AccentFg    color.Color
	SuccessFg   color.Color
	ErrorFg     color.Color
	WarningFg   color.Color
	InfoFg      color.Color
	MutedFg     color.Color
	StatusBarBg color.Color
	SelectedBg  color.Color

	AppStyle = lipgloss.NewStyle()

	ErrorStyle lipgloss.Style

	ProfileListStyle lipgloss.Style
	BucketListStyle  lipgloss.Style
	ObjectListStyle  lipgloss.Style
	LocalListStyle   lipgloss.Style
	PreviewStyle     lipgloss.Style

	TitleStyle           lipgloss.Style
	StatusMessageStyle   lipgloss.Style
	StatusErrorStyle     lipgloss.Style
	listTitleUnfocusedFg color.Color

	// SelectedMarkFg colours rows toggled for multi-select.
	SelectedMarkFg color.Color
	// CursorHighlightFg is the cursor-row foreground (and its left-border
	// indicator) shared by the bucket and profile pickers.
	CursorHighlightFg color.Color
)

// newPaneStyle is the shared frame for the four list panes.
func newPaneStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(FocusedBorderColor)
}

// buildStyles derives every style from p, so the palette is the single
// source of truth for colours.
func buildStyles(p palette) {
	FocusedBorderColor = lipgloss.Color(p.border)
	UnfocusedBorderColor = lipgloss.Color(p.borderDim)

	TextFg = lipgloss.Color(p.text)
	DimFg = lipgloss.Color(p.dim)
	AccentFg = lipgloss.Color(p.accent)
	SuccessFg = lipgloss.Color(p.green)
	ErrorFg = lipgloss.Color(p.red)
	WarningFg = lipgloss.Color(p.yellow)
	InfoFg = lipgloss.Color(p.cyan)
	MutedFg = lipgloss.Color(p.muted)
	StatusBarBg = lipgloss.Color(p.bgAlt)
	SelectedBg = lipgloss.Color(p.selected)

	AppStyle = lipgloss.NewStyle()

	ErrorStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color(p.red)).
		Bold(true)

	ProfileListStyle = newPaneStyle()
	BucketListStyle = newPaneStyle()
	ObjectListStyle = newPaneStyle()
	LocalListStyle = newPaneStyle()

	PreviewStyle = lipgloss.NewStyle().
		Background(lipgloss.Color(p.bgAlt)).
		Foreground(lipgloss.Color(p.text)).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(p.cyan))

	TitleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(p.bg)).
		Background(lipgloss.Color(p.border)).
		Padding(0, 1)

	listTitleUnfocusedFg = lipgloss.Color(p.muted)

	StatusMessageStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(p.green))

	SelectedMarkFg = lipgloss.Color(p.green)
	CursorHighlightFg = lipgloss.Color(p.yellow)

	StatusErrorStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color(p.red)).
		Padding(0, 1)
}

// ApplyDark and ApplyLight install the built-in palettes. Apply lets
// config.yaml override individual colours on top of the result.
func ApplyDark()  { buildStyles(darkPalette) }
func ApplyLight() { buildStyles(lightPalette) }

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

func init() {
	// The dark palette is the default: it matches the README screenshots
	// and is the safer choice on the terminals people run lazys3 from.
	buildStyles(darkPalette)
}
