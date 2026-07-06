package s3store

import (
	"testing"
	"time"
)

func TestNormalizePresignExpiry(t *testing.T) {
	tests := []struct {
		name    string
		in      time.Duration
		want    time.Duration
		wantErr bool
	}{
		{name: "zero defaults to 1h", in: 0, want: time.Hour},
		{name: "min accepted", in: time.Second, want: time.Second},
		{name: "max accepted", in: 7 * 24 * time.Hour, want: 7 * 24 * time.Hour},
		{name: "typical value", in: 15 * time.Minute, want: 15 * time.Minute},
		{name: "below min rejected", in: 500 * time.Millisecond, wantErr: true},
		{name: "negative rejected", in: -time.Hour, wantErr: true},
		{name: "above max rejected", in: 7*24*time.Hour + time.Second, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizePresignExpiry(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizePresignExpiry(%v) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizePresignExpiry(%v): %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("normalizePresignExpiry(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
