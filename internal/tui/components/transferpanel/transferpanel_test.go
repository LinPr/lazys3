package transferpanel

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

func TestProgressReportMaxSoFar(t *testing.T) {
	p := NewProgress()
	if _, total := p.Load(); total != -1 {
		t.Fatalf("new progress total = %d, want -1", total)
	}
	p.Report(100, 1000)
	// An upload over plain HTTP may reset the count to 0 once (SDK
	// pre-read for payload hashing); the max-so-far must survive it.
	p.Report(0, 1000)
	done, total := p.Load()
	if done != 100 || total != 1000 {
		t.Fatalf("Load() = (%d, %d), want (100, 1000)", done, total)
	}
	p.Report(1000, 1000)
	if done, _ := p.Load(); done != 1000 {
		t.Fatalf("done = %d, want 1000", done)
	}
}

func TestTickRefreshesRunningRows(t *testing.T) {
	m := NewModel()
	prog := NewProgress()
	m, cmd := m.Update(TransferAddMsg{Transfer: Transfer{
		ID:       "t1",
		Op:       OpDownload,
		Label:    "s3://b/k -> ./k",
		Status:   StatusRunning,
		Progress: prog,
	}})
	if cmd == nil {
		t.Fatal("TransferAddMsg with Progress should arm the tick loop")
	}

	prog.Report(50, 100)
	m, cmd = m.Update(TickMsg{})
	if cmd == nil {
		t.Fatal("TickMsg with a running row should re-arm")
	}
	if m.transfers[0].Done != 50 || m.transfers[0].Total != 100 {
		t.Fatalf("row progress = (%d, %d), want (50, 100)",
			m.transfers[0].Done, m.transfers[0].Total)
	}

	m, _ = m.Update(TransferDoneMsg{ID: "t1", Op: OpDownload})
	if m.transfers[0].Status != StatusDone {
		t.Fatalf("status = %q, want done", m.transfers[0].Status)
	}
	if m.transfers[0].Done != 100 {
		t.Fatalf("done row Done = %d, want 100 (snapped to total)", m.transfers[0].Done)
	}
	m, cmd = m.Update(TickMsg{})
	if cmd != nil {
		t.Fatal("TickMsg with no running rows should stop the loop")
	}
	if m.ticking {
		t.Fatal("ticking flag should clear once no rows are running")
	}
}

func TestDoneMsgWithCanceledErr(t *testing.T) {
	m := NewModel()
	m, _ = m.Update(TransferAddMsg{Transfer: Transfer{ID: "t1", Status: StatusRunning}})
	m, _ = m.Update(TransferDoneMsg{ID: "t1", Err: context.Canceled})
	if got := m.transfers[0].Status; got != StatusCanceled {
		t.Fatalf("status = %q, want canceled", got)
	}
}

func TestCancelLatest(t *testing.T) {
	m := NewModel()
	var c1, c2 bool
	m, _ = m.Update(TransferAddMsg{Transfer: Transfer{
		ID: "t1", Status: StatusRunning, Cancel: func() { c1 = true },
	}})
	m, _ = m.Update(TransferAddMsg{Transfer: Transfer{
		ID: "t2", Status: StatusRunning, Cancel: func() { c2 = true },
	}})

	id, ok := m.CancelLatest()
	if !ok || id != "t2" {
		t.Fatalf("CancelLatest() = (%q, %v), want (t2, true)", id, ok)
	}
	if !c2 || c1 {
		t.Fatalf("cancel funcs called: c1=%v c2=%v, want only c2", c1, c2)
	}
	if m.transfers[1].Status != StatusCanceled {
		t.Fatalf("t2 status = %q, want canceled", m.transfers[1].Status)
	}
	// The op goroutine reports the ctx error afterwards; the row must
	// stay canceled, not flip to failed.
	m, _ = m.Update(TransferDoneMsg{ID: "t2", Err: context.Canceled})
	if m.transfers[1].Status != StatusCanceled {
		t.Fatalf("t2 status after done = %q, want canceled", m.transfers[1].Status)
	}

	m.CancelAll()
	if !c1 {
		t.Fatal("CancelAll should cancel t1")
	}
	if m.transfers[0].Status != StatusCanceled {
		t.Fatalf("t1 status = %q, want canceled", m.transfers[0].Status)
	}
	if _, ok := m.CancelLatest(); ok {
		t.Fatal("nothing left to cancel")
	}
}

