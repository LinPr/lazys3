package storage

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LinPr/lazys3/internal/parallel"
)

func TestRemoteRel(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		prefix string
		want   string
		wantOk bool
	}{
		{"plain key under prefix", "sync/a.txt", "sync/", "a.txt", true},
		{"nested key under prefix", "sync/sub/a.txt", "sync/", "sub/a.txt", true},
		{"empty key", "", "sync/", "", false},
		{"prefix placeholder itself", "sync/", "sync/", "", false},
		{"nested folder placeholder", "sync/sub/", "sync/", "", false},
		{"deep folder placeholder", "sync/a/b/", "sync/", "", false},
		{"bucket root listing", "a.txt", "", "a.txt", true},
		{"root folder placeholder", "dir/", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := remoteRel(tt.key, tt.prefix)
			if got != tt.want || ok != tt.wantOk {
				t.Errorf("remoteRel(%q, %q) = (%q, %v), want (%q, %v)",
					tt.key, tt.prefix, got, ok, tt.want, tt.wantOk)
			}
		})
	}
}

func TestRemoteSyncKey(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"s3://bucket", ""},
		{"s3://bucket/foo", "foo"},
		{"s3://bucket/foo/", "foo/"},
		{"s3://bucket/foo/bar.txt", "foo/bar.txt"},
	}
	for _, tt := range tests {
		u, err := NewStorageURL(tt.url)
		if err != nil {
			t.Fatalf("NewStorageURL(%q): %v", tt.url, err)
		}
		if got := remoteSyncKey(u); got != tt.want {
			t.Errorf("remoteSyncKey(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestListLocalObjects_RejectsFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	u, err := NewStorageURL(file)
	if err != nil {
		t.Fatalf("NewStorageURL(%q): %v", file, err)
	}
	if _, err := listLocalObjects(u); err == nil {
		t.Errorf("listLocalObjects(%q) = nil error, want non-directory rejection", file)
	}
}

func TestListLocalObjects_Directory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for p, content := range map[string]string{
		"a.txt":     "a",
		"sub/b.txt": "bb",
	} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(p)), []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", p, err)
		}
	}
	u, err := NewStorageURL(dir)
	if err != nil {
		t.Fatalf("NewStorageURL(%q): %v", dir, err)
	}
	out, err := listLocalObjects(u)
	if err != nil {
		t.Fatalf("listLocalObjects: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2 (%+v)", len(out), out)
	}
	if out[0].rel != "a.txt" || out[1].rel != "sub/b.txt" {
		t.Errorf("rels = %q, %q; want a.txt, sub/b.txt", out[0].rel, out[1].rel)
	}
	if out[1].size != 2 {
		t.Errorf("sub/b.txt size = %d, want 2", out[1].size)
	}
}

func TestJoinS3Key(t *testing.T) {
	tests := []struct {
		prefix, rel, want string
	}{
		{"", "a.txt", "a.txt"},
		{"sync/", "a.txt", "sync/a.txt"},
		{"sync", "a.txt", "sync/a.txt"},
		{"sync/", "sub/a.txt", "sync/sub/a.txt"},
	}
	for _, tt := range tests {
		if got := joinS3Key(tt.prefix, tt.rel); got != tt.want {
			t.Errorf("joinS3Key(%q, %q) = %q, want %q", tt.prefix, tt.rel, got, tt.want)
		}
	}
}

// TestPlanAndRunOnPlan pins the OnPlan contract: invoked exactly once,
// after the merge-compare and before any task runs, with every to-transfer
// file (nested rels included, sizes set) and every planned delete (Delete
// true), while skipped and filtered files stay out of the plan. Uses
// planAndRun directly with stub task builders — no network.
func TestPlanAndRunOnPlan(t *testing.T) {
	mustURL := func(s string) *StorageURL {
		u, err := NewStorageURL(s)
		if err != nil {
			t.Fatalf("NewStorageURL(%q): %v", s, err)
		}
		return u
	}
	now := time.Now()
	src := []syncObject{
		{rel: "a.txt", size: 100, mtime: now, url: mustURL("/src/a.txt"), isLocal: true},
		{rel: "nested/dir/b.bin", size: 300, mtime: now, url: mustURL("/src/nested/dir/b.bin"), isLocal: true},
		{rel: "same.txt", size: 50, mtime: now, url: mustURL("/src/same.txt"), isLocal: true},
		{rel: "skip.log", size: 10, mtime: now, url: mustURL("/src/skip.log"), isLocal: true},
	}
	dst := []syncObject{
		{rel: "only-dst.txt", size: 7, mtime: now, url: mustURL("s3://b/p/only-dst.txt")},
		{rel: "same.txt", size: 50, mtime: now, url: mustURL("s3://b/p/same.txt")},
	}

	var ranBeforePlan bool
	var taskRan atomic.Bool
	buildTransfer := func(srcObj, _ syncObject) (parallel.Task, transferKind) {
		return func() error { taskRan.Store(true); return nil }, kindUpload
	}
	buildDelete := func(_ syncObject) (parallel.Task, transferKind) {
		return func() error { taskRan.Store(true); return nil }, kindDelete
	}

	var calls int
	var plan []PlannedTransfer
	opt := SyncOptions{
		Delete:      true,
		SizeOnly:    true,
		Exclude:     []string{"*.log"},
		Concurrency: 2,
		OnPlan: func(files []PlannedTransfer) {
			calls++
			ranBeforePlan = taskRan.Load()
			plan = append([]PlannedTransfer(nil), files...)
		},
	}

	var s Storage
	res, err := s.planAndRun(context.Background(), src, dst, opt, nil, buildTransfer, buildDelete)
	if err != nil {
		t.Fatalf("planAndRun: %v", err)
	}
	if calls != 1 {
		t.Fatalf("OnPlan called %d times, want exactly 1", calls)
	}
	if ranBeforePlan {
		t.Fatal("a task ran before OnPlan was invoked")
	}
	want := []PlannedTransfer{
		{Rel: "a.txt", Size: 100},
		{Rel: "nested/dir/b.bin", Size: 300},
		{Rel: "only-dst.txt", Delete: true},
	}
	if len(plan) != len(want) {
		t.Fatalf("plan = %+v, want %+v", plan, want)
	}
	for i, p := range plan {
		if p != want[i] {
			t.Errorf("plan[%d] = %+v, want %+v", i, p, want[i])
		}
	}
	// same.txt (in sync) and skip.log (excluded) counted as skipped, and
	// the run counters match the plan.
	if res.Uploaded != 2 || res.Deleted != 1 || res.Skipped != 2 {
		t.Fatalf("result = %+v, want 2 up / 1 rm / 2 skip", res)
	}

	// Dry-run: OnPlan still fires once, counters bump, no task runs.
	taskRan.Store(false)
	calls = 0
	opt.DryRun = true
	res, err = s.planAndRun(context.Background(), src, dst, opt, nil, buildTransfer, buildDelete)
	if err != nil {
		t.Fatalf("planAndRun dry-run: %v", err)
	}
	if calls != 1 || taskRan.Load() {
		t.Fatalf("dry-run: OnPlan calls = %d, taskRan = %v; want 1, false", calls, taskRan.Load())
	}
	if res.Uploaded != 2 || res.Deleted != 1 {
		t.Fatalf("dry-run result = %+v, want 2 up / 1 rm", res)
	}
}
