package syncmodal

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/LinPr/lazys3/internal/storage"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
	"github.com/LinPr/lazys3/internal/tui/types"
)

func TestParseFlags(t *testing.T) {
	f := ParseFlags("--delete --size-only --dry-run --exclude=*.log --include=*.txt --bogus")
	if !f.Delete || !f.SizeOnly || !f.DryRun {
		t.Fatalf("flags = %+v, want delete/size-only/dry-run set", f)
	}
	if len(f.Exclude) != 1 || f.Exclude[0] != "*.log" {
		t.Fatalf("exclude = %v", f.Exclude)
	}
	if len(f.Include) != 1 || f.Include[0] != "*.txt" {
		t.Fatalf("include = %v", f.Include)
	}
}

// TestProgressStateStreamingCallbacks verifies the done-counting rule
// under the new Sync semantics: the callback fires repeatedly DURING each
// file transfer plus a final call with transferred==total, so a file must
// count exactly once — never once per callback.
func TestProgressStateStreamingCallbacks(t *testing.T) {
	ps := newProgressState()

	// Streaming updates for one file: only the terminal call counts.
	ps.record("a.txt", 10, 100)
	ps.record("a.txt", 50, 100)
	ps.record("a.txt", 100, 100)
	// The tracker's final call repeats transferred==total; no double count.
	ps.record("a.txt", 100, 100)
	if done, _, _, _ := ps.snapshot(); done != 1 {
		t.Fatalf("filesDone = %d, want 1", done)
	}

	// A delete reports (0, 0) and counts as done immediately.
	ps.record("deleted.txt", 0, 0)
	if done, _, _, _ := ps.snapshot(); done != 2 {
		t.Fatalf("filesDone after delete = %d, want 2", done)
	}

	// Unknown total (-1) never counts as done.
	ps.record("stream.bin", 500, -1)
	done, file, bytes, total := ps.snapshot()
	if done != 2 {
		t.Fatalf("filesDone after unknown-total update = %d, want 2", done)
	}
	if file != "stream.bin" || bytes != 500 || total != -1 {
		t.Fatalf("snapshot current = (%q, %d, %d)", file, bytes, total)
	}

	// An in-flight file does not count until it reaches its total.
	ps.record("b.txt", 99, 100)
	if done, _, _, _ := ps.snapshot(); done != 2 {
		t.Fatalf("filesDone with in-flight file = %d, want 2", done)
	}
	ps.record("b.txt", 100, 100)
	if done, _, _, _ := ps.snapshot(); done != 3 {
		t.Fatalf("filesDone = %d, want 3", done)
	}
}

// TestProgressStateConcurrent exercises record from several goroutines
// (Sync invokes the callback from its worker pool).
func TestProgressStateConcurrent(t *testing.T) {
	ps := newProgressState()
	const workers = 8
	const perWorker = 50
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				name := string(rune('a'+w)) + "-" + string(rune('0'+i%10))
				ps.record(name, 50, 100)
				ps.record(name, 100, 100)
				ps.record(name, 100, 100)
			}
		}(w)
	}
	wg.Wait()
	// Each worker touches 10 distinct names.
	if done, _, _, _ := ps.snapshot(); done != workers*10 {
		t.Fatalf("filesDone = %d, want %d", done, workers*10)
	}
}

// TestNewCmdLabelPlumbing pins that a caller-supplied Label is echoed on
// the TransferDoneMsg (so the done message matches the panel row) and
// that an empty Label falls back to "sync <src> -> <dst>". The nil-storage
// error path terminates NewCmd without any network.
func TestNewCmdLabelPlumbing(t *testing.T) {
	msg := NewCmd(CmdDeps{
		Src:        "/tmp/src",
		Dst:        "s3://bkt/pre/",
		TransferID: "label-test-1",
		Label:      "dir: src/ -> s3://bkt/pre/src/",
	})()
	done, ok := msg.(transferpanel.TransferDoneMsg)
	if !ok || done.Err == nil {
		t.Fatalf("msg = %+v, want a failed TransferDoneMsg (nil storage)", msg)
	}
	if want := "dir: src/ -> s3://bkt/pre/src/"; done.Label != want {
		t.Fatalf("label = %q, want the caller-supplied %q", done.Label, want)
	}

	msg = NewCmd(CmdDeps{
		Src:        "/tmp/src",
		Dst:        "s3://bkt/pre/",
		TransferID: "label-test-2",
	})()
	done = msg.(transferpanel.TransferDoneMsg)
	if want := "sync /tmp/src -> s3://bkt/pre/"; done.Label != want {
		t.Fatalf("label = %q, want the %q fallback", done.Label, want)
	}
}

