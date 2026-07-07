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

// TestStatsBatchAndBytes pins the status-bar snapshot while transfers
// run: per-direction batch done/total counts, aggregate bytes over the
// rows with KNOWN totals only (an unknown-total row never poisons the
// percentage), and a failed row leaving the batch total.
func TestStatsBatchAndBytes(t *testing.T) {
	m := NewModel()
	known := NewProgress()
	known.Report(50, 200)
	unknown := NewProgress() // total stays -1
	m, _ = m.Update(TransferAddMsg{Transfer: Transfer{
		ID: "u1", Op: OpUpload, Status: StatusRunning, Progress: known,
	}})
	m, _ = m.Update(TransferAddMsg{Transfer: Transfer{
		ID: "u2", Op: OpUpload, Status: StatusRunning, Progress: unknown,
	}})
	m, _ = m.Update(TransferAddMsg{Transfer: Transfer{
		ID: "d1", Op: OpDownload, Status: StatusQueued,
	}})
	// A running sync must not join the up/down segment.
	m, _ = m.Update(TransferAddMsg{Transfer: Transfer{
		ID: "s1", Op: OpSync, Status: StatusRunning,
	}})

	st := m.Stats()
	if st.UpActive != 2 || st.DownActive != 1 {
		t.Fatalf("active = %d up / %d down, want 2/1", st.UpActive, st.DownActive)
	}
	if st.UpTotal != 2 || st.UpDone != 0 || st.DownTotal != 1 || st.DownDone != 0 {
		t.Fatalf("batch = %+v, want up 0/2, down 0/1", st)
	}
	if st.BytesDone != 50 || st.BytesTotal != 200 {
		t.Fatalf("bytes = %d/%d, want 50/200 (known totals only)", st.BytesDone, st.BytesTotal)
	}

	// One upload done, the other failed: done counts, failed leaves the
	// batch, and the failure shows in the ✗ tally.
	m, _ = m.Update(TransferDoneMsg{ID: "u1", Op: OpUpload})
	m, _ = m.Update(TransferDoneMsg{ID: "u2", Op: OpUpload, Err: context.DeadlineExceeded})
	st = m.Stats()
	if st.UpDone != 1 || st.UpTotal != 1 {
		t.Fatalf("batch after done+fail = %d/%d, want 1/1", st.UpDone, st.UpTotal)
	}
	if st.Failed != 1 {
		t.Fatalf("failed = %d, want 1", st.Failed)
	}
	if st.LifetimeUp != 1 {
		t.Fatalf("lifetime uploads = %d, want 1 (failures never count)", st.LifetimeUp)
	}

	// All-unknown totals: the aggregate must flag indeterminate (0 total).
	m2 := NewModel()
	m2, _ = m2.Update(TransferAddMsg{Transfer: Transfer{
		ID: "x", Op: OpDownload, Status: StatusRunning, Progress: NewProgress(),
	}})
	if st := m2.Stats(); st.BytesTotal != 0 {
		t.Fatalf("bytes total = %d with only unknown totals, want 0", st.BytesTotal)
	}
}

// TestStatsBytesMonotonicMidBatch pins the aggregate bar's contract: a
// row turning terminal folds its final bytes into the batch accumulators,
// so completions/failures mid-burst never move the bar backwards (and a
// known total finishing never flips the bar indeterminate). The
// accumulators reset with the batch.
func TestStatsBytesMonotonicMidBatch(t *testing.T) {
	m := NewModel()
	pa, pb := NewProgress(), NewProgress()
	pa.Report(100, 100)
	pb.Report(10, 100)
	m, _ = m.Update(TransferAddMsg{Transfer: Transfer{
		ID: "a", Op: OpDownload, Status: StatusRunning, Progress: pa,
	}})
	m, _ = m.Update(TransferAddMsg{Transfer: Transfer{
		ID: "b", Op: OpDownload, Status: StatusRunning, Progress: pb,
	}})
	if st := m.Stats(); st.BytesDone != 110 || st.BytesTotal != 200 {
		t.Fatalf("bytes = %d/%d, want 110/200", st.BytesDone, st.BytesTotal)
	}

	// a completes: its 100/100 must stay in the aggregate, not vanish.
	m, _ = m.Update(TransferDoneMsg{ID: "a", Op: OpDownload})
	st := m.Stats()
	if st.BytesDone != 110 || st.BytesTotal != 200 {
		t.Fatalf("bytes after done = %d/%d, want 110/200 (no regression)", st.BytesDone, st.BytesTotal)
	}
	if st.BytesTotal <= 0 {
		t.Fatalf("bar flipped indeterminate mid-batch")
	}

	// b fails at 10/100: it keeps its moved bytes and its remaining 90
	// leave the denominator (mirroring the row leaving the batch total).
	m, _ = m.Update(TransferDoneMsg{ID: "b", Op: OpDownload, Err: context.DeadlineExceeded})
	if st := m.Stats(); st.BytesDone != 110 || st.BytesTotal != 110 {
		t.Fatalf("bytes after fail = %d/%d, want 110/110", st.BytesDone, st.BytesTotal)
	}

	// A new burst starts a fresh byte aggregate.
	pc := NewProgress()
	pc.Report(0, 50)
	m, _ = m.Update(TransferAddMsg{Transfer: Transfer{
		ID: "c", Op: OpUpload, Status: StatusRunning, Progress: pc,
	}})
	if st := m.Stats(); st.BytesDone != 0 || st.BytesTotal != 50 {
		t.Fatalf("bytes after new burst = %d/%d, want 0/50", st.BytesDone, st.BytesTotal)
	}
}

