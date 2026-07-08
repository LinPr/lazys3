package transferview

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/lazys3/internal/tui/components/style"
	"github.com/LinPr/lazys3/internal/tui/components/syncmodal"
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

// stubPerFile replaces the detail view's syncmodal data source for one test.
func stubPerFile(t *testing.T, plans map[string][]syncmodal.FileProgress) {
	t.Helper()
	prev := perFileFn
	perFileFn = func(id string) ([]syncmodal.FileProgress, bool) {
		files, ok := plans[id]
		return files, ok
	}
	t.Cleanup(func() { perFileFn = prev })
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

// fakeRows builds n running download rows for cursor tests.
func fakeRows(n int) []transferpanel.Transfer {
	rows := make([]transferpanel.Transfer, n)
	for i := range rows {
		rows[i] = transferpanel.Transfer{
			ID: fmt.Sprintf("t%d", n-i), Op: transferpanel.OpDownload,
			Label: "s3://b/k -> ./k", Status: transferpanel.StatusRunning,
			Done: 1, Total: 100,
		}
	}
	return rows
}

// TestScrollClamping pins the cursor clamping: it never leaves the row
// range, g/G jump to the ends, and paging stays in bounds.
func TestScrollClamping(t *testing.T) {
	m := shownModel(80, 10)
	rows := fakeRows(30)
	total := len(rows)

	m.HandleKey("k", rows)
	if m.Cursor() != 0 {
		t.Fatalf("k at the top moved the cursor to %d", m.Cursor())
	}
	m.HandleKey("G", rows)
	if m.Cursor() != total-1 {
		t.Fatalf("G moved the cursor to %d, want %d", m.Cursor(), total-1)
	}
	m.HandleKey("j", rows)
	if m.Cursor() != total-1 {
		t.Fatalf("j at the bottom moved the cursor to %d", m.Cursor())
	}
	m.HandleKey("g", rows)
	if m.Cursor() != 0 {
		t.Fatalf("g moved the cursor to %d, want 0", m.Cursor())
	}
	m.HandleKey("pgdown", rows)
	if m.Cursor() <= 0 || m.Cursor() >= total {
		t.Fatalf("pgdown moved the cursor out of range: %d", m.Cursor())
	}
	for i := 0; i < 20; i++ {
		m.HandleKey("pgup", rows)
	}
	if m.Cursor() != 0 {
		t.Fatalf("pgup spam left the cursor at %d, want 0", m.Cursor())
	}
	// A shrunk listing re-clamps on the next key.
	m.HandleKey("G", rows)
	m.HandleKey("j", fakeRows(5))
	if m.Cursor() != 4 {
		t.Fatalf("cursor after shrink = %d, want 4", m.Cursor())
	}
}

// TestViewFits80Cols pins that every rendered line fits an 80-col canvas,
// even with long labels, notes and error snippets (the overflow becomes
// horizontal scroll, never a wider line).
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

// TestTableColumnsAligned pins the reworked table layout: a dim
// op/file/progress/st/note header (in that order) and every row's columns
// starting at the same cell offsets no matter how long the label or which
// state the row is in.
func TestTableColumnsAligned(t *testing.T) {
	m := shownModel(140, 24)
	rows := []transferpanel.Transfer{
		{
			ID: "t3", Op: transferpanel.OpDownload,
			Label:  "s3://bucket/prefix/object.bin -> ./x",
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
		case strings.Contains(l, "scroll"): // footer (mentions "sync detail")
		case strings.Contains(l, "download") || strings.Contains(l, "sync") || strings.Contains(l, "upload"):
			body = append(body, l)
		}
	}
	if header == "" {
		t.Fatalf("no op/file/progress header row:\n%s", view)
	}
	if len(body) != 3 {
		t.Fatalf("found %d transfer rows, want 3:\n%s", len(body), view)
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
	// Column ORDER: op before file before progress before st before note.
	opCol := cellCol(header, "op")
	fileCol := cellCol(header, "file")
	progCol := cellCol(header, "progress")
	stCol := cellCol(header, "st")
	noteCol := cellCol(header, "note")
	// "st" appears inside other words; find the standalone header cell.
	stCol = cellCol(header, " st ") + 1
	if stCol <= 0 {
		t.Fatalf("header has no st column:\n%q", header)
	}
	if !(opCol < fileCol && fileCol < progCol && progCol < stCol && stCol < noteCol) {
		t.Fatalf("header order wrong (op=%d file=%d progress=%d st=%d note=%d):\n%q",
			opCol, fileCol, progCol, stCol, noteCol, header)
	}
	// The op column starts at the same cell in the header and every row.
	for _, ops := range []string{"download", "sync", "upload"} {
		for _, l := range body {
			if c := cellCol(l, ops); c >= 0 && c != opCol {
				t.Errorf("%q starts at cell %d, header op at %d:\n%s", ops, c, opCol, view)
			}
		}
	}
	// The file column: every label starts under the header's "file".
	for _, label := range []string{"s3://bucket", "sync ./d", "./f ->"} {
		found := false
		for _, l := range body {
			if c := cellCol(l, label); c >= 0 {
				found = true
				if c != fileCol {
					t.Errorf("label %q at cell %d, header file at %d:\n%q", label, c, fileCol, l)
				}
			}
		}
		if !found {
			t.Errorf("label %q missing:\n%s", label, view)
		}
	}
	// The progress column: every bracket/status word aligns under the
	// header's "progress".
	for _, l := range body {
		c := cellCol(l, "[")
		if c < 0 {
			c = cellCol(l, "failed")
		}
		if c < 0 {
			c = cellCol(l, "done ")
		}
		if c >= 0 && c != progCol {
			t.Errorf("progress cell at %d, header at %d:\n%q", c, progCol, l)
		}
	}
	// The failed row's error snippet lands in the note column.
	found := false
	for _, l := range body {
		if c := cellCol(l, "access denied"); c >= 0 {
			found = true
			if c != noteCol {
				t.Errorf("error snippet at cell %d, note header at %d:\n%q", c, noteCol, l)
			}
		}
	}
	if !found {
		t.Errorf("failed row's error snippet missing:\n%s", view)
	}
}

// TestDynamicColumnWidths pins the per-render width computation: columns
// hug the longest VISIBLE cell (short labels leave a narrow file column)
// and are clamped to their caps.
func TestDynamicColumnWidths(t *testing.T) {
	short := []transferpanel.Transfer{{
		ID: "t1", Op: transferpanel.OpMakeBucket, Label: "bkt",
		Status: transferpanel.StatusDone,
	}}
	w := computeWidths(short)
	if w.file != ansi.StringWidth("file") {
		t.Errorf("short labels: file width = %d, want the header width %d", w.file, len("file"))
	}
	if w.op != ansi.StringWidth("op") {
		t.Errorf("op %q: width = %d, want the header width", "mb", w.op)
	}

	long := []transferpanel.Transfer{{
		ID: "t1", Op: transferpanel.OpDownload,
		Label:  strings.Repeat("x", 200),
		Status: transferpanel.StatusFailed,
		Err:    errors.New(strings.Repeat("e", 200)),
	}}
	w = computeWidths(long)
	if w.file != fileCap {
		t.Errorf("long label: file width = %d, want the cap %d", w.file, fileCap)
	}
	if w.note != noteCap {
		t.Errorf("long error: note width = %d, want the cap %d", w.note, noteCap)
	}
	if w.op != ansi.StringWidth("download") {
		t.Errorf("op width = %d, want %d", w.op, len("download"))
	}
	if w.prog != progW {
		t.Errorf("progress width = %d, want the fixed %d", w.prog, progW)
	}
}

// TestHorizontalScroll pins the ←/→ mechanics: the offset shifts the
// columns left (marker column fixed), clamps at the content edge, resets
// below zero, and the footer advertises the scroll while overflowing.
func TestHorizontalScroll(t *testing.T) {
	m := shownModel(60, 10)
	rows := []transferpanel.Transfer{{
		ID: "t1", Op: transferpanel.OpDownload,
		Label:  "s3://bucket/" + strings.Repeat("p/", 30) + "obj.bin -> ./obj.bin",
		Status: transferpanel.StatusRunning, Done: 25, Total: 100,
		Note: "long note that overflows the sixty column terminal",
	}}

	base := ansi.Strip(m.View(rows))
	if !strings.Contains(base, "←/→ scroll") {
		t.Fatalf("overflowing table should advertise ←/→ scroll:\n%s", base)
	}
	if !strings.Contains(base, "download") {
		t.Fatalf("unscrolled view should show the op column:\n%s", base)
	}

	m.HandleKey("right", rows)
	scrolled := ansi.Strip(m.View(rows))
	if scrolled == base {
		t.Fatal("right did not shift the table")
	}
	// Every line still fits the canvas after scrolling.
	for i, line := range strings.Split(scrolled, "\n") {
		if w := ansi.StringWidth(line); w > 60 {
			t.Errorf("scrolled line %d is %d cells wide", i, w)
		}
	}

	// Scrolling right far past the end clamps: two more presses than the
	// content needs land on the same rightmost view.
	for i := 0; i < 100; i++ {
		m.HandleKey("right", rows)
	}
	rightmost := ansi.Strip(m.View(rows))
	m.HandleKey("right", rows)
	if got := ansi.Strip(m.View(rows)); got != rightmost {
		t.Fatal("right past the content edge kept scrolling")
	}
	// And scrolling back left restores the origin exactly.
	for i := 0; i < 200; i++ {
		m.HandleKey("left", rows)
	}
	if got := ansi.Strip(m.View(rows)); got != base {
		t.Fatalf("left past the origin did not restore the unscrolled view:\ngot:\n%s\nwant:\n%s", got, base)
	}

	// A table that fits never scrolls and never advertises it.
	m2 := shownModel(160, 10)
	fits := []transferpanel.Transfer{{
		ID: "t1", Op: transferpanel.OpDownload, Label: "s3://b/k -> ./k",
		Status: transferpanel.StatusRunning, Done: 25, Total: 100,
	}}
	before := ansi.Strip(m2.View(fits))
	if strings.Contains(before, "←/→ scroll") {
		t.Fatalf("fitting table must not advertise ←/→ scroll:\n%s", before)
	}
	m2.HandleKey("right", fits)
	if got := ansi.Strip(m2.View(fits)); got != before {
		t.Fatal("right on a fitting table shifted it")
	}
}

// TestHSliceCJKSafe pins the ANSI-safe slicing: a double-width rune
// straddling the cut is padded with a space (the PlaceOverlay trick), so
// the window is always exactly the requested cells and never corrupts.
func TestHSliceCJKSafe(t *testing.T) {
	line := "ab中文文件名xyz"
	total := ansi.StringWidth(line)
	for off := 0; off <= total; off++ {
		for w := 1; w <= total; w++ {
			got := hslice(line, off, w)
			gw := ansi.StringWidth(got)
			want := total - off
			if want > w {
				want = w
			}
			if want < 0 {
				want = 0
			}
			if gw > w {
				t.Fatalf("hslice(%q, %d, %d) is %d cells wide, budget %d", line, off, w, gw, w)
			}
			// The RIGHT edge may fall one cell short when the cut lands on
			// a wide rune (the rune is dropped, the screen edge hides the
			// gap); the LEFT edge is always padded so columns stay aligned.
			if gw < want-1 {
				t.Fatalf("hslice(%q, %d, %d) is %d cells wide, want >= %d", line, off, w, gw, want-1)
			}
		}
	}
	// The straddle case pads with a space instead of half a rune.
	if got := hslice("中文", 1, 3); !strings.HasPrefix(got, " ") {
		t.Fatalf("hslice mid-CJK = %q, want a leading pad space", got)
	}
}

// TestDetailModeLifecycle pins the enter/esc flow: enter on a sync row
// opens the per-file detail (title, per-file rows, live progress), enter or
// esc-as-CloseDetail returns to the list, enter on a non-sync row is a
// no-op, and a plan-less sync shows the fallback note.
func TestDetailModeLifecycle(t *testing.T) {
	withNerdFont(t)
	stubPerFile(t, map[string][]syncmodal.FileProgress{
		"sync-1": {
			{Rel: "a.txt", Size: 100, Transferred: 100, Done: true},
			{Rel: "nested/dir/b.bin", Size: 200, Transferred: 50},
			{Rel: "old.log", Deleted: true, Done: true},
		},
	})

	m := shownModel(100, 20)
	rows := []transferpanel.Transfer{
		{ID: "sync-1", Op: transferpanel.OpSync, Label: "dir: src/ -> s3://b/p/src/",
			Status: transferpanel.StatusRunning},
		{ID: "dl-1", Op: transferpanel.OpDownload, Label: "s3://b/k -> ./k",
			Status: transferpanel.StatusRunning, Done: 1, Total: 2},
	}

	// Enter on a non-sync row: no-op.
	m.HandleKey("j", rows)
	m.HandleEnter(rows)
	if m.InDetail() {
		t.Fatal("enter on a download row opened the detail")
	}

	// Enter on the sync row: detail mode with title and per-file rows.
	m.HandleKey("k", rows)
	m.HandleEnter(rows)
	if !m.InDetail() {
		t.Fatal("enter on a sync row did not open the detail")
	}
	view := ansi.Strip(m.View(rows))
	if !strings.Contains(view, "dir: src/ -> s3://b/p/src/") {
		t.Fatalf("detail title missing:\n%s", view)
	}
	if !strings.Contains(view, "a.txt") || !strings.Contains(view, "nested/dir/b.bin") {
		t.Fatalf("detail rows missing (nested rel included):\n%s", view)
	}
	if !strings.Contains(view, "100%") || !strings.Contains(view, "25%") {
		t.Fatalf("detail should render per-file percents (100%% and 25%%):\n%s", view)
	}
	if !strings.Contains(view, "delete") {
		t.Fatalf("planned delete row missing:\n%s", view)
	}
	if !strings.Contains(view, "3 file(s)") || !strings.Contains(view, "2 done") {
		t.Fatalf("detail footer counts wrong (delete counts as a file, done=2):\n%s", view)
	}

	// j/k scroll the detail cursor, not the transfer cursor.
	m.HandleKey("j", rows)
	if m.Cursor() != 0 {
		t.Fatalf("detail j moved the transfer cursor to %d", m.Cursor())
	}

	// Enter returns to the list.
	m.HandleEnter(rows)
	if m.InDetail() {
		t.Fatal("enter inside the detail did not return to the list")
	}

	// CloseDetail is what the TUI routes esc to.
	m.HandleEnter(rows)
	m.CloseDetail()
	if m.InDetail() {
		t.Fatal("CloseDetail left the detail open")
	}

	// A sync without a recorded plan (never planned / cache evicted).
	rows[0].ID = "sync-unknown"
	m.HandleEnter(rows)
	if !m.InDetail() {
		t.Fatal("enter should open the detail even without a plan")
	}
	if view := ansi.Strip(m.View(rows)); !strings.Contains(view, "no per-file plan") {
		t.Fatalf("plan-less detail should render the fallback note:\n%s", view)
	}

	// Show resets any lingering detail state.
	m.Show()
	if m.InDetail() {
		t.Fatal("Show did not reset the detail mode")
	}
}

// TestDetailDoneCountsDeleted pins the file glyphs in the detail: done
// files get ✓, failed ones ✗, started ones ▸, untouched ones ….
func TestDetailFileGlyphs(t *testing.T) {
	withNerdFont(t)
	if got := fileGlyph(syncmodal.FileProgress{Done: true}); got != "✓" {
		t.Errorf("done glyph = %q, want ✓", got)
	}
	if got := fileGlyph(syncmodal.FileProgress{Failed: true, Transferred: 1, Size: 2}); got != "✗" {
		t.Errorf("failed glyph = %q, want ✗", got)
	}
	if got := fileGlyph(syncmodal.FileProgress{Transferred: 1, Size: 2}); got != "▸" {
		t.Errorf("running glyph = %q, want ▸", got)
	}
	if got := fileGlyph(syncmodal.FileProgress{Size: 2}); got != "…" {
		t.Errorf("queued glyph = %q, want …", got)
	}
}

// TestDetailFailedProgressCell pins the terminal word for failed entries:
// no stuck partial bar for a failed transfer, no pending-looking "delete"
// for a failed delete.
func TestDetailFailedProgressCell(t *testing.T) {
	if got := fileProgressCell(syncmodal.FileProgress{Failed: true, Transferred: 20, Size: 50}); got != "failed" {
		t.Errorf("failed transfer cell = %q, want failed", got)
	}
	if got := fileProgressCell(syncmodal.FileProgress{Failed: true, Deleted: true}); got != "failed" {
		t.Errorf("failed delete cell = %q, want failed", got)
	}
}

// TestEmptyAndFooter pins the empty state and the footer legend.
func TestEmptyAndFooter(t *testing.T) {
	m := shownModel(120, 20)
	view := ansi.Strip(m.View(nil))
	if !strings.Contains(view, "no transfers this session") {
		t.Fatalf("empty view missing the empty-state line:\n%s", view)
	}
	if !strings.Contains(view, "x cancel highlighted") || !strings.Contains(view, "t/esc close") {
		t.Fatalf("footer legend missing:\n%s", view)
	}
	if !strings.Contains(view, "enter sync detail") {
		t.Fatalf("footer should advertise the enter detail:\n%s", view)
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
