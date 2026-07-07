package syncmodal

import (
	"sync"
	"testing"
	"time"

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
