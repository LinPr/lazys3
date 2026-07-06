//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/LinPr/lazys3/internal/storage"
)

// progressRecorder collects (transferred, total) pairs from a progress
// callback. Callbacks may arrive from worker goroutines, so access is
// mutex-guarded.
type progressRecorder struct {
	mu          sync.Mutex
	transferred []int64
	totals      []int64
}

func (p *progressRecorder) record(transferred, total int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.transferred = append(p.transferred, transferred)
	p.totals = append(p.totals, total)
}

func (p *progressRecorder) snapshot() ([]int64, []int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]int64(nil), p.transferred...), append([]int64(nil), p.totals...)
}

// assertProgressSequence checks a recorded byte-progress sequence:
// at least one callback, every value within [0, total], every reported
// total equal to want, the final value equal to want, and the suffix
// after the last reset (a decrease happens when the SDK rewinds a
// seekable body, e.g. to compute a payload hash over plain HTTP)
// monotonically non-decreasing.
func assertProgressSequence(t *testing.T, transferred, totals []int64, want int64) {
	t.Helper()
	if len(transferred) == 0 {
		t.Fatal("no progress callbacks received")
	}
	for i, v := range transferred {
		if v < 0 || v > want {
			t.Errorf("transferred[%d] = %d, want within [0, %d]", i, v, want)
		}
		if totals[i] != want {
			t.Errorf("totals[%d] = %d, want %d", i, totals[i], want)
		}
	}
	if last := transferred[len(transferred)-1]; last != want {
		t.Errorf("final transferred = %d, want %d", last, want)
	}
	// Find the start of the last ascending run and check monotonicity
	// from there.
	start := 0
	for i := 1; i < len(transferred); i++ {
		if transferred[i] < transferred[i-1] {
			start = i
		}
	}
	for i := start + 1; i < len(transferred); i++ {
		if transferred[i] < transferred[i-1] {
			t.Errorf("non-monotonic progress after last reset: transferred[%d]=%d < transferred[%d]=%d",
				i, transferred[i], i-1, transferred[i-1])
		}
	}
}

// TestE2E_UploadProgress verifies that UploadFileWithProgress reports
// increasing byte counts that end at the file size.
func TestE2E_UploadProgress(t *testing.T) {
	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	workdir := t.TempDir()
	src := filepath.Join(workdir, "big.bin")
	body := bytes.Repeat([]byte("progress-upload-"), 64*1024) // 1 MiB
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	st := clientFor(t, endpoint)
	rec := &progressRecorder{}
	if _, err := st.UploadFileWithProgress(context.Background(), src, bucket, "big.bin", rec.record); err != nil {
		t.Fatalf("UploadFileWithProgress: %v", err)
	}

	transferred, totals := rec.snapshot()
	assertProgressSequence(t, transferred, totals, int64(len(body)))
	if got := objectContent(t, client, bucket, "big.bin"); got != string(body) {
		t.Errorf("remote content mismatch: len=%d, want len=%d", len(got), len(body))
	}
}

// TestE2E_DownloadProgress verifies that DownloadFileWithProgress reports
// monotonically increasing byte counts that end at the object size.
func TestE2E_DownloadProgress(t *testing.T) {
	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	body := bytes.Repeat([]byte("progress-download-"), 64*1024) // ~1.1 MiB
	putObject(t, client, bucket, "big.bin", string(body))

	workdir := t.TempDir()
	dst := filepath.Join(workdir, "out.bin")

	st := clientFor(t, endpoint)
	rec := &progressRecorder{}
	if err := st.DownloadFileWithProgress(context.Background(), bucket, "big.bin", dst, rec.record); err != nil {
		t.Fatalf("DownloadFileWithProgress: %v", err)
	}

	transferred, totals := rec.snapshot()
	assertProgressSequence(t, transferred, totals, int64(len(body)))
	// Downloads are single-pass; the whole sequence must be monotonic.
	for i := 1; i < len(transferred); i++ {
		if transferred[i] < transferred[i-1] {
			t.Errorf("download progress decreased: transferred[%d]=%d < transferred[%d]=%d",
				i, transferred[i], i-1, transferred[i-1])
		}
	}
	if got := fileContent(t, dst); got != string(body) {
		t.Errorf("local content mismatch: len=%d, want len=%d", len(got), len(body))
	}
}

// TestE2E_SyncProgress verifies that Sync fires the per-file progress
// callback during transfers and that each file's final event reports
// transferred == total == the file size.
func TestE2E_SyncProgress(t *testing.T) {
	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	workdir := t.TempDir()
	srcDir := filepath.Join(workdir, "src")
	contentA := string(bytes.Repeat([]byte("a"), 64*1024))
	contentB := string(bytes.Repeat([]byte("b"), 32*1024))
	writeFile(t, filepath.Join(srcDir, "a.bin"), contentA)
	writeFile(t, filepath.Join(srcDir, "sub", "b.bin"), contentB)

	st := syncClient(t, endpoint)
	src := syncSourceURL(t, srcDir)
	dst := syncSourceURL(t, "s3://"+bucket+"/sync/")

	var mu sync.Mutex
	events := map[string][]int64{}
	finals := map[string]int64{}
	progress := func(file string, transferred, total int64) {
		mu.Lock()
		defer mu.Unlock()
		events[file] = append(events[file], transferred)
		finals[file] = transferred
		if total != int64(len(contentA)) && total != int64(len(contentB)) {
			t.Errorf("progress(%q): total = %d, want %d or %d", file, total, len(contentA), len(contentB))
		}
	}

	res, err := st.Sync(context.Background(), src, dst, storage.SyncOptions{}, progress)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Uploaded != 2 {
		t.Fatalf("uploaded = %d, want 2 (res=%+v)", res.Uploaded, res)
	}

	mu.Lock()
	defer mu.Unlock()
	want := map[string]int64{
		"a.bin":     int64(len(contentA)),
		"sub/b.bin": int64(len(contentB)),
	}
	for rel, size := range want {
		if len(events[rel]) == 0 {
			t.Errorf("no progress events for %q", rel)
			continue
		}
		if finals[rel] != size {
			t.Errorf("final transferred for %q = %d, want %d", rel, finals[rel], size)
		}
	}
}

// TestE2E_SyncCancel verifies that cancelling the context mid-sync makes
// Sync return promptly with a context error instead of hanging or
// finishing all transfers.
func TestE2E_SyncCancel(t *testing.T) {
	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	workdir := t.TempDir()
	srcDir := filepath.Join(workdir, "src")
	const fileCount = 40
	for i := 0; i < fileCount; i++ {
		writeFile(t, filepath.Join(srcDir, fmt.Sprintf("f%03d.txt", i)), fmt.Sprintf("content-%03d", i))
	}

	st := syncClient(t, endpoint)
	src := syncSourceURL(t, srcDir)
	dst := syncSourceURL(t, "s3://"+bucket+"/sync/")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var once sync.Once
	progress := func(file string, transferred, total int64) {
		// Cancel as soon as the first file makes progress.
		once.Do(cancel)
	}

	type syncOut struct {
		res *storage.SyncResult
		err error
	}
	done := make(chan syncOut, 1)
	go func() {
		res, err := st.Sync(ctx, src, dst, storage.SyncOptions{Concurrency: 2}, progress)
		done <- syncOut{res, err}
	}()

	select {
	case out := <-done:
		if !errors.Is(out.err, context.Canceled) {
			t.Fatalf("Sync err = %v, want context.Canceled", out.err)
		}
		if out.res == nil {
			t.Fatal("Sync returned nil result with context error")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Sync did not return within 30s after cancellation")
	}
}
