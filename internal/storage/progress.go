package storage

import (
	"io"
	"sync"
	"time"
)

// ProgressFunc reports byte-level transfer progress. transferredBytes is
// the number of bytes moved so far; totalBytes is the expected total, or
// -1 when unknown (e.g. streaming from stdin).
type ProgressFunc func(transferredBytes, totalBytes int64)

// defaultProgressInterval is the minimum time between two throttled
// progress callbacks, so a fast transfer does not flood the callback.
const defaultProgressInterval = 100 * time.Millisecond

// progressTracker counts transferred bytes and invokes a ProgressFunc,
// throttled to at most one call per interval. finish() always fires a
// final unthrottled callback with the terminal count. A nil tracker is
// valid and all methods are no-ops — that is how nil callbacks are
// guarded throughout the storage layer.
type progressTracker struct {
	fn       ProgressFunc
	total    int64
	interval time.Duration

	mu          sync.Mutex
	transferred int64
	lastReport  time.Time
}

// newProgressTracker builds a tracker for a transfer of total bytes
// (-1 when unknown). It returns nil when fn is nil.
func newProgressTracker(total int64, fn ProgressFunc) *progressTracker {
	if fn == nil {
		return nil
	}
	return &progressTracker{fn: fn, total: total, interval: defaultProgressInterval}
}

// add records n more transferred bytes and fires the callback when the
// throttle interval has elapsed since the last report. The first add
// always reports.
//
// Streamed reports are clamped below total, so transferred == total is
// only ever observable from finish() — that is, after the operation
// actually succeeded. Without the clamp, the SDK's payload-hashing
// pre-read (which consumes the whole body before the network send) or a
// fully-written-but-not-yet-renamed download would look like a completed
// transfer to consumers that treat transferred >= total as done.
func (t *progressTracker) add(n int) {
	if t == nil || n <= 0 {
		return
	}
	t.mu.Lock()
	t.transferred += int64(n)
	now := time.Now()
	fire := now.Sub(t.lastReport) >= t.interval
	if fire {
		t.lastReport = now
	}
	transferred := t.transferred
	t.mu.Unlock()
	if fire {
		if t.total >= 0 && transferred >= t.total {
			transferred = t.total - 1
			if transferred < 0 {
				return
			}
		}
		t.fn(transferred, t.total)
	}
}

// set resets the transferred count to an absolute offset. It is used when
// the underlying reader seeks (e.g. the AWS SDK rewinding the body to
// compute a payload hash or retry a request) and does not fire the
// callback.
func (t *progressTracker) set(offset int64) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.transferred = offset
	t.mu.Unlock()
}

// finish fires an unthrottled final callback with the current count so
// callers always observe the terminal value.
func (t *progressTracker) finish() {
	if t == nil {
		return
	}
	t.mu.Lock()
	transferred := t.transferred
	t.lastReport = time.Now()
	t.mu.Unlock()
	t.fn(transferred, t.total)
}

// finishComplete is finish for consumers that treat transferred >= total
// (total non-negative) as the completion event (sync): when the object
// shrank between listing and transfer the terminal count sits below the
// planned total — and an unknown total never satisfies the contract at
// all — so the report snaps total to the actual count in both cases.
// Only call it after the operation succeeded.
func (t *progressTracker) finishComplete() {
	if t == nil {
		return
	}
	t.mu.Lock()
	transferred := t.transferred
	t.lastReport = time.Now()
	t.mu.Unlock()
	total := t.total
	if total < 0 || total > transferred {
		total = transferred
	}
	t.fn(transferred, total)
}

// progressReader counts bytes flowing through Read.
type progressReader struct {
	r io.Reader
	t *progressTracker
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.t.add(n)
	return n, err
}

// progressReadSeeker is progressReader for seekable bodies. Forwarding
// Seek lets the AWS SDK discover the content length and rewind on retry;
// a seek resets the byte count to the new absolute offset so retried
// bytes are not double-counted.
type progressReadSeeker struct {
	rs io.ReadSeeker
	t  *progressTracker
}

func (p *progressReadSeeker) Read(b []byte) (int, error) {
	n, err := p.rs.Read(b)
	p.t.add(n)
	return n, err
}

func (p *progressReadSeeker) Seek(offset int64, whence int) (int64, error) {
	pos, err := p.rs.Seek(offset, whence)
	if err == nil {
		p.t.set(pos)
	}
	return pos, err
}

// progressWriter counts bytes flowing through Write.
type progressWriter struct {
	w io.Writer
	t *progressTracker
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.t.add(n)
	return n, err
}
