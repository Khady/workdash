package main

import (
	"fmt"
	"math"
	"time"
)

// Thresholds for formatAge. Each unit is used until the next one has at least
// two whole units to report, so the column never shows "1w" where "9d" is the
// more informative answer.
const (
	weeksAfter  = 14 * 24 * time.Hour
	monthsAfter = 10 * 7 * 24 * time.Hour
)

// formatAge renders a commit time as the compact relative age the old TUI used:
// "14h", "2d", "3mo". The unit ladder is deliberately the same one
// internal/ui.FormatCoarseRelativeTime walks, so a workspace does not appear to
// have a different age in the two frontends.
//
// now is a parameter rather than time.Now because the thresholds are the whole
// content of this function and have to be testable.
func formatAge(now time.Time, at *time.Time) string {
	if at == nil {
		return ""
	}
	// A commit dated in the future is a clock skew between two machines, not an
	// error worth reporting in a table cell.
	d := now.Sub(*at)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < weeksAfter:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < monthsAfter:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	default:
		return fmt.Sprintf("%dmo", int(math.Max(1, d.Hours()/(24*30))))
	}
}
