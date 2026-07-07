package transferview

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/lazys3/internal/tui/components/style"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
)

func shownModel(w, h int) Model {
	m := NewModel()
	m.SetSize(w, h)
	m.Show()
	return m
}

// withNerdFont enables the unicode status glyphs for one test and restores
// the previous state (icons are opt-in; the default is the ASCII fallback).
func withNerdFont(t *testing.T) {
	t.Helper()
	prev := style.NerdFontEnabled()
	style.SetNerdFont(true)
	t.Cleanup(func() { style.SetNerdFont(prev) })
}

// TestDoneWithTotalRendersFullBarAnd100 pins the terminal-state fix: a done
// transfer with a known total renders a FULL bar and 100%, never the frozen
// last tick value carried on the row.
func TestDoneWithTotalRendersFullBarAnd100(t *testing.T) {
	m := shownModel(120, 24)
	rows := []transferpanel.Transfer{{
		ID:     "t1",
		Op:     transferpanel.OpDownload,
		Label:  "s3://b/k -> ./k",
		Status: transferpanel.StatusDone,
		Done:   37, // frozen pre-completion tick value
		Total:  100,
	}}
	view := ansi.Strip(m.View(rows))
	if !strings.Contains(view, "] 100%") {
		t.Fatalf("done row should render 100%%, got:\n%s", view)
	}
	if strings.Contains(view, "37%") {
		t.Fatalf("done row must not render the frozen tick percent, got:\n%s", view)
	}
	if !strings.Contains(view, "["+strings.Repeat("█", barWidth)+"]") {
		t.Fatalf("done row should render a full bar, got:\n%s", view)
	}
}

// TestDoneIndeterminateRendersNoBar pins that a done transfer with an
// unknown total renders the done marker without any (bouncing) bar.
func TestDoneIndeterminateRendersNoBar(t *testing.T) {
	withNerdFont(t)
	m := shownModel(120, 24)
	rows := []transferpanel.Transfer{{
		ID:     "t1",
		Op:     transferpanel.OpSync,
		Label:  "sync ./dir -> s3://b/p",
		Status: transferpanel.StatusDone,
		Done:   5,
		Total:  -1,
	}}
	view := ansi.Strip(m.View(rows))
	if !strings.Contains(view, "done") {
		t.Fatalf("row should render done, got:\n%s", view)
	}
	if strings.Contains(view, "█") {
		t.Fatalf("indeterminate done row must not render a bar, got:\n%s", view)
	}
	if !strings.Contains(view, "✓") {
		t.Fatalf("done row should carry the ✓ marker, got:\n%s", view)
	}
}

// TestFailedAndCanceledMarkers pins the failure/cancel rendering: markers,
// status words, and the error snippet on failed rows.
func TestFailedAndCanceledMarkers(t *testing.T) {
	withNerdFont(t)
	m := shownModel(120, 24)
	rows := []transferpanel.Transfer{
		{
			ID:     "t2",
			Op:     transferpanel.OpUpload,
			Label:  "./f -> s3://b/f",
			Status: transferpanel.StatusFailed,
			Err:    errors.New("access denied"),
		},
		{
			ID:     "t1",
			Op:     transferpanel.OpDownload,
			Label:  "s3://b/k -> ./k",
			Status: transferpanel.StatusCanceled,
		},
	}
	view := ansi.Strip(m.View(rows))
	if !strings.Contains(view, "✗") || !strings.Contains(view, "failed") {
		t.Fatalf("failed row should render ✗ failed, got:\n%s", view)
	}
	if !strings.Contains(view, "access denied") {
		t.Fatalf("failed row should render the error snippet, got:\n%s", view)
	}
	if !strings.Contains(view, "⊘") || !strings.Contains(view, "canceled") {
		t.Fatalf("canceled row should render ⊘ canceled, got:\n%s", view)
	}
}

// TestASCIIFallbackWithoutNerdFont pins the marker fallbacks when nerd_font
// is off: ok / x / - instead of ✓ / ✗ / ⊘.
func TestASCIIFallbackWithoutNerdFont(t *testing.T) {
	prev := style.NerdFontEnabled()
	style.SetNerdFont(false)
	t.Cleanup(func() { style.SetNerdFont(prev) })

	if got := statusGlyph(transferpanel.StatusDone); got != "ok" {
		t.Errorf("done glyph = %q, want ok", got)
	}
	if got := statusGlyph(transferpanel.StatusFailed); got != "x" {
		t.Errorf("failed glyph = %q, want x", got)
	}
	if got := statusGlyph(transferpanel.StatusCanceled); got != "-" {
		t.Errorf("canceled glyph = %q, want -", got)
	}

	style.SetNerdFont(true)
	if got := statusGlyph(transferpanel.StatusDone); got != "✓" {
		t.Errorf("nerd done glyph = %q, want ✓", got)
	}
}

