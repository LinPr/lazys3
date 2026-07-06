package s3store

import "testing"

func TestCopySourcePath(t *testing.T) {
	tests := []struct {
		name   string
		bucket string
		key    string
		want   string
	}{
		{"plain", "bucket", "a/b/c.txt", "bucket/a/b/c.txt"},
		{"question mark", "bucket", "dir/a?b.txt", "bucket/dir/a%3Fb.txt"},
		{"plus", "bucket", "a+b.txt", "bucket/a%2Bb.txt"},
		{"percent", "bucket", "100%.txt", "bucket/100%25.txt"},
		{"hash", "bucket", "a#b.txt", "bucket/a%23b.txt"},
		{"space", "bucket", "a b.txt", "bucket/a+b.txt"},
		{"non-ascii", "bucket", "目录/文件.txt", "bucket/%E7%9B%AE%E5%BD%95/%E6%96%87%E4%BB%B6.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := copySourcePath(tt.bucket, tt.key); got != tt.want {
				t.Errorf("copySourcePath(%q, %q) = %q, want %q", tt.bucket, tt.key, got, tt.want)
			}
		})
	}
}
