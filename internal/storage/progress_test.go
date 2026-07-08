package storage

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// record is a captured progress callback invocation.
type record struct {
	transferred int64
	total       int64
}

// collectTracker builds a tracker with the given throttle interval that
// appends every callback to the returned slice. Callbacks in these tests
// run on the test goroutine, so no locking is needed.
func collectTracker(total int64, interval time.Duration) (*progressTracker, *[]record) {
	var got []record
	t := newProgressTracker(total, func(transferred, tot int64) {
		got = append(got, record{transferred, tot})
	})
	t.interval = interval
	return t, &got
}

func TestProgressTrackerNilCallback(t *testing.T) {
	tr := newProgressTracker(10, nil)
	if tr != nil {
		t.Fatalf("newProgressTracker(nil fn) = %v, want nil", tr)
	}
	// All methods must be no-ops on the nil tracker.
	tr.add(5)
	tr.set(3)
	tr.finish()
}

func TestProgressTrackerThrottles(t *testing.T) {
	tr, got := collectTracker(1000, time.Hour)
	for i := 0; i < 100; i++ {
		tr.add(10)
	}
	// The first add reports (lastReport is zero), then the hour-long
	// interval suppresses the rest.
	if len(*got) != 1 {
		t.Fatalf("callbacks = %d, want 1 (throttled)", len(*got))
	}
	if (*got)[0] != (record{10, 1000}) {
		t.Errorf("first callback = %+v, want {10 1000}", (*got)[0])
	}
	tr.finish()
	if len(*got) != 2 {
		t.Fatalf("callbacks after finish = %d, want 2", len(*got))
	}
	if last := (*got)[len(*got)-1]; last != (record{1000, 1000}) {
		t.Errorf("final callback = %+v, want {1000 1000}", last)
	}
}

func TestProgressTrackerFinishReportsTotal(t *testing.T) {
	tr, got := collectTracker(64, 0)
	tr.add(32)
	tr.add(32)
	tr.finish()
	if len(*got) == 0 {
		t.Fatal("no callbacks")
	}
	if last := (*got)[len(*got)-1]; last.transferred != 64 || last.total != 64 {
		t.Errorf("final callback = %+v, want transferred == total == 64", last)
	}
	// With a zero interval every add reports; values must be monotonic.
	prev := int64(-1)
	for _, r := range *got {
		if r.transferred < prev {
			t.Errorf("non-monotonic progress: %d after %d", r.transferred, prev)
		}
		prev = r.transferred
	}
}

// TestFinishCompleteSnapsTotal pins the sync completion contract: the
// terminal report always satisfies transferred >= total, snapping total
// down when the object shrank below its planned size (or was never
// known) and leaving it untouched otherwise.
func TestFinishCompleteSnapsTotal(t *testing.T) {
	tr, got := collectTracker(1000, time.Hour)
	tr.add(400) // object shrank: only 400 of the planned 1000 exist
	tr.finishComplete()
	if last := (*got)[len(*got)-1]; last != (record{400, 400}) {
		t.Errorf("shrunk final callback = %+v, want {400 400}", last)
	}

	tr, got = collectTracker(1000, time.Hour)
	tr.add(1000)
	tr.finishComplete()
	if last := (*got)[len(*got)-1]; last != (record{1000, 1000}) {
		t.Errorf("full-size final callback = %+v, want {1000 1000}", last)
	}

	tr, got = collectTracker(-1, time.Hour)
	tr.add(7)
	tr.finishComplete()
	if last := (*got)[len(*got)-1]; last != (record{7, 7}) {
		t.Errorf("unknown-total final callback = %+v, want {7 7}", last)
	}

	var nilTr *progressTracker
	nilTr.finishComplete() // must be a no-op
}

func TestProgressReaderCounts(t *testing.T) {
	tr, got := collectTracker(11, 0)
	r := &progressReader{r: strings.NewReader("hello world"), t: tr}
	if _, err := io.Copy(io.Discard, r); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	tr.finish()
	if last := (*got)[len(*got)-1]; last.transferred != 11 {
		t.Errorf("final transferred = %d, want 11", last.transferred)
	}
}

func TestProgressReadSeekerSeekResetsCount(t *testing.T) {
	data := bytes.Repeat([]byte("a"), 100)
	tr, got := collectTracker(100, 0)
	rs := &progressReadSeeker{rs: bytes.NewReader(data), t: tr}

	// First pass (e.g. the SDK computing a payload hash).
	if _, err := io.Copy(io.Discard, rs); err != nil {
		t.Fatalf("Copy (first pass): %v", err)
	}
	// Rewind, as the SDK does before transmitting.
	if pos, err := rs.Seek(0, io.SeekStart); err != nil || pos != 0 {
		t.Fatalf("Seek = (%d, %v), want (0, nil)", pos, err)
	}
	// Second pass must count from zero again, not accumulate to 200.
	if _, err := io.Copy(io.Discard, rs); err != nil {
		t.Fatalf("Copy (second pass): %v", err)
	}
	tr.finish()
	if last := (*got)[len(*got)-1]; last.transferred != 100 {
		t.Errorf("final transferred = %d, want 100 (seek must reset the count)", last.transferred)
	}
}

func TestProgressWriterCounts(t *testing.T) {
	tr, got := collectTracker(26, 0)
	var buf bytes.Buffer
	w := &progressWriter{w: &buf, t: tr}
	src := strings.NewReader("abcdefghijklmnopqrstuvwxyz")
	if _, err := io.Copy(w, src); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	tr.finish()
	if buf.Len() != 26 {
		t.Fatalf("wrote %d bytes, want 26", buf.Len())
	}
	if last := (*got)[len(*got)-1]; last.transferred != 26 || last.total != 26 {
		t.Errorf("final callback = %+v, want {26 26}", last)
	}
}

func TestProgressWrappersNilTracker(t *testing.T) {
	// Readers/writers must work with a nil tracker (nil progress callback).
	r := &progressReader{r: strings.NewReader("data"), t: nil}
	if _, err := io.Copy(io.Discard, r); err != nil {
		t.Fatalf("progressReader with nil tracker: %v", err)
	}
	rs := &progressReadSeeker{rs: strings.NewReader("data"), t: nil}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek with nil tracker: %v", err)
	}
	var buf bytes.Buffer
	w := &progressWriter{w: &buf, t: nil}
	if _, err := io.Copy(w, strings.NewReader("data")); err != nil {
		t.Fatalf("progressWriter with nil tracker: %v", err)
	}
}