// TestAggregateDirectoryProgress is the regression test for the dir-sync
// row bar: with a 3-file plan (one nested rel) plus a delete, the aggregate
// is the SUM over the whole directory — partial transfers add up, deletes
// stay out of the byte totals, per-file counts clamp to the planned size,
// and the row's shared Progress mirrors the aggregate on every record.
func TestAggregateDirectoryProgress(t *testing.T) {
	ps := newProgressState()
	row := transferpanel.NewProgress()
	ps.row = row

	// Before the plan: no aggregate, poll snapshot carries the raw pair.
	if _, _, ok := ps.aggregateBytes(); ok {
		t.Fatal("aggregate must not be known before the plan")
	}

	ps.setPlan([]storage.PlannedTransfer{
		{Rel: "a.txt", Size: 100},
		{Rel: "nested/dir/b.bin", Size: 300},
		{Rel: "c.dat", Size: 600},
		{Rel: "old.log", Delete: true},
	})
	done, total, ok := ps.aggregateBytes()
	if !ok || done != 0 || total != 1000 {
		t.Fatalf("aggregate after plan = (%d, %d, %v), want (0, 1000, true) — deletes excluded", done, total, ok)
	}
	// The row Progress turns determinate at 0/total immediately.
	if d, tt := row.Load(); d != 0 || tt != 1000 {
		t.Fatalf("row progress after plan = (%d, %d), want (0, 1000)", d, tt)
	}

	// Partial transfers on two files (nested rel included) sum up.
	ps.record("a.txt", 40, 100)
	ps.record("nested/dir/b.bin", 150, 300)
	if done, total, _ = ps.aggregateBytes(); done != 190 || total != 1000 {
		t.Fatalf("aggregate after partials = (%d, %d), want (190, 1000)", done, total)
	}
	// The delete's (0,0) completion event moves no bytes.
	ps.record("old.log", 0, 0)
	if done, _, _ = ps.aggregateBytes(); done != 190 {
		t.Fatalf("delete changed the byte aggregate: %d, want 190", done)
	}
	// An SDK re-read regression (bytes drop) never regresses the aggregate.
	ps.record("a.txt", 10, 100)
	if done, _, _ = ps.aggregateBytes(); done != 190 {
		t.Fatalf("aggregate regressed on a byte-count reset: %d, want 190", done)
	}
	// A file's completion event snaps it to its planned size.
	ps.record("a.txt", 100, 100)
	ps.record("nested/dir/b.bin", 300, 300)
	ps.record("c.dat", 600, 600)
	if done, total, _ = ps.aggregateBytes(); done != 1000 || total != 1000 {
		t.Fatalf("aggregate at completion = (%d, %d), want (1000, 1000)", done, total)
	}
	if d, tt := row.Load(); d != 1000 || tt != 1000 {
		t.Fatalf("row progress at completion = (%d, %d), want (1000, 1000)", d, tt)
	}

	// The poll snapshot now carries the aggregate, not the last file.
	filesDone, _, bytes, tot := ps.snapshot()
	if bytes != 1000 || tot != 1000 {
		t.Fatalf("snapshot bytes = (%d, %d), want the aggregate (1000, 1000)", bytes, tot)
	}
	if filesDone != 4 {
		t.Fatalf("filesDone = %d, want 4 (delete counts as a file)", filesDone)
	}

	// PerFile snapshots reflect state and flags.
	files, ok := ps.perFile()
	if !ok || len(files) != 4 {
		t.Fatalf("perFile = (%d files, %v), want 4", len(files), ok)
	}
	for _, f := range files {
		if !f.Done {
			t.Errorf("file %q not done", f.Rel)
		}
	}
	if !files[3].Deleted || files[3].Transferred != 0 {
		t.Fatalf("delete entry = %+v, want Deleted with 0 bytes", files[3])
	}
}

// TestPerFileClampsOversizedTransfer pins the per-file clamp: a transfer
// reporting past its planned size (object grew between listing and copy)
// contributes at most Size to the aggregate.
func TestPerFileClampsOversizedTransfer(t *testing.T) {
	ps := newProgressState()
	ps.setPlan([]storage.PlannedTransfer{{Rel: "a", Size: 100}})
	ps.record("a", 250, 300)
	done, total, _ := ps.aggregateBytes()
	if done != 100 || total != 100 {
		t.Fatalf("aggregate = (%d, %d), want the clamped (100, 100)", done, total)
	}
	files, _ := ps.perFile()
	if files[0].Transferred != 100 {
		t.Fatalf("Transferred = %d, want clamped to Size 100", files[0].Transferred)
	}
}

