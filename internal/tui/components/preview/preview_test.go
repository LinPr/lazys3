package preview

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// showFile writes content to a temp file, opens the overlay on it and
// applies the fetch result.
func showFile(t *testing.T, m *Model, content []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := m.ShowLocal(path)
	if cmd == nil {
		t.Fatal("ShowLocal returned no fetch cmd")
	}
	nm, _ := m.Update(cmd())
	*m = nm
}

// stripped returns the overlay render with ANSI sequences removed.
func stripped(m Model) string { return ansi.Strip(m.View()) }

func TestTextRenderWrapsCJKAt80Cols(t *testing.T) {
	m := NewModel()
	// Box width min(100, 90-6)=84, inner 80: the wrap budget under test.
	m.SetSize(90, 30)
	line := strings.Repeat("汉字宽度", 30) // 240 display cells on one line
	showFile(t, &m, []byte(line+"\nascii tail\n"))

	out := stripped(m)
	if !strings.Contains(out, "ascii tail") {
		t.Fatalf("content missing from render:\n%s", out)
	}
	for i, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w > 84 {
			t.Fatalf("line %d is %d cells wide, want <= 84 (box width):\n%s", i, w, l)
		}
	}
	// The 240-cell CJK line must have been hard-wrapped over several lines
	// without splitting a wide rune (no width overflow above).
	if lines := m.contentLines(); len(lines) < 4 {
		t.Fatalf("CJK line wrapped into %d lines, want >= 4", len(lines))
	}
	if !strings.Contains(out, "lines ·") || !strings.Contains(out, "p/esc close") {
		t.Fatalf("footer missing:\n%s", out)
	}
}

func TestBinaryFileShowsNoteInsteadOfContent(t *testing.T) {
	m := NewModel()
	m.SetSize(90, 30)
	showFile(t, &m, []byte("ELF\x00\x01\x02garbage"))

	out := stripped(m)
	if !strings.Contains(out, "(binary file — 13 bytes, no preview)") {
		t.Fatalf("binary note missing:\n%s", out)
	}
	if strings.Contains(out, "garbage") {
		t.Fatalf("binary content leaked into the render:\n%s", out)
	}
}

func TestMostlyInvalidUTF8CountsAsBinary(t *testing.T) {
	m := NewModel()
	m.SetSize(90, 30)
	// >10% invalid UTF-8, no NUL.
	showFile(t, &m, bytes.Repeat([]byte{0xff, 'a', 0xfe, 'b'}, 8))
	if !strings.Contains(stripped(m), "no preview") {
		t.Fatalf("invalid-UTF-8 sample not detected as binary:\n%s", stripped(m))
	}
}

func TestEmptyFileShowsEmptyNote(t *testing.T) {
	m := NewModel()
	m.SetSize(90, 30)
	showFile(t, &m, nil)
	if !strings.Contains(stripped(m), "(empty file)") {
		t.Fatalf("empty note missing:\n%s", stripped(m))
	}
}

func TestTruncatedFileShowsMarkerAndFooterSizes(t *testing.T) {
	m := NewModel()
	m.SetSize(90, 30)
	content := bytes.Repeat([]byte("0123456789abcde\n"), MaxBytes/16+4) // > 256 KiB
	showFile(t, &m, content)

	if !m.truncated() {
		t.Fatal("sample not marked truncated")
	}
	lines := m.contentLines()
	if !strings.Contains(ansi.Strip(lines[len(lines)-1]), "── truncated at 256 KiB ──") {
		t.Fatalf("truncation marker missing from the tail line: %q", lines[len(lines)-1])
	}
	// Jump to the bottom so the marker is inside the visible window.
	m.HandleKey("G")
	out := stripped(m)
	if !strings.Contains(out, "truncated at 256 KiB") {
		t.Fatalf("truncation marker not rendered at the bottom:\n%s", out)
	}
	if !strings.Contains(out, "256.0K shown of") {
		t.Fatalf("footer does not show the shown/total sizes:\n%s", out)
	}
}

func TestScrollClamping(t *testing.T) {
	m := NewModel()
	m.SetSize(90, 20)
	showFile(t, &m, []byte(strings.Repeat("line\n", 100)))

	total := len(m.contentLines())
	page := m.pageSize()
	m.HandleKey("G")
	if want := total - page; m.offset != want {
		t.Fatalf("offset after G = %d, want %d", m.offset, want)
	}
	m.HandleKey("j")
	if want := total - page; m.offset != want {
		t.Fatalf("offset after j at bottom = %d, want clamped %d", m.offset, want)
	}
	m.HandleKey("g")
	if m.offset != 0 {
		t.Fatalf("offset after g = %d, want 0", m.offset)
	}
	m.HandleKey("k")
	if m.offset != 0 {
		t.Fatalf("offset after k at top = %d, want clamped 0", m.offset)
	}
	m.HandleKey("pgdown")
	if m.offset != page {
		t.Fatalf("offset after pgdown = %d, want %d", m.offset, page)
	}
	m.HandleKey("pgup")
	if m.offset != 0 {
		t.Fatalf("offset after pgup = %d, want 0", m.offset)
	}
}

func TestStaleContentMsgDropped(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.txt")
	cur := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(old, []byte("OLD CONTENT"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cur, []byte("NEW CONTENT"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewModel()
	m.SetSize(90, 30)
	staleCmd := m.ShowLocal(old)
	freshCmd := m.ShowLocal(cur)

	// The stale fetch completes last; its content must be dropped.
	nm, _ := m.Update(staleCmd())
	m = nm
	if !m.loading || strings.Contains(m.text, "OLD") {
		t.Fatalf("stale msg applied: loading=%v text=%q", m.loading, m.text)
	}
	nm, _ = m.Update(freshCmd())
	m = nm
	if m.loading || !strings.Contains(m.text, "NEW CONTENT") {
		t.Fatalf("fresh msg not applied: loading=%v text=%q", m.loading, m.text)
	}
}

func TestHideDropsInFlightFetch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewModel()
	m.SetSize(90, 30)
	cmd := m.ShowLocal(path)
	m.Hide()
	nm, _ := m.Update(cmd())
	m = nm
	if m.IsVisible() {
		t.Fatal("a late fetch result re-opened the overlay")
	}
	if m.text != "" {
		t.Fatalf("a late fetch result populated the hidden overlay: %q", m.text)
	}
}

func TestShowRemoteSkipsFetchForEmptyObject(t *testing.T) {
	m := NewModel()
	m.SetSize(90, 30)
	cmd := m.ShowRemote(&PreviewRequest{Bucket: "b", Key: "empty.txt", Size: 0})
	if cmd == nil {
		t.Fatal("ShowRemote returned no cmd")
	}
	// The cmd must complete without touching the network (a ranged GET on
	// a zero-byte object would 416 anyway).
	nm, _ := m.Update(cmd())
	m = nm
	if !strings.Contains(stripped(m), "(empty file)") {
		t.Fatalf("empty object did not render the empty note:\n%s", stripped(m))
	}
	if !strings.Contains(stripped(m), "s3://b/empty.txt") {
		t.Fatalf("title missing:\n%s", stripped(m))
	}
}

func TestTotalFromContentRange(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"bytes 0-65535/123456", 123456},
		{"bytes 0-9/10", 10},
		{"bytes 0-9/*", -1},
		{"", -1},
	}
	for _, c := range cases {
		if got := totalFromContentRange(c.in); got != c.want {
			t.Errorf("totalFromContentRange(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
