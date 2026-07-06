package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LinPr/lazys3/internal/tui/components/modal"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
)

func presignDone(url string) objectlist.PresignDoneMsg {
	return objectlist.PresignDoneMsg{
		Bucket: "bkt",
		Key:    "dir/file.txt",
		URL:    url,
		Expiry: time.Hour,
	}
}

func updateModel(t *testing.T, m Model, msg any) Model {
	t.Helper()
	nm, _ := m.Update(msg)
	out, ok := nm.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want tui.Model", nm)
	}
	return out
}

// TestPresignDoneOpensConfirmModal pins the happy path: no overlay open, so
// the URL lands in a confirm modal, with a plain-HTTP warning only for
// http:// links.
func TestPresignDoneOpensConfirmModal(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, presignDone("https://bkt.example.com/dir/file.txt?X-Amz-Expires=3600"))
	if !m.modal.IsVisible() {
		t.Fatal("modal not visible after PresignDoneMsg")
	}
	if m.modal.Title() != "Presigned URL" {
		t.Fatalf("modal title = %q, want Presigned URL", m.modal.Title())
	}
	if !strings.Contains(m.modal.Body(), "https://bkt.example.com") {
		t.Fatalf("modal body missing URL: %q", m.modal.Body())
	}
	if strings.Contains(m.modal.Body(), "plain-HTTP") {
		t.Fatalf("https URL must not carry the plain-HTTP warning: %q", m.modal.Body())
	}

	m2 := NewLazyS3Model()
	m2 = updateModel(t, m2, presignDone("http://bkt.example.com/dir/file.txt?X-Amz-Expires=3600"))
	if !strings.Contains(m2.modal.Body(), "plain-HTTP") {
		t.Fatalf("http URL must carry the plain-HTTP warning: %q", m2.modal.Body())
	}
}

// TestPresignDoneDoesNotClobberOpenModal pins that an async presign result
// never replaces a live modal (e.g. a download prompt or a pending delete
// confirm): the URL falls back to a status-bar note instead.
func TestPresignDoneDoesNotClobberOpenModal(t *testing.T) {
	m := NewLazyS3Model()
	m.modal.Show("Download to", "/tmp", nil)

	m = updateModel(t, m, presignDone("https://bkt.example.com/f?sig"))
	if m.modal.Title() != "Download to" {
		t.Fatalf("live modal replaced: title = %q, want Download to", m.modal.Title())
	}
	if m.modal.Mode() != modal.ModeInput {
		t.Fatalf("live modal mode flipped to %v, want ModeInput", m.modal.Mode())
	}
	if !strings.Contains(m.statusBar.Info(), "copied to clipboard") {
		t.Fatalf("status bar info = %q, want clipboard note", m.statusBar.Info())
	}
}

// TestPresignDoneSkipsModalBehindHelp pins that the confirm modal is never
// opened invisibly behind the help overlay (where it would swallow keys).
func TestPresignDoneSkipsModalBehindHelp(t *testing.T) {
	m := NewLazyS3Model()
	m.help.Show()

	m = updateModel(t, m, presignDone("https://bkt.example.com/f?sig"))
	if m.modal.IsVisible() {
		t.Fatal("modal opened behind the help overlay")
	}
	if !strings.Contains(m.statusBar.Info(), "copied to clipboard") {
		t.Fatalf("status bar info = %q, want clipboard note", m.statusBar.Info())
	}
}

// TestPresignDoneErrDoesNotOpenModal pins the failure path: errors surface
// via the status bar, never a modal.
func TestPresignDoneErrDoesNotOpenModal(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, objectlist.PresignDoneMsg{Key: "f", Err: errors.New("boom")})
	if m.modal.IsVisible() {
		t.Fatal("modal visible after failed presign")
	}
}