// TestRunningRowKeepsLiveProgress pins that a running row renders the live
// (max-so-far) percent, not a terminal state.
func TestRunningRowKeepsLiveProgress(t *testing.T) {
	m := shownModel(120, 24)
	rows := []transferpanel.Transfer{{
		ID:     "t1",
		Op:     transferpanel.OpDownload,
		Label:  "s3://b/k -> ./k",
		Status: transferpanel.StatusRunning,
		Done:   25,
		Total:  100,
	}}
	view := ansi.Strip(m.View(rows))
	if !strings.Contains(view, "25%") {
		t.Fatalf("running row should render the live percent, got:\n%s", view)
	}
	if strings.Contains(view, "100%") {
		t.Fatalf("running row must not render a terminal state, got:\n%s", view)
	}
}

// TestIndeterminateBarAnimatesFromWallClock pins the sync-animation fix:
// the bounce position derives from wall-clock time, not the panel's tick
// frame (which never advances for Progress-less sync rows), so any 200ms
// repaint moves the bar.
func TestIndeterminateBarAnimatesFromWallClock(t *testing.T) {
	m := shownModel(120, 24)
	rows := []transferpanel.Transfer{{
		ID:     "t1",
		Op:     transferpanel.OpSync,
		Label:  "sync ./dir -> s3://b/p",
		Status: transferpanel.StatusRunning,
		Total:  -1,
	}}
	if view := ansi.Strip(m.View(rows)); !strings.Contains(view, "█") {
		t.Fatalf("running indeterminate row should render a block bar, got:\n%s", view)
	}
	// Adjacent frames always render different bounce positions...
	f := animFrame()
	if bar(0, -1, f) == bar(0, -1, f+1) {
		t.Fatal("adjacent frames render the same indeterminate bar")
	}
	// ...and the frame itself advances with time across a repaint interval.
	time.Sleep(210 * time.Millisecond)
	if animFrame() == f {
		t.Fatal("animFrame did not advance across a 200ms repaint interval")
	}
}

// TestScrollClamping pins the cursor clamping: it never leaves the row
// range, g/G jump to the ends, and paging stays in bounds.
func TestScrollClamping(t *testing.T) {
	m := shownModel(80, 10)
	const total = 30

	m.HandleKey("k", total)
	if m.Cursor() != 0 {
		t.Fatalf("k at the top moved the cursor to %d", m.Cursor())
	}
	m.HandleKey("G", total)
	if m.Cursor() != total-1 {
		t.Fatalf("G moved the cursor to %d, want %d", m.Cursor(), total-1)
	}
	m.HandleKey("j", total)
	if m.Cursor() != total-1 {
		t.Fatalf("j at the bottom moved the cursor to %d", m.Cursor())
	}
	m.HandleKey("g", total)
	if m.Cursor() != 0 {
		t.Fatalf("g moved the cursor to %d, want 0", m.Cursor())
	}
	m.HandleKey("pgdown", total)
	if m.Cursor() <= 0 || m.Cursor() >= total {
		t.Fatalf("pgdown moved the cursor out of range: %d", m.Cursor())
	}
	for i := 0; i < 20; i++ {
		m.HandleKey("pgup", total)
	}
	if m.Cursor() != 0 {
		t.Fatalf("pgup spam left the cursor at %d, want 0", m.Cursor())
	}
	// A shrunk listing re-clamps on the next key.
	m.HandleKey("G", total)
	m.HandleKey("j", 5)
	if m.Cursor() != 4 {
		t.Fatalf("cursor after shrink = %d, want 4", m.Cursor())
	}
}

// TestViewFits80Cols pins that every rendered line fits an 80-col canvas,
// even with long labels, notes and error snippets.
func TestViewFits80Cols(t *testing.T) {
	m := shownModel(80, 12)
	var rows []transferpanel.Transfer
	for i := 0; i < 8; i++ {
		rows = append(rows, transferpanel.Transfer{
			ID:     fmt.Sprintf("t%d", i),
			Op:     transferpanel.OpDownload,
			Label:  "s3://bucket/a/very/long/prefix/with/many/segments/object-name.bin -> /home/user/downloads/object-name.bin",
			Status: transferpanel.StatusRunning,
			Done:   int64(i * 10),
			Total:  100,
			Note:   "a fairly long note about the transfer state",
		})
	}
	rows[0].Status = transferpanel.StatusFailed
	rows[0].Err = errors.New("connection reset by peer while reading the response body")
	rows[1].Status = transferpanel.StatusDone

	view := ansi.Strip(m.View(rows))
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > 80 {
			t.Errorf("line %d is %d cells wide:\n%q", i, w, line)
		}
	}
	if len(lines) > 12 {
		t.Errorf("view is %d lines tall, want <= 12", len(lines))
	}
}

