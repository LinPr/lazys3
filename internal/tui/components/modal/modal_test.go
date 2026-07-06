package modal

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea/v2"
)

func press(t *testing.T, m Model, k tea.Key) (Model, tea.Cmd) {
	t.Helper()
	return m.Update(tea.KeyPressMsg(k))
}

func typeString(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m, _ = press(t, m, tea.Key{Code: r, Text: string(r)})
	}
	return m
}

func showWithCapture(m *Model, title, placeholder string, got *string) {
	m.Show(title, placeholder, func(v string) tea.Cmd {
		*got = v
		return nil
	})
}

func TestEnterWithoutTypingSubmitsPlaceholderDefault(t *testing.T) {
	m := NewModel()
	got := "unset"
	showWithCapture(&m, "Download to", "file.txt", &got)

	m, _ = press(t, m, tea.Key{Code: tea.KeyEnter})

	if got != "file.txt" {
		t.Fatalf("onConfirm got %q, want placeholder default %q", got, "file.txt")
	}
	if m.IsVisible() {
		t.Fatal("modal should be hidden after confirm")
	}
}

func TestEnterWithTypedValueOverridesPlaceholder(t *testing.T) {
	m := NewModel()
	got := "unset"
	showWithCapture(&m, "Download to", "file.txt", &got)

	m = typeString(t, m, "other.txt")
	m, _ = press(t, m, tea.Key{Code: tea.KeyEnter})

	if got != "other.txt" {
		t.Fatalf("onConfirm got %q, want typed value %q", got, "other.txt")
	}
}

func TestEnterWithEmptyPlaceholderSubmitsEmpty(t *testing.T) {
	m := NewModel()
	got := "unset"
	showWithCapture(&m, "Sync destination", "", &got)

	m, _ = press(t, m, tea.Key{Code: tea.KeyEnter})

	if got != "" {
		t.Fatalf("onConfirm got %q, want empty string", got)
	}
}
