package main

// The executor selection and where-dispatch these tests used to cover moved to
// internal/dispatch, which is where both frontends now get them; see
// internal/dispatch/dispatch_test.go.
import (
	"strings"
	"testing"
)

func TestSnapshotFailedReportsTotalCollectorFailure(t *testing.T) {
	if !snapshotFailed(0, []string{"repo: fatal: not a git repository"}) {
		t.Fatal("an empty snapshot with warnings is a failure, not an empty list")
	}
	if snapshotFailed(0, nil) {
		t.Fatal("an empty snapshot with no warnings is a legitimately empty list")
	}
	if snapshotFailed(3, []string{"tmux: permission denied"}) {
		t.Fatal("a partial snapshot is a warning, not a failure")
	}
}

func TestPrintSnapshotListsFacetsAndSessions(t *testing.T) {
	var out strings.Builder
	printSnapshot(&out, sampleSnapshot())
	text := out.String()
	if !strings.Contains(text, "topic") || !strings.Contains(text, "solo") {
		t.Fatalf("output = %q, want every workspace listed", text)
	}
	if strings.Contains(text, "\t") {
		t.Fatalf("output = %q, want aligned columns rather than tabs", text)
	}
}