func TestBarRendering(t *testing.T) {
	if got := bar(50, 100, 0); strings.Count(got, "█") != 5 {
		t.Fatalf("bar(50,100) = %q, want 5 filled cells", got)
	}
	if got := bar(100, 100, 0); strings.Count(got, "█") != 10 {
		t.Fatalf("bar(100,100) = %q, want full bar", got)
	}
	if got := bar(0, 100, 0); strings.Count(got, "█") != 0 {
		t.Fatalf("bar(0,100) = %q, want empty bar", got)
	}
	// Indeterminate: a moving block, position depends on the frame.
	a, b := bar(0, -1, 0), bar(0, -1, 3)
	if len([]rune(a)) != 10 || len([]rune(b)) != 10 {
		t.Fatalf("indeterminate bars must stay 10 cells: %q %q", a, b)
	}
	if a == b {
		t.Fatalf("indeterminate bar should move between frames: %q vs %q", a, b)
	}
	if strings.Count(a, "█") != 3 {
		t.Fatalf("indeterminate bar = %q, want a 3-cell block", a)
	}
}

func TestPercent(t *testing.T) {
	if got := percent(50, 100); got != "  50%" {
		t.Fatalf("percent(50,100) = %q", got)
	}
	if got := percent(0, -1); got != "" {
		t.Fatalf("percent with unknown total = %q, want empty", got)
	}
	if got := percent(200, 100); got != " 100%" {
		t.Fatalf("percent overshoot = %q, want clamped 100%%", got)
	}
}

func TestTruncateLabelCJK(t *testing.T) {
	s := "s3://bucket/文档/一个很长的路径/报告.txt -> /home/user/下载/报告.txt"
	for width := 2; width <= ansi.StringWidth(s)+2; width++ {
		got := truncateLabel(s, width)
		if !utf8.ValidString(got) {
			t.Fatalf("truncateLabel(width=%d) = %q, invalid UTF-8", width, got)
		}
		if w := ansi.StringWidth(got); w > width {
			t.Fatalf("truncateLabel(width=%d) = %q, %d cells wide", width, got, w)
		}
	}
	if got := truncateLabel("short", 20); got != "short" {
		t.Fatalf("short label truncated: %q", got)
	}
	got := truncateLabel(s, 20)
	if !strings.HasPrefix(got, "s3://") || !strings.Contains(got, "…") {
		t.Fatalf("truncateLabel(20) = %q, want head kept and middle elided", got)
	}
}

func TestViewFitsHeight(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 6)
	for i := 0; i < 8; i++ {
		m, _ = m.Update(TransferAddMsg{Transfer: Transfer{
			Op:     OpDownload,
			Label:  fmt.Sprintf("s3://b/k%d -> ./k%d", i, i),
			Status: StatusRunning,
		}})
	}
	view := m.View()
	if h := strings.Count(view, "\n") + 1; h > 6 {
		t.Fatalf("View() is %d lines, want <= 6:\n%s", h, view)
	}
	if !strings.Contains(view, "more") {
		t.Fatalf("View() should show an overflow footer:\n%s", view)
	}
}

func TestPruneFinishedCapsHistory(t *testing.T) {
	m := NewModel()
	m, _ = m.Update(TransferAddMsg{Transfer: Transfer{ID: "active", Status: StatusRunning}})
	for i := 0; i < maxHistory+50; i++ {
		m, _ = m.Update(TransferAddMsg{Transfer: Transfer{
			ID:     fmt.Sprintf("f%d", i),
			Status: StatusDone,
		}})
	}
	if len(m.transfers) > maxHistory {
		t.Fatalf("history = %d rows, want <= %d", len(m.transfers), maxHistory)
	}
	if _, ok := m.Status("active"); !ok {
		t.Fatal("pruning must never evict a running row")
	}
	if _, ok := m.Status("f0"); ok {
		t.Fatal("oldest finished row should have been evicted")
	}
}

