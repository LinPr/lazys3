package objectlist

import (
	"testing"
	"time"
)

func TestParsePresignExpiry(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    time.Duration
		wantErr bool
	}{
		{name: "empty defaults to 1h", in: "", want: time.Hour},
		{name: "blank defaults to 1h", in: "   ", want: time.Hour},
		{name: "minutes", in: "15m", want: 15 * time.Minute},
		{name: "hours", in: "24h", want: 24 * time.Hour},
		{name: "surrounding spaces", in: " 30s ", want: 30 * time.Second},
		{name: "garbage", in: "tomorrow", wantErr: true},
		{name: "bare number", in: "15", wantErr: true},
		{name: "typed zero", in: "0", wantErr: true},
		{name: "typed zero seconds", in: "0s", wantErr: true},
		{name: "negative", in: "-5m", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePresignExpiry(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParsePresignExpiry(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePresignExpiry(%q) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("ParsePresignExpiry(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestPresignCmdBadExpiry verifies a garbage expiry comes back as a
// PresignDoneMsg with Err set (surfaced on the status bar) rather than a
// panic, and that it fails before any storage/network work.
func TestPresignCmdBadExpiry(t *testing.T) {
	opt := Option{S3Uri: "s3://test-bucket/prefix/"}
	msg := PresignCmd(opt, "prefix/file.txt", "not-a-duration")()
	done, ok := msg.(PresignDoneMsg)
	if !ok {
		t.Fatalf("PresignCmd returned %T, want PresignDoneMsg", msg)
	}
	if done.Err == nil {
		t.Fatal("PresignDoneMsg.Err = nil, want parse error")
	}
	if done.Bucket != "test-bucket" {
		t.Fatalf("PresignDoneMsg.Bucket = %q, want test-bucket", done.Bucket)
	}
}
