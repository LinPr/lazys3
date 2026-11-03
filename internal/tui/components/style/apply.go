package style

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/LinPr/lazys3/internal/config"
)

// SelectedItemFg overrides the highlighted-row foreground (and its left
// border) in the list delegates when non-nil. It is read at delegate
// construction time, so Apply must run before the components are built.
var SelectedItemFg color.Color

// Apply installs the built-in palette named by theme, then applies any
// per-colour overrides from config.yaml on top. It must run once at
// startup, BEFORE any component is constructed: delegates and custom item
// styles copy from these vars at construction, while the pane Views re-read
// the border colours on every render.
func Apply(theme string, t config.Theme) {
	switch theme {
	case "light":
		ApplyLight()
	default:
		ApplyDark()
	}

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