// TestUnregisterMarksUnfinishedFailed pins the terminal-state rule: when
// the sync Cmd returns, plan entries that never produced a completion
// event (failed task, or a cancel that kept it from running) are cached
// as Failed, while a shrunk object's snapped completion event (total
// lowered to the actual count) still counts as Done.
func TestUnregisterMarksUnfinishedFailed(t *testing.T) {
	const id = "failed-mark-test"
	ps := newProgressState()
	ps.setPlan([]storage.PlannedTransfer{
		{Rel: "ok.txt", Size: 10},
		{Rel: "shrunk.bin", Size: 100},
		{Rel: "boom.dat", Size: 50},
		{Rel: "gone.log", Delete: true},
	})
	ps.record("ok.txt", 10, 10)
	ps.record("shrunk.bin", 60, 60) // finishComplete's snapped report
	ps.record("boom.dat", 20, 50)   // failed mid-transfer, no completion
	register(id, ps)

	// Live snapshots never carry Failed — only the cached final one does.
	if files, _ := PerFile(id); files[2].Failed || files[3].Failed {
		t.Fatal("live snapshot must not mark files failed")
	}
	unregister(id)

	files, ok := PerFile(id)
	if !ok || len(files) != 4 {
		t.Fatalf("PerFile = (%d, %v), want the cached 4-entry snapshot", len(files), ok)
	}
	want := map[string][2]bool{ // {Done, Failed}
		"ok.txt":     {true, false},
		"shrunk.bin": {true, false},
		"boom.dat":   {false, true},
		"gone.log":   {false, true},
	}
	for _, f := range files {
		w := want[f.Rel]
		if f.Done != w[0] || f.Failed != w[1] {
			t.Errorf("%s: Done/Failed = %v/%v, want %v/%v", f.Rel, f.Done, f.Failed, w[0], w[1])
		}
	}
}

// TestCompletedPlanCache pins the detail view's lifetime choice: when a
// sync unregisters, its final per-file snapshot moves into the capped
// completed-plan cache, so PerFile keeps answering after completion and the
// oldest entries are evicted beyond the cap.
func TestCompletedPlanCache(t *testing.T) {
	const id = "cache-test-0"
	ps := newProgressState()
	ps.setPlan([]storage.PlannedTransfer{{Rel: "f.txt", Size: 10}})
	ps.record("f.txt", 10, 10)
	register(id, ps)

	if files, ok := PerFile(id); !ok || len(files) != 1 {
		t.Fatalf("live PerFile = (%d, %v), want the registry snapshot", len(files), ok)
	}
	unregister(id)
	files, ok := PerFile(id)
	if !ok || len(files) != 1 || !files[0].Done {
		t.Fatalf("post-completion PerFile = (%+v, %v), want the cached final snapshot", files, ok)
	}
	if _, _, ok := AggregateBytes(id); ok {
		t.Fatal("AggregateBytes must go inactive after unregister")
	}

	// A sync that never reached planning caches nothing.
	register("cache-test-planless", newProgressState())
	unregister("cache-test-planless")
	if _, ok := PerFile("cache-test-planless"); ok {
		t.Fatal("plan-less sync must not leave a cache entry")
	}

	// The cache caps at completedCap: the oldest entry is evicted.
	for i := 1; i <= completedCap; i++ {
		p := newProgressState()
		p.setPlan([]storage.PlannedTransfer{{Rel: "x", Size: 1}})
		key := fmt.Sprintf("cache-test-%d", i)
		register(key, p)
		unregister(key)
	}
	if _, ok := PerFile(id); ok {
		t.Fatalf("oldest cache entry survived %d newer completions", completedCap)
	}
	if _, ok := PerFile(fmt.Sprintf("cache-test-%d", completedCap)); !ok {
		t.Fatal("newest cache entry missing")
	}
}

func TestPollCmdRegistry(t *testing.T) {
	const id = "poll-test"
	ps := newProgressState()
	register(id, ps)
	ps.record("f.txt", 30, 90)

	msg, ok := PollCmd(id)(time.Now()).(types.SyncPollMsg)
	if !ok {
		t.Fatal("PollCmd should emit a types.SyncPollMsg")
	}
	if !msg.Active {
		t.Fatal("poll on a registered sync should be Active")
	}
	if msg.CurrentFile != "f.txt" || msg.Bytes != 30 || msg.Total != 90 {
		t.Fatalf("poll snapshot = %+v", msg)
	}

	unregister(id)
	msg = PollCmd(id)(time.Now()).(types.SyncPollMsg)
	if msg.Active {
		t.Fatal("poll after unregister must be inactive so the loop stops")
	}
}
