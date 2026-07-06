package style

import (
	"testing"

	"github.com/LinPr/lazys3/internal/config"
	"github.com/charmbracelet/lipgloss/v2"
)

// saveVars snapshots every var Apply mutates and restores them on cleanup
// so other tests in the package keep seeing the defaults.
func saveVars(t *testing.T) {
	t.Helper()
	focused, unfocused := FocusedBorderColor, UnfocusedBorderColor
	title, statusErr := TitleStyle, StatusErrorStyle
	selected := SelectedItemFg
	profile, bucket, object, local := ProfileListStyle, BucketListStyle, ObjectListStyle, LocalListStyle
	t.Cleanup(func() {
		FocusedBorderColor, UnfocusedBorderColor = focused, unfocused
		TitleStyle, StatusErrorStyle = title, statusErr
		SelectedItemFg = selected
		ProfileListStyle, BucketListStyle, ObjectListStyle, LocalListStyle = profile, bucket, object, local
	})
}

func TestApplyOverridesVars(t *testing.T) {
	saveVars(t)
	Apply(config.Theme{
		FocusedBorder:   "#ff0000",
		UnfocusedBorder: "#00ff00",
		TitleFg:         "#111111",
		TitleBg:         "#222222",
		StatusErrorFg:   "#333333",
		SelectedFg:      "#444444",
	})
	if FocusedBorderColor != lipgloss.Color("#ff0000") {
		t.Errorf("FocusedBorderColor = %v", FocusedBorderColor)
	}
	if UnfocusedBorderColor != lipgloss.Color("#00ff00") {
		t.Errorf("UnfocusedBorderColor = %v", UnfocusedBorderColor)
	}
	if got := TitleStyle.GetForeground(); got != lipgloss.Color("#111111") {
		t.Errorf("TitleStyle fg = %v", got)
	}
	if got := TitleStyle.GetBackground(); got != lipgloss.Color("#222222") {
		t.Errorf("TitleStyle bg = %v", got)
	}
	if got := StatusErrorStyle.GetForeground(); got != lipgloss.Color("#333333") {
		t.Errorf("StatusErrorStyle fg = %v", got)
	}
	if SelectedItemFg != lipgloss.Color("#444444") {
		t.Errorf("SelectedItemFg = %v", SelectedItemFg)
	}
	// The single-pane list boxes follow the focused border override.
	for name, s := range map[string]lipgloss.Style{
		"ProfileListStyle": ProfileListStyle,
		"BucketListStyle":  BucketListStyle,
		"ObjectListStyle":  ObjectListStyle,
		"LocalListStyle":   LocalListStyle,
	} {
		if got := s.GetBorderTopForeground(); got != lipgloss.Color("#ff0000") {
			t.Errorf("%s border fg = %v, want #ff0000", name, got)
		}
	}
}

func TestApplyZeroThemeKeepsDefaults(t *testing.T) {
	saveVars(t)
	focused, unfocused := FocusedBorderColor, UnfocusedBorderColor
	title := TitleStyle
	Apply(config.Theme{})
	if FocusedBorderColor != focused || UnfocusedBorderColor != unfocused {
		t.Error("zero theme must not change the border colors")
	}
	if TitleStyle.GetForeground() != title.GetForeground() {
		t.Error("zero theme must not change TitleStyle")
	}
	if SelectedItemFg != nil {
		t.Errorf("zero theme must keep SelectedItemFg nil, got %v", SelectedItemFg)
	}
}
