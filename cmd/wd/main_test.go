package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/Khady/workdash/internal/action"
	"github.com/Khady/workdash/internal/wdconf"
)

type fakeExecutor struct {
	pane    []action.Resolved
	capture []action.Resolved
	err     error
}

func (f *fakeExecutor) Pane(resolved action.Resolved) (string, error) {
	f.pane = append(f.pane, resolved)
	return "paned", f.err
}

func (f *fakeExecutor) Capture(resolved action.Resolved) (string, error) {
	f.capture = append(f.capture, resolved)
	return "captured", f.err
}

func TestExecuteRoutesPaneAndCaptureToTheRightVerb(t *testing.T) {
	executor := &fakeExecutor{}
	if _, err := execute(executor, action.Resolved{ID: "agent", Where: wdconf.WherePane}); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if _, err := execute(executor, action.Resolved{ID: "status", Where: wdconf.WhereCapture}); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if len(executor.pane) != 1 || executor.pane[0].ID != "agent" {
		t.Fatalf("pane calls = %+v, want one for agent", executor.pane)
	}
	if len(executor.capture) != 1 || executor.capture[0].ID != "status" {
		t.Fatalf("capture calls = %+v, want one for status", executor.capture)
	}
}

func TestExecuteReturnsExecutorFailure(t *testing.T) {
	executor := &fakeExecutor{err: errors.New("no server running")}
	if _, err := execute(executor, action.Resolved{ID: "agent", Where: wdconf.WherePane}); err == nil {
		t.Fatal("execute() error = nil, want the executor failure")
	}
}

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
	printSnapshot(&out, snapshot())
	text := out.String()
	if !strings.Contains(text, "topic") || !strings.Contains(text, "solo") {
		t.Fatalf("output = %q, want every workspace listed", text)
	}
	if strings.Contains(text, "\t") {
		t.Fatalf("output = %q, want aligned columns rather than tabs", text)
	}
}
