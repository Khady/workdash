package main

import (
	"testing"
	"time"
)

func TestFormatAge(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) *time.Time {
		at := now.Add(-d)
		return &at
	}
	tests := []struct {
		name string
		at   *time.Time
		want string
	}{
		{"no timestamp is blank rather than epoch", nil, ""},
		{"a clock skewed into the future reads as now", ago(-3 * time.Hour), "now"},
		{"seconds", ago(20 * time.Second), "now"},
		{"one minute", ago(time.Minute), "1m"},
		{"minutes", ago(59 * time.Minute), "59m"},
		{"one hour", ago(time.Hour), "1h"},
		{"hours", ago(14 * time.Hour), "14h"},
		{"just under a day", ago(23*time.Hour + 59*time.Minute), "23h"},
		{"one day", ago(24 * time.Hour), "1d"},
		{"days", ago(2 * 24 * time.Hour), "2d"},
		{"just under two weeks", ago(13 * 24 * time.Hour), "13d"},
		{"two weeks switch to weeks", ago(14 * 24 * time.Hour), "2w"},
		{"weeks", ago(69 * 24 * time.Hour), "9w"},
		{"ten weeks switch to months", ago(70 * 24 * time.Hour), "2mo"},
		{"months", ago(95 * 24 * time.Hour), "3mo"},
		{"a year", ago(400 * 24 * time.Hour), "13mo"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := formatAge(now, test.at); got != test.want {
				t.Fatalf("formatAge(%v) = %q, want %q", test.at, got, test.want)
			}
		})
	}
}