// TestStatsBatchResetsOnNewBurst pins the batch lifecycle: once every
// upload/download is terminal, the next add starts a fresh batch, while
// the lifetime counters keep accumulating.
func TestStatsBatchResetsOnNewBurst(t *testing.T) {
	m := NewModel()
	m, _ = m.Update(TransferAddMsg{Transfer: Transfer{ID: "a", Op: OpUpload, Status: StatusRunning}})
	m, _ = m.Update(TransferDoneMsg{ID: "a", Op: OpUpload})
	if st := m.Stats(); st.UpDone != 1 || st.UpTotal != 1 || st.LifetimeUp != 1 {
		t.Fatalf("first burst stats = %+v", st)
	}

	m, _ = m.Update(TransferAddMsg{Transfer: Transfer{ID: "b", Op: OpDownload, Status: StatusRunning}})
	st := m.Stats()
	if st.UpDone != 0 || st.UpTotal != 0 || st.DownTotal != 1 {
		t.Fatalf("second burst must reset the batch, got %+v", st)
	}
	if st.LifetimeUp != 1 {
		t.Fatalf("lifetime uploads = %d after batch reset, want 1", st.LifetimeUp)
	}
}

// TestLifetimeCountsSurvivePruningAndCountOnce pins the lifetime
// counters' contract: they accumulate past maxHistory (the rows they were
// counted from get pruned), a duplicate TransferDoneMsg never double-
// counts, and cancellation counts as failed, not done.
func TestLifetimeCountsSurvivePruningAndCountOnce(t *testing.T) {
	m := NewModel()
	for i := 0; i < maxHistory+20; i++ {
		id := fmt.Sprintf("u%d", i)
		m, _ = m.Update(TransferAddMsg{Transfer: Transfer{ID: id, Op: OpUpload, Status: StatusRunning}})
		m, _ = m.Update(TransferDoneMsg{ID: id, Op: OpUpload})
	}
	if len(m.transfers) > maxHistory {
		t.Fatalf("history = %d rows, want <= %d", len(m.transfers), maxHistory)
	}
	if st := m.Stats(); st.LifetimeUp != maxHistory+20 {
		t.Fatalf("lifetime uploads = %d, want %d (must survive pruning)", st.LifetimeUp, maxHistory+20)
	}

	// A second done message for an already-terminal row is a no-op.
	m, _ = m.Update(TransferDoneMsg{ID: fmt.Sprintf("u%d", maxHistory+19), Op: OpUpload})
	if st := m.Stats(); st.LifetimeUp != maxHistory+20 {
		t.Fatalf("lifetime uploads = %d after duplicate done, want %d", st.LifetimeUp, maxHistory+20)
	}

	// Canceled transfers never reach the lifetime tallies.
	ctx, cancel := context.WithCancel(context.Background())
	m, _ = m.Update(TransferAddMsg{Transfer: Transfer{
		ID: "c", Op: OpDownload, Status: StatusRunning, Cancel: cancel,
	}})
	_ = ctx
	if ok := m.CancelByID("c"); !ok {
		t.Fatal("CancelByID failed")
	}
	m, _ = m.Update(TransferDoneMsg{ID: "c", Op: OpDownload, Err: context.Canceled})
	st := m.Stats()
	if st.LifetimeDown != 0 {
		t.Fatalf("lifetime downloads = %d after cancel, want 0", st.LifetimeDown)
	}
	if st.Failed != 1 {
		t.Fatalf("failed = %d after cancel, want 1", st.Failed)
	}
}