// TestTableColumnsAligned pins the table layout: a dim op/progress/file/note
// header, and every row's columns starting at the same cell offsets no
// matter how long the label or which state the row is in — at both 80 and
// 120 cols.
func TestTableColumnsAligned(t *testing.T) {
	for _, width := range []int{80, 120} {
		m := shownModel(width, 24)
		rows := []transferpanel.Transfer{
			{
				ID: "t3", Op: transferpanel.OpDownload,
				Label:  "s3://bucket/a/very/long/prefix/object-name-that-overflows-every-budget.bin -> ./x",
				Status: transferpanel.StatusRunning, Done: 25, Total: 100,
			},
			{
				ID: "t2", Op: transferpanel.OpSync, Label: "sync ./d -> s3://b/p",
				Status: transferpanel.StatusDone, Done: 5, Total: -1, Note: "3 file(s) done",
			},
			{
				ID: "t1", Op: transferpanel.OpUpload, Label: "./f -> s3://b/f",
				Status: transferpanel.StatusFailed, Err: errors.New("access denied"),
			},
		}
		view := ansi.Strip(m.View(rows))
		lines := strings.Split(view, "\n")

		var header string
		var body []string
		for _, l := range lines {
			switch {
			case strings.Contains(l, "op") && strings.Contains(l, "progress") && strings.Contains(l, "file"):
				header = l
			case strings.Contains(l, "download") || strings.Contains(l, "sync") || strings.Contains(l, "upload"):
				body = append(body, l)
			}
		}
		if header == "" {
			t.Fatalf("width %d: no op/progress/file header row:\n%s", width, view)
		}
		if len(body) != 3 {
			t.Fatalf("width %d: found %d transfer rows, want 3:\n%s", width, len(body), view)
		}
		// cellCol converts a substring match to its display-cell offset
		// (byte indexes drift on multi-byte runes like ▸ and │).
		cellCol := func(line, sub string) int {
			i := strings.Index(line, sub)
			if i < 0 {
				return -1
			}
			return ansi.StringWidth(line[:i])
		}
		// The op column must start at the same cell in the header and in
		// every row (fixed-width marker+glyph column ahead of it).
		opCol := cellCol(header, "op")
		for _, ops := range []string{"download", "sync", "upload"} {
			for _, l := range body {
				if c := cellCol(l, ops); c >= 0 && c != opCol {
					t.Errorf("width %d: %q starts at cell %d, header op at %d:\n%s", width, ops, c, opCol, view)
				}
			}
		}
		// The progress column: every bracket/status word aligns under the
		// header's "progress".
		progCol := cellCol(header, "progress")
		for _, l := range body {
			c := cellCol(l, "[")
			if c < 0 {
				c = cellCol(l, "failed")
			}
			if c < 0 {
				c = cellCol(l, "done ")
			}
			if c >= 0 && c != progCol {
				t.Errorf("width %d: progress cell at %d, header at %d:\n%q", width, c, progCol, l)
			}
		}
		// The failed row's error snippet lands in the note column.
		noteCol := cellCol(header, "note")
		if noteCol < 0 {
			t.Fatalf("width %d: header has no note column:\n%s", width, header)
		}
		found := false
		for _, l := range body {
			if c := cellCol(l, "access denied"); c >= 0 {
				found = true
				if c != noteCol {
					t.Errorf("width %d: error snippet at cell %d, note header at %d:\n%q", width, c, noteCol, l)
				}
			}
		}
		if !found {
			t.Errorf("width %d: failed row's error snippet missing:\n%s", width, view)
		}
	}
}

// TestEmptyAndFooter pins the empty state and the footer legend.
func TestEmptyAndFooter(t *testing.T) {
	m := shownModel(100, 20)
	view := ansi.Strip(m.View(nil))
	if !strings.Contains(view, "no transfers this session") {
		t.Fatalf("empty view missing the empty-state line:\n%s", view)
	}
	if !strings.Contains(view, "x cancel highlighted") || !strings.Contains(view, "t/esc close") {
		t.Fatalf("footer legend missing:\n%s", view)
	}
	if m.View(nil) == "" {
		t.Fatal("visible overlay rendered empty")
	}
	m.Hide()
	if m.View(nil) != "" {
		t.Fatal("hidden overlay rendered content")
	}
}

// TestPadDoubleWidthTruncation pins that pad always yields exactly w
// display cells: a truncation landing on a double-width rune produces w-1
// cells and must be re-padded, or every column after the label shifts left
// one cell for that row.
func TestPadDoubleWidthTruncation(t *testing.T) {
	// The two labels differ in ASCII lead length so one of them straddles
	// the truncation boundary at any width parity.
	labels := []string{"中文文件名中文文件名", "a中文文件名中文文件名"}
	for w := 3; w <= 12; w++ {
		for _, s := range labels {
			if got := ansi.StringWidth(pad(s, w)); got != w {
				t.Errorf("pad(%q, %d) renders %d cells, want %d", s, w, got, w)
			}
		}
	}
}