func TestViewLinesFitWidthAndKeepStatus(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 6)
	m, _ = m.Update(TransferAddMsg{Transfer: Transfer{
		ID:     "t1",
		Op:     OpDownload,
		Label:  "s3://bucket/a/very/long/prefix/with/many/segments/object-name.bin -> /home/user/downloads/object-name.bin",
		Status: StatusRunning,
		Done:   25,
		Total:  100,
	}})
	m, _ = m.Update(TransferDoneMsg{ID: "t1", Op: OpDownload, Err: context.Canceled})
	m, _ = m.Update(TransferAddMsg{Transfer: Transfer{
		ID:     "t2",
		Op:     OpSync,
		Label:  "sync s3://bucket/another/deep/prefix -> /home/user/some/local/directory",
		Status: StatusRunning,
	}})
	m, _ = m.Update(TransferDoneMsg{ID: "t2", Op: OpSync, Note: "2 up, 0 down"})

	view := ansi.Strip(m.View())
	for i, line := range strings.Split(view, "\n") {
		if w := ansi.StringWidth(line); w > 80 {
			t.Errorf("line %d is %d cells wide:\n%q", i, w, line)
		}
	}
	// The label truncation must leave room for the status word and note.
	if !strings.Contains(view, string(StatusCanceled)) {
		t.Errorf("view should show the canceled status, got:\n%s", view)
	}
	if !strings.Contains(view, "2 up, 0 down") {
		t.Errorf("view should show the sync summary note, got:\n%s", view)
	}
}

func TestDoneMsgNoteReplacesStaleNote(t *testing.T) {
	m := NewModel()
	m.SetSize(120, 6)
	m, _ = m.Update(TransferAddMsg{Transfer: Transfer{
		ID:     "t1",
		Op:     OpSync,
		Label:  "sync ./dir -> s3://b/p",
		Status: StatusRunning,
	}})
	// A fast sync finishes before its first 200ms poll observed anything;
	// the row may still carry a stale in-flight note.
	m.SetNote("t1", "0 file(s) done")
	m, _ = m.Update(TransferDoneMsg{ID: "t1", Op: OpSync, Note: "2 up, 0 down, 0 deleted"})
	if got := m.transfers[0].Note; got != "2 up, 0 down, 0 deleted" {
		t.Fatalf("note = %q, want the final summary", got)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "2 up, 0 down, 0 deleted") {
		t.Errorf("view should render the final note, got:\n%s", view)
	}
	if strings.Contains(view, "0 file(s) done") {
		t.Errorf("view should not render the stale note, got:\n%s", view)
	}

	// A stale poll snapshot dequeued after the row turned terminal must
	// not overwrite the final summary.
	m.SetNote("t1", "1 file(s) done")
	if got := m.transfers[0].Note; got != "2 up, 0 down, 0 deleted" {
		t.Fatalf("terminal note overwritten: %q", got)
	}
}

func TestDoneMsgWithoutNoteKeepsOldNote(t *testing.T) {
	m := NewModel()
	m.SetSize(120, 6)
	m, _ = m.Update(TransferAddMsg{Transfer: Transfer{
		ID:     "t1",
		Op:     OpSync,
		Label:  "sync ./dir -> s3://b/p",
		Status: StatusRunning,
	}})
	m.SetNote("t1", "3 file(s) done")
	m, _ = m.Update(TransferDoneMsg{ID: "t1", Op: OpSync})
	if got := m.transfers[0].Note; got != "3 file(s) done" {
		t.Fatalf("note = %q, want the old note kept", got)
	}
}

func TestRenderRowShowsProgress(t *testing.T) {
	m := NewModel()
	m.SetSize(120, 6)
	row := m.renderRow(Transfer{
		ID:     "t1",
		Op:     OpDownload,
		Label:  "s3://b/k -> ./k",
		Status: StatusRunning,
		Done:   25,
		Total:  100,
		Note:   "note-text",
	})
	if !strings.Contains(row, "25%") {
		t.Fatalf("row %q should contain percentage", row)
	}
	if !strings.Contains(row, "note-text") {
		t.Fatalf("row %q should contain the note", row)
	}
	if !strings.Contains(row, string(StatusRunning)) {
		t.Fatalf("row %q should contain the status", row)
	}
}
