package strutil

import (
	"testing"
	"time"
)

// TestLocalTime pins the timezone conversion: a fixed UTC instant renders
// as its LOCAL equivalent (the expectation is built with .Local() so the
// test is deterministic on any machine), and a +08:00 zone crosses the day
// boundary correctly.
func TestLocalTime(t *testing.T) {
	utc := time.Date(2026, 7, 6, 22, 30, 0, 0, time.UTC)
	if got, want := LocalTime(utc), utc.Local().Format("2006-01-02 15:04"); got != want {
		t.Errorf("LocalTime(%v) = %q, want %q", utc, got, want)
	}

	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	prev := time.Local
	time.Local = loc
	defer func() { time.Local = prev }()
	if got, want := LocalTime(utc), "2026-07-07 06:30"; got != want {
		t.Errorf("LocalTime under +08:00 = %q, want %q", got, want)
	}
}

func TestCapitalizeFirstLetter(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want string
	}{
		{
			name: "empty string",
			arg:  "",
			want: "",
		},
		{
			name: "single rune",
			arg:  "s",
			want: "S",
		},
		{
			name: "normal word",
			arg:  "sUsPend",
			want: "Suspend",
		},
		{
			name: "with number",
			arg:  "numb3r",
			want: "Numb3r",
		},
		{
			name: "two words",
			arg:  "two words",
			want: "Two words",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CapitalizeFirstRune(tt.arg); got != tt.want {
				t.Errorf("CapitalizeFirstRune() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_WildCardToRegexp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pattern string
		wanted  string
	}{
		{
			name:    "main*",
			pattern: "main*",
			wanted:  "main.*",
		},
		{
			name:    "*.txt",
			pattern: "*.txt",
			wanted:  ".*\\.txt",
		},
		{
			name:    "?_main*.txt",
			pattern: "?_main*.txt",
			wanted:  "._main.*\\.txt",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WildCardToRegexp(tt.pattern); got != tt.wanted {
				t.Errorf("wildCardToRegexp() = %v, want %v", got, tt.wanted)
			}
		})
	}
}
