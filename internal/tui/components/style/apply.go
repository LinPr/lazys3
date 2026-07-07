package style

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/LinPr/lazys3/internal/config"
)

// SelectedItemFg, when non-nil, overrides the highlighted-row foreground
// (and its left border) in the list delegates. It is read at delegate
// construction time, so Apply must run before the components are built.
var SelectedItemFg color.Color

// Apply mutates the package-level style vars from the loaded (already
// validated) theme. It must run once at startup, BEFORE any component is
// constructed: delegates and custom item styles copy from these vars at
// construction, while the pane Views re-read the border colors on every
// render.
func Apply(t config.Theme) {
	if c := t.FocusedBorder; c != "" {
		FocusedBorderColor = lipgloss.Color(c)
		ProfileListStyle = ProfileListStyle.BorderForeground(FocusedBorderColor)
		BucketListStyle = BucketListStyle.BorderForeground(FocusedBorderColor)
		ObjectListStyle = ObjectListStyle.BorderForeground(FocusedBorderColor)
		LocalListStyle = LocalListStyle.BorderForeground(FocusedBorderColor)
	}
	if c := t.UnfocusedBorder; c != "" {
		UnfocusedBorderColor = lipgloss.Color(c)
	}
	if c := t.TitleFg; c != "" {
		TitleStyle = TitleStyle.Foreground(lipgloss.Color(c))
	}
	if c := t.TitleBg; c != "" {
		TitleStyle = TitleStyle.Background(lipgloss.Color(c))
	}
	if c := t.StatusErrorFg; c != "" {
		StatusErrorStyle = StatusErrorStyle.Foreground(lipgloss.Color(c))
	}
	if c := t.SelectedFg; c != "" {
		SelectedItemFg = lipgloss.Color(c)
	}
}
