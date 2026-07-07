package objectlist

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// presignTimeout bounds the presign call. Signing is local CPU work, but
// building the storage client can resolve credentials over the network.
const presignTimeout = 30 * time.Second

// PresignDoneMsg carries the result of a PresignCmd. On success URL holds
// the shareable link and Expiry the validity window; on failure Err is set
// and the TUI surfaces it via the status bar.
type PresignDoneMsg struct {
	Bucket string
	Key    string
	URL    string
	Expiry time.Duration
	Err    error
}

// ParsePresignExpiry parses the user-typed expiry for a presigned URL.
// An empty (or blank) string falls back to the 1h default; anything else
// must be a positive Go duration ("15m", "1h", "24h"). A typed zero ("0",
// "0s") is rejected rather than silently promoted to the 1h default, so
// the expiry shown to the user always matches the signing window. Upper
// range validation (7d) is the storage layer's job.
func ParsePresignExpiry(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("presign: invalid expiry %q (use a duration like 15m, 1h, 24h)", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("presign: expiry %q must be at least 1s", s)
	}
	return d, nil
}

// PresignCmd generates a presigned GET URL for key with the given expiry
// string. All failure paths (bad expiry, storage construction, out-of-range
// expiry) come back as PresignDoneMsg.Err rather than panicking the Update
// loop.
func PresignCmd(opt Option, key, expiryStr string) tea.Cmd {
	return func() tea.Msg {
		bucket, _, err := bucketFromOption(opt)
		if err != nil {
			return PresignDoneMsg{Key: key, Err: err}
		}
		expiry, err := ParsePresignExpiry(expiryStr)
		if err != nil {
			return PresignDoneMsg{Bucket: bucket, Key: key, Err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), presignTimeout)
		defer cancel()
		st, err := newStorageFromOption(ctx, opt)
		if err != nil {
			return PresignDoneMsg{Bucket: bucket, Key: key, Err: err}
		}
		url, err := st.PresignGetObject(ctx, bucket, key, expiry)
		return PresignDoneMsg{Bucket: bucket, Key: key, URL: url, Expiry: expiry, Err: err}
	}
}
