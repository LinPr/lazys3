package storage

import (
	"os"
	"path/filepath"
	"testing"
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
